package db

import (
	"strings"
	"testing"

	"github.com/Autumn-27/artex/harness"
)

// M4.1 提案挖掘回归（真实隔离 PG）：失效轨迹与不区分实验各自生成结构化
// 提案；提案校验通过（RequiresHumanApproval 恒真）；干净轨迹零提案。
func TestMineChangeProposals(t *testing.T) {
	d, s := m31Store(t)
	_ = d
	// 干净轨迹：无提案。
	ps, err := s.MineChangeProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Fatalf("clean trajectory must yield no proposals: %+v", ps)
	}
	// 构造不区分实验。
	if err := s.UpsertHypotheses("q?", []Hypothesis{{Statement: "a"}, {Statement: "b"}}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	intent, _ := s.AddIntentWithParents(map[string]any{"summary": "x"}, 5, nil, "planner", nil)
	expID, err := s.DeclareExperiment(Experiment{
		Question: "q?", Action: "ambiguous probe",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations:  map[int64]string{hyps[0].ID: "same output", hyps[1].ID: "same output"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	// 同文本期望会同时匹配两个 → inconclusive。
	if out, err := s.RecordExperimentOutcome(expID, "same output", 0); err != nil || out.Status != "inconclusive" {
		t.Fatalf("setup: %+v %v", out, err)
	}
	// 构造失效轨迹：supported 假设 + basis finding 反证。
	f, err := s.AddNode(KindFinding, map[string]any{"summary": "basis"}, 0, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertHypotheses("q2?", []Hypothesis{
		{Statement: "c", BasisNodeIDs: []int64{f}}, {Statement: "d", BasisNodeIDs: []int64{f}},
	}); err != nil {
		t.Fatal(err)
	}
	h2, _ := s.ListHypotheses()
	var cID int64
	for _, h := range h2 {
		if h.Question == "q2?" && h.Statement == "c" {
			cID = h.ID
		}
	}
	if _, err := d.Exec(`UPDATE hypotheses SET status='supported' WHERE id=$1`, cID); err != nil {
		t.Fatal(err)
	}
	// 显式事务直接驱动失效级联（语义路径的存储层入口）。
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InvalidateHypothesesOnNodeTx(tx, f, "test basis retraction"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ps, err = s.MineChangeProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("want 2 proposals (inconclusive + invalidation), got %d: %+v", len(ps), ps)
	}
	kinds := map[string]bool{}
	for _, p := range ps {
		if err := harness.ChangeProposal(p).Validate(); err != nil {
			t.Fatalf("proposal %s invalid: %v", p.ID, err)
		}
		if !p.RequiresHumanApproval {
			t.Fatalf("proposal %s must require human approval", p.ID)
		}
		if strings.Contains(p.ID, "discrimination") {
			kinds["discrimination"] = true
		}
		if strings.Contains(p.ID, "basis-hardening") {
			kinds["basis"] = true
		}
	}
	if !kinds["discrimination"] || !kinds["basis"] {
		t.Fatalf("missing proposal kinds: %v", kinds)
	}
}
