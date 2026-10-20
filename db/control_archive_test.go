package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"strings"
	"testing"
	"time"
)

func TestControlMigrationTwice(t *testing.T) {
	d, task, _ := admissionFixture(t)
	before, err := d.ActiveControlContract(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := withSchemaMigrationLock(t.Context(), d.DB, func(c *sql.Conn) error { return applySchemaWithRetry(t.Context(), c, time.Sleep) }); err != nil {
			t.Fatal(err)
		}
	}
	after, err := d.ActiveControlContract(t.Context(), task.ID)
	if err != nil || after.Digest != before.Digest || after.Epoch != before.Epoch {
		t.Fatal("migration changed authority", after, err)
	}
	legacy, err := d.CreateTask("old snapshot", "legacy facts", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(legacy.ID) })
	var mode string
	if err = d.QueryRow(`SELECT control_mode FROM tasks WHERE id=$1`, legacy.ID).Scan(&mode); err != nil || mode != "legacy" {
		t.Fatal(mode, err)
	}
}
func TestControlArchiveRoundTrip(t *testing.T) {
	d, task, h, _, req, reg := outcomeControlFixture(t)
	if _, err := d.CommitValidatedOutcome(t.Context(), req, h, reg); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SaveTaskPlan(TaskPlan{ExplorationID: task.ExplorationID, Steps: []PlanStep{{ID: 1, Content: "historical", Status: "pending", ContextVersion: "initial"}}}); err != nil {
		t.Fatal(err)
	}
	snap, err := d.SnapshotTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskArchiveSnapshot
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Control == nil || rawRowCount(decoded.Control.Tables["harness_observations"]) != 1 || rawRowCount(decoded.Control.Tables["task_control_contracts"]) != 1 || rawRowCount(decoded.Control.Tables["task_budget_accounts"]) == 0 {
		t.Fatal("control audit missing")
	}
	for _, name := range []string{"harness_evaluations", "harness_acceptance", "control_claims", "task_plans"} {
		if rawRowCount(decoded.Control.Tables[name]) != 1 {
			t.Fatal("missing historical table", name)
		}
	}
	if !strings.Contains(string(decoded.Control.Tables["harness_observations"]), req.Evidence[0].ObservationID) || strings.Contains(string(raw), "grant_expires") || strings.Contains(string(raw), "lease_owner") {
		t.Fatal("missing evidence or exported reusable grant")
	}
}
func TestControlRestoreDoesNotReplay(t *testing.T) {
	d, task, p := admissionFixture(t)
	r, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := d.SnapshotTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The existing archive worker owns the transition; no fake execution adapter.
	if err = d.SetPaused(task.ID, true); err != nil {
		t.Fatal(err)
	}
	job, err := d.QueueTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimTaskArchiveJob(t.Context()); err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatal(claimed, err)
	}
	if err = d.CompleteTaskArchive(job.ID, snap, "synthetic-retained-package", "synthetic-digest", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = d.QueueTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimTaskArchiveJob(t.Context()); err != nil || claimed == nil || claimed.State != Restoring {
		t.Fatal(claimed, err)
	}
	restored, err := d.RestoreControlTaskArchive(t.Context(), job.ID, snap)
	if err != nil {
		t.Fatal(err)
	}
	if restored == task.ID {
		t.Fatal("restored identity reused")
	}
	t.Cleanup(func() { _ = d.DeleteTask(restored) })
	again, err := d.RestoreControlTaskArchive(t.Context(), job.ID, snap)
	if err != nil || again != restored {
		t.Fatal("restore not idempotent", again, err)
	}
	var exp int64
	var queued, revoked, paused bool
	var version int64
	if err = d.QueryRow(`SELECT exploration_id,queued,control_revoked,paused,contract_version FROM tasks WHERE id=$1`, restored).Scan(&exp, &queued, &revoked, &paused, &version); err != nil || exp == task.ExplorationID || queued || !revoked || !paused || version != 0 {
		t.Fatal("authority replayed", err)
	}
	var n int
	if err = d.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, restored).Scan(&n); err != nil || n != 0 {
		t.Fatal("execution replayed", n, err)
	}
	if _, err = d.ActiveControlContract(t.Context(), restored); err == nil {
		t.Fatal("historical approval live")
	}
	if err = d.CheckExecutionGrant(t.Context(), control.TaskID(restored), r.ID); err == nil {
		t.Fatal("old grant usable by restored task")
	}
	history, err := d.ControlRestoreHistory(t.Context(), restored)
	if err != nil || history.SourceTaskID != task.ID || history.TaskID != restored || history.ExplorationID != exp || !strings.Contains(string(history.Tables["action_executions"]), string(r.ID)) {
		t.Fatal("historical references lost", history, err)
	}
}
func TestControlLegacyNotPromoted(t *testing.T) {
	d, task, _ := controlContractFixture(t)
	if _, err := d.Exploration(task.ExplorationID).AddFactWithYields(map[string]any{"summary": "PASS"}, nil, "model", 0); err != nil {
		t.Fatal(err)
	}
	snap, err := d.SnapshotTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Control != nil {
		t.Fatal("legacy snapshot promoted")
	}
	if _, err = d.ActiveControlContract(t.Context(), task.ID); err == nil {
		t.Fatal("fact became permission")
	}
}
func TestControlDeleteWithLateResult(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	if err := d.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	result := execution.HTTPResult{ExecutionID: a.ExecutionID, ContextVersion: "initial", Requests: 1, Complete: true, Outcome: control.Outcome{Run: control.RunSucceeded, Effect: control.EffectUnknown, Cleanup: control.CleanupConfirmed}}
	if err := d.CommitControlHTTPResult(context.Background(), a, result); err == nil {
		t.Fatal("late result accepted after deletion")
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM task_control_contracts WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("audit removed", n, err)
	}
	if live, err := d.GetTask(task.ID); err != nil || live != nil {
		t.Fatal("task revived", live, err)
	}
}

func TestControlDeleteRacePreservesAudit(t *testing.T) {
	d, task, a := leasedControlFixture(t)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; done <- d.DeleteTask(task.ID) }()
	go func() {
		<-start
		done <- d.CommitControlHTTPResult(context.Background(), a, execution.HTTPResult{ExecutionID: a.ExecutionID, ContextVersion: "initial", Requests: 1, Complete: true, Outcome: control.Outcome{Run: control.RunSucceeded, Effect: control.EffectUnknown, Cleanup: control.CleanupConfirmed}})
	}()
	close(start)
	e1, e2 := <-done, <-done
	if e1 != nil && e2 != nil {
		t.Fatal("neither serialized action succeeded", e1, e2)
	}
	if live, err := d.GetTask(task.ID); err != nil || live != nil {
		t.Fatal("delete race revived task", live, err)
	}
	if _, err := d.CaptureControlObservations(t.Context(), a); err == nil {
		t.Fatal("deleted task created fresh evidence")
	}
}
