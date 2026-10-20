package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/harness"
)

type outcomeResolver struct {
	tx   *sql.Tx
	task int64
}

func (r outcomeResolver) Resolve(ctx context.Context, id string) (harness.Observation, error) {
	return resolveControlObservation(ctx, r.tx, r.task, id)
}
func (r outcomeResolver) ResolveAcceptance(ctx context.Context, task, goal int64) (harness.AcceptanceSpec, error) {
	var s harness.AcceptanceSpec
	if task != r.task {
		return s, control.LineageMismatch
	}
	err := r.tx.QueryRowContext(ctx, `SELECT version,criterion_id,criterion_version,claim,experiment_id,context_version FROM harness_acceptance WHERE task_id=$1 AND goal_id=$2 AND is_current FOR SHARE`, task, goal).Scan(&s.Version, &s.Criterion.ID, &s.Criterion.Version, &s.Claim, &s.ExperimentID, &s.ContextVersion)
	s.TaskID, s.GoalID = task, goal
	return s, err
}

// registry is trusted Go verifier code installed by the host, never model input.
func (d *DB) CommitValidatedOutcome(ctx context.Context, req harness.EvaluationRequest, hypothesis int64, registry map[harness.Criterion]harness.Verifier) (harness.EvaluationReport, error) {
	var none harness.EvaluationReport
	if err := req.Validate(); err != nil {
		return none, err
	}
	if hypothesis <= 0 {
		return none, control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	var exp, epoch int64
	var version, mode string
	if err = tx.QueryRowContext(ctx, `SELECT exploration_id,control_context_version,control_mode,control_epoch FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, req.TaskID).Scan(&exp, &version, &mode, &epoch); err != nil {
		return none, err
	}
	if mode == "legacy" || version != req.ContextVersion {
		return none, control.StaleContext
	}
	fingerprint, err := actionHash(struct {
		Request    harness.EvaluationRequest
		Hypothesis int64
	}{req, hypothesis})
	if err != nil {
		return none, err
	}
	var oldDigest string
	var oldRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT c.request_digest,e.report FROM control_claims c JOIN harness_evaluations e ON e.request_id=c.request_id WHERE c.request_id=$1 AND c.task_id=$2`, req.ID, req.TaskID).Scan(&oldDigest, &oldRaw)
	if err == nil {
		if oldDigest != fingerprint {
			return none, control.IdempotencyConflict
		}
		err = json.Unmarshal(oldRaw, &none)
		return none, err
	}
	if err != sql.ErrNoRows {
		return none, err
	}
	var statement, status string
	if err = tx.QueryRowContext(ctx, `SELECT statement,status FROM hypotheses WHERE id=$1 AND exploration_id=$2 FOR UPDATE`, hypothesis, exp).Scan(&statement, &status); err != nil || statement != req.Claim {
		return none, control.LineageMismatch
	}
	var owned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exploration_nodes WHERE id=$1 AND exploration_id=$2 AND kind='goal')`, req.GoalID, exp).Scan(&owned); err != nil || !owned {
		return none, control.LineageMismatch
	}
	resolver := outcomeResolver{tx, req.TaskID}
	premiseValid := true
	var contractRaw []byte
	var currentContract control.Contract
	if err = tx.QueryRowContext(ctx, `SELECT c.body FROM task_control_contracts c JOIN tasks t ON t.id=c.task_id AND t.contract_version=c.version WHERE t.id=$1`, req.TaskID).Scan(&contractRaw); err != nil {
		return none, err
	}
	if err = json.Unmarshal(contractRaw, &currentContract); err != nil {
		return none, err
	}
	if currentContract.AcceptanceVersion != req.AcceptanceVersion {
		return none, control.StaleContext
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT NOT t.control_revoked AND c.expires_at>now() FROM tasks t JOIN task_control_contracts c ON c.task_id=t.id AND c.version=t.contract_version WHERE t.id=$1`, req.TaskID).Scan(&active); err != nil {
		return none, err
	}
	if !active {
		premiseValid = false
	}
	for _, ref := range req.Evidence {
		o, e := resolver.Resolve(ctx, ref.ObservationID)
		if e != nil || harness.Digest(o.Payload) != ref.SHA256 || o.ExperimentID != req.ExperimentID || o.ContextVersion != req.ContextVersion {
			return none, control.EvidenceUnavailable
		}
		var validity string
		var capturedEpoch int64
		if err = tx.QueryRowContext(ctx, `SELECT o.capture_validity,e.control_epoch FROM harness_observations o JOIN action_executions e ON e.id=o.execution_id WHERE o.id=$1 AND o.task_id=$2`, ref.ObservationID, req.TaskID).Scan(&validity, &capturedEpoch); err != nil {
			return none, err
		}
		if validity != "valid" || capturedEpoch != epoch || !o.Complete {
			premiseValid = false
		}
	}
	report := harness.NewEvaluatorWithAcceptance(resolver, registry, resolver).Evaluate(ctx, req)
	if len(report.Issues) > 0 {
		return none, fmt.Errorf("evaluation rejected: %s", report.Issues[0].Code)
	}
	if !premiseValid {
		report.Validity = harness.ValidityUnknown
		report.Conclusion = harness.Inconclusive
		report.Disposition = harness.Review
		report.Reason = "capture or identity premise is not current and valid"
		report.UsedEvidence = nil
	}
	if report.AcceptanceVersion == "" {
		return none, control.EvidenceUnavailable
	}
	if err = recordHarnessEvaluation(ctx, tx, report); err != nil {
		return none, err
	}
	var claimID int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO control_claims(task_id,hypothesis_id,request_id,request_digest,context_version,validity,conclusion) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, req.TaskID, hypothesis, req.ID, fingerprint, version, string(report.Validity), string(report.Conclusion)).Scan(&claimID); err != nil {
		return none, err
	}
	for _, ref := range req.Evidence {
		if _, err = tx.ExecContext(ctx, `INSERT INTO control_claim_dependencies(task_id,claim_id,observation_id) VALUES($1,$2,$3)`, req.TaskID, claimID, ref.ObservationID); err != nil {
			return none, err
		}
	}
	if report.Validity == harness.ValidityValid && (report.Conclusion == harness.Supported || report.Conclusion == harness.Refuted) {
		target := string(report.Conclusion)
		if _, err = tx.ExecContext(ctx, `UPDATE hypotheses SET status=$2,updated_at=now() WHERE id=$1`, hypothesis, target); err != nil {
			return none, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO hypothesis_events(hypothesis_id,from_status,to_status,reason) VALUES($1,$2,$3,$4)`, hypothesis, status, target, "validated outcome "+req.ID); err != nil {
			return none, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'outcome_committed',1,$3)`, req.TaskID, "claim:"+req.ID, json.RawMessage(fmt.Sprintf(`{"claim_id":%d}`, claimID))); err != nil {
		return none, err
	}
	return report, tx.Commit()
}

