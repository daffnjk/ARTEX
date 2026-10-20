package db

import (
	"github.com/Autumn-27/artex/control"
	"testing"
)

func TestControlReviewBindsEvidenceVersion(t *testing.T) {
	d, task, _, obs := capturedControlFixture(t)
	session, packet, err := d.StartControlReviewSession(t.Context(), control.TaskID(task.ID), 1, "admin:fixture")
	if err != nil || packet.Control == nil || packet.Control.PacketDigest == "" {
		t.Fatal(packet, err)
	}
	if err = session.Archive(t.Context(), ControlReviewSuggestion{Decision: "manual_required", Summary: "bounded advisory only"}); err != nil {
		t.Fatal(err)
	}
	if err = d.InvalidateControlEvidence(t.Context(), control.TaskID(task.ID), obs[0].ID, "host:fixture", "withdraw after review"); err != nil {
		t.Fatal(err)
	}
	if err = session.Archive(t.Context(), ControlReviewSuggestion{Decision: "no_issue_detected", Summary: "stale packet"}); err == nil {
		t.Fatal("stale review reused after evidence change")
	}
	if err = d.Exploration(task.ExplorationID).StoreReview("worker", map[string]any{"reviewer": "admin", "decision": "approved"}, nil); err == nil {
		t.Fatal("legacy worker writer accepted controlled review")
	}
	if _, err = d.Exec(`UPDATE hypothesis_reviews SET advisory='{}' WHERE task_id=$1`, task.ID); err == nil {
		t.Fatal("review archive mutable")
	}
}
