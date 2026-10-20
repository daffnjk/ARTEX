package db

import (
	"github.com/Autumn-27/artex/control"
	"testing"
)

func TestControlPlanPatchKeepsID(t *testing.T) {
	d, task, _ := admissionFixture(t)
	plan, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: []PlanStep{{ID: 1, Content: "old text", Status: "pending", ContextVersion: "initial"}}})
	if err != nil {
		t.Fatal(err)
	}
	step := plan.Steps[0]
	step.Content = "edited text"
	saved, err := d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, plan.Revision, step)
	if err != nil || saved.Steps[0].ID != step.ID || saved.RowID != plan.RowID || saved.Revision != plan.Revision+1 {
		t.Fatal("patch lost stable identity", saved, err)
	}
	if _, err = d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, plan.Revision, step); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err = d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, task.ExplorationID); err != nil {
		t.Fatal(err)
	}
	replacement, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: []PlanStep{step}})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.RowID == plan.RowID {
		t.Fatal("fixture row id unexpectedly equal")
	}
	if _, err = d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, replacement.Revision, step); err == nil {
		t.Fatal("ABA row replacement accepted")
	}
}
func TestControlPlanCycleRejected(t *testing.T) {
	d, task, _ := admissionFixture(t)
	for _, steps := range [][]PlanStep{
		{{ID: 1, Content: "self", Status: "pending", ContextVersion: "initial", RequiresStepIDs: []int64{1}}},
		{{ID: 1, Content: "one", Status: "pending", ContextVersion: "initial", RequiresStepIDs: []int64{2}}, {ID: 2, Content: "two", Status: "pending", ContextVersion: "initial", RequiresStepIDs: []int64{1}}},
		{{ID: 1, Content: "missing", Status: "pending", ContextVersion: "initial", RequiresStepIDs: []int64{999}}},
	} {
		if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: steps}); err == nil {
			t.Fatal("invalid DAG accepted")
		}
	}
}
func TestControlDependencyContextRequired(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t)
	if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err != nil {
		t.Fatal(err)
	}
	var claim int64
	if err := d.QueryRow(`SELECT id FROM control_claims WHERE request_id=$1`, req.ID).Scan(&claim); err != nil {
		t.Fatal(err)
	}
	plan, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: []PlanStep{{ID: 1, Content: "first", Status: "completed", ContextVersion: "initial"}, {ID: 2, Content: "dependent", Status: "pending", ContextVersion: "initial", RequiresClaimIDs: []int64{claim}}}})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(2)
	identity := "anonymous"
	p := control.ActionProposal{SchemaVersion: 1, MethodID: "http.get", MethodVersion: "1", Target: control.Target{Scheme: "http", Host: "localhost", Port: 8080, Path: "/next"}, IdentityRef: &identity, Purpose: "dependency fixture", ContextVersion: "initial", IdempotencyKey: "dependent", StepID: &id}
	r, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal("valid dependency refused", err)
	}
	// Planner's completed note never substitutes for a successful captured action.
	step := plan.Steps[1]
	step.RequiresStepIDs = []int64{1}
	plan, err = d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, plan.Revision, step)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CheckExecutionGrant(t.Context(), control.TaskID(task.ID), r.ID); err == nil {
		t.Fatal("live premise change did not block existing grant")
	}
	step.RequiresStepIDs = nil
	step.ContextVersion = "foreign-identity-target-version"
	plan, err = d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, plan.Revision, step)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CheckExecutionGrant(t.Context(), control.TaskID(task.ID), r.ID); err == nil {
		t.Fatal("stale identity/context allowed grant")
	}
	step.ContextVersion = "initial"
	plan, err = d.PatchPlanStep(t.Context(), control.TaskID(task.ID), plan.RowID, plan.Revision, step)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.InvalidateControlEvidence(t.Context(), control.TaskID(task.ID), req.Evidence[0].ObservationID, "host:fixture", "withdrawn prerequisite"); err != nil {
		t.Fatal(err)
	}
	if err = d.CheckExecutionGrant(t.Context(), control.TaskID(task.ID), r.ID); err == nil {
		t.Fatal("withdrawn claim still dispatchable")
	}
}
