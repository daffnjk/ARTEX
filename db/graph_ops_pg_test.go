package db

import (
	"errors"
	"testing"
)

// M1b.2a 图写入原子性回归（真实隔离 PG）：节点+血缘边必须同事务（边失败整体
// 回滚、不留脱链节点）、prove_goal 原子且幂等安全、边界拒绝跨探索血缘。
// 复用 task_plans_pg_test.go 的 researchPlanDSN 隔离守卫与 runner 激活方式。

func openGraphOpsDB(t *testing.T) (*DB, *ExplorationStore, int64) {
	t.Helper()
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatalf("open isolated research PG: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	expID, err := d.CreateExploration("graph ops atomicity", "M1b.2a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	})
	return d, d.Exploration(expID), expID
}

func countNodes(t *testing.T, d *DB, expID int64, kind string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind=$2`, expID, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func hasEdge(t *testing.T, d *DB, expID, src int64, rel string, dst int64) bool {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND src_id=$2 AND rel=$3 AND dst_id=$4`,
		expID, src, rel, dst).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// TestGraphOpsIntentParentsAtomic 意图与 derived_from 边原子：真父节点全部连上；
// 伪造/跨探索父节点 → 显式错误且意图节点不落库（无脱链孤儿）。
func TestGraphOpsIntentParentsAtomic(t *testing.T) {
	d, s, expID := openGraphOpsDB(t)
	other, err := d.CreateExploration("neighbor", "boundary")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, other)
	fact1, err := s.AddNode(KindFact, map[string]any{"summary": "f1"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	fact2, err := s.AddNode(KindFact, map[string]any{"summary": "f2"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddIntentWithParents(map[string]any{"summary": "from two facts"}, 5, nil, "planner", []int64{fact1, fact2})
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(t, d, expID, fact1, RelDerivedFrom, id) || !hasEdge(t, d, expID, fact2, RelDerivedFrom, id) {
		t.Fatal("both parent lineage edges must commit with the intent")
	}
	foreign, err := d.Exploration(other).AddNode(KindFact, map[string]any{"summary": "foreign"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := countNodes(t, d, expID, KindIntent)
	if _, err := s.AddIntentWithParents(map[string]any{"summary": "bad parent"}, 5, nil, "planner", []int64{foreign}); !errors.Is(err, errNodeBoundary) {
		t.Fatalf("cross-exploration parent must be rejected explicitly, got %v", err)
	}
	if after := countNodes(t, d, expID, KindIntent); after != before {
		t.Fatalf("rejected intent must roll back its node: before=%d after=%d", before, after)
	}
	if _, err := s.AddIntentWithParents(map[string]any{"summary": "ghost parent"}, 5, nil, "planner", []int64{999999}); err == nil {
		t.Fatal("nonexistent parent must fail (boundary check), not create an orphan intent")
	}
	if after := countNodes(t, d, expID, KindIntent); after != before {
		t.Fatalf("nonexistent-parent intent must roll back: before=%d after=%d", before, after)
	}
}

// TestGraphOpsFactYieldsAtomic fact 与 yields 边原子；伪造 intent → 回滚无 fact。
func TestGraphOpsFactYieldsAtomic(t *testing.T) {
	d, s, expID := openGraphOpsDB(t)
	intent, err := s.AddIntent(map[string]any{"summary": "probe"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	fid, err := s.AddFactWithYields(map[string]any{"summary": "found"}, nil, "worker", intent)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(t, d, expID, intent, RelYields, fid) {
		t.Fatal("yields edge must commit with the fact")
	}
	before := countNodes(t, d, expID, KindFact)
	if _, err := s.AddFactWithYields(map[string]any{"summary": "orphan"}, nil, "worker", 999999); err == nil {
		t.Fatal("fact with nonexistent intent must fail")
	}
	if after := countNodes(t, d, expID, KindFact); after != before {
		t.Fatalf("rejected fact must roll back: before=%d after=%d", before, after)
	}
}

// TestGraphOpsGoalRootAtomic goal 与 spawns 边原子（挂任务根 origin fact）。
func TestGraphOpsGoalRootAtomic(t *testing.T) {
	d, s, expID := openGraphOpsDB(t)
	origin, err := s.AddNode(KindFact, map[string]any{"summary": "task root"}, 0, StateOrigin, "system", nil)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := s.AddGoalWithRoot(map[string]any{"text": "get admin"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEdge(t, d, expID, origin, RelSpawns, gid) {
		t.Fatal("spawns edge from origin fact must commit with the goal")
	}
}

// TestGraphOpsProveGoalAtomic prove_goal 原子语义：正常证明（边+翻转一次事务）；
// 重复证明幂等；非 goal / 跨探索 goal 显式拒绝；异常态显式 ErrGoalNotOpen。
func TestGraphOpsProveGoalAtomic(t *testing.T) {
	d, s, expID := openGraphOpsDB(t)
	other, err := d.CreateExploration("neighbor", "prove boundary")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, other)
	goal, err := s.AddNode(KindGoal, map[string]any{"text": "g"}, 0, "open", "human", nil)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := s.AddNode(KindFact, map[string]any{"summary": "proof"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProveGoal(goal, ev); err != nil {
		t.Fatalf("first proof must succeed: %v", err)
	}
	if !hasEdge(t, d, expID, ev, RelProves, goal) {
		t.Fatal("proves edge must commit with the met flip")
	}
	var state string
	if err := d.QueryRow(`SELECT state FROM exploration_nodes WHERE id=$1`, goal).Scan(&state); err != nil || state != "met" {
		t.Fatalf("goal must be met after ProveGoal: %q %v", state, err)
	}
	var version int64
	if err := d.QueryRow(`SELECT content_version FROM exploration_nodes WHERE id=$1`, goal).Scan(&version); err != nil {
		t.Fatal(err)
	}
	// 重复证明（同证据）→ 幂等成功，不双跳版本。
	if err := s.ProveGoal(goal, ev); err != nil {
		t.Fatalf("repeat proof must be idempotent: %v", err)
	}
	var version2 int64
	if err := d.QueryRow(`SELECT content_version FROM exploration_nodes WHERE id=$1`, goal).Scan(&version2); err != nil {
		t.Fatal(err)
	}
	if version2 != version {
		t.Fatalf("idempotent repeat proof must not bump content_version again: %d -> %d", version, version2)
	}
	// 非 goal 节点 → 显式拒绝。
	intent, _ := s.AddIntent(map[string]any{"summary": "x"}, 5, nil, "planner")
	if err := s.ProveGoal(intent, ev); err == nil {
		t.Fatal("proving a non-goal must fail explicitly")
	}
	// 跨探索 goal → not found。
	foreignGoal, err := d.Exploration(other).AddNode(KindGoal, map[string]any{"text": "fg"}, 0, "open", "human", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProveGoal(foreignGoal, ev); err == nil {
		t.Fatal("proving another exploration's goal must fail")
	}
	// open 之外的异常态 → ErrGoalNotOpen。
	weird, err := s.AddNode(KindGoal, map[string]any{"text": "w"}, 0, "abandoned", "human", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProveGoal(weird, ev); !errors.Is(err, ErrGoalNotOpen) {
		t.Fatalf("non-open goal must return ErrGoalNotOpen, got %v", err)
	}
}

// TestGraphOpsInheritedParentFromSourceTask 直接来源任务的 fact 可以作为
// derived_from 父节点（审查 P1：与 GetNodeWithSources 工具层继承语义对齐）；
// 无任务关系的探索仍被拒（见 IntentParentsAtomic 的 neighbor 用例）。
func TestGraphOpsInheritedParentFromSourceTask(t *testing.T) {
	dsn := researchPlanDSN(t)
	d, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	owner, err := d.CreateTaskWithOptions("owner task", "inherits lineage", TaskCreateOptions{Name: "m1b2a-owner"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.CreateTaskWithOptions("source task", "provides facts", TaskCreateOptions{Name: "m1b2a-source"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_relations WHERE task_id=$1 OR source_task_id=$1`, owner.ID)
		_, _ = d.Exec(`DELETE FROM task_relations WHERE task_id=$1 OR source_task_id=$1`, source.ID)
		_ = d.DeleteTask(owner.ID)
		_ = d.DeleteTask(source.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id IN ($1,$2)`, owner.ExplorationID, source.ExplorationID)
	})
	if _, err := d.Exec(`INSERT INTO task_relations(task_id, source_task_id) VALUES ($1,$2)`, owner.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	inherited, err := d.Exploration(source.ExplorationID).AddNode(KindFact, map[string]any{"summary": "from source task"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	os := d.Exploration(owner.ExplorationID)
	id, err := os.AddIntentWithParents(map[string]any{"summary": "builds on inherited fact"}, 5, nil, "planner", []int64{inherited})
	if err != nil {
		t.Fatalf("direct-source parent must be accepted (matches tool-layer inheritance): %v", err)
	}
	if !hasEdge(t, d, owner.ExplorationID, inherited, RelDerivedFrom, id) {
		t.Fatal("inherited parent lineage edge must commit")
	}
}

// TestGraphOpsFindingMirrorAtomic setFindingCol：findings 行与节点 payload 镜像
// 同事务提交（正向同步断言；失败回滚路径由事务语义保证）。
func TestGraphOpsFindingMirrorAtomic(t *testing.T) {
	dsn := researchPlanDSN(t)
	d, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("finding mirror", "verify mirror sync", TaskCreateOptions{Name: "m1b2a-mirror"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
	})
	nodeID, err := d.Exploration(task.ExplorationID).AddNode(KindFinding, map[string]any{"summary": "reflected xss", "vulnclass": "XSS", "severity": SeverityHigh}, 0, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddFinding(task.ID, nodeID, "XSS", "reflected xss", SeverityHigh, "sum", "evid", "worker", nil); err != nil {
		t.Fatal(err)
	}
	var fid int64
	if err := d.QueryRow(`SELECT id FROM findings WHERE node_id=$1 ORDER BY id LIMIT 1`, nodeID).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	if n, err := d.SetFindingSeverity(fid, SeverityCritical); err != nil || n != 1 {
		t.Fatalf("SetFindingSeverity: n=%d err=%v", n, err)
	}
	var sev string
	if err := d.QueryRow(`SELECT payload->>'severity' FROM exploration_nodes WHERE id=$1`, nodeID).Scan(&sev); err != nil || sev != SeverityCritical {
		t.Fatalf("node payload mirror must be updated in the same transaction: %q %v", sev, err)
	}
}
