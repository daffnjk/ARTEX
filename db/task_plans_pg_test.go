package db

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// M1b.1 专用 PostgreSQL 集成套件：在真实隔离库上验证 CAS/恢复/删除/归档/错误
// 语义。与 db 包旧有的"无 PG 即跳过"测试不同，本套件经 scripts/test-research-pg.sh
// 专用 runner 激活（ARTEX_TEST_PG_DSN 必填，runner 与这里的守卫都拒绝非隔离
// 地址/库名）；未设置该变量时给出显式跳过说明——不是静默通过。

// researchPlanDSN 读取专用套件的隔离 DSN 并做最小隔离守卫：主机必须是本机，
// 库名必须以 artex_research_test 开头。不满足即 Fatal（拒绝运行，而非跳过）。
func researchPlanDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ARTEX_TEST_PG_DSN"))
	if dsn == "" {
		t.Skipf("ARTEX_TEST_PG_DSN not set — task-plan PG matrix runs only via scripts/test-research-pg.sh (never silently)")
	}
	// 拒绝带查询参数的 DSN：pgx 会在 URL authority 之后应用 ?host=/?database=
	// 覆盖项（pgconn/config.go 的查询参数循环），只看 authority 的守卫可被绕过。
	if strings.Contains(dsn, "?") {
		t.Fatalf("ARTEX_TEST_PG_DSN must not carry query parameters (pgx ?host=/?database= overrides bypass the isolation guard)")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("ARTEX_TEST_PG_DSN is not a valid URL: %v", err)
	}
	host := u.Hostname()
	if host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		t.Fatalf("refusing non-local test DSN host %q", host)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasPrefix(name, "artex_research_test") {
		t.Fatalf("refusing non-isolated test database %q (must be artex_research_test*)", name)
	}
	return dsn
}

// openResearchPlanDB 打开隔离库并建一个专属探索行，返回句柄与探索 id；t.Cleanup
// 负责删除（级联清掉计划）与关闭。
func openResearchPlanDB(t *testing.T) (*DB, int64) {
	t.Helper()
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatalf("open isolated research PG: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	expID, err := d.CreateExploration("research plan test", "verify M1b.1 persistence semantics")
	if err != nil {
		t.Fatalf("create exploration: %v", err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, expID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	})
	return d, expID
}

// TestTaskPlanPGMigrationIdempotent 迁移幂等：完整 Open（应用 schema + seed）
// 连续两次必须成功且表可用（验收矩阵"迁移执行两次=幂等且不损伤旧数据"）。
func TestTaskPlanPGMigrationIdempotent(t *testing.T) {
	dsn := researchPlanDSN(t)
	d1, err := Open(dsn)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// 在两次迁移之间落一条数据，验证第二次 Open 不损伤旧数据。
	expID, err := d1.CreateExploration("migration idempotence", "plan survives re-open")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := d1.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "keep me", Status: planStatusPending}}})
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()
	d2, err := Open(dsn)
	if err != nil {
		t.Fatalf("second open (idempotent migration): %v", err)
	}
	defer d2.Close()
	loaded, err := d2.LoadTaskPlan(expID)
	if err != nil || loaded == nil {
		t.Fatalf("plan lost after re-migration: %v, %+v", err, loaded)
	}
	if loaded.Revision != saved.Revision || len(loaded.Steps) != 1 || loaded.Steps[0].Content != "keep me" {
		t.Fatalf("plan mutated by re-migration: %+v", loaded)
	}
	_, _ = d2.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, expID)
	_, _ = d2.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
}

