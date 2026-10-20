package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/harness"
	"github.com/google/uuid"
)

type ControlReviewSuggestion struct {
	Decision string   `json:"decision"`
	Summary  string   `json:"summary"`
	Issues   []string `json:"issues,omitempty"`
}

func (s ControlReviewSuggestion) Validate() error {
	if s.Decision != "manual_required" && s.Decision != "advisory_concern" && s.Decision != "no_issue_detected" {
		return control.PreconditionInvalid
	}
	if strings.TrimSpace(s.Summary) == "" || len(s.Summary) > 4096 || len(s.Issues) > 16 {
		return control.UnboundedEffect
	}
	for _, issue := range s.Issues {
		if len(issue) > 1024 {
			return control.UnboundedEffect
		}
	}
	return nil
}

type ControlReportClaim struct {
	ID, GoalID                                                                     int64
	Statement, Validity, Conclusion, ContextVersion, CriterionID, CriterionVersion string
	Evidence                                                                       []harness.EvidenceRef
}
type ControlReviewObservation struct {
	Observation harness.Observation
	Validity    control.Validity
	AttemptID   string
	LeaseEpoch  int64
}
type ControlReviewEvidence struct {
	TaskID                                          control.TaskID
	ContractVersion, ControlEpoch                   int64
	ContextVersion, AcceptanceVersion, PacketDigest string
	Standards                                       []harness.AcceptanceSpec
	Observations                                    []ControlReviewObservation
	Claims                                          []ControlReportClaim
	Invalidations                                   []string
	TrustDomain                                     string
	Independent                                     bool
}
type ControlReviewSession struct {
	d                                 *DB
	task                              control.TaskID
	sessionID, packetDigest           string
	contractVersion                   int64
	contextVersion, acceptanceVersion string
	model                             string
}

