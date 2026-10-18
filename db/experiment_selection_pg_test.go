package db

import (
	"strings"
	"testing"
)

// M3.3 可解释实验选择回归（真实隔离 PG）：区分潜力排序、在跑优先、
// 未决问题加权、deny 约束显式排除（不静默）。
func TestExperimentSelection(t *testing.T) {
	d, s := m31Store(t)
	_ = d
	if err := s.UpsertHypotheses("open question?", []Hypothesis{
		{Statement: "alpha"}, {Statement: "beta"}, {Statement: "gamma"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	ids := []int64{}
	for _, h := range hyps {
		ids = append(ids, h.ID)
	}
	exp := map[int64]string{}
	for _, id := range ids {
		exp[id] = "observation-" + strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(string(rune('a'+int(id%10))), ""), ""))
	}
	// 简化：期望与假设一一对应。
	for i, id := range ids {
		exp[id] = strings.Repeat("x", 0) + "expected-" + string(rune('A'+i))
	}
	good, err := s.DeclareExperiment(Experiment{
		Question: "open question?", Action: "benign probe covering three hypotheses",
		HypothesisIDs: ids, Expectations: exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 边界冲突实验：deny 约束与 action 互含。
	if _, err := s.AddConstraint("deny", "绝不使用 sqlmap 自动注入工具", "operator"); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.DeclareExperiment(Experiment{
		Question: "open question?", Action: "绝不使用 sqlmap 自动注入工具跑全量",
		HypothesisIDs: ids[:2],
		Expectations:  map[int64]string{ids[0]: "p1", ids[1]: "p2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cands, err := s.RecommendExperiments()
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(cands))
	}
	var goodC, blockedC *ExperimentCandidate
	for i := range cands {
		if cands[i].ID == good {
			goodC = &cands[i]
		}
		if cands[i].ID == blocked {
			blockedC = &cands[i]
		}
	}
	if goodC == nil || blockedC == nil {
		t.Fatal("candidates missing")
	}
	if blockedC.Excluded == false || !strings.Contains(blockedC.ExclusionReason, "deny") {
		t.Fatalf("deny-constraint collision must be explicitly excluded: %+v", blockedC)
	}
	if goodC.Excluded {
		t.Fatalf("benign experiment must not be excluded: %+v", goodC)
	}
	if goodC.ActiveHypotheses != 3 || !goodC.QuestionOpen {
		t.Fatalf("discriminating potential wrong: %+v", goodC)
	}
	if len(goodC.Reasons) < 2 || !strings.Contains(strings.Join(goodC.Reasons, ";"), "活跃假设") {
		t.Fatalf("reasons must be explicit and informative: %v", goodC.Reasons)
	}
	// 未排除者排在被排除者之前。
	if cands[0].Excluded {
		t.Fatal("excluded candidate must sort after non-excluded")
	}
	// 在跑优先：派发 good 后再声明一个同潜力实验，dispatched 应排首。
	intent, _ := s.AddIntentWithParents(map[string]any{"summary": "run"}, 5, nil, "planner", nil)
	if err := s.DispatchExperiment(good, intent); err != nil {
		t.Fatal(err)
	}
	late, err := s.DeclareExperiment(Experiment{
		Question: "open question?", Action: "another benign probe",
		HypothesisIDs: ids,
		Expectations:  map[int64]string{ids[0]: "q1", ids[1]: "q2", ids[2]: "q3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cands2, _ := s.RecommendExperiments()
	if cands2[0].ID != good || cands2[0].Status != "dispatched" {
		var order []int64
		for _, c := range cands2 {
			order = append(order, c.ID)
		}
		t.Fatalf("dispatched experiment must rank first: order=%v (late=%d good=%d)", order, late, good)
	}
}
