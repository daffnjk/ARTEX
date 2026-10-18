package db

import (
	"strings"
	"testing"
)

// M1b.1 纯逻辑单元测试：步骤 ID 合并（稳定、顺序无关、新内容新 ID）与输入校验
// （状态/内容/basis 边界）。不依赖数据库；真实 PostgreSQL CAS 矩阵见
// task_plans_pg_test.go。

func TestMergePlanStepsKeepsIDAcrossReorder(t *testing.T) {
	existing := []PlanStep{
		{ID: 1, Content: "Reconnaissance example.com", Status: planStatusPending},
		{ID: 2, Content: "Fingerprinting the admin backend", Status: planStatusPending},
		{ID: 3, Content: "Verify SQL Injection", Status: planStatusPending},
	}
	// The model submits a whole-list replace with the same content in a different order.
	incoming := []PlanStep{
		{Content: "Fingerprinting the admin backend", Status: planStatusInProgress},
		{Content: "Reconnaissance example.com", Status: planStatusCompleted},
	}
	merged := MergePlanSteps(existing, incoming)
	if len(merged) != 2 {
		t.Fatalf("want 2 steps, got %d", len(merged))
	}
	byContent := map[string]int64{}
	for _, s := range merged {
		byContent[s.Content] = s.ID
	}
	if byContent["Fingerprinting the admin backend"] != 2 {
		t.Errorf("reordered step lost its stable id: %+v", merged)
	}
	if byContent["Reconnaissance example.com"] != 1 {
		t.Errorf("completed step lost its stable id: %+v", merged)
	}
	if merged[0].Status != planStatusInProgress || merged[1].Status != planStatusCompleted {
		t.Errorf("statuses not carried with the merge: %+v", merged)
	}
}

func TestMergePlanStepsAssignsNewIDsAndDropsRemoved(t *testing.T) {
	existing := []PlanStep{
		{ID: 1, Content: "a", Status: planStatusPending},
		{ID: 2, Content: "b", Status: planStatusPending},
	}
	merged := MergePlanSteps(existing, []PlanStep{
		{Content: "a", Status: planStatusCompleted},
		{Content: "c", Status: planStatusPending}, // a brand-new step
	})
	if len(merged) != 2 {
		t.Fatalf("want 2 steps, got %d", len(merged))
	}
	if merged[0].ID != 1 {
		t.Errorf("existing step id must stay 1, got %d", merged[0].ID)
	}
	if merged[1].ID != 3 {
		t.Errorf("new step must get id 3 (max+1), got %d", merged[1].ID)
	}
	// A later replace that re-adds "b" treats it as a new step (no resurrection memory).
	merged2 := MergePlanSteps(merged, []PlanStep{{Content: "b", Status: planStatusPending}})
	if merged2[0].ID != 4 {
		t.Errorf("removed-then-readded step must get a fresh id, got %d", merged2[0].ID)
	}
}

func TestMergePlanStepsDuplicateContentGetsDistinctIDs(t *testing.T) {
	merged := MergePlanSteps(nil, []PlanStep{
		{Content: "same", Status: planStatusPending},
		{Content: "same", Status: planStatusPending},
	})
	if merged[0].ID == merged[1].ID {
		t.Fatalf("duplicate content must not share one id: %+v", merged)
	}
}

func TestValidatePlanStepsBounds(t *testing.T) {
	cases := []struct {
		name  string
		steps []PlanStep
		err   string
	}{
		{"ok", []PlanStep{{Content: "x", Status: planStatusPending}}, ""},
		{"bad status", []PlanStep{{Content: "x", Status: "done"}}, "invalid status"},
		{"empty content", []PlanStep{{Content: "", Status: planStatusPending}}, "empty content"},
		{"content too large", []PlanStep{{Content: strings.Repeat("x", PlanMaxContentBytes+1), Status: planStatusPending}}, "content exceeds"},
		{"expect too large", []PlanStep{{Content: "x", Status: planStatusPending, ExpectedOutput: strings.Repeat("y", PlanMaxExpectBytes+1)}}, "expected_output exceeds"},
		{"too many basis", []PlanStep{{Content: "x", Status: planStatusPending, BasisNodeIDs: make([]int64, PlanMaxBasisPerStep+1)}}, "basis node ids"},
	}
	for _, c := range cases {
		err := ValidatePlanSteps(c.steps)
		if c.err == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.err, err)
		}
	}
	tooMany := make([]PlanStep, PlanMaxSteps+1)
	for i := range tooMany {
		tooMany[i] = PlanStep{Content: "s", Status: planStatusPending}
	}
	if err := ValidatePlanSteps(tooMany); err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Errorf("step count cap not enforced: %v", err)
	}
	if err := ValidatePlanNotes(strings.Repeat("n", PlanMaxNotesBytes+1)); err == nil {
		t.Error("notes cap not enforced")
	}
}
