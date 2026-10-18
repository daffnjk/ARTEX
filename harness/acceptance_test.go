package harness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type acceptanceResolverFunc func(context.Context, int64, int64) (AcceptanceSpec, error)

func (f acceptanceResolverFunc) ResolveAcceptance(ctx context.Context, taskID, goalID int64) (AcceptanceSpec, error) {
	return f(ctx, taskID, goalID)
}

// Host fixtures are independent of the request under test. In particular, a
// mutated request must never be used to construct its own trusted goal state.
func fixtureAcceptance() AcceptanceSpec {
	return AcceptanceSpec{TaskID: 7, GoalID: 9, Version: "goal-spec-1",
		Criterion: Criterion{ID: "fixture-only", Version: "1"}, Claim: "fixture value is 42",
		ExperimentID: "exp-1", ContextVersion: "ctx-3"}
}

func fixtureEvaluator(resolver Resolver, registry map[Criterion]Verifier) *Evaluator {
	source, err := NewFrozenAcceptanceSource([]AcceptanceSpec{fixtureAcceptance()})
	if err != nil {
		panic(err)
	}
	return NewEvaluatorWithAcceptance(resolver, registry, source)
}

func TestFrozenAcceptanceBinding(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest)
		code   string
	}{
		{"matching", func(*EvaluationRequest) {}, ""},
		{"weak registered criterion", func(r *EvaluationRequest) { r.CriterionID = "weak-fixture" }, "acceptance_binding_mismatch"},
		{"other registered version", func(r *EvaluationRequest) { r.CriterionVersion = "2" }, "acceptance_binding_mismatch"},
		{"missing acceptance revision", func(r *EvaluationRequest) { r.AcceptanceVersion = "" }, "acceptance_binding_mismatch"},
		{"old acceptance revision", func(r *EvaluationRequest) { r.AcceptanceVersion = "goal-spec-0" }, "acceptance_binding_mismatch"},
		{"widened claim", func(r *EvaluationRequest) { r.Claim = "all goals are established" }, "acceptance_binding_mismatch"},
		{"different experiment", func(r *EvaluationRequest) { r.ExperimentID = "exp-2" }, "acceptance_binding_mismatch"},
		{"different context", func(r *EvaluationRequest) { r.ContextVersion = "ctx-4" }, "acceptance_binding_mismatch"},
		{"wrong task ownership", func(r *EvaluationRequest) { r.TaskID++ }, "acceptance_unavailable"},
		{"missing goal", func(r *EvaluationRequest) { r.GoalID++ }, "acceptance_unavailable"},
		{"blank acceptance revision", func(r *EvaluationRequest) { r.AcceptanceVersion = " " }, "invalid_request"},
		{"oversized acceptance revision", func(r *EvaluationRequest) { r.AcceptanceVersion = strings.Repeat("x", 4097) }, "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, obs := fixture()
			tc.mutate(&req)
			// These observations are internally consistent with the request so
			// the test exercises goal binding, not an earlier integrity failure.
			obs.TaskID, obs.ExperimentID, obs.ContextVersion = req.TaskID, req.ExperimentID, req.ContextVersion
			correctCalls, wrongCalls := 0, 0
			correct := func(ctx context.Context, r EvaluationRequest, o []Observation) (Verdict, error) {
				correctCalls++
				return fixtureVerifier(ctx, r, o)
			}
			wrong := func(ctx context.Context, r EvaluationRequest, o []Observation) (Verdict, error) {
				wrongCalls++
				return fixtureVerifier(ctx, r, o)
			}
			e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{{"fixture-only", "1"}: correct, {"fixture-only", "2"}: wrong, {"weak-fixture", "1"}: wrong})
			got := e.Evaluate(context.Background(), req)
			if wrongCalls != 0 {
				t.Fatalf("unrelated verifier called %d times", wrongCalls)
			}
			if tc.code == "" {
				if correctCalls != 1 || got.Disposition != Confirm || got.AcceptanceVersion != "goal-spec-1" {
					t.Fatalf("matched frozen state not dispatched: calls=%d report=%+v", correctCalls, got)
				}
			} else if correctCalls != 0 || got.Disposition != Review || got.Conclusion != Inconclusive || len(got.Issues) != 1 || got.Issues[0].Code != tc.code || got.AcceptanceVersion != "" {
				t.Fatalf("unbound state accepted: calls=%d report=%+v", correctCalls, got)
			}
		})
	}
}

