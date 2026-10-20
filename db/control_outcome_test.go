package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/harness"
	"github.com/google/uuid"
)

func outcomeControlFixture(t *testing.T, options ...func(*control.Contract)) (*DB, *Task, int64, int64, harness.EvaluationRequest, map[harness.Criterion]harness.Verifier) {
	return outcomeControlStatusFixture(t, 200, options...)
}
func outcomeControlStatusFixture(t *testing.T, status int, options ...func(*control.Contract)) (*DB, *Task, int64, int64, harness.EvaluationRequest, map[harness.Criterion]harness.Verifier) {
	t.Helper()
	d, task, _, obs := capturedControlStatusFixture(t, status, options...)
	store := d.Exploration(task.ExplorationID)
	goal, err := store.AddGoal(map[string]any{"summary": "captured bytes match"}, "host")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertHypotheses("fixture question", []Hypothesis{{Statement: "captured bytes match"}, {Statement: "independent alternative"}}); err != nil {
		t.Fatal(err)
	}
	hs, err := store.ListHypotheses()
	if err != nil {
		t.Fatal(err)
	}
	spec := harness.AcceptanceSpec{TaskID: task.ID, GoalID: goal, Version: "fixture-v1", Criterion: harness.Criterion{ID: "synthetic.bytes", Version: "1"}, Claim: "captured bytes match", ExperimentID: obs[0].ExperimentID, ContextVersion: "initial"}
	if err = d.SetHarnessAcceptance(spec); err != nil {
		t.Fatal(err)
	}
	req := harness.EvaluationRequest{SchemaVersion: "1", ID: uuid.NewString(), TaskID: task.ID, GoalID: goal, AcceptanceVersion: spec.Version, CriterionID: spec.Criterion.ID, CriterionVersion: "1", Claim: spec.Claim, ExperimentID: spec.ExperimentID, ContextVersion: "initial", Evidence: []harness.EvidenceRef{{ObservationID: obs[0].ID, SHA256: harness.Digest(obs[0].Payload)}}}
	registry := map[harness.Criterion]harness.Verifier{spec.Criterion: func(_ context.Context, _ harness.EvaluationRequest, os []harness.Observation) (harness.Verdict, error) {
		v := harness.Verdict{Validity: harness.ValidityValid, Conclusion: harness.Inconclusive, Reason: "exact synthetic bytes comparison"}
		if len(os) == 1 && string(os[0].Payload) == "synthetic captured bytes" {
			v.Conclusion = harness.Supported
			v.EvidenceIDs = []string{os[0].ID}
		}
		return v, nil
	}}
	var chosen, other int64
	for _, h := range hs {
		if h.Statement == spec.Claim {
			chosen = h.ID
		} else {
			other = h.ID
		}
	}
	return d, task, chosen, other, req, registry
}
func assertOutcomeUnchanged(t *testing.T, d *DB, task *Task, h int64) {
	t.Helper()
	var n int
	var state string
	if err := d.QueryRow(`SELECT count(*) FROM harness_evaluations WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial evaluation", n, err)
	}
	if err := d.QueryRow(`SELECT status FROM hypotheses WHERE id=$1`, h).Scan(&state); err != nil || state != "active" {
		t.Fatal("partial hypothesis", state, err)
	}
}
func TestControlOutcomeEvidenceRequired(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t)
	for _, refs := range [][]harness.EvidenceRef{nil, {{ObservationID: "missing", SHA256: harness.Digest(nil)}}, {{ObservationID: req.Evidence[0].ObservationID, SHA256: harness.Digest(nil)}}} {
		r := req
		r.Evidence = refs
		if _, err := d.CommitValidatedOutcome(t.Context(), r, h, reg); err == nil {
			t.Fatal("invalid evidence accepted")
		}
		assertOutcomeUnchanged(t, d, task, h)
	}
	if err := d.Harness(task.ID).RecordEvaluation(harness.EvaluationReport{RequestID: "forged", TaskID: task.ID, Conclusion: harness.Supported}); err == nil {
		t.Fatal("legacy report writer accepted controlled task")
	}
	if _, err := d.Exploration(task.ExplorationID).RecordExperimentOutcome(1, "matches", 0); err == nil {
		t.Fatal("legacy string outcome accepted controlled task")
	}
}
func TestControlOutcomeAtomicRollback(t *testing.T) {
	for _, table := range []string{"control_claims", "control_claim_dependencies", "control_outbox"} {
		t.Run(table, func(t *testing.T) {
			d, task, h, _, req, reg := outcomeControlFixture(t)
			name := fmt.Sprintf("eng10_fault_%d", task.ID)
			if _, err := d.Exec(`CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic transaction failure'; END $$; CREATE TRIGGER ` + name + ` BEFORE INSERT ON ` + table + ` FOR EACH ROW WHEN (NEW.task_id=` + fmt.Sprint(task.ID) + `) EXECUTE FUNCTION ` + name + `()`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, err := d.Exec(`DROP TRIGGER ` + name + ` ON ` + table + `; DROP FUNCTION ` + name + `() `)
				if err != nil {
					t.Error(err)
				}
			})
			if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err == nil {
				t.Fatal("fault ignored")
			}
			assertOutcomeUnchanged(t, d, task, h)
			var n int
			if err := d.QueryRow(`SELECT count(*) FROM control_claims WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
				t.Fatal("partial claim", n, err)
			}
		})
	}
}
func TestControlNonexclusiveHypotheses(t *testing.T) {
	d, task, h, other, req, reg := outcomeControlFixture(t)
	r, err := d.CommitValidatedOutcome(t.Context(), req, h, reg)
	if err != nil || r.Conclusion != harness.Supported {
		t.Fatal(r, err)
	}
	var state string
	if err = d.QueryRow(`SELECT status FROM hypotheses WHERE id=$1`, other).Scan(&state); err != nil || state != "active" {
		t.Fatal("nonexclusive alternative refuted", state, err)
	}
	again, err := d.CommitValidatedOutcome(t.Context(), req, h, reg)
	if err != nil || again.RequestID != req.ID {
		t.Fatal("retry not idempotent", again, err)
	}
	req.Claim = "conflicting replay"
	if _, err = d.CommitValidatedOutcome(t.Context(), req, h, reg); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	_ = task
}
func TestControlExpiredIdentityInconclusive(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlStatusFixture(t, 401)
	if _, err := d.Exec(`UPDATE tasks SET control_epoch=control_epoch+1 WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	r, err := d.CommitValidatedOutcome(t.Context(), req, h, reg)
	if err != nil || r.Conclusion != harness.Inconclusive || r.Validity == harness.ValidityValid {
		t.Fatal("expired premise produced valid result", r, err)
	}
	var state string
	if err = d.QueryRow(`SELECT status FROM hypotheses WHERE id=$1`, h).Scan(&state); err != nil || state != "active" {
		t.Fatal(state, err)
	}
}
func TestControlInvalidateEvidencePropagates(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t)
	if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err != nil {
		t.Fatal(err)
	}
	if err := d.InvalidateControlEvidence(t.Context(), control.TaskID(task.ID), req.Evidence[0].ObservationID, "host:fixture", "synthetic withdrawal"); err != nil {
		t.Fatal(err)
	}
	var state, validity string
	if err := d.QueryRow(`SELECT h.status,c.validity FROM hypotheses h JOIN control_claims c ON c.hypothesis_id=h.id WHERE h.id=$1`, h).Scan(&state, &validity); err != nil || state != "invalidated" || validity != "invalid" {
		t.Fatal(state, validity, err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM control_outbox WHERE task_id=$1 AND event_type='evidence_invalidated'`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("planner wake absent", n, err)
	}
	if _, err := d.ControlObservationResolver(control.TaskID(task.ID)).Resolve(t.Context(), req.Evidence[0].ObservationID); err == nil {
		t.Fatal("withdrawn evidence remains usable")
	}
}
