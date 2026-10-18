package db

import (
	"testing"
)

// M1b.2b 图一致性诊断回归（真实隔离 PG）：七类检查各自能查出人工种下的病灶；
// 干净探索零报告；悬挂 tool_use 配对检出。只读——诊断本身不修改任何数据。
func TestGraphIntegrityReportDetectsSeededOrphans(t *testing.T) {
	dsn := researchPlanDSN(t)
	d, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// 病灶探索：全部用"绕过原子原语"的旧式裸 AddNode/AddGoal 种下无边节点。
	orphanExp, err := d.CreateExploration("integrity orphans", "seeded breakage")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, orphanExp)
	os := d.Exploration(orphanExp)
	// 1) fact 无 yields（裸 AddNode，非 origin）。
	bareFact, err := os.AddNode(KindFact, map[string]any{"summary": "bare fact"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 2) intent 无 derived_from（裸 AddIntent）。
	bareIntent, err := os.AddIntent(map[string]any{"summary": "bare intent"}, 5, nil, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	// 3) goal 无 spawns（裸 AddNode kind=goal）。
	bareGoal, err := os.AddNode(KindGoal, map[string]any{"text": "bare goal"}, 0, "open", "human", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 4) met goal 无 proves（裸置 met，不经 ProveGoal）。
	metGoal, err := os.AddNode(KindGoal, map[string]any{"text": "met no proof"}, 0, "open", "human", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.SetNodeState(metGoal, "met"); err != nil {
		t.Fatal(err)
	}
	// 5) finding 节点无 yields。
	bareFinding, err := os.AddNode(KindFinding, map[string]any{"summary": "bare finding"}, 0, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 6) 悬挂 tool_use（无 tool_result 配对）。
	if _, err := d.Exec(`INSERT INTO activity(exploration_id, kind, tool, tool_use_id, summary)
		VALUES ($1,'tool_use','Bash','dangling-1','echo probe')`, orphanExp); err != nil {
		t.Fatal(err)
	}

	findings, counts, err := d.GraphIntegrityReport()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[IntegrityCheck]map[int64]bool{}
	for _, f := range findings {
		if f.ExplorationID != orphanExp {
			continue // 其他探索的历史病灶不属本用例断言范围（报告是全局只读的）
		}
		if byKey[f.Check] == nil {
			byKey[f.Check] = map[int64]bool{}
		}
		id := f.NodeID
		if f.Check == CheckDanglingToolUse {
			id = f.ActivityID
		}
		byKey[f.Check][id] = true
	}
	if !byKey[CheckFactNoYields][bareFact] {
		t.Errorf("bare fact %d not flagged", bareFact)
	}
	if !byKey[CheckIntentNoDerived][bareIntent] {
		t.Errorf("bare intent %d not flagged", bareIntent)
	}
	if !byKey[CheckGoalNoSpawns][bareGoal] {
		t.Errorf("bare goal %d not flagged", bareGoal)
	}
	if !byKey[CheckMetGoalNoProves][metGoal] {
		t.Errorf("met-without-proof goal %d not flagged", metGoal)
	}
	if !byKey[CheckFindingNoYields][bareFinding] {
		t.Errorf("bare finding %d not flagged", bareFinding)
	}
	if len(byKey[CheckDanglingToolUse]) == 0 {
		t.Error("dangling tool_use not flagged")
	}
	if counts[CheckDanglingToolUse] < 1 {
		t.Errorf("dangling count = %d, want >=1", counts[CheckDanglingToolUse])
	}

	// 干净探索：原子原语建图后该探索不应出现在任何检查里。经任务创建（种子
	// origin fact），否则无边是"无根"的合法形态而非病灶。
	task, err := d.CreateTaskWithOptions("integrity clean", "atomic primitives only", TaskCreateOptions{Name: "m1b2b-clean"})
	if err != nil {
		t.Fatal(err)
	}
	cleanExp := task.ExplorationID
	defer func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, cleanExp)
	}()
	cs := d.Exploration(cleanExp)
	intent, err := cs.AddIntentWithParents(map[string]any{"summary": "clean"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	fact, err := cs.AddFactWithYields(map[string]any{"summary": "clean fact"}, nil, "worker", intent)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := cs.AddGoalWithRoot(map[string]any{"text": "clean goal"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.ProveGoal(goal, fact); err != nil {
		t.Fatal(err)
	}
	// 配对的 tool_use/tool_result 不算悬挂。
	if _, err := d.Exec(`INSERT INTO activity(exploration_id, kind, tool, tool_use_id, summary)
		VALUES ($1,'tool_use','Bash','clean-1','echo ok'), ($1,'tool_result','Bash','clean-1','ok')`, cleanExp); err != nil {
		t.Fatal(err)
	}
	findings2, _, err := d.GraphIntegrityReport()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings2 {
		if f.ExplorationID == cleanExp {
			t.Errorf("clean exploration flagged: %+v", f)
		}
	}
}

// TestSoftDeleteRepeatStaysStrictAtDbLayer db 层保持严格：对已删除意图再次
// SoftDeleteIntent 必须显式冲突（幂等吸收发生在控制层，不放松存储语义）。
func TestSoftDeleteRepeatStaysStrictAtDbLayer(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	s := d.Exploration(expID)
	intent, err := s.AddIntent(map[string]any{"summary": "to delete"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SoftDeleteIntent(intent, "test reason"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SoftDeleteIntent(intent, "repeat"); err == nil {
		t.Fatal("db layer must reject repeat soft-delete of a deleted intent (API layer absorbs idempotency)")
	}
}