func (d *DB) InvalidateControlEvidence(ctx context.Context, task control.TaskID, id, actor, reason string) error {
	if !strings.HasPrefix(actor, "host:") || strings.TrimSpace(reason) == "" || len(reason) > 1024 {
		return control.LineageMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockControlBudgetTask(ctx, tx, int64(task)); err != nil {
		return err
	}
	var owned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM harness_observations WHERE task_id=$1 AND id=$2 AND capture_source='host_http_v1')`, int64(task), id).Scan(&owned); err != nil || !owned {
		return control.EvidenceUnavailable
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO control_evidence_invalidations(task_id,observation_id,actor,reason) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, int64(task), id, actor, reason)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE control_claims c SET validity='invalid' WHERE c.task_id=$1 AND EXISTS(SELECT 1 FROM control_claim_dependencies d WHERE d.claim_id=c.id AND d.observation_id=$2)`, int64(task), id); err != nil {
		return err
	}
	// Another valid support keeps the hypothesis live; invalidating one source is not a refutation.
	if _, err = tx.ExecContext(ctx, `WITH candidates AS (SELECT h.id,h.status FROM hypotheses h WHERE h.id IN (SELECT c.hypothesis_id FROM control_claims c JOIN control_claim_dependencies d ON d.claim_id=c.id WHERE c.task_id=$1 AND d.observation_id=$2) AND h.status IN ('supported','refuted') AND NOT EXISTS(SELECT 1 FROM control_claims c WHERE c.hypothesis_id=h.id AND c.validity='valid' AND c.context_version=(SELECT control_context_version FROM tasks WHERE id=$1)) FOR UPDATE), changed AS (UPDATE hypotheses h SET status='invalidated',updated_at=now() FROM candidates p WHERE h.id=p.id RETURNING h.id,p.status) INSERT INTO hypothesis_events(hypothesis_id,from_status,to_status,reason) SELECT id,status,'invalidated','trusted observation withdrawn' FROM changed`, int64(task), id); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"observation_id": id, "reason": reason})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'evidence_invalidated',1,$3)`, int64(task), "observation:"+id, payload); err != nil {
		return err
	}
	return tx.Commit()
}
