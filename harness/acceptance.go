package harness

import (
	"context"
	"fmt"
	"strings"
)

// AcceptanceSpec is a trusted host's frozen definition for ONE task/goal.
// Claim is an exact proposition, not an instruction or a fuzzy semantic match.
// ExperimentID and ContextVersion constrain where that proposition is evaluated.
// Version must identify an immutable revision; a live store must never reuse it.
// Constructing a valid Go value does not establish trust in its source.
type AcceptanceSpec struct {
	TaskID         int64
	GoalID         int64
	Version        string
	Criterion      Criterion
	Claim          string
	ExperimentID   string
	ContextVersion string
}

func (s AcceptanceSpec) Validate() error {
	if s.TaskID <= 0 || s.GoalID <= 0 {
		return fmt.Errorf("acceptance task_id and goal_id must be positive")
	}
	for _, field := range []string{s.Version, s.Criterion.ID, s.Criterion.Version, s.Claim, s.ExperimentID, s.ContextVersion} {
		if strings.TrimSpace(field) == "" || len(field) > 4096 {
			return fmt.Errorf("missing or oversized acceptance field")
		}
	}
	return nil
}

func (s AcceptanceSpec) matches(req EvaluationRequest) bool {
	return s.TaskID == req.TaskID && s.GoalID == req.GoalID &&
		s.Version == req.AcceptanceVersion && s.Criterion.ID == req.CriterionID &&
		s.Criterion.Version == req.CriterionVersion && s.Claim == req.Claim &&
		s.ExperimentID == req.ExperimentID && s.ContextVersion == req.ContextVersion
}

// AcceptanceResolver is installed by the trusted application, never by the
// evaluation request. Resolve by task/goal, NOT by caller-selected criterion or
// configuration ID. Enforce caller/task access, goal ownership and frozen/current
// revision semantics in the backing store. Returned values must be snapshots.
// Evaluate rereads this source after verification; final database completion
// must still atomically revalidate the revision and applicable conditions.
type AcceptanceResolver interface {
	ResolveAcceptance(context.Context, int64, int64) (AcceptanceSpec, error)
}

type acceptanceGoal struct{ taskID, goalID int64 }

// FrozenAcceptanceSource holds immutable host-provided offline goal contracts.
// It has no write method and returns values, not mutable map/slice references.
// It is NOT a trusted ingestion path for client JSON or model-provided specs.
type FrozenAcceptanceSource struct {
	specs map[acceptanceGoal]AcceptanceSpec
}

func NewFrozenAcceptanceSource(specs []AcceptanceSpec) (*FrozenAcceptanceSource, error) {
	source := &FrozenAcceptanceSource{specs: make(map[acceptanceGoal]AcceptanceSpec, len(specs))}
	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		key := acceptanceGoal{spec.TaskID, spec.GoalID}
		if _, exists := source.specs[key]; exists {
			return nil, fmt.Errorf("duplicate frozen task/goal acceptance")
		}
		source.specs[key] = spec
	}
	return source, nil
}

func (s *FrozenAcceptanceSource) ResolveAcceptance(ctx context.Context, taskID, goalID int64) (AcceptanceSpec, error) {
	if err := ctx.Err(); err != nil {
		return AcceptanceSpec{}, err
	}
	if s == nil {
		return AcceptanceSpec{}, fmt.Errorf("acceptance source unavailable")
	}
	spec, exists := s.specs[acceptanceGoal{taskID, goalID}]
	if !exists {
		return AcceptanceSpec{}, fmt.Errorf("frozen goal acceptance not found")
	}
	return spec, nil
}

// NewEvaluatorWithAcceptance snapshots the verifier registry and installs a
// host-owned goal source. Neither this constructor nor EvaluationReport grants
// runtime write authority. The offline CLI intentionally does not use it.
func NewEvaluatorWithAcceptance(resolver Resolver, registry map[Criterion]Verifier, source AcceptanceResolver) *Evaluator {
	e := NewEvaluator(resolver, registry)
	e.acceptance = source
	return e
}

func resolveAcceptance(ctx context.Context, source AcceptanceResolver, taskID, goalID int64) (spec AcceptanceSpec, err error) {
	defer func() {
		if recover() != nil {
			spec = AcceptanceSpec{}
			err = fmt.Errorf("acceptance resolver panic")
		}
	}()
	if source == nil {
		return AcceptanceSpec{}, fmt.Errorf("acceptance source unavailable")
	}
	return source.ResolveAcceptance(ctx, taskID, goalID)
}
