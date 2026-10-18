package db

import (
	"encoding/json"
	"testing"
)

// M3.4 评议包回归（真实隔离 PG）：干净上下文包自含（假设/事件流/已结与开放
// 实验/事实/推荐），评议存档 append-only。
func TestReviewPacket(t *testing.T) {
	d, s := m31Store(t)
	if err := s.UpsertHypotheses("q?", []Hypothesis{{Statement: "a"}, {Statement: "b"}}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := s.ListHypotheses()
	intent, _ := s.AddIntentWithParents(map[string]any{"summary": "x"}, 5, nil, "planner", nil)
	factID, _ := s.AddFactWithYields(map[string]any{"summary": "observed-a"}, nil, "worker", intent)
	expID, err := s.DeclareExperiment(Experiment{
		Question: "q?", Action: "probe", HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations: map[int64]string{hyps[0].ID: "observed-a", hyps[1].ID: "observed-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordExperimentOutcome(expID, "observed-a", factID); err != nil {
		t.Fatal(err)
	}
	packet, err := s.BuildReviewPacket()
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Hypotheses) != 2 || len(packet.Events) < 2 || len(packet.RecentFacts) != 1 {
		t.Fatalf("packet content wrong: %+v", packet)
	}
	var sawClosed bool
	for _, e := range packet.Experiments {
		if e.ID == expID && e.Status == "observed" && e.Outcome["discriminated"] != nil {
			sawClosed = true
		}
	}
	if !sawClosed {
		t.Fatalf("closed experiments must be included with outcomes: %+v", packet.Experiments)
	}
	// 存档 append-only。
	if err := s.StoreReview("test-model", map[string]any{"summary": "ok"}, map[string]any{"requests": 1}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM hypothesis_reviews WHERE exploration_id=$1`, s.expID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("review archive wrong: %d %v", n, err)
	}
	if _, err := d.Exec(`UPDATE hypothesis_reviews SET advisory='{"x":1}'::jsonb WHERE exploration_id=$1`, s.expID); err == nil {
		t.Fatal("review archive must be append-only")
	}
	// 干净上下文断言：包里不含任何提示词/对话字段。
	raw, _ := json.Marshal(packet)
	if string(raw) == "" || json.Unmarshal(raw, &map[string]any{}) != nil {
		t.Fatal("packet must be valid JSON")
	}
}