func TestAcceptanceResolverIdentityAndValidity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AcceptanceSpec)
		code   string
	}{
		{"wrong returned task", func(s *AcceptanceSpec) { s.TaskID++ }, "acceptance_binding_mismatch"},
		{"wrong returned goal", func(s *AcceptanceSpec) { s.GoalID++ }, "acceptance_binding_mismatch"},
		{"invalid returned state", func(s *AcceptanceSpec) { s.Version = "" }, "acceptance_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, obs := fixture()
			spec := fixtureAcceptance()
			tc.mutate(&spec)
			called := false
			source := acceptanceResolverFunc(func(_ context.Context, task, goal int64) (AcceptanceSpec, error) {
				if task != req.TaskID || goal != req.GoalID {
					t.Fatalf("lookup was not task/goal scoped: %d/%d", task, goal)
				}
				return spec, nil
			})
			e := NewEvaluatorWithAcceptance(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{fixtureAcceptance().Criterion: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
					called = true
					return Verdict{}, nil
				}}, source)
			got := e.Evaluate(context.Background(), req)
			if called || got.Disposition != Review || got.Conclusion != Inconclusive || got.Issues[0].Code != tc.code {
				t.Fatalf("invalid resolver binding reached verifier: called=%v report=%+v", called, got)
			}
		})
	}
}

func TestAcceptanceChangeDiscardsVerdict(t *testing.T) {
	changes := []struct {
		name   string
		mutate func(*AcceptanceSpec)
	}{
		{"revision", func(s *AcceptanceSpec) { s.Version = "goal-spec-2" }},
		{"same revision claim change", func(s *AcceptanceSpec) { s.Claim = "different claim" }},
		{"same revision criterion change", func(s *AcceptanceSpec) { s.Criterion.ID = "different-rule" }},
		{"same revision context change", func(s *AcceptanceSpec) { s.ContextVersion = "ctx-4" }},
		{"same revision experiment change", func(s *AcceptanceSpec) { s.ExperimentID = "exp-2" }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			req, obs := fixture()
			reads, verifies := 0, 0
			source := acceptanceResolverFunc(func(context.Context, int64, int64) (AcceptanceSpec, error) {
				reads++
				s := fixtureAcceptance()
				if reads > 1 {
					tc.mutate(&s)
				}
				return s, nil
			})
			e := NewEvaluatorWithAcceptance(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{fixtureAcceptance().Criterion: func(ctx context.Context, r EvaluationRequest, o []Observation) (Verdict, error) {
					verifies++
					return fixtureVerifier(ctx, r, o)
				}}, source)
			got := e.Evaluate(context.Background(), req)
			if reads != 2 || verifies != 1 || got.Disposition != Review || got.Conclusion != Inconclusive || got.Issues[0].Code != "acceptance_changed" || len(got.UsedEvidence) != 0 {
				t.Fatalf("changed state retained verdict: reads=%d verifies=%d report=%+v", reads, verifies, got)
			}
		})
	}
}

func TestAcceptanceResolutionFailureAndCancellation(t *testing.T) {
	for _, phase := range []int{1, 2} {
		for _, kind := range []string{"missing", "error", "panic", "cancel"} {
			t.Run(kind+string(rune('0'+phase)), func(t *testing.T) {
				req, obs := fixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reads, verifies := 0, 0
				source := acceptanceResolverFunc(func(context.Context, int64, int64) (AcceptanceSpec, error) {
					reads++
					if reads == phase {
						switch kind {
						case "missing":
							return AcceptanceSpec{}, errors.New("not found")
						case "error":
							return AcceptanceSpec{}, errors.New("private backend detail")
						case "panic":
							panic("private backend detail")
						case "cancel":
							cancel()
						}
					}
					return fixtureAcceptance(), nil
				})
				e := NewEvaluatorWithAcceptance(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
					map[Criterion]Verifier{fixtureAcceptance().Criterion: func(ctx context.Context, r EvaluationRequest, o []Observation) (Verdict, error) {
						verifies++
						return fixtureVerifier(ctx, r, o)
					}}, source)
				got := e.Evaluate(ctx, req)
				code := "acceptance_unavailable"
				if kind == "cancel" {
					code = "cancelled"
				}
				if verifies != phase-1 || got.Disposition != Review || got.Conclusion != Inconclusive || got.Issues[0].Code != code || strings.Contains(got.Reason, "private") || len(got.UsedEvidence) != 0 {
					t.Fatalf("resolution failure did not fail closed: verifies=%d report=%+v", verifies, got)
				}
			})
		}
	}
}

