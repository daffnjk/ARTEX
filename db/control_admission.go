package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"github.com/google/uuid"
)

const controlExecutionColumns = `id,task_id,exploration_id,action_json,digest,semantic_key,contract_version,control_epoch,grant_expires,state,effect_status,cleanup_status`

func scanControlExecution(row interface{ Scan(...any) error }) (execution.Record, error) {
	var r execution.Record
	var raw []byte
	err := row.Scan(&r.ID, &r.TaskID, &r.ExplorationID, &raw, &r.Digest, &r.SemanticKey, &r.ContractVersion, &r.ControlEpoch, &r.GrantExpires, &r.Outcome.Run, &r.Outcome.Effect, &r.Outcome.Cleanup)
	if err == nil {
		err = json.Unmarshal(raw, &r.Proposal)
	}
	return r, err
}
func actionHash(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (d *DB) AdmitAndReserve(ctx context.Context, taskID control.TaskID, p control.ActionProposal) (execution.Record, error) {
	var none execution.Record
	if err := p.Validate(); err != nil {
		return none, err
	}
	target, err := control.NormalizeTarget(p.Target.URL())
	if err != nil {
		return none, err
	}
	p.Target = target
	if p.IdentityRef == nil {
		anonymous := "anonymous"
		p.IdentityRef = &anonymous
	}
	digest, err := actionHash(p)
	if err != nil {
		return none, err
	}
	baseKey, err := controlBaseActionKey(p)
	if err != nil {
		return none, err
	}
	semantic, err := actionHash(struct{ Base, Context string }{baseKey, p.ContextVersion})
	if err != nil {
		return none, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	var expID, version, epoch int64
	var phase, status, contextVersion string
	var revoked bool
	err = tx.QueryRowContext(ctx, `SELECT exploration_id,contract_version,control_epoch,control_phase,control_revoked,status,control_context_version FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, int64(taskID)).Scan(&expID, &version, &epoch, &phase, &revoked, &status, &contextVersion)
	if err != nil {
		return none, err
	}
	if err = backfillControlBaseKeys(ctx, tx, int64(taskID)); err != nil {
		return none, err
	}
	old, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND idempotency_key=$2`, int64(taskID), p.IdempotencyKey))
	if err == nil {
		if old.Digest != digest {
			return none, control.IdempotencyConflict
		}
		return old, nil
	}
	if err != sql.ErrNoRows {
		return none, err
	}
	if revoked || phase != "accepting" || status == "done" || status == "timeout" || status == "failed" {
		return none, control.ContractRevoked
	}
	if version == 0 {
		return none, control.ContractMissing
	}
	if p.ContextVersion != contextVersion {
		return none, control.StaleContext
	}
	var body []byte
	var expires time.Time
	if err = tx.QueryRowContext(ctx, `SELECT body,expires_at FROM task_control_contracts WHERE task_id=$1 AND version=$2`, int64(taskID), version).Scan(&body, &expires); err != nil {
		return none, err
	}
	if !expires.After(time.Now()) {
		return none, control.ContractExpired
	}
	var c control.Contract
	if err = json.Unmarshal(body, &c); err != nil {
		return none, err
	}
	allowed := false
	for _, m := range c.Methods {
		allowed = allowed || (m.ID == p.MethodID && m.Version == p.MethodVersion)
	}
	if !allowed || p.MethodID != "http.get" || p.MethodVersion != "1" {
		return none, control.MethodUnsupported
	}
	// This release admits only synthetic local HTTP; approval never widens it.
	if target.Host != "localhost" {
		ip, e := netip.ParseAddr(target.Host)
		if e != nil || !ip.IsLoopback() {
			return none, control.ScopeDenied
		}
	}
	if err = control.MatchTarget(c.Targets, target); err != nil {
		return none, err
	}
	identity := "anonymous"
	if p.IdentityRef != nil {
		identity = *p.IdentityRef
	}
	allowed = false
	for _, ref := range c.IdentityRefs {
		allowed = allowed || ref == identity
	}
	if !allowed {
		return none, control.ScopeDenied
	}
	if err = checkControlPlanAction(ctx, tx, int64(taskID), expID, p); err != nil {
		return none, err
	}
	if p.ExperimentID != nil {
		var ok bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM experiments WHERE id=$1 AND exploration_id=$2)`, *p.ExperimentID, expID).Scan(&ok); err != nil {
			return none, err
		}
		if !ok {
			return none, control.LineageMismatch
		}
	}

	existing, existingErr := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND base_key=$2 AND state IN ('queued','running') ORDER BY created_at DESC LIMIT 1`, int64(taskID), baseKey))
	if existingErr == nil {
		return existing, control.AlreadyRunning
	}
	if existingErr != sql.ErrNoRows {
		return none, existingErr
	}
	attribution := control.CallAttributionFrom(ctx)
	closing := attribution.Role == "reconcile" && attribution.TaskID == taskID
	var unresolved bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM action_executions WHERE task_id=$1 AND state IN ('lost','failed','cancelled','timed_out') AND effect_status='unknown')`, int64(taskID)).Scan(&unresolved); err != nil {
		return none, err
	}
	if unresolved && !closing {
		return none, control.EffectUncertain
	}
	prior, priorErr := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND base_key=$2 ORDER BY created_at DESC LIMIT 1`, int64(taskID), baseKey))
	if priorErr != nil && priorErr != sql.ErrNoRows {
		return none, priorErr
	}
	if priorErr == nil && p.RetestRef == nil {
		return prior, control.ResultAvailable
	}
	if priorErr == sql.ErrNoRows && p.RetestRef != nil {
		return none, control.PreconditionInvalid
	}
	id := control.ExecutionID(uuid.NewString())
	if priorErr == nil {
		if err = consumeControlRetest(ctx, tx, int64(taskID), p, baseKey, epoch, id); err != nil {
			return none, err
		}
	}
	role := "worker"
	if a := control.CallAttributionFrom(ctx); a.Role == "reconcile" {
		if a.TaskID != taskID {
			return none, control.LineageMismatch
		}
		role = "reconcile"
		closing = true
	}
	if err = reserveControlBudgetTx(ctx, tx, int64(taskID), string(id), role+":"+string(id), map[string]int64{"requests": execution.HTTPMaxRequests, "response_bytes": execution.HTTPMaxRequests * execution.HTTPMaxResponseBytes, "concurrency": 1}, closing); err != nil {
		return none, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return none, err
	}
	grantExpires := time.Now().UTC().Add(5 * time.Second)
	if expires.Before(grantExpires) {
		grantExpires = expires
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO action_executions(id,task_id,exploration_id,action_json,digest,idempotency_key,semantic_key,contract_version,control_epoch,context_version,grant_expires,base_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, string(id), int64(taskID), expID, raw, digest, p.IdempotencyKey, semantic, version, epoch, contextVersion, grantExpires, baseKey); err != nil {
		return none, err
	}
	payload, _ := json.Marshal(map[string]any{"execution_id": id})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'execution_admitted',1,$3)`, int64(taskID), string(id), payload); err != nil {
		return none, err
	}
	record, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE id=$1`, string(id)))
	if err != nil {
		return none, err
	}
	return record, tx.Commit()
}
func (d *DB) CheckExecutionGrant(ctx context.Context, taskID control.TaskID, id control.ExecutionID) error {
	var currentVersion, currentEpoch, version, epoch int64
	var revoked, cancelRequested bool
	var phase, state, contextVersion, actionContext, status string
	var contractExpiry, grantExpiry time.Time
	err := d.QueryRowContext(ctx, `SELECT t.contract_version,t.control_epoch,t.control_revoked,t.control_phase,t.control_context_version,t.status,e.contract_version,e.control_epoch,e.context_version,e.grant_expires,e.state,e.cancel_requested,c.expires_at FROM tasks t JOIN action_executions e ON e.task_id=t.id JOIN task_control_contracts c ON c.task_id=t.id AND c.version=e.contract_version WHERE t.id=$1 AND e.id=$2 AND t.deleted_at IS NULL`, int64(taskID), string(id)).Scan(&currentVersion, &currentEpoch, &revoked, &phase, &contextVersion, &status, &version, &epoch, &actionContext, &grantExpiry, &state, &cancelRequested, &contractExpiry)
	if err == sql.ErrNoRows {
		return control.LineageMismatch
	}
	if err != nil {
		return err
	}
	if revoked || cancelRequested || phase != "accepting" || status == "done" || status == "failed" || status == "timeout" || currentVersion != version || currentEpoch != epoch {
		return control.ContractRevoked
	}
	if contextVersion != actionContext {
		return control.StaleContext
	}
	if !grantExpiry.After(time.Now()) || !contractExpiry.After(time.Now()) {
		return control.ContractExpired
	}
	if state != "queued" && state != "running" {
		return control.ResultAvailable
	}
	var raw []byte
	var exp int64
	if err = d.QueryRowContext(ctx, `SELECT action_json,exploration_id FROM action_executions WHERE id=$1 AND task_id=$2`, string(id), int64(taskID)).Scan(&raw, &exp); err != nil {
		return err
	}
	var proposal control.ActionProposal
	if err = json.Unmarshal(raw, &proposal); err != nil {
		return err
	}
	return checkControlPlanAction(ctx, d, int64(taskID), exp, proposal)
}
func (d *DB) PendingControlEvents(ctx context.Context, limit int) ([]execution.OutboxEvent, error) {
	if limit < 1 || limit > 128 {
		return nil, control.UnboundedEffect
	}
	rows, err := d.QueryContext(ctx, `SELECT id,task_id,aggregate_id,event_version FROM control_outbox WHERE delivered_at IS NULL AND event_type='execution_admitted' ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []execution.OutboxEvent{}
	for rows.Next() {
		var e execution.OutboxEvent
		if err := rows.Scan(&e.ID, &e.TaskID, &e.ExecutionID, &e.Version); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) ExecutionPolicy(ctx context.Context, taskID control.TaskID, id control.ExecutionID) (execution.Record, control.Contract, error) {
	var none execution.Record
	if err := d.CheckExecutionGrant(ctx, taskID, id); err != nil {
		return none, control.Contract{}, err
	}
	record, err := scanControlExecution(d.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE task_id=$1 AND id=$2`, int64(taskID), string(id)))
	if err != nil {
		return none, control.Contract{}, err
	}
	c, err := d.ActiveControlContract(ctx, int64(taskID))
	if err != nil {
		return none, control.Contract{}, err
	}
	if c.Version != record.ContractVersion || c.Epoch != record.ControlEpoch {
		return none, control.Contract{}, control.StaleContext
	}
	return record, c.Body, nil
}
