package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) (Observation, error)

func (f resolverFunc) Resolve(ctx context.Context, id string) (Observation, error) { return f(ctx, id) }

func fixture() (EvaluationRequest, Observation) {
	o := Observation{ID: "obs-1", TaskID: 7, ExperimentID: "exp-1", ExecutionID: "exec-1",
		ContextVersion: "ctx-3", SubjectRef: "fixture-user", ObjectRef: "fixture-object",
		ObservedAt: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), Complete: true,
		Payload: []byte(`{"fixture_value":42}`)}
	r := EvaluationRequest{SchemaVersion: SchemaVersion, ID: "eval-1", TaskID: 7, GoalID: 9,
		ExperimentID: "exp-1", ContextVersion: "ctx-3", Claim: "fixture value is 42",
		CriterionID: "fixture-only", CriterionVersion: "1", AcceptanceVersion: "goal-spec-1",
		Evidence: []EvidenceRef{{ObservationID: o.ID, SHA256: Digest(o.Payload)}}}
	return r, o
}

// A synthetic checker used ONLY to test the engine's dispatch. It is not
// exported, shipped in a registry, or applicable to a security finding.
func fixtureVerifier(_ context.Context, _ EvaluationRequest, observations []Observation) (Verdict, error) {
	conclusion := Refuted
	if string(observations[0].Payload) == `{"fixture_value":42}` {
		conclusion = Supported
	}
	return Verdict{Validity: ValidityValid, Conclusion: conclusion,
		EvidenceIDs: []string{observations[0].ID}, Reason: "Exact local fixture comparison."}, nil
}

func TestUnimplementedCriterionNeverConfirms(t *testing.T) {
	req, obs := fixture()
	r := NewEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }), nil).Evaluate(context.Background(), req)
	if r.Disposition != Review || r.Validity != ValidityUnknown || r.Conclusion != Inconclusive || len(r.CheckedEvidence) != 1 || r.Issues[0].Code != "criterion_unimplemented" {
		t.Fatalf("integrity was mistaken for domain proof: %+v", r)
	}
}

func TestEvidenceFailuresCannotReachVerifier(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest, *Observation)
		code   string
	}{
		{"schema", func(r *EvaluationRequest, _ *Observation) { r.SchemaVersion = "2" }, "invalid_request"},
		{"negative task", func(r *EvaluationRequest, _ *Observation) { r.TaskID = -1 }, "invalid_request"},
		{"empty identity", func(r *EvaluationRequest, _ *Observation) { r.ID = " " }, "invalid_request"},
		{"empty claim", func(r *EvaluationRequest, _ *Observation) { r.Claim = "" }, "invalid_request"},
		{"oversized claim", func(r *EvaluationRequest, _ *Observation) { r.Claim = strings.Repeat("x", 4097) }, "invalid_request"},
		{"no evidence", func(r *EvaluationRequest, _ *Observation) { r.Evidence = nil }, "invalid_request"},
		{"duplicate evidence", func(r *EvaluationRequest, _ *Observation) { r.Evidence = append(r.Evidence, r.Evidence[0]) }, "invalid_request"},
		{"bad digest", func(r *EvaluationRequest, _ *Observation) { r.Evidence[0].SHA256 = "observed" }, "invalid_request"},
		{"uppercase digest", func(r *EvaluationRequest, _ *Observation) {
			r.Evidence[0].SHA256 = strings.ToUpper(r.Evidence[0].SHA256)
		}, "invalid_request"},
		{"wrong record", func(_ *EvaluationRequest, o *Observation) { o.ID = "obs-other" }, "evidence_binding_mismatch"},
		{"cross task", func(_ *EvaluationRequest, o *Observation) { o.TaskID++ }, "evidence_binding_mismatch"},
		{"cross experiment", func(_ *EvaluationRequest, o *Observation) { o.ExperimentID = "other" }, "evidence_binding_mismatch"},
		{"stale context", func(_ *EvaluationRequest, o *Observation) { o.ContextVersion = "ctx-2" }, "stale_context"},
		{"no execution", func(_ *EvaluationRequest, o *Observation) { o.ExecutionID = "" }, "capture_metadata_missing"},
		{"no subject", func(_ *EvaluationRequest, o *Observation) { o.SubjectRef = "" }, "capture_metadata_missing"},
		{"no object", func(_ *EvaluationRequest, o *Observation) { o.ObjectRef = "" }, "capture_metadata_missing"},
		{"no capture time", func(_ *EvaluationRequest, o *Observation) { o.ObservedAt = time.Time{} }, "capture_metadata_missing"},
		{"truncated artifact", func(_ *EvaluationRequest, o *Observation) { o.Complete = false }, "capture_incomplete"},
		{"oversized artifact", func(_ *EvaluationRequest, o *Observation) { o.Payload = make([]byte, MaxArtifactBytes+1) }, "artifact_too_large"},
		{"tampered bytes", func(_ *EvaluationRequest, o *Observation) { o.Payload = []byte("model says it worked") }, "digest_mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, obs := fixture()
			tc.mutate(&req, &obs)
			called := false
			e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{{"fixture-only", "1"}: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
					called = true
					return Verdict{}, nil
				}})
			r := e.Evaluate(context.Background(), req)
			if called || r.Conclusion != Inconclusive || r.Disposition == Confirm || len(r.Issues) == 0 || r.Issues[0].Code != tc.code {
				t.Fatalf("unexpected result/called=%v: %+v", called, r)
			}
		})
	}
}

