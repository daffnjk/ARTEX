package db

import (
	"errors"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
)

func leasedControlFixture(t *testing.T) (*DB, *Task, execution.Attempt) {
	t.Helper()
	d, task, p := admissionFixture(t)
	record, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	store := d.Exploration(task.ExplorationID)
	id, err := store.AddIntent(map[string]any{"summary": "lease fixture"}, 1, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetIntentState(id, "running"); err != nil {
		t.Fatal(err)
	}
	a, err := d.ClaimControlExecution(t.Context(), control.TaskID(task.ID), record.ID, id, "worker:lease-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return d, task, a
}
func TestControlLeaseFencesStaleWorker(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	n, err := d.RecoverControlLeases(t.Context(), time.Now().Add(time.Minute), 128)
	if err != nil || n < 1 {
		t.Fatal(n, err)
	}
	if err := d.CheckControlAttempt(t.Context(), a); !errors.Is(err, control.StaleLease) {
		t.Fatal("old worker still permitted", err)
	}
	result := execution.HTTPResult{ExecutionID: a.ExecutionID, ContextVersion: "initial", Complete: true, Outcome: control.Outcome{Run: control.RunSucceeded, Effect: control.EffectApplied, Cleanup: control.CleanupConfirmed}}
	if err := d.CommitControlHTTPResult(t.Context(), a, result); !errors.Is(err, control.StaleLease) {
		t.Fatal("stale outcome accepted", err)
	}
	record, err := d.GetControlExecution(t.Context(), control.TaskID(task.ID), a.ExecutionID)
	if err != nil || record.Outcome.Run != control.RunLost || record.Outcome.Effect != control.EffectUnknown {
		t.Fatal(record, err)
	}
}
func TestControlRecoveryKeepsBudget(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	before, err := d.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecoverControlLeases(t.Context(), time.Now().Add(time.Minute), 128); err != nil {
		t.Fatal(err)
	}
	after, err := d.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil || len(after) != len(before) {
		t.Fatal(after, err)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("lease expiry refunded budget", before[i], after[i])
		}
	}
	reopened, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil || len(again) != len(before) {
		t.Fatal(err)
	}
	for i := range before {
		if before[i] != again[i] {
			t.Fatal("reopen reset budget")
		}
	}
	if _, err := d.ClaimControlExecution(t.Context(), control.TaskID(task.ID), a.ExecutionID, a.IntentID, "new-worker"); err == nil {
		t.Fatal("lost execution was automatically replayable")
	}
}

func TestControlRestartDoesNotReplayGrants(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	before, err := d.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SuspendControlTaskOnRestore(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckControlAttempt(t.Context(), a); err == nil {
		t.Fatal("restart reused old authority")
	}
	after, err := d.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil || len(before) != len(after) {
		t.Fatal(err)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("restart cleared unknown resource reservation")
		}
	}
	var phase string
	if err := d.QueryRow(`SELECT control_phase FROM tasks WHERE id=$1`, task.ID).Scan(&phase); err != nil || phase != "draining" {
		t.Fatal(phase, err)
	}
}
