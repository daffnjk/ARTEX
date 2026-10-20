package db

import (
	"bytes"
	"context"
	"testing"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"github.com/Autumn-27/artex/harness"
)

func capturedControlFixture(t *testing.T) (*DB, *Task, execution.Attempt, []harness.Observation) {
	t.Helper()
	d, task, a := leasedControlFixture(t)
	payload := []byte("synthetic captured bytes")
	result := execution.HTTPResult{ExecutionID: a.ExecutionID, ContextVersion: "initial", Requests: 1, ResponseBytes: int64(len(payload)), Complete: true, Outcome: control.Outcome{Run: control.RunSucceeded, Effect: control.EffectUnknown, Cleanup: control.CleanupConfirmed}, Hops: []execution.HTTPHop{{Target: control.Target{Scheme: "http", Host: "localhost", Port: 8080, Path: "/fixture"}, StatusCode: 200, Body: payload, SHA256: harness.Digest(payload), Complete: true}}}
	if err := d.CommitControlHTTPResult(t.Context(), a, result); err != nil {
		t.Fatal(err)
	}
	obs, err := d.CaptureControlObservations(t.Context(), a)
	if err != nil || len(obs) != 1 {
		t.Fatal(obs, err)
	}
	return d, task, a, obs
}
func TestControlObservationLineage(t *testing.T) {
	d, task, a, obs := capturedControlFixture(t)
	for _, modify := range []func(*execution.Attempt){func(x *execution.Attempt) { x.TaskID++ }, func(x *execution.Attempt) { x.ExecutionID = "foreign" }, func(x *execution.Attempt) { x.ID = "foreign" }, func(x *execution.Attempt) { x.Epoch++ }, func(x *execution.Attempt) { x.IntentID++ }} {
		wrong := a
		modify(&wrong)
		if _, err := d.CaptureControlObservations(t.Context(), wrong); err == nil {
			t.Fatal("accepted grafted observation identity")
		}
	}
	if _, err := d.ControlObservationResolver(control.TaskID(task.ID+1)).Resolve(context.Background(), obs[0].ID); err == nil {
		t.Fatal("cross-task observation visible")
	}
	// A mismatched task->exploration mapping cannot relabel already captured bytes.
	foreign, err := d.CreateExploration("foreign observation fixture", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, foreign) })
	if _, err := d.Exec(`UPDATE tasks SET exploration_id=$2 WHERE id=$1`, task.ID, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CaptureControlObservations(t.Context(), a); err == nil {
		t.Fatal("exploration graft accepted")
	}
	if _, err := d.Exec(`UPDATE tasks SET exploration_id=$2 WHERE id=$1`, task.ID, task.ExplorationID); err != nil {
		t.Fatal(err)
	}
}
func TestControlObservationIDConflict(t *testing.T) {
	d, task, a, obs := capturedControlFixture(t)
	again, err := d.CaptureControlObservations(t.Context(), a)
	if err != nil || len(again) != 1 || again[0].ID != obs[0].ID {
		t.Fatal("capture not idempotent", again, err)
	}
	forged := obs[0]
	forged.Payload = []byte("forged replacement")
	if err := d.StoreHarnessObservation(forged); err == nil {
		t.Fatal("legacy writer overwrote controlled observation")
	}
	got, err := d.ControlObservationResolver(control.TaskID(task.ID)).Resolve(t.Context(), obs[0].ID)
	if err != nil || !bytes.Equal(got.Payload, obs[0].Payload) {
		t.Fatal("original payload changed", got, err)
	}
	if _, err := d.Exec(`UPDATE action_executions SET result_json='{}' WHERE id=$1`, string(a.ExecutionID)); err == nil {
		t.Fatal("captured result was mutable")
	}
}