// TestTaskPlanPGRoundTrip 常规路径：无记录=显式空计划；保存→递增 revision→回读一致。
func TestTaskPlanPGRoundTrip(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	if p, err := d.LoadTaskPlan(expID); err != nil || p != nil {
		t.Fatalf("absent plan must be (nil,nil), got %+v, %v", p, err)
	}
	saved, err := d.SaveTaskPlan(TaskPlan{
		ExplorationID: expID,
		Steps: []PlanStep{
			{Content: "step one", Status: planStatusCompleted, BasisNodeIDs: []int64{7}},
			{Content: "step two", Status: planStatusInProgress, ExpectedOutput: "admin session cookie"},
		},
		ModelNotes: "chain via upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 {
		t.Fatalf("first save revision = %d, want 1", saved.Revision)
	}
	if saved.RowID == 0 {
		t.Fatal("first save must return a nonzero row id (storage row identity)")
	}
	loaded, err := d.LoadTaskPlan(expID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 1 || loaded.RowID != saved.RowID || loaded.ModelNotes != "chain via upload" || len(loaded.Steps) != 2 {
		t.Fatalf("roundtrip mismatch: %+v", loaded)
	}
	if loaded.Steps[0].Content != "step one" || loaded.Steps[0].Status != planStatusCompleted || len(loaded.Steps[0].BasisNodeIDs) != 1 || loaded.Steps[0].BasisNodeIDs[0] != 7 {
		t.Fatalf("basis not roundtripped: %+v", loaded.Steps[0])
	}
	if loaded.Steps[1].ExpectedOutput != "admin session cookie" {
		t.Fatalf("expected_output not roundtripped: %+v", loaded.Steps[1])
	}
	again, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Revision: 1, RowID: loaded.RowID, Steps: []PlanStep{{Content: "step one", Status: planStatusCompleted}}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != 2 || again.RowID != loaded.RowID {
		t.Fatalf("second save revision = %d rowid = %d, want 2/%d", again.Revision, again.RowID, loaded.RowID)
	}
}

// TestTaskPlanPGConcurrentSameRevisionUpdate 同 revision 真并发双写：两个写入方
// 持相同期望 (N,R)，恰一方成功推进 revision，另一方显式冲突。
func TestTaskPlanPGConcurrentSameRevisionUpdate(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	first, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "seed", Status: planStatusPending}}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var wins, conflicts int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.SaveTaskPlan(TaskPlan{
				ExplorationID: expID, Revision: first.Revision, RowID: first.RowID,
				Steps: []PlanStep{{Content: fmt.Sprintf("racer %d", i), Status: planStatusInProgress}},
			})
			var pc *PlanConflictError
			switch {
			case err == nil:
				atomic.AddInt64(&wins, 1)
			case errors.As(err, &pc):
				atomic.AddInt64(&conflicts, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != 1 {
		t.Fatalf("same-revision race: wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	got, err := d.LoadTaskPlan(expID)
	if err != nil || got.Revision != 2 {
		t.Fatalf("winner must advance to revision 2 exactly once: %+v %v", got, err)
	}
}

// TestTaskPlanPGConcurrentFirstWrite 并发首写：两个写入方都期望 revision 0，
// 恰好一方成功，另一方得到 *PlanConflictError。
func TestTaskPlanPGConcurrentFirstWrite(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	var wg sync.WaitGroup
	var wins, conflicts int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{
				Content: fmt.Sprintf("writer %d claim", i), Status: planStatusPending}}})
			var pc *PlanConflictError
			switch {
			case err == nil:
				atomic.AddInt64(&wins, 1)
			case errors.As(err, &pc):
				atomic.AddInt64(&conflicts, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != 1 {
		t.Fatalf("first-write race: wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
}

// TestTaskPlanPGStaleWriteRejected 迟到写入拒绝（非 last-write-wins）：A、B 同读
// revision 1，B 先写成功（rev 2），A 再写必须冲突且数据保持 B 的版本。
func TestTaskPlanPGStaleWriteRejected(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "base", Status: planStatusPending}}}); err != nil {
		t.Fatal(err)
	}
	a, err := d.LoadTaskPlan(expID) // A 读到 rev1
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.LoadTaskPlan(expID) // B 也读到 rev1
	if err != nil {
		t.Fatal(err)
	}
	b.Steps = MergePlanSteps(b.Steps, []PlanStep{{Content: "B newer state", Status: planStatusInProgress}})
	if _, err := d.SaveTaskPlan(*b); err != nil { // B 先落盘 → rev2
		t.Fatal(err)
	}
	a.Steps = MergePlanSteps(a.Steps, []PlanStep{{Content: "A stale state", Status: planStatusPending}})
	_, err = d.SaveTaskPlan(*a) // A 迟到：必须显式冲突
	var pc *PlanConflictError
	if !errors.As(err, &pc) || pc.HaveRevision != 2 {
		t.Fatalf("stale write must conflict at revision 2, got %v", err)
	}
	loaded, err := d.LoadTaskPlan(expID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 2 || len(loaded.Steps) != 1 || loaded.Steps[0].Content != "B newer state" {
		t.Fatalf("late write must NOT overwrite (no last-write-wins): %+v", loaded)
	}
}

// TestTaskPlanPGStaleWriteAfterRowDeleted 行消失后的迟到写入（审查 P1#1）：
// hydrate rev N → 行被删除（如归档清除）→ 持旧期望（N>0, 旧行 id）的写入必须
// 得到显式冲突（HaveRevision 0），绝不被 INSERT 分支复活。
func TestTaskPlanPGStaleWriteAfterRowDeleted(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	first, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "v1", Status: planStatusPending}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, expID); err != nil {
		t.Fatal(err)
	}
	// 迟到写入方仍持有 (rev1, rowID)。
	stale := TaskPlan{ExplorationID: expID, Revision: first.Revision, RowID: first.RowID,
		Steps: []PlanStep{{Content: "resurrected stale plan", Status: planStatusPending}}}
	_, err = d.SaveTaskPlan(stale)
	var pc *PlanConflictError
	if !errors.As(err, &pc) || pc.HaveRevision != 0 {
		t.Fatalf("write against a deleted plan row must conflict with HaveRevision 0, got %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM task_plans WHERE exploration_id=$1`, expID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale write must not resurrect a deleted plan, found %d rows", n)
	}
}

