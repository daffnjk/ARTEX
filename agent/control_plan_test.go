package agent

import (
	"context"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"testing"
)

func TestControlTodoLegacyPreservesDependencies(t *testing.T) {
	original := db.PlanStep{ID: 7, Content: "keep stable", Status: "pending", ContextVersion: "identity-target-1", RequiresStepIDs: []int64{3}, RequiresClaimIDs: []int64{9}}
	fp := &fakePlanPersister{}
	st := &plannerPlanState{persister: fp, plan: db.TaskPlan{ExplorationID: 42, RowID: 11, Revision: 2, Steps: []db.PlanStep{{ID: 3, Content: "prerequisite", Status: "pending"}, original}}}
	tool := st.todoTool(nil)
	if _, err := tool.Call(t.Context(), todoWriteInput(t, map[string]any{"todos": []map[string]any{{"content": "prerequisite", "status": "completed"}, {"content": "keep stable", "status": "in_progress"}}}), nil); err != nil {
		t.Fatal(err)
	}
	got := st.plan.Steps[1]
	if got.ID != 7 || got.ContextVersion != original.ContextVersion || len(got.RequiresStepIDs) != 1 || got.RequiresStepIDs[0] != 3 || len(got.RequiresClaimIDs) != 1 || got.RequiresClaimIDs[0] != 9 {
		t.Fatal("legacy write dropped dependency", got)
	}
	before := fp.saveCalls
	_, err := tool.Call(t.Context(), todoWriteInput(t, map[string]any{"todos": []map[string]any{{"content": "renamed to bypass dependencies", "status": "completed"}}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fp.saveCalls != before {
		t.Fatal("legacy replacement silently removed protected dependency step")
	}
}

type controlPatchPersister struct {
	fakePlanPersister
	calls int
}

func (p *controlPatchPersister) PatchPlanStep(ctx context.Context, task control.TaskID, row, rev int64, s db.PlanStep) (db.TaskPlan, error) {
	p.calls++
	plan := *p.loaded
	plan.Steps = []db.PlanStep{s}
	plan.Revision++
	p.loaded = &plan
	return plan, nil
}
func TestControlPlanPatchToolCAS(t *testing.T) {
	original := db.TaskPlan{ExplorationID: 44, RowID: 12, Revision: 3, Steps: []db.PlanStep{{ID: 8, Content: "old", Status: "pending"}}}
	p := &controlPatchPersister{fakePlanPersister: fakePlanPersister{loaded: &original}}
	st := &plannerPlanState{persister: p, plan: original}
	tool := st.patchTool(99)
	input := map[string]any{"row_id": 12, "revision": 3, "step": map[string]any{"id": 8, "content": "edited", "status": "pending", "context_version": "initial"}}
	if _, err := tool.Call(t.Context(), todoWriteInput(t, input), nil); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || st.plan.Steps[0].ID != 8 || st.plan.Steps[0].Content != "edited" || st.plan.Revision != 4 {
		t.Fatal("ID patch not persisted", st.plan, p.calls)
	}
	if _, err := tool.Call(t.Context(), todoWriteInput(t, input), nil); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatal("stale model patch reached storage")
	}
}
