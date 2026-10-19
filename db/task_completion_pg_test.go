package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Autumn-27/artex/harness"
)

// M1b.3b 共享事务性完成操作回归（真实隔离 PG）：终态 CAS 先到者胜、来源分记、
// done 附图一致性（无 open goal）与当前冻结验收重校验。

func TestCompleteTaskGuardedSemantics(t *testing.T) {
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("completion guarded", "verify M1b.3b", TaskCreateOptions{Name: "m1b3b"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM harness_acceptance WHERE task_id=$1`, task.ID)
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
	})
	s := d.Exploration(task.ExplorationID)

	// 1) 有 open goal → done 被拒（ErrCompletionGoalOpen），状态不变。
	goal, err := s.AddGoalWithRoot(map[string]any{"text": "must be met"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompleteTaskGuarded(task.ID, "done", "model:prove_goal"); !errors.Is(err, ErrCompletionGoalOpen) {
		t.Fatalf("open goal must refuse completion, got %v", err)
	}
	var status string
	_ = d.QueryRow(`SELECT status FROM tasks WHERE id=$1`, task.ID).Scan(&status)
	if status == "done" {
		t.Fatal("refused completion must not write terminal state")
	}

	// 2) 证明目标后 → done 成功，来源分记。
	ev, err := s.AddNode(KindFact, map[string]any{"summary": "proof"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProveGoal(goal, ev); err != nil {
		t.Fatal(err)
	}
	if won, err := d.CompleteTaskGuarded(task.ID, "done", "model:prove_goal"); err != nil || !won {
		t.Fatalf("all-met completion must win: won=%v err=%v", won, err)
	}
	var src string
	_ = d.QueryRow(`SELECT completed_source FROM tasks WHERE id=$1`, task.ID).Scan(&src)
	if src != "model:prove_goal" {
		t.Fatalf("completion source must be recorded separately, got %q", src)
	}

	// 3) 终态 CAS：已 done 后迟到的 timeout 不覆盖、来源保留。
	if won, err := d.CompleteTaskGuarded(task.ID, "timeout", "system:timeout"); err != nil || won {
		t.Fatalf("late terminal must not overwrite: won=%v err=%v", won, err)
	}
	_ = d.QueryRow(`SELECT status, completed_source FROM tasks WHERE id=$1`, task.ID).Scan(&status, &src)
	if status != "done" || src != "model:prove_goal" {
		t.Fatalf("first terminal state and source must be preserved: %q %q", status, src)
	}

	// 4) 验收重校验：新任务当前验收版本引用未 met 的 goal → done 拒绝。
	task2, err := d.CreateTaskWithOptions("completion acceptance", "verify acceptance revalidation", TaskCreateOptions{Name: "m1b3b-2"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM harness_acceptance WHERE task_id=$1`, task2.ID)
		_ = d.DeleteTask(task2.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task2.ExplorationID)
	})
	s2 := d.Exploration(task2.ExplorationID)
	goal2, err := s2.AddGoalWithRoot(map[string]any{"text": "accepted but unmet"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetHarnessAcceptance(harness.AcceptanceSpec{
		TaskID: task2.ID, GoalID: goal2, Version: "v1",
		Criterion:    harness.Criterion{ID: "premise-contains", Version: "1"},
		Claim:        fmt.Sprintf("Accepted goal %d is met.", goal2),
		ExperimentID: "exp-m1b3b", ContextVersion: "ctx-m1b3b",
	}); err != nil {
		t.Fatal(err)
	}
	// 把验收目标置为 abandoned（已了结但未达成）：图中已无 open goal，但当前
	// 冻结验收要求它 met——正是验收重校验独立于 open-goal 检查的场景。
	if err := s2.SetNodeState(goal2, "abandoned"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompleteTaskGuarded(task2.ID, "done", "model:prove_goal"); !errors.Is(err, ErrCompletionAcceptanceUnmet) {
		t.Fatalf("current acceptance referencing an unmet goal must refuse, got %v", err)
	}
}
