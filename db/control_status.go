package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
)

// ControlStatusSnapshot is read-only metadata; it never returns secrets, grants or raw bodies.
func (d *DB) ControlStatusSnapshot(ctx context.Context, task control.TaskID, executionAfter, evidenceAfter string) (map[string]any, error) {
	if len(executionAfter) > 128 || len(evidenceAfter) > 256 {
		return nil, control.UnboundedEffect
	}
	tx, err := d.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var mode, phase, status, cleanup string
	var version, epoch int64
	var revoked bool
	var raw, closeRequest []byte
	if err = tx.QueryRowContext(ctx, `SELECT control_mode,control_phase,status,control_cleanup,contract_version,control_epoch,control_revoked,control_outcome,control_close_request FROM tasks WHERE id=$1 AND deleted_at IS NULL`, int64(task)).Scan(&mode, &phase, &status, &cleanup, &version, &epoch, &revoked, &raw, &closeRequest); err != nil {
		return nil, err
	}
	closed := ControlCloseResult{Phase: phase, LegacyStatus: status, Cleanup: control.CleanupStatus(cleanup)}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &closed); err != nil {
			return nil, err
		}
	}
	result := map[string]any{"phase": closed.Phase, "run": closed.Run, "result": closed.Result, "reason": closed.Reason, "cleanup": closed.Cleanup, "legacy_status": closed.LegacyStatus}
	methods := []control.MethodRef{}
	targets := []control.ScopeRule{}
	var expires any
	anonymous := false
	if version > 0 {
		var body []byte
		var contract control.Contract
		if err = tx.QueryRowContext(ctx, `SELECT body FROM task_control_contracts WHERE task_id=$1 AND version=$2`, int64(task), version).Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &contract); err != nil {
			return nil, err
		}
		methods = contract.Methods
		targets = contract.Targets
		expires = contract.ExpiresAt
		for _, identity := range contract.IdentityRefs {
			anonymous = anonymous || identity == "anonymous"
		}
	}
	output := map[string]any{"task_id": int64(task), "mode": mode, "contract": map[string]any{"version": version, "epoch": epoch, "revoked": revoked, "expires_at": expires, "methods": methods, "targets": targets, "anonymous_allowed": anonymous}, "result": result}
	budgets := []map[string]any{}
	rows, err := tx.QueryContext(ctx, `SELECT dimension,measurement_kind,limit_units,used_units,reserved_units,close_reserve_units,overrun FROM task_budget_accounts WHERE task_id=$1 ORDER BY dimension`, int64(task))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var dimension, measurement string
		var limit, used, reserved, closing int64
		var overrun bool
		if err = rows.Scan(&dimension, &measurement, &limit, &used, &reserved, &closing, &overrun); err != nil {
			rows.Close()
			return nil, err
		}
		remaining := max(int64(0), limit-used-reserved-closing)
		budgets = append(budgets, map[string]any{"dimension": dimension, "measurement": measurement, "limit": limit, "used": used, "reserved": reserved, "close_reserve": closing, "remaining": remaining, "overrun": overrun})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	output["budgets"] = budgets
	executions := []map[string]any{}
	rows, err = tx.QueryContext(ctx, `SELECT id,state,effect_status,cleanup_status,cancel_requested,context_version,grant_expires FROM action_executions WHERE task_id=$1 AND id>$2 ORDER BY id LIMIT 51`, int64(task), executionAfter)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, run, effect, cleanup, contextVersion string
		var cancelled bool
		var expires time.Time
		if err = rows.Scan(&id, &run, &effect, &cleanup, &cancelled, &contextVersion, &expires); err != nil {
			rows.Close()
			return nil, err
		}
		executions = append(executions, map[string]any{"id": id, "run": run, "effect": effect, "cleanup": cleanup, "cancel_requested": cancelled, "context_version": contextVersion, "grant_expires": expires})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	executionNext := ""
	if len(executions) > 50 {
		executionNext = executions[49]["id"].(string)
		executions = executions[:50]
	}
	output["executions"] = executions
	evidence := []map[string]any{}
	evidenceEpoch := epoch
	if len(closeRequest) > 0 {
		var saved struct{ Epoch int64 }
		if err = json.Unmarshal(closeRequest, &saved); err != nil {
			return nil, err
		}
		if saved.Epoch > 0 {
			evidenceEpoch = saved.Epoch
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT o.id,o.payload_sha256,o.complete,o.capture_validity,octet_length(o.payload),o.context_version,o.execution_id,e.control_epoch,t.control_context_version,EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=o.task_id AND i.observation_id=o.id) FROM harness_observations o JOIN action_executions e ON e.id=o.execution_id JOIN tasks t ON t.id=o.task_id WHERE o.task_id=$1 AND o.capture_source='host_http_v1' AND o.id>$2 ORDER BY o.id LIMIT 51`, int64(task), evidenceAfter)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, digest, validity, contextVersion, execution, currentContext string
		var complete, withdrawn bool
		var size, sourceEpoch int64
		if err = rows.Scan(&id, &digest, &complete, &validity, &size, &contextVersion, &execution, &sourceEpoch, &currentContext, &withdrawn); err != nil {
			rows.Close()
			return nil, err
		}
		if withdrawn {
			validity = "invalid"
		} else if revoked || contextVersion != currentContext || sourceEpoch != evidenceEpoch {
			validity = "unknown"
		}
		evidence = append(evidence, map[string]any{"id": id, "sha256": digest, "complete": complete, "validity": validity, "bytes": size, "context_version": contextVersion, "execution_id": execution})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	evidenceNext := ""
	if len(evidence) > 50 {
		evidenceNext = evidence[49]["id"].(string)
		evidence = evidence[:50]
	}
	output["evidence"] = evidence
	var reviewCount int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM hypothesis_reviews WHERE task_id=$1`, int64(task)).Scan(&reviewCount); err != nil {
		return nil, err
	}
	output["review"] = map[string]any{"advisory_records": reviewCount, "trust_domain": "same_host_advisory", "independent": false}
	output["next"] = map[string]string{"execution_after": executionNext, "evidence_after": evidenceNext}
	return output, tx.Commit()
}
func (d *DB) CancelControlExecutionCAS(ctx context.Context, task control.TaskID, id control.ExecutionID, expectedEpoch int64, actor string) error {
	if !strings.HasPrefix(actor, "admin:") {
		return control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT control_epoch FROM tasks WHERE id=$1 AND control_mode<>'legacy' AND deleted_at IS NULL FOR UPDATE`, int64(task)).Scan(&epoch); err != nil {
		return err
	}
	if epoch != expectedEpoch {
		return control.StaleContext
	}
	if err = requestControlCancellationTx(ctx, tx, task, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) CloseControlTaskCAS(ctx context.Context, task control.TaskID, expectedEpoch int64, actor string, request ControlCloseRequest) (ControlCloseResult, error) {
	var empty ControlCloseResult
	if !strings.HasPrefix(actor, "admin:") {
		return empty, control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT control_epoch FROM tasks WHERE id=$1 AND control_mode<>'legacy' AND deleted_at IS NULL FOR UPDATE`, int64(task)).Scan(&epoch); err != nil {
		return empty, err
	}
	if epoch != expectedEpoch {
		return empty, control.StaleContext
	}
	request.Source = actor
	out, err := closeControlTaskTx(ctx, tx, int64(task), request)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
