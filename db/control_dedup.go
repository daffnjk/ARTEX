package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/google/uuid"
)

// The only enabled method is a fixed bounded read; narrative, nonce and plan IDs are not wire parameters.
func controlBaseActionKey(p control.ActionProposal) (string, error) {
	identity := "anonymous"
	if p.IdentityRef != nil {
		identity = *p.IdentityRef
	}
	return actionHash(struct {
		Method, Version string
		Target          control.Target
		Identity        string
	}{p.MethodID, p.MethodVersion, p.Target, identity})
}
func backfillControlBaseKeys(ctx context.Context, tx *sql.Tx, task int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,action_json FROM action_executions WHERE task_id=$1 AND base_key='' LIMIT 257`, task)
	if err != nil {
		return err
	}
	type row struct {
		id string
		p  control.ActionProposal
	}
	var records []row
	for rows.Next() {
		var r row
		var raw []byte
		if err = rows.Scan(&r.id, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &r.p); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(records) > 256 {
		return control.UnboundedEffect
	}
	for _, r := range records {
		key, err := controlBaseActionKey(r.p)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET base_key=$2 WHERE id=$1 AND base_key=''`, r.id, key); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ApproveControlRetest(ctx context.Context, task control.TaskID, prior control.ExecutionID, reason string, expectedEpoch int64, actor string) (string, error) {
	if !strings.HasPrefix(actor, "admin:") || len(actor) > 128 {
		return "", control.LineageMismatch
	}
	if reason != "evidence_invalidated" && reason != "environment_changed" && reason != "independent_confirmation" {
		return "", control.PreconditionInvalid
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var epoch int64
	var current string
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT t.control_epoch,t.control_context_version,NOT t.control_revoked AND t.control_phase='accepting' AND c.expires_at>now() FROM tasks t JOIN task_control_contracts c ON c.task_id=t.id AND c.version=t.contract_version WHERE t.id=$1 AND t.deleted_at IS NULL FOR UPDATE OF t`, int64(task)).Scan(&epoch, &current, &active); err != nil {
		return "", err
	}
	if epoch != expectedEpoch || !active {
		return "", control.ContractRevoked
	}
	record, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2`, int64(task), string(prior)))
	if err != nil {
		return "", err
	}
	if record.Outcome.Run == control.RunQueued || record.Outcome.Run == control.RunRunning {
		return "", control.AlreadyRunning
	}
	if record.Outcome.Run == control.RunLost && record.Outcome.Effect == control.EffectUnknown {
		return "", control.EffectUncertain
	}
	switch reason {
	case "environment_changed":
		if record.Proposal.ContextVersion == current {
			return "", control.PreconditionInvalid
		}
	case "evidence_invalidated":
		var invalid bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM harness_observations o WHERE o.task_id=$1 AND o.execution_id=$2 AND (o.capture_validity='invalid' OR EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id)))`, int64(task), string(prior)).Scan(&invalid); err != nil {
			return "", err
		}
		if !invalid {
			return "", control.PreconditionInvalid
		}
	}
	base, err := controlBaseActionKey(record.Proposal)
	if err != nil {
		return "", err
	}
	ref := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_retest_approvals(id,task_id,prior_execution_id,base_key,context_version,control_epoch,reason,actor,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, ref, int64(task), string(prior), base, current, epoch, reason, actor, time.Now().UTC().Add(5*time.Minute)); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]string{"prior_execution_id": string(prior), "reason": reason})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'retest_approved',1,$3)`, int64(task), "retest:"+ref, payload); err != nil {
		return "", err
	}
	return ref, tx.Commit()
}
func consumeControlRetest(ctx context.Context, tx *sql.Tx, task int64, p control.ActionProposal, base string, epoch int64, execution control.ExecutionID) error {
	if p.RetestRef == nil {
		return control.PreconditionInvalid
	}
	var ref string
	err := tx.QueryRowContext(ctx, `UPDATE control_retest_approvals SET consumed_execution_id=$1 WHERE id=$2 AND task_id=$3 AND base_key=$4 AND context_version=$5 AND control_epoch=$6 AND expires_at>now() AND consumed_execution_id IS NULL RETURNING id`, string(execution), *p.RetestRef, task, base, p.ContextVersion, epoch).Scan(&ref)
	if err == sql.ErrNoRows {
		return control.PreconditionInvalid
	}
	return err
}
