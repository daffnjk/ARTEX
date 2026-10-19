package server

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/harness"
)

// M2.3 只读检查面（真实隔离 PG）：装配函数聚合完成事实（含来源分记）、任务
// 范围图一致性、评估存档与验收历史。未设 ARTEX_TEST_PG_DSN 时显式跳过。
func TestBuildResearchInspection(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("ARTEX_TEST_PG_DSN not set — inspection assembly runs via scripts/test-research-pg.sh")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("inspection", "verify M2.3 read-only surface", db.TaskCreateOptions{Name: "m23-inspect"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM harness_acceptance WHERE task_id=$1`, task.ID)
		_, _ = d.Exec(`DELETE FROM harness_evaluations WHERE task_id=$1`, task.ID)
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
	})
	s := d.Exploration(task.ExplorationID)
	goal, err := s.AddGoalWithRoot(map[string]any{"text": "inspect me"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetHarnessAcceptance(harness.AcceptanceSpec{
		TaskID: task.ID, GoalID: goal, Version: "v1",
		Criterion:    harness.Criterion{ID: "premise-contains", Version: "1"},
		Claim:        fmt.Sprintf("Goal %d met with marker X.", goal),
		ExperimentID: "exp-m23", ContextVersion: "ctx-m23",
	}); err != nil {
		t.Fatal(err)
	}
	// 完成任务（先证明再守卫完成），产生评估存档行。证明事实走原子链
	//（intent + yields 边），避免夹具自身制造一致性病灶。
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "prove the goal"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	evFact, err := s.AddFactWithYields(map[string]any{"summary": "proof"}, nil, "worker", intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProveGoal(goal, evFact); err != nil {
		t.Fatal(err)
	}
	if won, err := d.CompleteTaskGuarded(task.ID, "done", "model:prove_goal"); err != nil || !won {
		t.Fatalf("complete: won=%v err=%v", won, err)
	}
	if err := d.Harness(task.ID).RecordEvaluation(harness.EvaluationReport{
		RequestID: fmt.Sprintf("m23-eval-1-%d", task.ID), TaskID: task.ID, GoalID: goal,
		Validity: harness.ValidityValid, Conclusion: harness.Supported, Disposition: harness.Confirm,
	}); err != nil {
		t.Fatal(err)
	}

	out, code, err := buildResearchInspection(d, fmt.Sprint(task.ID))
	if err != nil || code != 200 {
		t.Fatalf("inspection: code=%d err=%v", code, err)
	}
	if out.Task.Status != "done" || out.Task.CompletedSource != "model:prove_goal" {
		t.Fatalf("completion facts wrong: %+v", out.Task)
	}
	if out.Task.CompletedAt == "" {
		t.Fatal("completed_at missing")
	}
	if out.Integrity == nil || len(out.Integrity.Findings) != 0 {
		t.Fatalf("clean task must have zero integrity findings: %+v", out.Integrity)
	}
	if out.Integrity.Counts[db.CheckMetGoalNoProves] != 0 {
		t.Fatalf("proved goal flagged: %+v", out.Integrity.Counts)
	}
	if len(out.Evaluations) != 1 || out.Evaluations[0].Disposition != "confirm" || out.Evaluations[0].CreatedAt == "" {
		t.Fatalf("evaluation records wrong: %+v", out.Evaluations)
	}
	if len(out.Acceptance) != 1 || !out.Acceptance[0].Current || out.Acceptance[0].Version != "v1" {
		t.Fatalf("acceptance history wrong: %+v", out.Acceptance)
	}
	// 无效/不存在任务 → 404。
	if _, code, err := buildResearchInspection(d, "999999999"); err == nil || code != 404 {
		t.Fatalf("missing task must 404: code=%d err=%v", code, err)
	}
}