func TestFrozenAcceptanceSourceSnapshotsAndValidates(t *testing.T) {
	spec := fixtureAcceptance()
	input := []AcceptanceSpec{spec}
	source, err := NewFrozenAcceptanceSource(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Claim = "overwritten by caller"
	got, err := source.ResolveAcceptance(context.Background(), spec.TaskID, spec.GoalID)
	if err != nil || got != spec {
		t.Fatalf("not frozen: %+v %v", got, err)
	}
	got.Claim = "overwrite returned value"
	got, err = source.ResolveAcceptance(context.Background(), spec.TaskID, spec.GoalID)
	if err != nil || got != spec {
		t.Fatalf("returned value aliases source: %+v %v", got, err)
	}
	if _, err := source.ResolveAcceptance(context.Background(), spec.TaskID+1, spec.GoalID); err == nil {
		t.Fatal("cross-task lookup succeeded")
	}
	if _, err := source.ResolveAcceptance(context.Background(), spec.TaskID, spec.GoalID+1); err == nil {
		t.Fatal("unknown goal lookup succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.ResolveAcceptance(ctx, spec.TaskID, spec.GoalID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup: %v", err)
	}
	var missing *FrozenAcceptanceSource
	if _, err := missing.ResolveAcceptance(context.Background(), spec.TaskID, spec.GoalID); err == nil {
		t.Fatal("nil source succeeded")
	}
	if _, err := NewFrozenAcceptanceSource([]AcceptanceSpec{spec, spec}); err == nil {
		t.Fatal("duplicate goal accepted")
	}
	invalid := []AcceptanceSpec{spec, spec, spec, spec, spec, spec, spec, spec, spec}
	invalid[0].TaskID = 0
	invalid[1].GoalID = -1
	invalid[2].Version = ""
	invalid[3].Criterion.ID = " "
	invalid[4].Criterion.Version = ""
	invalid[5].Claim = ""
	invalid[6].ExperimentID = ""
	invalid[7].ContextVersion = ""
	invalid[8].Claim = strings.Repeat("x", 4097)
	for i, bad := range invalid {
		if _, err := NewFrozenAcceptanceSource([]AcceptanceSpec{bad}); err == nil {
			t.Fatalf("invalid frozen spec %d accepted", i)
		}
	}
}

func TestFrozenAcceptanceConcurrentEvaluation(t *testing.T) {
	req, obs := fixture()
	e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
		map[Criterion]Verifier{fixtureAcceptance().Criterion: fixtureVerifier})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := e.Evaluate(context.Background(), req); got.Disposition != Confirm {
				t.Errorf("concurrent frozen evaluation: %+v", got)
			}
		}()
	}
	wg.Wait()
}

func TestAcceptanceAllowsDifferentControlIdentities(t *testing.T) {
	req, obs := fixture()
	control := obs
	control.ID, control.SubjectRef, control.ObjectRef = "obs-control", "control-user", "control-object"
	req.Evidence = append(req.Evidence, EvidenceRef{control.ID, Digest(control.Payload)})
	called := false
	e := fixtureEvaluator(resolverFunc(func(_ context.Context, id string) (Observation, error) {
		if id == control.ID {
			return control, nil
		}
		return obs, nil
	}), map[Criterion]Verifier{fixtureAcceptance().Criterion: func(_ context.Context, _ EvaluationRequest, observations []Observation) (Verdict, error) {
		called = true
		if len(observations) != 2 || observations[0].SubjectRef == observations[1].SubjectRef || observations[0].ObjectRef == observations[1].ObjectRef {
			t.Fatal("legitimate control conditions were collapsed")
		}
		return Verdict{ValidityValid, Supported, []string{obs.ID, control.ID}, "Synthetic paired control fixture."}, nil
	}})
	if got := e.Evaluate(context.Background(), req); !called || got.Disposition != Confirm {
		t.Fatalf("control pair rejected: %+v", got)
	}
}

func TestAcceptanceMissingRegisteredRule(t *testing.T) {
	req, obs := fixture()
	source, err := NewFrozenAcceptanceSource([]AcceptanceSpec{fixtureAcceptance()})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	e := NewEvaluatorWithAcceptance(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
		map[Criterion]Verifier{{"unrelated", "1"}: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
			called = true
			return Verdict{}, nil
		}}, source)
	got := e.Evaluate(context.Background(), req)
	if called || got.Disposition != Review || got.Conclusion != Inconclusive || len(got.Issues) != 1 || got.Issues[0].Code != "criterion_unimplemented" {
		t.Fatalf("missing frozen rule used a fallback: called=%v report=%+v", called, got)
	}
}

func TestCancellationAfterLastEvidenceCannotReachVerifier(t *testing.T) {
	req, obs := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) {
		cancel()
		return obs, nil
	}), map[Criterion]Verifier{fixtureAcceptance().Criterion: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
		called = true
		return Verdict{}, nil
	}})
	got := e.Evaluate(ctx, req)
	if called || got.Disposition != Review || got.Conclusion != Inconclusive || len(got.Issues) != 1 || got.Issues[0].Code != "cancelled" {
		t.Fatalf("cancelled evidence read reached verifier: called=%v report=%+v", called, got)
	}
}
