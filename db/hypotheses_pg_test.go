package db

import (
	"strings"
	"testing"
)

// M3.1 假设/实验存储回归（真实隔离 PG）：生命周期事件流、预注册不可变、
// 区分性结果判定（唯一匹配/歧义不强拟合）、basis 边界、开放实验预算。

func m31Store(t *testing.T) (*DB, *ExplorationStore) {
	t.Helper()
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	task, err := d.CreateTaskWithOptions("hypotheses", "verify M3.1", TaskCreateOptions{Name: "m31"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_ = d.DeleteTask(task.ID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
	})
	return d, d.Exploration(task.ExplorationID)
}

func TestHypothesisLifecycle(t *testing.T) {
	d, s := m31Store(t)
	_ = d
	if err := s.UpsertHypotheses("why 500s", []Hypothesis{
		{Statement: "WAF blocks the payload"}, {Statement: "app filter rejects it"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, err := s.ListHypotheses()
	if err != nil || len(hyps) != 2 {
		t.Fatalf("want 2 hypotheses, got %d %v", len(hyps), err)
	}
	// 重述集合：替换一条 → 旧 active 变 superseded（事件留痕），已判定的不动。
	keep := hyps[0].Statement
	if err := s.UpsertHypotheses("why 500s", []Hypothesis{
		{Statement: keep}, {Statement: "reverse proxy timeout"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ = s.ListHypotheses()
	var active, superseded int
	for _, h := range hyps {
		switch h.Status {
		case "active":
			active++
		case "superseded":
			superseded++
		}
	}
	if active != 2 || superseded != 1 {
		t.Fatalf("lifecycle wrong: active=%d superseded=%d (%+v)", active, superseded, hyps)
	}
	var events int
	_ = d.QueryRow(`SELECT count(*) FROM hypothesis_events e JOIN hypotheses h ON h.id=e.hypothesis_id
		WHERE h.exploration_id=$1`, hyps[0].ExplorationID).Scan(&events)
	if events != 1 {
		t.Fatalf("supersede must leave an event, got %d", events)
	}
	// basis 边界：跨探索节点拒绝。
	if err := s.UpsertHypotheses("bad basis", []Hypothesis{{Statement: "x", BasisNodeIDs: []int64{999999}}}); err == nil || !strings.Contains(err.Error(), "basis") {
		t.Fatalf("cross-exploration basis must be rejected, got %v", err)
	}
}

func TestExperimentPreRegistrationAndOutcome(t *testing.T) {
	d, s := m31Store(t)
	if err := s.UpsertHypotheses("auth bypass?", []Hypothesis{
		{Statement: "token check is off"}, {Statement: "token check exists but jwt alg none"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	if len(hyps) != 2 {
		t.Fatalf("setup: %d", len(hyps))
	}
	// 期望 ≥2。
	if _, err := s.DeclareExperiment(Experiment{Question: "q", Action: "send alg=none token",
		Expectations: map[int64]string{hyps[0].ID: "200 with admin"}}); err == nil || !strings.Contains(err.Error(), "2 hypotheses") {
		t.Fatalf("single-expectation experiment must be rejected, got %v", err)
	}
	expID, err := s.DeclareExperiment(Experiment{
		Question: "auth bypass?", Action: "send alg=none token",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations: map[int64]string{
			hyps[0].ID: "200 admin panel",
			hyps[1].ID: "401 invalid token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "run auth experiment"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	// 派发后规格不可变。
	if _, err := d.Exec(`UPDATE experiments SET expectations='{"1":"forged"}'::jsonb WHERE id=$1`, expID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("post-dispatch expectation rewrite must be rejected, got %v", err)
	}
	// 唯一匹配 → 区分：h0 supported、h1 refuted。
	out, err := s.RecordExperimentOutcome(expID, "server returned 200 admin panel", 0)
	if err != nil || out.Status != "observed" || out.Discriminated != hyps[0].ID {
		t.Fatalf("discriminating outcome wrong: %+v %v", out, err)
	}
	hyps2, _ := s.ListHypotheses()
	stat := map[int64]string{}
	for _, h := range hyps2 {
		stat[h.ID] = h.Status
	}
	if stat[hyps[0].ID] != "supported" || stat[hyps[1].ID] != "refuted" {
		t.Fatalf("hypothesis transitions wrong: %v", stat)
	}
	// 已判定的不能重复判定。
	if _, err := s.RecordExperimentOutcome(expID, "again", 0); err == nil || !strings.Contains(err.Error(), "dispatched") {
		t.Fatalf("double outcome must be rejected, got %v", err)
	}

	// 歧义 → inconclusive，假设状态不动。
	if err := s.UpsertHypotheses("which cache?", []Hypothesis{
		{Statement: "redis"}, {Statement: "memcached"},
	}); err != nil {
		t.Fatal(err)
	}
	h3, _ := s.ListHypotheses()
	var c1, c2 int64
	for _, h := range h3 {
		if h.Question == "which cache?" {
			if c1 == 0 {
				c1 = h.ID
			} else {
				c2 = h.ID
			}
		}
	}
	exp2, err := s.DeclareExperiment(Experiment{
		Question: "which cache?", Action: "probe info",
		HypothesisIDs: []int64{c1, c2},
		Expectations:  map[int64]string{c1: "redis_version", c2: "memcached stats"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent2, _ := s.AddIntentWithParents(map[string]any{"summary": "cache probe"}, 5, nil, "planner", nil)
	if err := s.DispatchExperiment(exp2, intent2); err != nil {
		t.Fatal(err)
	}
	out2, err := s.RecordExperimentOutcome(exp2, "generic error page", 0)
	if err != nil || out2.Status != "inconclusive" || out2.Discriminated != 0 {
		t.Fatalf("no-match must be inconclusive: %+v %v", out2, err)
	}
	for _, h := range h3 {
		if h.Question == "which cache?" && h.Status != "active" {
			t.Fatalf("inconclusive outcome must not change hypothesis status: %+v", h)
		}
	}
	// 开放实验预算。
	var declared int
	for i := 0; i < MaxExperimentsOpen+2; i++ {
		_, err := s.DeclareExperiment(Experiment{
			Question: "budget", Action: "filler",
			Expectations: map[int64]string{c1: "a", c2: "b"},
		})
		if err == nil {
			declared++
		}
	}
	if declared > MaxExperimentsOpen {
		t.Fatalf("open experiment budget not enforced: %d", declared)
	}
}