// TestTaskPlanPGDeleteRecreateRejectsABA 删除重建后的 ABA（审查 P1#2）：
// 旧行 (rev1, R1) 删除 → 新计划重建 (rev1, R2) → 持 (1, R1) 的旧写入即使
// revision 撞值也必须因行身份不匹配被拒绝；新计划不被旧控制器覆盖。
func TestTaskPlanPGDeleteRecreateRejectsABA(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	old, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "old generation", Status: planStatusPending}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, expID); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "new generation", Status: planStatusInProgress}}})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Revision != 1 || fresh.RowID == old.RowID {
		t.Fatalf("recreated plan must be a new row (rev from 1, distinct id): old=%+v new=%+v", old, fresh)
	}
	stale := TaskPlan{ExplorationID: expID, Revision: old.Revision, RowID: old.RowID,
		Steps: []PlanStep{{Content: "stale ABA overwrite", Status: planStatusPending}}}
	_, err = d.SaveTaskPlan(stale)
	var pc *PlanConflictError
	if !errors.As(err, &pc) || pc.HaveRevision != 1 {
		t.Fatalf("stale write with old row identity must conflict even when revision collides, got %v", err)
	}
	got, err := d.LoadTaskPlan(expID)
	if err != nil || got == nil || got.RowID != fresh.RowID || got.Steps[0].Content != "new generation" {
		t.Fatalf("new generation plan must survive untouched: %+v %v", got, err)
	}
}

