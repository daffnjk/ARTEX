package db

import (
	"github.com/Autumn-27/artex/control"
	"testing"
)

func TestControlClosePartialOnBudget(t *testing.T) {
	d, task, _ := admissionFixture(t)
	if _, err := d.Exploration(task.ExplorationID).AddGoal(map[string]any{"summary": "unresolved"}, "host"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE task_budget_accounts SET used_units=limit_units-close_reserve_units WHERE task_id=$1 AND dimension='requests'`, task.ID); err != nil {
		t.Fatal(err)
	}
	result, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopBudget, Source: "system:budget"})
	if err != nil || result.Phase != "closed" || result.Result != "inconclusive" || result.LegacyStatus != "timeout" || result.Reason != control.StopBudget {
		t.Fatal("budget close falsely required solving every unknown", result, err)
	}
}
func TestControlDirectNoAutoExpansion(t *testing.T) {
	d, task, _, _ := capturedControlFixture(t)
	var before int
	if err := d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1`, task.ExplorationID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	result, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopComplete, Source: "host:fixture"})
	if err != nil || result.Result != "complete" || result.LegacyStatus != "done" {
		t.Fatal(result, err)
	}
	var after int
	if err = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1`, task.ExplorationID).Scan(&after); err != nil || after != before {
		t.Fatal("direct close expanded exploration scope", before, after, err)
	}
}
func TestControlLateCompletionCannotOverwrite(t *testing.T) {
	for _, reason := range []control.StopReason{control.StopDeadline, control.StopRevoked} {
		t.Run(string(reason), func(t *testing.T) {
			d, task, _ := admissionFixture(t)
			if reason == control.StopDeadline {
				if _, err := d.Exec(`UPDATE tasks SET deadline_at=now()-interval '1 second' WHERE id=$1`, task.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := d.RevokeControlContract(t.Context(), task.ID, 1, "admin:fixture", "synthetic revoke"); err != nil {
					t.Fatal(err)
				}
			}
			first, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: reason, Source: "host:first"})
			if err != nil {
				t.Fatal(err)
			}
			won, err := d.CompleteTaskGuarded(task.ID, "done", "model:late-met")
			if err != nil || won {
				t.Fatal("late met overwrote prior terminal", won, err)
			}
			last, err := d.ControlTaskSummary(t.Context(), control.TaskID(task.ID))
			if err != nil || last.Close.Reason != first.Reason || last.Close.LegacyStatus != first.LegacyStatus {
				t.Fatal(last, err)
			}
		})
	}
}
func TestControlCloseRechecksEvidence(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t, func(c *control.Contract) { c.Mode = control.ModeGoal })
	if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err != nil {
		t.Fatal(err)
	}
	if err := d.InvalidateControlEvidence(t.Context(), control.TaskID(task.ID), req.Evidence[0].ObservationID, "host:fixture", "withdraw before completion"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopComplete, Source: "host:fixture"}); err == nil {
		t.Fatal("withdrawn proof closed task as complete")
	}
	var phase string
	if err := d.QueryRow(`SELECT control_phase FROM tasks WHERE id=$1`, task.ID).Scan(&phase); err != nil || phase == "closed" {
		t.Fatal("rejected completion changed terminal", phase, err)
	}
}

func TestControlGoalCompleteFromCurrentClaims(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t, func(c *control.Contract) { c.Mode = control.ModeGoal })
	if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err != nil {
		t.Fatal(err)
	}
	result, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopComplete, Source: "host:fixture"})
	if err != nil || result.Phase != "closed" || result.Result != "complete" {
		t.Fatal("valid formal goal did not complete", result, err)
	}
}

func TestControlCloseDrainsAndRecordsUnconfirmedHandoff(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	first, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopUnknown, Source: "host:fixture"})
	if err != nil || first.Phase != "draining" || first.Cleanup != control.CleanupUnconfirmed || first.Won {
		t.Fatal("running work falsely confirmed closed", first, err)
	}
	if err = d.CheckControlAttempt(t.Context(), a); err == nil {
		t.Fatal("draining left attempt grant live")
	}
	last, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopComplete, Source: "model:late", Handoff: true})
	if err != nil || last.Phase != "closed" || last.Cleanup != control.CleanupUnconfirmed || last.Reason != control.StopUnknown || last.Result == "complete" {
		t.Fatal("handoff changed first reason or invented cleanup", last, err)
	}
}
func TestControlCloseRejectsInventedBudget(t *testing.T) {
	d, task, _ := admissionFixture(t)
	if _, err := d.TryCloseTask(t.Context(), control.TaskID(task.ID), ControlCloseRequest{Reason: control.StopBudget, Source: "host:fixture"}); err == nil {
		t.Fatal("unexhausted budget was reported exhausted")
	}
}
