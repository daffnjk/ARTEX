package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"github.com/google/uuid"
)

func (d *DB) ClaimControlExecution(ctx context.Context, taskID control.TaskID, id control.ExecutionID, intentID int64, owner string) (execution.Attempt, error) {
	var out execution.Attempt
	if intentID <= 0 || owner == "" || len(owner) > 128 {
		return out, control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = lockControlBudgetTask(ctx, tx, int64(taskID)); err != nil {
		return out, err
	}
	var expID, version, epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT exploration_id,contract_version,control_epoch FROM tasks WHERE id=$1`, int64(taskID)).Scan(&expID, &version, &epoch); err != nil {
		return out, err
	}
	record, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE id=$1 AND task_id=$2 FOR UPDATE`, string(id), int64(taskID)))
	if err != nil {
		return out, err
	}
	if record.ContractVersion != version || record.ControlEpoch != epoch || !record.GrantExpires.After(time.Now()) {
		return out, control.ContractRevoked
	}
	if record.Outcome.Run != control.RunQueued {
		return out, control.AlreadyRunning
	}
	var owned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exploration_nodes WHERE id=$1 AND exploration_id=$2 AND kind='intent' AND state='running')`, intentID, expID).Scan(&owned); err != nil {
		return out, err
	}
	if !owned {
		return out, control.LineageMismatch
	}
	out = execution.Attempt{ID: control.AttemptID(uuid.NewString()), ExecutionID: id, TaskID: taskID, IntentID: intentID, Epoch: 1, Owner: owner, Expires: time.Now().UTC().Add(30 * time.Second)}
	if _, err = tx.ExecContext(ctx, `INSERT INTO execution_attempts(id,execution_id,attempt_no,lease_epoch,lease_owner,lease_expires,intent_id) VALUES($1,$2,1,1,$3,$4,$5)`, string(out.ID), string(id), owner, out.Expires, intentID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET state='running',intent_id=$2 WHERE id=$1`, string(id), intentID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE control_outbox SET delivered_at=now() WHERE aggregate_id=$1 AND event_type='execution_admitted'`, string(id)); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (d *DB) CommitControlHTTPResult(ctx context.Context, attempt execution.Attempt, result execution.HTTPResult) error {
	if result.ExecutionID != attempt.ExecutionID || result.Requests < 0 || result.Requests > execution.HTTPMaxRequests || result.ResponseBytes < 0 || result.ResponseBytes > execution.HTTPMaxRequests*execution.HTTPMaxResponseBytes {
		return control.LineageMismatch
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 512*1024 {
		return control.UnboundedEffect
	}
	digest, err := actionHash(result)
	if err != nil {
		return err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var task int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, int64(attempt.TaskID)).Scan(&task); err != nil {
		return err
	}
	var epoch int64
	var leaseExpires time.Time
	var cancelRequested bool
	var owner, state, contextVersion string
	var prior sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT a.lease_epoch,a.lease_owner,a.result_kind,e.context_version,e.result_digest,a.lease_expires,e.cancel_requested FROM execution_attempts a JOIN action_executions e ON e.id=a.execution_id WHERE a.id=$1 AND e.id=$2 AND e.task_id=$3 FOR UPDATE OF e,a`, string(attempt.ID), string(attempt.ExecutionID), int64(attempt.TaskID)).Scan(&epoch, &owner, &state, &contextVersion, &prior, &leaseExpires, &cancelRequested)
	if err != nil {
		return err
	}
	if epoch != attempt.Epoch || owner != attempt.Owner {
		return control.StaleLease
	}
	if result.ContextVersion != contextVersion {
		return control.StaleContext
	}
	if prior.Valid {
		if prior.String != digest {
			return control.IdempotencyConflict
		}
		return nil
	}
	if state != "running" || !leaseExpires.After(time.Now()) {
		return control.StaleLease
	}
	state = "failed"
	if result.Complete && result.Outcome.Run == control.RunSucceeded {
		state = "succeeded"
	}
	if cancelRequested {
		state = "cancelled"
	}
	zero := int64(0)
	if err = settleControlBudgetTx(ctx, tx, int64(attempt.TaskID), string(attempt.ExecutionID), map[string]*int64{"requests": &result.Requests, "response_bytes": &result.ResponseBytes, "concurrency": &zero}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET result_kind=$2,effect_status=$3,cleanup_status=$4,finished_at=now() WHERE id=$1`, string(attempt.ID), state, string(result.Outcome.Effect), string(result.Outcome.Cleanup)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET state=$2,effect_status=$3,cleanup_status=$4,result_json=$5,result_digest=$6 WHERE id=$1`, string(attempt.ExecutionID), state, string(result.Outcome.Effect), string(result.Outcome.Cleanup), raw, digest); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"execution_id": attempt.ExecutionID, "attempt_id": attempt.ID, "state": state})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'execution_finished',1,$3)`, int64(attempt.TaskID), string(attempt.ExecutionID), payload); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) ControlExecutionResult(ctx context.Context, taskID control.TaskID, id control.ExecutionID) (json.RawMessage, error) {
	var raw []byte
	err := d.QueryRowContext(ctx, `SELECT result_json FROM action_executions WHERE task_id=$1 AND id=$2`, int64(taskID), string(id)).Scan(&raw)
	return raw, err
}
