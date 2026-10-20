// Package control contains deterministic domain contracts, without I/O or database dependencies.
package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type TaskID int64
type ExplorationID int64
type ExecutionID string
type AttemptID string

func parseID(s, prefix string) (int64, error) {
	if !strings.HasPrefix(s, prefix+":") {
		return 0, fmt.Errorf("expected %s ID", prefix)
	}
	v, err := strconv.ParseInt(strings.TrimPrefix(s, prefix+":"), 10, 64)
	if err != nil || v <= 0 {
		return 0, errors.New("invalid positive ID")
	}
	return v, nil
}
func ParseTaskID(s string) (TaskID, error) { v, e := parseID(s, "task"); return TaskID(v), e }
func ParseExplorationID(s string) (ExplorationID, error) {
	v, e := parseID(s, "exploration")
	return ExplorationID(v), e
}

type TaskContext struct {
	TaskID           TaskID
	ExplorationID    ExplorationID
	ContractVersion  int64
	ControlEpoch     int64
	ContextVersion   string
	CriterionVersion string
}

func (c TaskContext) Validate() error {
	if c.TaskID <= 0 || c.ExplorationID <= 0 || c.ContractVersion <= 0 || c.ControlEpoch <= 0 || !bounded(c.ContextVersion, 128) || !bounded(c.CriterionVersion, 128) {
		return errors.New("incomplete host task context")
	}
	return nil
}

type TaskMode string

const (
	ModeDirect   TaskMode = "direct"
	ModeGoal     TaskMode = "goal"
	ModeCoverage TaskMode = "coverage"
)

func (m TaskMode) Valid() bool { return m == ModeDirect || m == ModeGoal || m == ModeCoverage }

type RunStatus string
type EffectStatus string
type CleanupStatus string

const (
	RunQueued           RunStatus     = "queued"
	RunRunning          RunStatus     = "running"
	RunSucceeded        RunStatus     = "succeeded"
	RunFailed           RunStatus     = "failed"
	RunCancelled        RunStatus     = "cancelled"
	RunTimedOut         RunStatus     = "timed_out"
	RunLost             RunStatus     = "lost"
	EffectNotApplicable EffectStatus  = "not_applicable"
	EffectApplied       EffectStatus  = "known_applied"
	EffectNotApplied    EffectStatus  = "known_not_applied"
	EffectUnknown       EffectStatus  = "unknown"
	CleanupNotRequired  CleanupStatus = "not_required"
	CleanupPending      CleanupStatus = "pending"
	CleanupConfirmed    CleanupStatus = "confirmed"
	CleanupUnconfirmed  CleanupStatus = "unconfirmed"
	CleanupFailed       CleanupStatus = "failed"
)

type Outcome struct {
	Run     RunStatus     `json:"run"`
	Effect  EffectStatus  `json:"effect"`
	Cleanup CleanupStatus `json:"cleanup"`
}

func (o Outcome) Validate() error {
	switch o.Run {
	case RunQueued, RunRunning, RunSucceeded, RunFailed, RunCancelled, RunTimedOut, RunLost:
	default:
		return errors.New("invalid run status")
	}
	switch o.Effect {
	case EffectNotApplicable, EffectApplied, EffectNotApplied, EffectUnknown:
	default:
		return errors.New("invalid effect status")
	}
	switch o.Cleanup {
	case CleanupNotRequired, CleanupPending, CleanupConfirmed, CleanupUnconfirmed, CleanupFailed:
	default:
		return errors.New("invalid cleanup status")
	}
	return nil
}

type Validity string

const (
	Valid   Validity = "valid"
	Invalid Validity = "invalid"
	Unknown Validity = "unknown"
)

type StopReason string

const (
	StopComplete StopReason = "complete"
	StopDeadline StopReason = "deadline"
	StopBudget   StopReason = "budget"
	StopUnknown  StopReason = "inconclusive"
	StopRevoked  StopReason = "revoked"
)

func LegacyStatus(r StopReason) string {
	switch r {
	case StopComplete:
		return "done"
	case StopDeadline, StopBudget:
		return "timeout"
	default:
		return "failed"
	}
}

type ErrorCode string