func TestMissingResolverAndEvidence(t *testing.T) {
	req, _ := fixture()
	for _, e := range []*Evaluator{nil, NewEvaluator(nil, nil), NewEvaluator(resolverFunc(func(context.Context, string) (Observation, error) {
		return Observation{}, errors.New("private backend detail")
	}), nil)} {
		r := e.Evaluate(context.Background(), req)
		if r.Disposition != Review || r.Conclusion != Inconclusive || strings.Contains(r.Reason, "private") {
			t.Fatalf("unexpected %+v", r)
		}
	}
}

func TestTrustedVerifierResults(t *testing.T) {
	tests := []struct {
		name    string
		verdict Verdict
		err     error
		want    Disposition
	}{
		{"supported", Verdict{ValidityValid, Supported, []string{"obs-1"}, "fixture proof"}, nil, Confirm},
		{"refuted", Verdict{ValidityValid, Refuted, []string{"obs-1"}, "fixture counterexample"}, nil, NotSupported},
		{"inconclusive", Verdict{ValidityValid, Inconclusive, nil, "more observations needed"}, nil, Review},
		{"invalid experiment", Verdict{ValidityInvalid, Inconclusive, nil, "control failed"}, nil, Retry},
		{"unknown experiment", Verdict{ValidityUnknown, Inconclusive, nil, "control unavailable"}, nil, Review},
		{"false positive on invalid experiment", Verdict{ValidityInvalid, Supported, []string{"obs-1"}, "wrong"}, nil, Review},
		{"false negative on unknown experiment", Verdict{ValidityUnknown, Refuted, []string{"obs-1"}, "wrong"}, nil, Review},
		{"fabricated evidence", Verdict{ValidityValid, Supported, []string{"invented"}, "wrong"}, nil, Review},
		{"no proof reference", Verdict{ValidityValid, Supported, nil, "trust me"}, nil, Review},
		{"duplicate proof", Verdict{ValidityValid, Supported, []string{"obs-1", "obs-1"}, "wrong"}, nil, Review},
		{"no reason", Verdict{ValidityValid, Supported, []string{"obs-1"}, ""}, nil, Review},
		{"oversized reason", Verdict{ValidityValid, Supported, []string{"obs-1"}, strings.Repeat("x", 4097)}, nil, Review},
		{"unknown validity", Verdict{"made-up", Supported, []string{"obs-1"}, "wrong"}, nil, Review},
		{"unknown conclusion", Verdict{ValidityValid, "made-up", []string{"obs-1"}, "wrong"}, nil, Review},
		{"checker error", Verdict{}, errors.New("backend error"), Review},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, obs := fixture()
			e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
				map[Criterion]Verifier{{"fixture-only", "1"}: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) { return tc.verdict, tc.err }})
			r := e.Evaluate(context.Background(), req)
			if r.Disposition != tc.want {
				t.Fatalf("want %s, got %+v", tc.want, r)
			}
			if tc.want == Review && r.Conclusion == Supported {
				t.Fatal("review cannot support claim")
			}
		})
	}
}

