package db

import (
	"github.com/Autumn-27/artex/control"
	"testing"
)

func TestControlScoringRespectsHardFilter(t *testing.T) {
	d, task, p := admissionFixture(t)
	p.Target.Host = "outside.invalid"
	scores, err := d.ScoreControlCandidates(t.Context(), control.TaskID(task.ID), []ControlScoreCandidate{{Proposal: p, Predictions: []string{"yes", "no"}, GoalBenefit: 100}})
	if err != nil || len(scores) != 1 || !scores[0].Excluded || scores[0].Score != 0 {
		t.Fatal("proxy benefit bypassed authorization filter", scores, err)
	}
	var n int
	if err = d.QueryRow(`SELECT count(*) FROM control_score_snapshots WHERE task_id=$1 AND mode='shadow' AND scorer_version='proxy-v1'`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("shadow audit missing", n, err)
	}
}
func TestControlSamePredictionsNoDiscrimination(t *testing.T) {
	d, task, p := admissionFixture(t)
	scores, err := d.ScoreControlCandidates(t.Context(), control.TaskID(task.ID), []ControlScoreCandidate{{Proposal: p, Predictions: []string{" 200 ", "200", "200"}}, {Proposal: p, Predictions: []string{"200", "403", "404"}}})
	if err != nil || scores[0].Discrimination != 0 || scores[1].Discrimination != 2 || scores[1].Score <= scores[0].Score {
		t.Fatal("same predictions inflated discrimination", scores, err)
	}
	var n int
	if err = d.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("shadow scoring dispatched work", n, err)
	}
}
