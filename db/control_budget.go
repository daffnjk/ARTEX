package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
)

type ControlBudgetAccount struct {
	Dimension                           string `json:"dimension"`
	Limit, Used, Reserved, CloseReserve int64
	Measurement                         string
	Overrun                             bool
}

func initializeControlBudgetTx(ctx context.Context, tx *sql.Tx, taskID int64, limits []control.BudgetLimit) error {
	list := append([]control.BudgetLimit{}, limits...)
	sort.Slice(list, func(i, j int) bool { return list[i].Dimension < list[j].Dimension })
	for _, b := range list {
		res, err := tx.ExecContext(ctx, `INSERT INTO task_budget_accounts(task_id,dimension,limit_units,close_reserve_units,measurement_kind) VALUES($1,$2,$3,$4,$5) ON CONFLICT(task_id,dimension) DO UPDATE SET limit_units=excluded.limit_units,close_reserve_units=excluded.close_reserve_units,measurement_kind=excluded.measurement_kind WHERE task_budget_accounts.used_units+task_budget_accounts.reserved_units<=excluded.limit_units`, taskID, b.Dimension, b.Limit, b.CloseReserve, b.Measurement)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n != 1 {
			return control.BudgetExhausted
		}
	}
	return nil
}
func (d *DB) ControlBudgetAccounts(ctx context.Context, taskID int64) ([]ControlBudgetAccount, error) {
	rows, err := d.QueryContext(ctx, `SELECT dimension,limit_units,used_units,reserved_units,close_reserve_units,measurement_kind,overrun FROM task_budget_accounts WHERE task_id=$1 ORDER BY dimension`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ControlBudgetAccount{}
	for rows.Next() {
		var a ControlBudgetAccount
		if err := rows.Scan(&a.Dimension, &a.Limit, &a.Used, &a.Reserved, &a.CloseReserve, &a.Measurement, &a.Overrun); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (d *DB) ControlMode(ctx context.Context, taskID int64) (string, error) {
	var mode string
	err := d.QueryRowContext(ctx, `SELECT control_mode FROM tasks WHERE id=$1 AND deleted_at IS NULL`, taskID).Scan(&mode)
	return mode, err
}
func (d *DB) ReserveControlBudget(ctx context.Context, taskID int64, key, attribution string, units map[string]int64, closing bool) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockControlBudgetTask(ctx, tx, taskID); err != nil {
		return err
	}
	if err = reserveControlBudgetTx(ctx, tx, taskID, key, attribution, units, closing); err != nil {
		return err
	}
	return tx.Commit()
}
func lockControlBudgetTask(ctx context.Context, tx *sql.Tx, taskID int64) error {
	var revoked bool
	var phase string
	var expires time.Time
	if err := tx.QueryRowContext(ctx, `SELECT t.control_revoked,t.control_phase,c.expires_at FROM tasks t JOIN task_control_contracts c ON c.task_id=t.id AND c.version=t.contract_version WHERE t.id=$1 AND t.deleted_at IS NULL FOR UPDATE OF t`, taskID).Scan(&revoked, &phase, &expires); err != nil {
		return err
	}
	if revoked || phase != "accepting" {
		return control.ContractRevoked
	}
	if !expires.After(time.Now()) {
		return control.ContractExpired
	}
	return nil
}
func reserveControlBudgetTx(ctx context.Context, tx *sql.Tx, taskID int64, key, attr string, units map[string]int64, closing bool) error {
	role, _, ok := strings.Cut(attr, ":")
	if len(key) == 0 || len(key) > 128 || !ok || !control.ValidCallRole(role) || len(attr) > 256 || len(units) == 0 || len(units) > 16 {
		return control.UnboundedEffect
	}
	if closing && role != "cleanup" && role != "reconcile" {
		return control.ApprovalRequired
	}
	raw, err := json.Marshal(struct {
		Units       map[string]int64
		Attribution string
		Closing     bool
	}{units, attr, closing})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM task_budget_reservations WHERE task_id=$1 AND request_key=$2 LIMIT 1`, taskID, key).Scan(&existing)
	if err == nil {
		if existing != digest {
			return control.IdempotencyConflict
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	dimensions := make([]string, 0, len(units))
	for dim, n := range units {
		if dim == "" || len(dim) > 64 || n <= 0 {
			return control.UnboundedEffect
		}
		dimensions = append(dimensions, dim)
	}
	sort.Strings(dimensions)
	for _, dim := range dimensions {
		var limit, used, reserved, closeReserve int64
		var overrun bool
		if err := tx.QueryRowContext(ctx, `SELECT limit_units,used_units,reserved_units,close_reserve_units,overrun FROM task_budget_accounts WHERE task_id=$1 AND dimension=$2 FOR UPDATE`, taskID, dim).Scan(&limit, &used, &reserved, &closeReserve, &overrun); err != nil {
			return control.BudgetExhausted
		}
		if overrun || used > limit || reserved > limit-used {
			return control.BudgetExhausted
		}
		available := limit - used - reserved
		if !closing {
			available -= closeReserve
		}
		if units[dim] > available {
			return control.BudgetExhausted
		}
		if _, err := tx.ExecContext(ctx, `UPDATE task_budget_accounts SET reserved_units=reserved_units+$3 WHERE task_id=$1 AND dimension=$2`, taskID, dim, units[dim]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_budget_reservations(task_id,request_key,dimension,attribution,digest,reserved_units) VALUES($1,$2,$3,$4,$5,$6)`, taskID, key, dim, attr, digest, units[dim]); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) SettleControlBudget(ctx context.Context, taskID int64, key string, actual map[string]*int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Settlement remains possible after expiry/revocation; it cannot authorize work.
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, taskID).Scan(&id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT dimension,reserved_units,actual_units,settlement_state FROM task_budget_reservations WHERE task_id=$1 AND request_key=$2 ORDER BY dimension`, taskID, key)
	if err != nil {
		return err
	}
	type item struct {
		dim      string
		reserved int64
		actual   sql.NullInt64
		state    string
	}
	var list []item
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.dim, &i.reserved, &i.actual, &i.state); err != nil {
			rows.Close()
			return err
		}
		list = append(list, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(list) == 0 || len(list) != len(actual) {
		return control.IdempotencyConflict
	}
	for _, i := range list {
		n, exists := actual[i.dim]
		if !exists || n != nil && *n < 0 {
			return control.UnboundedEffect
		}
		if i.state == "settled" {
			if n == nil || !i.actual.Valid || *n != i.actual.Int64 {
				return control.IdempotencyConflict
			}
			continue
		}
		if n == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE task_budget_reservations SET settlement_state='unknown' WHERE task_id=$1 AND request_key=$2 AND dimension=$3`, taskID, key, i.dim); err != nil {
				return err
			}
			continue
		}
		var measurement string
		if err = tx.QueryRowContext(ctx, `SELECT measurement_kind FROM task_budget_accounts WHERE task_id=$1 AND dimension=$2 FOR UPDATE`, taskID, i.dim).Scan(&measurement); err != nil {
			return err
		}
		if measurement == "exact" && *n > i.reserved {
			return fmt.Errorf("exact resource exceeds reservation: %w", control.BudgetExhausted)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE task_budget_accounts SET used_units=used_units+$3,reserved_units=reserved_units-$4,overrun=overrun OR (used_units+$3+reserved_units-$4>limit_units) WHERE task_id=$1 AND dimension=$2`, taskID, i.dim, *n, i.reserved); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE task_budget_reservations SET settlement_state='settled',actual_units=$4 WHERE task_id=$1 AND request_key=$2 AND dimension=$3`, taskID, key, i.dim, *n); err != nil {
			return err
		}
	}
	return tx.Commit()
}
