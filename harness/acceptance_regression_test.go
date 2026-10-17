package harness

import (
	"context"
	"testing"
)

// This regression deliberately uses the old public constructor. A registry is
// not a trusted mapping from the task/goal to its frozen success definition.
// It compiled and failed against the unmodified baseline before the fix.
func TestVerifierRequiresTrustedGoalAcceptance(t *testing.T) {
	for _, unrelatedClaim := range []bool{false, true} {
		name := "request_selected_weak_criterion"
		if unrelatedClaim {
			name = "same_criterion_unrelated_claim"
		}
		t.Run(name, func(t *testing.T) {
			req, obs := fixture()
			if unrelatedClaim {
				req.Claim = "an unrelated goal is fully established"
			} else {
				req.CriterionID = "weak-fixture-integrity"
			}
			called := false
			weak := func(ctx context.Context, r EvaluationRequest, observations []Observation) (Verdict, error) {
				called = true
				return fixtureVerifier(ctx, r, observations)
			}
			e := NewEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{{req.CriterionID, req.CriterionVersion}: weak})
			got := e.Evaluate(context.Background(), req)
			if called || got.Disposition != Review || got.Conclusion != Inconclusive {
				t.Fatalf("unbound request reached verifier: called=%v disposition=%s conclusion=%s", called, got.Disposition, got.Conclusion)
			}
		})
	}
}