// TestTaskPlanPGIsolationAndBasisOwnership 任务边界：计划互不串扰；basis 节点
// 必须属于本探索（跨任务引用被识别为 missing）。
func TestTaskPlanPGIsolationAndBasisOwnership(t *testing.T) {
	d, exp1 := openResearchPlanDB(t)
	exp2, err := d.CreateExploration("neighbor", "isolation")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, exp2)
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: exp1, Steps: []PlanStep{{Content: "exp1 step", Status: planStatusPending}}}); err != nil {
		t.Fatal(err)
	}
	p2, err := d.LoadTaskPlan(exp2)
	if err != nil || p2 != nil {
		t.Fatalf("neighbor exploration must see an empty plan, got %+v %v", p2, err)
	}
	// exp2 建一个 fact 节点；用它作为 exp1 的 basis 必须被拒绝为 missing。
	s2 := d.Exploration(exp2)
	foreign, err := s2.AddNode(KindFact, map[string]any{"summary": "foreign fact"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	s1 := d.Exploration(exp1)
	own, err := s1.AddNode(KindFact, map[string]any{"summary": "own fact"}, 1, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := d.PlanBasisNodesOwned(exp1, []int64{own, foreign})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != foreign {
		t.Fatalf("cross-task basis node must be missing, got %v", missing)
	}
	if missing, err = d.PlanBasisNodesOwned(exp1, []int64{own}); err != nil || len(missing) != 0 {
		t.Fatalf("own basis node must resolve, got %v %v", missing, err)
	}
}

// TestTaskPlanPGDeleteCascases 硬删除探索级联清除计划，无孤儿行。
func TestTaskPlanPGDeleteCascases(t *testing.T) {
	d, expID := openResearchPlanDB(t)
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "x", Status: planStatusPending}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM explorations WHERE id=$1`, expID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM task_plans WHERE exploration_id=$1`, expID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("orphan plan rows after exploration delete: %d", n)
	}
}

// TestTaskPlanPGErrorSurfaces 数据库错误必须上抛：句柄关闭后 Load/Save 不得
// 报告成功（"DB 读取/提交错误=不报告成功不静默丢计划"）。
func TestTaskPlanPGErrorSurfaces(t *testing.T) {
	dsn := researchPlanDSN(t)
	d, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	expID, err := d.CreateExploration("error surfacing", "closed handle")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "x", Status: planStatusPending}}}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := d.LoadTaskPlan(expID); err == nil {
		t.Fatal("LoadTaskPlan on closed handle must fail, not silently return empty")
	}
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: expID, Steps: []PlanStep{{Content: "y", Status: planStatusPending}}}); err == nil {
		t.Fatal("SaveTaskPlan on closed handle must fail, not report success")
	}
	// 清理：重开专用句柄删除探索。
	d2, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	_, _ = d2.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
}

// TestTaskPlanPGArchivePurgeRemovesPlan 归档清除：CompleteTaskArchive 的清除
// 枚举必须移除 task_plans（计划是可再生草稿，不进归档包；恢复后为显式空计划）。
func TestTaskPlanPGArchivePurgeRemovesPlan(t *testing.T) {
	dsn := researchPlanDSN(t)
	d, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := d.EnsureLLMRecordsTable(); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateTaskWithOptions("plan archive purge", "verify plan purged with archive", TaskCreateOptions{Name: "m1b1-purge"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, task.ExplorationID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
	})
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: []PlanStep{{
		Content: "chain step that dies with the archive", Status: planStatusInProgress}}}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPaused(task.ID, true); err != nil {
		t.Fatal(err)
	}
	archive, err := d.QueueTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := d.ClaimTaskArchiveJob(t.Context())
	if err != nil || claimed == nil || claimed.ID != archive.ID {
		t.Fatalf("claim archive: %+v %v", claimed, err)
	}
	snapshot, err := d.SnapshotTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CompleteTaskArchive(archive.ID, snapshot, "/tmp/research-plan-purge.tar.zst", "deadbeef", 10, 5); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM task_plans WHERE exploration_id=$1`, task.ExplorationID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("archive purge must remove task_plans, found %d rows", n)
	}
	// 归档后的任务 hydrate 出显式空计划（无记录=不臆造）。
	if p, err := d.LoadTaskPlan(task.ExplorationID); err != nil || p != nil {
		t.Fatalf("post-archive plan must be explicit empty, got %+v %v", p, err)
	}
}
