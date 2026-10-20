package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
)

func (d *DB) GetControlExecution(ctx context.Context, task control.TaskID, id control.ExecutionID) (execution.Record, error) {
	return scanControlExecution(d.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2`, int64(task), string(id)))
}
func (d *DB) CheckControlAttempt(ctx context.Context, a execution.Attempt) error {
	var epoch, intent int64
	var owner, state, actionState string
	var expires time.Time
	var cancelled bool
	err := d.QueryRowContext(ctx, `SELECT a.lease_epoch,a.lease_owner,a.lease_expires,a.result_kind,a.intent_id,e.state,e.cancel_requested FROM execution_attempts a JOIN action_executions e ON e.id=a.execution_id WHERE a.id=$1 AND e.id=$2 AND e.task_id=$3`, string(a.ID), string(a.ExecutionID), int64(a.TaskID)).Scan(&epoch, &owner, &expires, &state, &intent, &actionState, &cancelled)
	if err == sql.ErrNoRows {
		return control.StaleLease
	}
	if err != nil {
		return err
	}
	if epoch != a.Epoch || owner != a.Owner || intent != a.IntentID || state != "running" || actionState != "running" || cancelled || !expires.After(time.Now()) {
		return control.StaleLease
	}
	return d.CheckExecutionGrant(ctx, a.TaskID, a.ExecutionID)
}
func (d *DB) RecoverControlLeases(ctx context.Context, asOf time.Time, limit int) (int, error) {
	if limit < 1 || limit > 128 {
		return 0, control.UnboundedEffect
	}
	rows, err := d.QueryContext(ctx, `SELECT e.task_id,e.id,a.id FROM action_executions e JOIN execution_attempts a ON a.execution_id=e.id JOIN tasks t ON t.id=e.task_id WHERE e.state='running' AND a.result_kind='running' AND a.lease_expires<=$1 ORDER BY e.task_id,e.id LIMIT $2`, asOf, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		task               int64
		execution, attempt string
	}
	var list []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.task, &c.execution, &c.attempt); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range list {
		changed, err := d.recoverControlLease(ctx, c.task, c.execution, c.attempt, asOf)
		if err != nil {
			return count, err
		}
		if changed {
			count++
		}
	}
	return count, nil
}
func (d *DB) recoverControlLease(ctx context.Context, task int64, id, attempt string, asOf time.Time) (bool, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var root int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, task).Scan(&root); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var state string
	var epoch int64
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT a.result_kind,a.lease_epoch,a.lease_expires FROM action_executions e JOIN execution_attempts a ON a.execution_id=e.id WHERE e.id=$1 AND a.id=$2 FOR UPDATE OF e,a`, id, attempt).Scan(&state, &epoch, &expires)
	if err != nil {
		return false, err
	}
	if state != "running" || expires.After(asOf) {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET lease_epoch=lease_epoch+1,result_kind='lost',effect_status='unknown',cleanup_status='unconfirmed',finished_at=now() WHERE id=$1`, attempt); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET state='lost',effect_status='unknown',cleanup_status='unconfirmed' WHERE id=$1`, id); err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]any{"execution_id": id, "lease_epoch": epoch + 1, "effect": "unknown", "cleanup": "unconfirmed"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'execution_lost',$3,$4)`, task, id, epoch+1, payload); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
func (d *DB) RequestControlCancellation(ctx context.Context, task control.TaskID, id control.ExecutionID) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requestControlCancellationTx(ctx, tx, task, id); err != nil {
		return err
	}
	return tx.Commit()
}
func requestControlCancellationTx(ctx context.Context, tx *sql.Tx, task control.TaskID, id control.ExecutionID) error {
	var err error
	var root int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, int64(task)).Scan(&root); err != nil {
		return err
	}
	record, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2 FOR UPDATE`, int64(task), string(id)))
	if err != nil {
		return err
	}
	switch record.Outcome.Run {
	case control.RunSucceeded, control.RunFailed, control.RunCancelled, control.RunTimedOut:
		return nil
	}
	if record.Outcome.Run == control.RunQueued {
		zero := int64(0)
		if err = settleControlBudgetTx(ctx, tx, int64(task), string(id), map[string]*int64{"requests": &zero, "response_bytes": &zero, "concurrency": &zero}); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET cancel_requested=true,state='cancelled',effect_status='known_not_applied',cleanup_status='not_required' WHERE id=$1`, string(id)); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET cancel_requested=true,cleanup_status='unconfirmed' WHERE id=$1`, string(id)); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{"execution_id": id})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'cancel_requested',1,$3) ON CONFLICT DO NOTHING`, int64(task), string(id), payload); err != nil {
		return err
	}
	return nil
}
func (d *DB) ConfirmControlStopped(ctx context.Context, task control.TaskID, id control.ExecutionID) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var root int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, int64(task)).Scan(&root); err != nil {
		return err
	}
	var requested bool
	if err = tx.QueryRowContext(ctx, `SELECT cancel_requested FROM action_executions WHERE task_id=$1 AND id=$2 FOR UPDATE`, int64(task), string(id)).Scan(&requested); err != nil {
		return err
	}
	if !requested {
		return control.StaleContext
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET lease_epoch=lease_epoch+1,result_kind='cancelled',cleanup_status='confirmed',finished_at=now() WHERE execution_id=$1 AND result_kind='running'`, string(id)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET state='cancelled',effect_status='unknown',cleanup_status='confirmed' WHERE id=$1 AND state IN ('running','lost')`, string(id)); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) ReconcileControlEffect(ctx context.Context, task control.TaskID, id, probeID control.ExecutionID) error {
	if id == probeID {
		return control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var contextVersion string
	if err = tx.QueryRowContext(ctx, `SELECT control_context_version FROM tasks WHERE id=$1 FOR UPDATE`, int64(task)).Scan(&contextVersion); err != nil {
		return err
	}
	original, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2 FOR UPDATE`, int64(task), string(id)))
	if err != nil {
		return err
	}
	if original.Outcome.Run != control.RunLost || original.Proposal.ContextVersion != contextVersion {
		return control.StaleContext
	}
	probe, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2 FOR UPDATE`, int64(task), string(probeID)))
	if err != nil {
		return err
	}
	if probe.Outcome.Run != control.RunSucceeded || probe.Proposal.ContextVersion != contextVersion || probe.Proposal.MethodID != "http.get" || probe.Proposal.Target.Host != original.Proposal.Target.Host || probe.Proposal.Target.Port != original.Proposal.Target.Port || probe.Proposal.Target.Path != "/effects/"+string(id) {
		return control.LineageMismatch
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT result_json FROM action_executions WHERE id=$1`, string(probeID)).Scan(&raw); err != nil {
		return err
	}
	var result execution.HTTPResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if !result.Complete || len(result.Hops) != 1 || result.Hops[0].StatusCode != 200 {
		return control.EvidenceUnavailable
	}
	var receipt struct {
		ExecutionID string `json:"execution_id"`
		Applied     *bool  `json:"applied"`
	}
	if err = json.Unmarshal(result.Hops[0].Body, &receipt); err != nil || receipt.ExecutionID != string(id) || receipt.Applied == nil {
		return control.EvidenceUnavailable
	}
	effect := control.EffectNotApplied
	if *receipt.Applied {
		effect = control.EffectApplied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET effect_status=$2 WHERE id=$1`, string(id), string(effect)); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"execution_id": id, "probe_id": probeID, "effect": effect})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'effect_reconciled',1,$3) ON CONFLICT DO NOTHING`, int64(task), string(id), payload); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) SuspendControlTaskOnRestore(ctx context.Context, taskID int64) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var epoch int64
	var phase string
	if err = tx.QueryRowContext(ctx, `SELECT control_epoch,control_phase FROM tasks WHERE id=$1 AND control_mode<>'legacy' FOR UPDATE`, taskID).Scan(&epoch, &phase); err != nil {
		return err
	}
	if phase != "accepting" {
		return nil
	}
	epoch++
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET control_epoch=$2,control_phase='draining',control_stop_reason='restart_reconciliation_required' WHERE id=$1`, taskID, epoch); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"task_id": taskID, "epoch": epoch, "reason": "restart_reconciliation_required"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'task_recovery_required',$3,$4)`, taskID, "task-recovery:"+strconv.FormatInt(taskID, 10), epoch, payload); err != nil {
		return err
	}
	return tx.Commit()
}