const (
	ContractMissing       ErrorCode = "CONTRACT_MISSING"
	ContractExpired       ErrorCode = "CONTRACT_EXPIRED"
	ContractRevoked       ErrorCode = "CONTRACT_REVOKED"
	ScopeDenied           ErrorCode = "SCOPE_DENIED"
	MethodUnsupported     ErrorCode = "METHOD_UNSUPPORTED"
	UnboundedEffect       ErrorCode = "UNBOUNDED_EFFECT"
	ApprovalRequired      ErrorCode = "APPROVAL_REQUIRED"
	BudgetExhausted       ErrorCode = "BUDGET_EXHAUSTED"
	PreconditionInvalid   ErrorCode = "PRECONDITION_INVALID"
	EvidenceUnavailable   ErrorCode = "EVIDENCE_UNAVAILABLE"
	EvidenceConflict      ErrorCode = "EVIDENCE_CONFLICT"
	LineageMismatch       ErrorCode = "LINEAGE_MISMATCH"
	StaleContext          ErrorCode = "STALE_CONTEXT"
	PlanConflict          ErrorCode = "PLAN_CONFLICT"
	StaleLease            ErrorCode = "STALE_LEASE"
	IdempotencyConflict   ErrorCode = "IDEMPOTENCY_CONFLICT"
	AlreadyRunning        ErrorCode = "ALREADY_RUNNING"
	ResultAvailable       ErrorCode = "RESULT_AVAILABLE"
	WaitExpired           ErrorCode = "WAIT_EXPIRED"
	ToolUnavailable       ErrorCode = "TOOL_UNAVAILABLE"
	ObservationIncomplete ErrorCode = "OBSERVATION_INCOMPLETE"
	TargetRateLimited     ErrorCode = "TARGET_RATE_LIMITED"
	EffectUncertain       ErrorCode = "EFFECT_UNKNOWN"
	CleanupUncertain      ErrorCode = "CLEANUP_UNCONFIRMED"
)

func (e ErrorCode) Error() string { return string(e) }

const MaxProposalBytes = 16 * 1024
const MaxPlanSteps = 128

type Target struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Path   string `json:"path"`
}
type ActionProposal struct {
	SchemaVersion    int     `json:"schema_version"`
	MethodID         string  `json:"method_id"`
	MethodVersion    string  `json:"method_version"`
	Target           Target  `json:"target"`
	Purpose          string  `json:"purpose"`
	ContextVersion   string  `json:"context_version"`
	IdentityRef      *string `json:"identity_ref,omitempty"`
	ExperimentID     *int64  `json:"experiment_id,omitempty"`
	StepID           *int64  `json:"step_id,omitempty"`
	RequiredClaimIDs []int64 `json:"required_claim_ids,omitempty"`
	IdempotencyKey   string  `json:"idempotency_key"`
	RetestRef        *string `json:"retest_ref,omitempty"`
}

func bounded(s string, n int) bool { return strings.TrimSpace(s) != "" && len(s) <= n }
func (p ActionProposal) Validate() error {
	if p.SchemaVersion != 1 || !bounded(p.MethodID, 120) || !bounded(p.MethodVersion, 64) || !bounded(p.Purpose, 512) || !bounded(p.ContextVersion, 128) || !bounded(p.IdempotencyKey, 128) {
		return errors.New("invalid proposal fields")
	}
	if (p.Target.Scheme != "http" && p.Target.Scheme != "https") || !bounded(p.Target.Host, 253) || p.Target.Port < 1 || p.Target.Port > 65535 || !strings.HasPrefix(p.Target.Path, "/") || len(p.Target.Path) > 2048 {
		return errors.New("invalid structured target")
	}
	if p.RetestRef != nil && !bounded(*p.RetestRef, 128) {
		return errors.New("invalid retest reference")
	}
	if p.IdentityRef != nil && !bounded(*p.IdentityRef, 128) {
		return errors.New("invalid identity reference")
	}
	for _, v := range []*int64{p.ExperimentID, p.StepID} {
		if v != nil && *v <= 0 {
			return errors.New("invalid reference")
		}
	}
	if len(p.RequiredClaimIDs) > 32 {
		return errors.New("too many claim references")
	}
	seen := map[int64]bool{}
	for _, id := range p.RequiredClaimIDs {
		if id <= 0 || seen[id] {
			return errors.New("invalid or duplicate claim reference")
		}
		seen[id] = true
	}
	return nil
}
func DecodeProposal(raw []byte) (ActionProposal, error) {
	var p ActionProposal
	if len(raw) > MaxProposalBytes {
		return p, errors.New("proposal too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, errors.New("proposal must be a single JSON object")
	}
	return p, p.Validate()
}
