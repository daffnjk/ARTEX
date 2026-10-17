// Package harness provides read-only, evidence-based evaluation contracts.
// It does not execute tests against targets, authorize actions, or mutate goals.
package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion         = "1"
	MaxEvidenceRefs       = 32
	MaxArtifactBytes      = 1 << 20
	MaxTotalArtifactBytes = 4 << 20
)

type Validity string
type Conclusion string
type Disposition string

const (
	ValidityUnknown Validity    = "unknown"
	ValidityValid   Validity    = "valid"
	ValidityInvalid Validity    = "invalid"
	Inconclusive    Conclusion  = "inconclusive"
	Supported       Conclusion  = "supported"
	Refuted         Conclusion  = "refuted"
	Review          Disposition = "review"
	Confirm         Disposition = "confirm"
	NotSupported    Disposition = "not_supported"
	Retry           Disposition = "retry"
)

type EvidenceRef struct {
	ObservationID string `json:"observation_id"`
	SHA256        string `json:"sha256"`
}

// EvaluationRequest intentionally has no conversation history, model confidence,
// tool command, or permission to change a task's state.
type EvaluationRequest struct {
	SchemaVersion    string        `json:"schema_version"`
	ID               string        `json:"id"`
	TaskID           int64         `json:"task_id"`
	GoalID           int64         `json:"goal_id"`
	ExperimentID     string        `json:"experiment_id"`
	ContextVersion   string        `json:"context_version"`
	Claim            string        `json:"claim"`
	CriterionID      string        `json:"criterion_id"`
	CriterionVersion string        `json:"criterion_version"`
	Evidence         []EvidenceRef `json:"evidence"`
}

// Observation MUST be obtained from a trusted capture store, never reconstructed
// from the model's narrative. Payload is the captured artifact, not its summary.
// ContextVersion identifies a frozen experiment context; SubjectRef/ObjectRef
// retain per-observation conditions, including intentional role differences.
type Observation struct {
	ID             string    `json:"id"`
	TaskID         int64     `json:"task_id"`
	ExperimentID   string    `json:"experiment_id"`
	ExecutionID    string    `json:"execution_id"`
	ContextVersion string    `json:"context_version"`
	SubjectRef     string    `json:"subject_ref"`
	ObjectRef      string    `json:"object_ref"`
	ObservedAt     time.Time `json:"observed_at"`
	Complete       bool      `json:"complete"`
	Payload        []byte    `json:"payload"`
}

type Issue struct {
	Code          string `json:"code"`
	ObservationID string `json:"observation_id,omitempty"`
}

type EvaluationReport struct {
	SchemaVersion    string        `json:"schema_version"`
	RequestID        string        `json:"request_id"`
	TaskID           int64         `json:"task_id"`
	GoalID           int64         `json:"goal_id"`
	ExperimentID     string        `json:"experiment_id"`
	ContextVersion   string        `json:"context_version"`
	CriterionID      string        `json:"criterion_id"`
	CriterionVersion string        `json:"criterion_version"`
	Validity         Validity      `json:"validity"`
	Conclusion       Conclusion    `json:"conclusion"`
	Disposition      Disposition   `json:"disposition"`
	CheckedEvidence  []EvidenceRef `json:"checked_evidence"`
	UsedEvidence     []string      `json:"used_evidence,omitempty"`
	Reason           string        `json:"reason,omitempty"`
	Issues           []Issue       `json:"issues,omitempty"`
}

// Resolver implementations must enforce caller/task access and bound reads
// before allocating artifacts. Evaluate also checks task/context and byte caps.
type Resolver interface {
	Resolve(context.Context, string) (Observation, error)
}

type Criterion struct{ ID, Version string }

type Verdict struct {
	Validity    Validity
	Conclusion  Conclusion
	EvidenceIDs []string
	Reason      string
}

// Verifier is trusted, versioned application code. A second LLM's agreement or
// a non-empty "execution_evidence" string is NOT a Verifier implementation.
type Verifier func(context.Context, EvaluationRequest, []Observation) (Verdict, error)

type Evaluator struct {
	resolver  Resolver
	verifiers map[Criterion]Verifier
}

// NewEvaluator snapshots the registry. No domain verifier is registered by
// default: artifact integrity alone can never confirm a technical claim.
func NewEvaluator(resolver Resolver, registry map[Criterion]Verifier) *Evaluator {
	copyRegistry := make(map[Criterion]Verifier, len(registry))
	for key, verifier := range registry {
		copyRegistry[key] = verifier
	}
	return &Evaluator{resolver: resolver, verifiers: copyRegistry}
}