func (d *DB) BuildControlReviewPacket(ctx context.Context, task control.TaskID) (*ReviewPacket, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := buildControlReviewPacketTx(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	return p, tx.Commit()
}
func buildControlReviewPacketTx(ctx context.Context, tx *sql.Tx, task control.TaskID) (*ReviewPacket, error) {
	p := &ReviewPacket{Control: &ControlReviewEvidence{TaskID: task, TrustDomain: "same_host_advisory", Independent: false}}
	c := p.Control
	var body []byte
	var mode string
	if err := tx.QueryRowContext(ctx, `SELECT t.exploration_id,t.control_mode,t.contract_version,t.control_epoch,t.control_context_version,c.body FROM tasks t JOIN task_control_contracts c ON c.task_id=t.id AND c.version=t.contract_version WHERE t.id=$1 AND t.deleted_at IS NULL FOR UPDATE OF t`, int64(task)).Scan(&p.ExplorationID, &mode, &c.ContractVersion, &c.ControlEpoch, &c.ContextVersion, &body); err != nil {
		return nil, err
	}
	if mode == "legacy" {
		return nil, control.ContractMissing
	}
	var contract control.Contract
	if err := json.Unmarshal(body, &contract); err != nil {
		return nil, err
	}
	c.AcceptanceVersion = contract.AcceptanceVersion
	rows, err := tx.QueryContext(ctx, `SELECT goal_id,version,criterion_id,criterion_version,claim,experiment_id,context_version FROM harness_acceptance WHERE task_id=$1 AND is_current ORDER BY goal_id LIMIT 101`, int64(task))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var spec harness.AcceptanceSpec
		spec.TaskID = int64(task)
		if err = rows.Scan(&spec.GoalID, &spec.Version, &spec.Criterion.ID, &spec.Criterion.Version, &spec.Claim, &spec.ExperimentID, &spec.ContextVersion); err != nil {
			rows.Close()
			return nil, err
		}
		c.Standards = append(c.Standards, spec)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(c.Standards) > 100 {
		return nil, control.UnboundedEffect
	}
	rows, err = tx.QueryContext(ctx, `SELECT id FROM harness_observations WHERE task_id=$1 AND capture_source='host_http_v1' ORDER BY id LIMIT 65`, int64(task))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 64 {
		return nil, control.UnboundedEffect
	}
	total := 0
	for _, id := range ids {
		var o ControlReviewObservation
		var digest string
		if err = tx.QueryRowContext(ctx, `SELECT o.id,o.task_id,o.experiment_id,o.execution_id,o.context_version,o.subject_ref,o.object_ref,o.observed_at,o.complete,o.payload,o.payload_sha256,o.capture_validity,o.attempt_id,o.lease_epoch FROM harness_observations o JOIN tasks t ON t.id=o.task_id AND t.exploration_id=o.exploration_id WHERE o.task_id=$1 AND o.id=$2`, int64(task), id).Scan(&o.Observation.ID, &o.Observation.TaskID, &o.Observation.ExperimentID, &o.Observation.ExecutionID, &o.Observation.ContextVersion, &o.Observation.SubjectRef, &o.Observation.ObjectRef, &o.Observation.ObservedAt, &o.Observation.Complete, &o.Observation.Payload, &digest, &o.Validity, &o.AttemptID, &o.LeaseEpoch); err != nil {
			return nil, err
		}
		total += len(o.Observation.Payload)
		if len(o.Observation.Payload) > harness.MaxArtifactBytes || total > harness.MaxTotalArtifactBytes || harness.Digest(o.Observation.Payload) != digest {
			return nil, control.EvidenceConflict
		}
		var invalid bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control_evidence_invalidations WHERE task_id=$1 AND observation_id=$2)`, int64(task), id).Scan(&invalid); err != nil {
			return nil, err
		}
		if invalid {
			o.Validity = control.Invalid
			c.Invalidations = append(c.Invalidations, id)
		}
		c.Observations = append(c.Observations, o)
	}
	rows, err = tx.QueryContext(ctx, `SELECT c.id,e.goal_id,h.statement,c.validity,c.conclusion,c.context_version,e.criterion_id,e.criterion_version,e.report FROM control_claims c JOIN harness_evaluations e ON e.request_id=c.request_id JOIN hypotheses h ON h.id=c.hypothesis_id WHERE c.task_id=$1 ORDER BY c.id LIMIT 101`, int64(task))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var claim ControlReportClaim
		var raw []byte
		if err = rows.Scan(&claim.ID, &claim.GoalID, &claim.Statement, &claim.Validity, &claim.Conclusion, &claim.ContextVersion, &claim.CriterionID, &claim.CriterionVersion, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var report harness.EvaluationReport
		if err = json.Unmarshal(raw, &report); err != nil {
			rows.Close()
			return nil, err
		}
		claim.Evidence = report.CheckedEvidence
		c.Claims = append(c.Claims, claim)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(c.Claims) > 100 {
		return nil, control.UnboundedEffect
	}
	digest, err := actionHash(p)
	if err != nil {
		return nil, err
	}
	c.PacketDigest = digest
	return p, nil
}
func (d *DB) StartControlReviewSession(ctx context.Context, task control.TaskID, expectedEpoch int64, actor string, hostModel ...string) (*ControlReviewSession, *ReviewPacket, error) {
	if !strings.HasPrefix(actor, "admin:") {
		return nil, nil, control.LineageMismatch
	}
	model := "host-advisory"
	if len(hostModel) > 1 {
		return nil, nil, control.PreconditionInvalid
	}
	if len(hostModel) == 1 {
		if strings.TrimSpace(hostModel[0]) == "" || len(hostModel[0]) > 128 {
			return nil, nil, control.PreconditionInvalid
		}
		model = hostModel[0]
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	packet, err := buildControlReviewPacketTx(ctx, tx, task)
	if err != nil {
		return nil, nil, err
	}
	c := packet.Control
	if c.ControlEpoch != expectedEpoch {
		return nil, nil, control.StaleContext
	}
	session := &ControlReviewSession{d: d, task: task, sessionID: uuid.NewString(), packetDigest: c.PacketDigest, contractVersion: c.ContractVersion, contextVersion: c.ContextVersion, acceptanceVersion: c.AcceptanceVersion, model: model}
	payload, _ := json.Marshal(map[string]any{"session_id": session.sessionID, "packet_digest": c.PacketDigest, "contract_version": c.ContractVersion, "control_epoch": c.ControlEpoch, "context_version": c.ContextVersion, "acceptance_version": c.AcceptanceVersion, "actor": actor, "host_model": model, "trust_domain": "same_host_advisory", "independent": false})
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'review_session_created',1,$3)`, int64(task), "review:"+session.sessionID, payload); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return session, packet, nil
}
func (s *ControlReviewSession) Archive(ctx context.Context, suggestion ControlReviewSuggestion) error {
	if s == nil || s.d == nil || s.sessionID == "" {
		return control.LineageMismatch
	}
	if err := suggestion.Validate(); err != nil {
		return err
	}
	tx, err := s.d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	packet, err := buildControlReviewPacketTx(ctx, tx, s.task)
	if err != nil {
		return err
	}
	if packet.Control.PacketDigest != s.packetDigest {
		return control.StaleContext
	}
	raw, err := json.Marshal(suggestion)
	if err != nil {
		return err
	}
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT advisory FROM hypothesis_reviews WHERE session_id=$1`, s.sessionID).Scan(&prior)
	if err == nil {
		var old ControlReviewSuggestion
		if json.Unmarshal(prior, &old) != nil {
			return control.EvidenceConflict
		}
		a, _ := actionHash(old)
		b, _ := actionHash(suggestion)
		if a != b {
			return control.IdempotencyConflict
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hypothesis_reviews(exploration_id,model,advisory,usage,task_id,session_id,packet_digest,contract_version,context_version,acceptance_version,trust_domain,review_kind) VALUES($1,$9,$2,$10,$3,$4,$5,$6,$7,$8,'same_host_advisory','advisory_only')`, packet.ExplorationID, raw, int64(s.task), s.sessionID, s.packetDigest, s.contractVersion, s.contextVersion, s.acceptanceVersion, s.model, json.RawMessage(`{"cost_source":"task_budget_reservations","unknown_attempts_retained":true,"attribution_prefix":"reviewer:`+s.sessionID+`:"}`))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ControlReviewSession) SessionID() string { return s.sessionID }