func TestRegistryVersionAndSnapshot(t *testing.T) {
	req, obs := fixture()
	registry := map[Criterion]Verifier{{"fixture-only", "1"}: fixtureVerifier}
	e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }), registry)
	delete(registry, Criterion{"fixture-only", "1"})
	if got := e.Evaluate(context.Background(), req); got.Disposition != Confirm {
		t.Fatalf("registry not snapshotted: %+v", got)
	}
	req.CriterionVersion = "2"
	if got := e.Evaluate(context.Background(), req); got.Disposition != Review {
		t.Fatalf("unknown version accepted: %+v", got)
	}
}

func TestVerifierCannotMutateStoredPayload(t *testing.T) {
	req, obs := fixture()
	original := string(obs.Payload)
	called := false
	e := fixtureEvaluator(resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil }),
		map[Criterion]Verifier{{"fixture-only", "1"}: func(_ context.Context, _ EvaluationRequest, observations []Observation) (Verdict, error) {
			called = true
			observations[0].Payload[0] = 'X'
			return Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive, Reason: "test mutation"}, nil
		}})
	e.Evaluate(context.Background(), req)
	if !called || string(obs.Payload) != original {
		t.Fatal("checker mutated backing store")
	}
}

func TestPanicAndCancellationFailClosed(t *testing.T) {
	req, obs := fixture()
	resolver := resolverFunc(func(context.Context, string) (Observation, error) { return obs, nil })
	e := fixtureEvaluator(resolver, map[Criterion]Verifier{{"fixture-only", "1"}: func(context.Context, EvaluationRequest, []Observation) (Verdict, error) { panic("fixture panic") }})
	if got := e.Evaluate(context.Background(), req); got.Disposition != Review || got.Issues[0].Code != "verifier_error" {
		t.Fatalf("panic: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := e.Evaluate(ctx, req); got.Issues[0].Code != "cancelled" {
		t.Fatalf("cancellation: %+v", got)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	e = fixtureEvaluator(resolver, map[Criterion]Verifier{{"fixture-only", "1"}: func(ctx context.Context, r EvaluationRequest, observations []Observation) (Verdict, error) {
		cancel()
		return fixtureVerifier(ctx, r, observations)
	}})
	if got := e.Evaluate(ctx, req); got.Disposition != Review || got.Issues[0].Code != "cancelled" {
		t.Fatalf("late cancellation: %+v", got)
	}
}

func TestEvidenceResourceBounds(t *testing.T) {
	req, obs := fixture()
	req.Evidence = make([]EvidenceRef, MaxEvidenceRefs+1)
	if req.Validate() == nil {
		t.Fatal("oversized evidence list accepted")
	}
	req, obs = fixture()
	obs.Payload = make([]byte, MaxArtifactBytes)
	req.Evidence = nil
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		req.Evidence = append(req.Evidence, EvidenceRef{id, Digest(obs.Payload)})
	}
	e := NewEvaluator(resolverFunc(func(_ context.Context, id string) (Observation, error) { o := obs; o.ID = id; return o, nil }), nil)
	if got := e.Evaluate(context.Background(), req); got.Issues[0].Code != "artifact_too_large" {
		t.Fatalf("total cap: %+v", got)
	}
}

func TestDigestKnownValue(t *testing.T) {
	if Digest([]byte("abc")) != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("incorrect SHA-256")
	}
}