func (r EvaluationRequest) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version")
	}
	if r.TaskID <= 0 || r.GoalID <= 0 {
		return fmt.Errorf("task_id and goal_id must be positive")
	}
	for _, s := range []string{r.ID, r.ExperimentID, r.ContextVersion, r.Claim, r.CriterionID, r.CriterionVersion} {
		if strings.TrimSpace(s) == "" || len(s) > 4096 {
			return fmt.Errorf("missing or oversized request field")
		}
	}
	if len(r.Evidence) == 0 || len(r.Evidence) > MaxEvidenceRefs {
		return fmt.Errorf("evidence count out of range")
	}
	seen := make(map[string]bool, len(r.Evidence))
	for _, ref := range r.Evidence {
		if strings.TrimSpace(ref.ObservationID) == "" || len(ref.ObservationID) > 256 || seen[ref.ObservationID] {
			return fmt.Errorf("missing, oversized, or duplicate observation_id")
		}
		seen[ref.ObservationID] = true
		digest, err := hex.DecodeString(ref.SHA256)
		if err != nil || len(digest) != sha256.Size || strings.ToLower(ref.SHA256) != ref.SHA256 {
			return fmt.Errorf("sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	return nil
}

// Digest computes the hash of the exact captured bytes (before summarization).
func Digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (e *Evaluator) Evaluate(ctx context.Context, req EvaluationRequest) EvaluationReport {
	r := EvaluationReport{
		SchemaVersion: SchemaVersion, RequestID: req.ID, TaskID: req.TaskID,
		GoalID: req.GoalID, ExperimentID: req.ExperimentID, ContextVersion: req.ContextVersion,
		CriterionID: req.CriterionID, CriterionVersion: req.CriterionVersion,
		Validity: ValidityUnknown, Conclusion: Inconclusive, Disposition: Review,
		CheckedEvidence: []EvidenceRef{},
	}
	fail := func(code, id string, validity Validity, disposition Disposition) EvaluationReport {
		r.Validity, r.Disposition = validity, disposition
		r.Issues = append(r.Issues, Issue{Code: code, ObservationID: id})
		return r
	}
	if req.Validate() != nil {
		return fail("invalid_request", "", ValidityUnknown, Review)
	}
	if ctx.Err() != nil {
		return fail("cancelled", "", ValidityUnknown, Review)
	}
	if e == nil || e.resolver == nil {
		return fail("resolver_unavailable", "", ValidityUnknown, Review)
	}
	// A caller's later slice edits cannot change the evidence list passed to a checker.
	req.Evidence = append([]EvidenceRef(nil), req.Evidence...)
	observations := make([]Observation, 0, len(req.Evidence))
	totalBytes := 0
	for _, ref := range req.Evidence {
		if ctx.Err() != nil {
			return fail("cancelled", ref.ObservationID, ValidityUnknown, Review)
		}
		o, err := e.resolver.Resolve(ctx, ref.ObservationID)
		if err != nil {
			return fail("evidence_unavailable", ref.ObservationID, ValidityUnknown, Review)
		}
		if o.ID != ref.ObservationID || o.TaskID != req.TaskID || o.ExperimentID != req.ExperimentID {
			return fail("evidence_binding_mismatch", ref.ObservationID, ValidityInvalid, Review)
		}
		if o.ContextVersion != req.ContextVersion {
			return fail("stale_context", o.ID, ValidityInvalid, Retry)
		}
		if strings.TrimSpace(o.ExecutionID) == "" || strings.TrimSpace(o.SubjectRef) == "" || strings.TrimSpace(o.ObjectRef) == "" || o.ObservedAt.IsZero() {
			return fail("capture_metadata_missing", o.ID, ValidityUnknown, Review)
		}
		if !o.Complete {
			return fail("capture_incomplete", o.ID, ValidityUnknown, Review)
		}
		if len(o.Payload) > MaxArtifactBytes || totalBytes+len(o.Payload) > MaxTotalArtifactBytes {
			return fail("artifact_too_large", o.ID, ValidityUnknown, Review)
		}
		totalBytes += len(o.Payload)
		o.Payload = append([]byte(nil), o.Payload...)
		if Digest(o.Payload) != ref.SHA256 {
			return fail("digest_mismatch", o.ID, ValidityInvalid, Review)
		}
		observations = append(observations, o)
		r.CheckedEvidence = append(r.CheckedEvidence, ref)
	}
	verifier := e.verifiers[Criterion{ID: req.CriterionID, Version: req.CriterionVersion}]
	if verifier == nil {
		return fail("criterion_unimplemented", "", ValidityUnknown, Review)
	}
	v, err := invokeVerifier(ctx, verifier, req, observations)
	if ctx.Err() != nil {
		return fail("cancelled", "", ValidityUnknown, Review)
	}
	if err != nil {
		return fail("verifier_error", "", ValidityUnknown, Review)
	}
	if !validVerdict(v, r.CheckedEvidence) {
		return fail("invalid_verdict", "", ValidityUnknown, Review)
	}
	r.Validity, r.Conclusion, r.Reason = v.Validity, v.Conclusion, v.Reason
	r.UsedEvidence = append([]string(nil), v.EvidenceIDs...)
	switch {
	case v.Validity == ValidityValid && v.Conclusion == Supported:
		r.Disposition = Confirm
	case v.Validity == ValidityValid && v.Conclusion == Refuted:
		r.Disposition = NotSupported // This claim only; never "the target is safe".
	case v.Validity == ValidityInvalid:
		r.Disposition = Retry // A suggestion; Evaluate never repeats external actions.
	}
	return r
}

func invokeVerifier(ctx context.Context, verifier Verifier, req EvaluationRequest, observations []Observation) (v Verdict, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("verifier panic")
		}
	}()
	return verifier(ctx, req, observations)
}

func validVerdict(v Verdict, refs []EvidenceRef) bool {
	if strings.TrimSpace(v.Reason) == "" || len(v.Reason) > 4096 {
		return false
	}
	switch v.Validity {
	case ValidityValid, ValidityInvalid, ValidityUnknown:
	default:
		return false
	}
	switch v.Conclusion {
	case Supported, Refuted, Inconclusive:
	default:
		return false
	}
	if v.Validity != ValidityValid && v.Conclusion != Inconclusive {
		return false
	}
	if v.Conclusion != Inconclusive && len(v.EvidenceIDs) == 0 {
		return false
	}
	known, used := map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		known[ref.ObservationID] = true
	}
	for _, id := range v.EvidenceIDs {
		if !known[id] || used[id] {
			return false
		}
		used[id] = true
	}
	return true
}
