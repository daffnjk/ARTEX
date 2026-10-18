package db

import (
	"strings"
	"testing"
)

// M3.2–M4.1 批审修复回归（真实隔离 PG）。

// 审查 P1：区分性证据必须并入胜者 basis——RecordExperimentOutcome 把唯一匹配
// 假设翻成 supported 时，当次实验的 fact 要进 basis_node_ids；否则空 basis 的
// supported 假设在其真正支撑证据被撤回后仍永久 supported（失效监测按 basis 检索）。
func TestSupportedHypothesisInheritsDiscriminatingEvidence(t *testing.T) {
	d, s := m31Store(t)
	if err := s.UpsertHypotheses("which edge?", []Hypothesis{
		{Statement: "edge a is live"}, {Statement: "edge b is live"}, // 注意：空 basis
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	if len(hyps) != 2 {
		t.Fatalf("setup: %d hypotheses", len(hyps))
	}
	expID, err := s.DeclareExperiment(Experiment{
		Question: "which edge?", Action: "probe both edges once",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations:  map[int64]string{hyps[0].ID: "edge a answered rst", hyps[1].ID: "edge b answered rst"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "run edge probe"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	// 区分性证据：worker 落的 fact 节点。
	factID, err := s.AddFactWithYields(map[string]any{"summary": "edge a answered rst"}, nil, "worker", intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordExperimentOutcome(expID, "edge a answered rst", factID); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListHypotheses()
	var winner *Hypothesis
	for i := range after {
		if after[i].ID == hyps[0].ID {
			winner = &after[i]
		}
	}
	if winner == nil || winner.Status != "supported" {
		t.Fatalf("winner must be supported: %+v", winner)
	}
	if !containsID(winner.BasisNodeIDs, factID) {
		t.Fatalf("P1: discriminating fact %d must be unioned into winner basis, got %v", factID, winner.BasisNodeIDs)
	}
	// 撤回该证据 → 胜者必须被降级 invalidated（此前空 basis 会漏网）。
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	demoted, err := InvalidateHypothesesOnNodeTx(tx, factID, "test: discriminating evidence retracted")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if !containsID(demoted, hyps[0].ID) {
		t.Fatalf("retracting the actual supporting evidence must invalidate the winner, demoted=%v", demoted)
	}
	final, _ := s.ListHypotheses()
	for _, h := range final {
		if h.ID == hyps[0].ID && h.Status != "invalidated" {
			t.Fatalf("winner must end invalidated, got %q", h.Status)
		}
	}
}

// 审查 P2：deny 约束在派发路径强制拒绝（不能只靠推荐渲染提示）。
func TestDispatchExperimentEnforcesDenyConstraint(t *testing.T) {
	d, s := m31Store(t)
	_ = d
	if err := s.UpsertHypotheses("sqli?", []Hypothesis{
		{Statement: "injectable via login"}, {Statement: "not injectable"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	// deny 判据是整句互含（M3.3 语义）：操作员用关键词式 deny（如禁用工具名）。
	if _, err := s.AddConstraint("deny", "sqlmap", "operator"); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.DeclareExperiment(Experiment{
		Question: "sqli?", Action: "用 sqlmap 跑 login 参数全量",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations:  map[int64]string{hyps[0].ID: "dump ok", hyps[1].ID: "no injection"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "run"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.DispatchExperiment(blocked, intent)
	if err == nil || !strings.Contains(err.Error(), "deny") {
		t.Fatalf("denied experiment must refuse dispatch with explicit reason, got %v", err)
	}
	// 非冲突实验正常派发（同一意图）。
	ok, err := s.DeclareExperiment(Experiment{
		Question: "sqli?", Action: "手工布尔盲注探针",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations:  map[int64]string{hyps[0].ID: "diff in latency", hyps[1].ID: "no diff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(ok, intent); err != nil {
		t.Fatalf("benign experiment must dispatch, got %v", err)
	}
}

// 批审 P1（M4.2–M4.5）：模型上报的 factID 未过血缘校验前不得并入 basis——
// 跨探索（非直接来源）的 fact 即使真实存在也不并入；判定仍进行。
func TestBasisUnionRejectsForeignFactID(t *testing.T) {
	d, s := m31Store(t)
	if err := s.UpsertHypotheses("foreign fact?", []Hypothesis{
		{Statement: "f1"}, {Statement: "f2"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	expID, err := s.DeclareExperiment(Experiment{
		Question: "foreign fact?", Action: "probe", HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations: map[int64]string{hyps[0].ID: "obs-a", hyps[1].ID: "obs-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "run"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	// 另一个无关探索里的 fact 节点（无 task_relations 血缘）。
	other, err := d.CreateTaskWithOptions("foreign", "no lineage", TaskCreateOptions{Name: "m31foreign"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, other.ID)
		_, _ = d.DeleteTaskCascade(other.ID, false, false)
	})
	os := d.Exploration(other.ExplorationID)
	oIntent, err := os.AddIntentWithParents(map[string]any{"summary": "o"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	foreignFact, err := os.AddFactWithYields(map[string]any{"summary": "foreign obs-a"}, nil, "worker", oIntent)
	if err != nil {
		t.Fatal(err)
	}

	out, err := s.RecordExperimentOutcome(expID, "obs-a", foreignFact)
	if err != nil || out.Status != "observed" || out.Discriminated != hyps[0].ID {
		t.Fatalf("discrimination must proceed, got %+v %v", out, err)
	}
	after, _ := s.ListHypotheses()
	for _, h := range after {
		if h.ID == hyps[0].ID {
			if h.Status != "supported" {
				t.Fatalf("winner must still be supported: %q", h.Status)
			}
			if containsID(h.BasisNodeIDs, foreignFact) {
				t.Fatalf("P1: foreign fact %d must NOT be grafted into basis, got %v", foreignFact, h.BasisNodeIDs)
			}
		}
	}
	if !strings.Contains(out.Reason, "lineage") {
		t.Fatalf("rejection must be visible in outcome reason, got %q", out.Reason)
	}
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// 审查 P2#8：任务级联删除必须处置认知史——(a) experiments/hypothesis_reviews
// 触发器会拦下 FK 级联导致任务删不掉；(b) 他探索里 basis 指向被删探索节点的
// supported 假设必须被失效（证据随源任务消失）。
func TestDeleteTaskCascadeHandlesCognitionHistory(t *testing.T) {
	d, src := m31Store(t)
	srcExp := src.expID
	srcTask := int64(0)
	if err := d.QueryRow(`SELECT id FROM tasks WHERE exploration_id=$1 AND deleted_at IS NULL`, srcExp).Scan(&srcTask); err != nil {
		t.Fatal(err)
	}
	// 源探索：fact 节点 + 认知史行（experiment + review）。
	intent, err := src.AddIntentWithParents(map[string]any{"summary": "src"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	factID, err := src.AddFactWithYields(map[string]any{"summary": "source fact"}, nil, "worker", intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.UpsertHypotheses("src q?", []Hypothesis{{Statement: "s1"}, {Statement: "s2"}}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := src.ListHypotheses()
	if len(hyps) != 2 {
		t.Fatalf("setup hyps: %d", len(hyps))
	}
	expID, err := src.DeclareExperiment(Experiment{
		Question: "src q?", Action: "benign", HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations: map[int64]string{hyps[0].ID: "e1", hyps[1].ID: "e2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	if err := src.StoreReview("test-model", map[string]any{"verdict": "APPROVE"}, map[string]any{"requests": 1}); err != nil {
		t.Fatal(err)
	}

	// 依赖探索：把源任务挂为直接来源，假设 basis 指向源 fact（血缘合法）。
	depTask, err := d.CreateTaskWithOptions("dependent", "verify cascade", TaskCreateOptions{Name: "m31dep"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, depTask.ID)
		_, _ = d.DeleteTaskCascade(depTask.ID, false, false)
	})
	if _, err := d.Exec(`INSERT INTO task_relations(task_id, source_task_id) VALUES ($1,$2)`, depTask.ID, srcTask); err != nil {
		t.Fatal(err)
	}
	dep := d.Exploration(depTask.ExplorationID)
	if err := dep.UpsertHypotheses("depends?", []Hypothesis{
		{Statement: "based on source fact", BasisNodeIDs: []int64{factID}}, {Statement: "rival"},
	}); err != nil {
		t.Fatal(err)
	}
	// 把依赖假设推成 supported：走区分性实验唯一匹配路径。
	depHyps, _ := dep.ListHypotheses()
	var winnerID int64
	for _, h := range depHyps {
		if len(h.BasisNodeIDs) == 1 {
			winnerID = h.ID
		}
	}
	if winnerID == 0 {
		t.Fatal("setup: dependent winner not found")
	}
	depExp, err := dep.DeclareExperiment(Experiment{
		Question: "depends?", Action: "benign dep", HypothesisIDs: []int64{depHyps[0].ID, depHyps[1].ID},
		Expectations: map[int64]string{winnerID: "w", otherID(depHyps, winnerID): "o"},
	})
	if err != nil {
		t.Fatal(err)
	}
	depIntent, err := dep.AddIntentWithParents(map[string]any{"summary": "dep"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := dep.DispatchExperiment(depExp, depIntent); err != nil {
		t.Fatal(err)
	}
	if _, err := dep.RecordExperimentOutcome(depExp, "w", factID); err != nil {
		t.Fatal(err)
	}

	// 级联删除源任务：此前会因 experiments/reviews 触发器整体失败。
	if _, err := d.DeleteTaskCascade(srcTask, false, false); err != nil {
		t.Fatalf("cascade delete must handle cognition history, got %v", err)
	}
	var expGone int64
	_ = d.QueryRow(`SELECT count(*) FROM explorations WHERE id=$1`, srcExp).Scan(&expGone)
	if expGone != 0 {
		t.Fatalf("source exploration must be gone, %d left", expGone)
	}
	// 依赖假设必须被失效（其 basis 证据随源图消失）。
	final, _ := dep.ListHypotheses()
	for _, h := range final {
		if h.ID == winnerID && h.Status != "invalidated" {
			t.Fatalf("dependent supported hypothesis must be invalidated on source deletion, got %q", h.Status)
		}
	}
	// 源探索的认知史行必须清干净。
	var residualExps, residualReviews int64
	_ = d.QueryRow(`SELECT count(*) FROM experiments WHERE exploration_id=$1`, srcExp).Scan(&residualExps)
	_ = d.QueryRow(`SELECT count(*) FROM hypothesis_reviews WHERE exploration_id=$1`, srcExp).Scan(&residualReviews)
	if residualExps != 0 || residualReviews != 0 {
		t.Fatalf("source cognition history must be dropped, exps=%d reviews=%d", residualExps, residualReviews)
	}
}

func otherID(hyps []Hypothesis, not int64) int64 {
	for _, h := range hyps {
		if h.ID != not {
			return h.ID
		}
	}
	return not
}
