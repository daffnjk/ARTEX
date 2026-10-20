package agent

import (
	"context"
	"encoding/json"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/execution"
	"github.com/Autumn-27/artex/harness"
	actool "github.com/Autumn-27/norma/tool"
	"os"
	"testing"
	"time"
)

func planningControlFixture(t *testing.T) (*db.DB, *db.Task, *Planner, *controlAssemblyProvider) {
	t.Helper()
	d, err := db.Open(os.Getenv("ARTEX_TEST_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	task, err := d.CreateTaskWithOptions("planning window fixture", "synthetic bounded goal", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	c := control.Contract{Mode: control.ModeGoal, Methods: []control.MethodRef{{ID: "http.get", Version: "1"}}, Targets: []control.ScopeRule{{Kind: "exact_host", Value: "localhost", Ports: []int{8080}}}, IdentityRefs: []string{"anonymous"}, DataScope: []string{"synthetic"}, Budgets: []control.BudgetLimit{{Dimension: "requests", Limit: 20, Measurement: "exact"}, {Dimension: "response_bytes", Limit: 1048576, Measurement: "exact"}, {Dimension: "concurrency", Limit: 1, Measurement: "exact"}}, ExpiresAt: time.Now().Add(time.Hour), AcceptanceVersion: "fixture-v1"}
	if _, err = d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); err != nil {
		t.Fatal(err)
	}
	provider := &controlAssemblyProvider{}
	p := NewPlanner(provider, "synthetic", t.TempDir(), nil, 100000, 1)
	p.SetNonStreaming(func() bool { return true })
	return d, task, p, provider
}
func TestControlPlannerStallBound(t *testing.T) {
	d, task, p, prov := planningControlFixture(t)
	store := d.Exploration(task.ExplorationID)
	for i := 0; i < 8; i++ {
		_, _, err := p.Plan(t.Context(), task.ID, nil, store, task.Goal, nil, nil)
		if i >= 3 && err == nil {
			t.Fatal("unbounded empty planning")
		}
	}
	if len(prov.requests) != 3 {
		t.Fatal("stalled planner kept spending model calls", len(prov.requests))
	}
}
func TestControlUnexpectedEvidenceReplans(t *testing.T) {
	d, task, p, prov := planningControlFixture(t)
	store := d.Exploration(task.ExplorationID)
	for i := 0; i < 4; i++ {
		_, _, _ = p.Plan(t.Context(), task.ID, nil, store, task.Goal, nil, nil)
	}
	before := len(prov.requests)
	proposal := control.ActionProposal{SchemaVersion: 1, MethodID: "http.get", MethodVersion: "1", Target: control.Target{Scheme: "http", Host: "localhost", Port: 8080, Path: "/unexpected"}, Purpose: "synthetic unexpected fixture", ContextVersion: "initial", IdempotencyKey: "unexpected"}
	r, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), proposal)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.AddIntent(map[string]any{"summary": "fixture"}, 1, nil, "host")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetIntentState(intent, "running"); err != nil {
		t.Fatal(err)
	}
	a, err := d.ClaimControlExecution(t.Context(), control.TaskID(task.ID), r.ID, intent, "worker:fixture")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("surprising but complete synthetic result")
	result := execution.HTTPResult{ExecutionID: r.ID, ContextVersion: "initial", Complete: true, Requests: 1, ResponseBytes: int64(len(body)), Outcome: control.Outcome{Run: control.RunSucceeded, Effect: control.EffectUnknown, Cleanup: control.CleanupConfirmed}, Hops: []execution.HTTPHop{{Target: proposal.Target, Body: body, SHA256: harness.Digest(body), StatusCode: 200, Complete: true}}}
	if err = d.CommitControlHTTPResult(t.Context(), a, result); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CaptureControlObservations(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, _, err = p.Plan(t.Context(), task.ID, nil, store, task.Goal, nil, nil); err != nil {
		t.Fatal("valid unexpected evidence did not reset stall", err)
	}
	if len(prov.requests) != before+1 {
		t.Fatal("new result did not reach actual planner")
	}
}

func TestControlPlanningWindowBoundsNewIntents(t *testing.T) {
	calls := 0
	original := actool.Build(actool.Spec{Name: "add_intent", Run: func(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
		calls++
		return actool.Text("queued"), nil
	}})
	wrapped := controlWindowTools([]actool.CoreTool{original}, control.PlanningWindow{AllowNew: true})
	for i := 0; i < 2; i++ {
		if _, err := wrapped[0].Call(t.Context(), json.RawMessage(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("short window admitted multiple intents", calls)
	}
	wrapped = controlWindowTools([]actool.CoreTool{original}, control.PlanningWindow{AllowNew: false})
	if _, err := wrapped[0].Call(t.Context(), json.RawMessage(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("collect-only window dispatched new work")
	}
}
