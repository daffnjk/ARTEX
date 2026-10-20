package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
)

type ControlContract struct {
	TaskID        int64            `json:"task_id"`
	Version       int64            `json:"version"`
	Epoch         int64            `json:"epoch"`
	Digest        string           `json:"digest"`
	ApprovedActor string           `json:"approved_actor"`
	Body          control.Contract `json:"body"`
}

// ApproveControlContract is a host-only storage port, never an agent tool.
func (d *DB) ApproveControlContract(ctx context.Context, taskID, expectedEpoch int64, c control.Contract, actor string) (ControlContract, error) {
	var out ControlContract
	if !strings.HasPrefix(actor, "admin:") || len(actor) > 128 {
		return out, control.ApprovalRequired
	}
	if err := c.Validate(time.Now()); err != nil {
		return out, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return out, err
	}
	if len(raw) > 32768 {
		return out, control.UnboundedEffect
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var epoch, version int64
	var phase, status, mode string
	var started sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT control_epoch,contract_version,control_phase,status,control_mode,first_run_at FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, taskID).Scan(&epoch, &version, &phase, &status, &mode, &started)
	if err != nil {
		return out, err
	}
	if epoch != expectedEpoch {
		return out, control.StaleContext
	}
	if phase != "accepting" || status == "done" || status == "failed" || status == "timeout" {
		return out, control.ContractRevoked
	}
	// Legacy work must be drained, not silently relabelled as controlled execution.
	if mode == "legacy" && started.Valid {
		return out, control.ApprovalRequired
	}
	version++
	epoch++
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_control_contracts(task_id,version,body,digest,approved_actor,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, taskID, version, raw, digest, actor, c.ExpiresAt); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET control_mode=$2,contract_version=$3,control_epoch=$4,control_revoked=false WHERE id=$1`, taskID, string(c.Mode), version, epoch); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_asset_boundaries(asset_id,first_task_id) SELECT asset_id,task_id FROM task_asset_links WHERE task_id=$1 ON CONFLICT(asset_id) DO NOTHING`, taskID); err != nil {
		return out, err
	}
	if err = initializeControlBudgetTx(ctx, tx, taskID, c.Budgets); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_contract_events(task_id,version,epoch,event,reason,actor) VALUES($1,$2,$3,'approved','new immutable contract',$4)`, taskID, version, epoch, actor); err != nil {
		return out, err
	}
	out = ControlContract{TaskID: taskID, Version: version, Epoch: epoch, Digest: digest, ApprovedActor: actor, Body: c}
	return out, tx.Commit()
}
func (d *DB) RevokeControlContract(ctx context.Context, taskID, expectedEpoch int64, actor, reason string) (int64, error) {
	if !strings.HasPrefix(actor, "admin:") || len(actor) > 128 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return 0, control.ApprovalRequired
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var epoch, version int64
	if err = tx.QueryRowContext(ctx, `SELECT control_epoch,contract_version FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, taskID).Scan(&epoch, &version); err != nil {
		return 0, err
	}
	if epoch != expectedEpoch {
		return 0, control.StaleContext
	}
	epoch++
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET control_epoch=$2,control_revoked=true WHERE id=$1`, taskID, epoch); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_contract_events(task_id,version,epoch,event,reason,actor) VALUES($1,$2,$3,'revoked',$4,$5)`, taskID, version, epoch, reason, actor); err != nil {
		return 0, err
	}
	return epoch, tx.Commit()
}
func (d *DB) ActiveControlContract(ctx context.Context, taskID int64) (ControlContract, error) {
	var out ControlContract
	var raw []byte
	var revoked bool
	var expires time.Time
	var phase string
	err := d.QueryRowContext(ctx, `SELECT t.id,t.contract_version,t.control_epoch,t.control_revoked,t.control_phase,c.body,c.digest,c.approved_actor,c.expires_at FROM tasks t JOIN task_control_contracts c ON c.task_id=t.id AND c.version=t.contract_version WHERE t.id=$1 AND t.deleted_at IS NULL`, taskID).Scan(&out.TaskID, &out.Version, &out.Epoch, &revoked, &phase, &raw, &out.Digest, &out.ApprovedActor, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return out, control.ContractMissing
	}
	if err != nil {
		return out, err
	}
	if revoked || phase != "accepting" {
		return out, control.ContractRevoked
	}
	if !expires.After(time.Now()) {
		return out, control.ContractExpired
	}
	err = json.Unmarshal(raw, &out.Body)
	return out, err
}
