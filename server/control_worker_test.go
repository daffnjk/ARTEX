package server

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/execution"
	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

type controlledWorkerProvider struct {
	payload json.RawMessage
	name    string
	calls   int
	tools   []string
}

func (p *controlledWorkerProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.calls++
	for _, tool := range req.Tools {
		p.tools = append(p.tools, tool.Name)
	}
	if p.calls == 1 {
		return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse, ID: "synthetic-controlled-call", Name: p.name, Input: p.payload}}}, "tool_use", llm.Usage{}, nil
	}
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("synthetic complete")}}, "end_turn", llm.Usage{}, nil
}
func (p *controlledWorkerProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	panic("fixture uses complete only")
}
func workerControlFixture(t *testing.T, url string) (*Server, *db.Task, *db.ExplorationStore, *db.Node, control.ActionProposal) {
	t.Helper()
	dsn := os.Getenv("ARTEX_TEST_PG_DSN")
	if dsn == "" || dsn != os.Getenv("ARTEX_PG_DSN") {
		t.Fatal("explicit matching test DSNs required")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	task, err := d.CreateTaskWithOptions("controlled worker fixture", "one synthetic HTTP read", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	target, err := control.NormalizeTarget(url)
	if err != nil {
		t.Fatal(err)
	}
	c := control.Contract{Mode: control.ModeDirect, Methods: []control.MethodRef{{ID: "http.get", Version: "1"}}, Targets: []control.ScopeRule{{Kind: "exact_host", Value: target.Host, Ports: []int{target.Port}}}, IdentityRefs: []string{"anonymous"}, DataScope: []string{"synthetic"}, Budgets: []control.BudgetLimit{{Dimension: "requests", Limit: 10, CloseReserve: 1, Measurement: "exact"}, {Dimension: "response_bytes", Limit: 1048576, Measurement: "exact"}, {Dimension: "concurrency", Limit: 1, Measurement: "exact"}}, ExpiresAt: time.Now().Add(time.Hour), AcceptanceVersion: "synthetic-1"}
	if _, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); err != nil {
		t.Fatal(err)
	}
	store := d.Exploration(task.ExplorationID)
	intentID, err := store.AddIntent(map[string]any{"summary": "read only the local fixture"}, 1, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetIntentState(intentID, "running"); err != nil {
		t.Fatal(err)
	}
	intent, err := store.GetNode(intentID)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{m: &Manager{pg: d, dir: t.TempDir()}, controlSupervisor: execution.NewSupervisor()}
	proposal := control.ActionProposal{SchemaVersion: 1, MethodID: "http.get", MethodVersion: "1", Target: target, Purpose: "synthetic read", ContextVersion: "initial", IdempotencyKey: "worker-fixture-read"}
	return s, task, store, intent, proposal
}
func TestControlRealWorkerUsesGrant(t *testing.T) {
	var hits atomic.Int64
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("synthetic trusted response"))
	}))
	defer fixture.Close()
	s, task, store, intent, p := workerControlFixture(t, fixture.URL)
	payload, _ := json.Marshal(p)
	provider := &controlledWorkerProvider{payload: payload, name: "control_action"}
	worker := agent.NewWorker(provider, "fixture", t.TempDir(), nil, 100000, 2)
	worker.SetNonStreaming(func() bool { return true })
	worker.SetControlledTools(s.controlledWorkerTools)
	_, _, err := worker.Execute(t.Context(), "worker", task.ID, nil, store, intent, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var executions, attempts int
	if err := s.m.pg.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1 AND state='succeeded'`, task.ID).Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if err := s.m.pg.QueryRow(`SELECT count(*) FROM execution_attempts a JOIN action_executions e ON e.id=a.execution_id WHERE e.task_id=$1 AND a.result_kind='succeeded'`, task.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || executions != 1 || attempts != 1 {
		t.Fatal("worker did not persist the granted physical action", hits.Load(), executions, attempts)
	}
}
func TestControlUnapprovedToolUnavailable(t *testing.T) {
	s, task, store, intent, _ := workerControlFixture(t, "http://127.0.0.1:8080/fixture")
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	provider := &controlledWorkerProvider{name: "Bash", payload: json.RawMessage(`{"command":"touch ` + marker + `"}`)}
	worker := agent.NewWorker(provider, "fixture", t.TempDir(), nil, 100000, 2)
	worker.SetNonStreaming(func() bool { return true })
	worker.SetControlledTools(s.controlledWorkerTools)
	_, _, _ = worker.Execute(t.Context(), "worker", task.ID, nil, store, intent, nil, nil, nil, nil)
	for _, name := range provider.tools {
		if name != "control_action" {
			t.Errorf("unapproved tool exposed: %s", name)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("Bash executed", err)
	}
}
func TestControlBackgroundPathsEnforced(t *testing.T) {
	s, task, store, intent, _ := workerControlFixture(t, "http://127.0.0.1:8080/fixture")
	worker := agent.NewWorker(&controlledWorkerProvider{}, "fixture", t.TempDir(), nil, 0, 1)
	if _, _, err := worker.Execute(t.Context(), "manual", task.ID, nil, store, intent, nil, nil, nil, nil); err == nil {
		t.Fatal("legacy/manual worker accepted a controlled task")
	}
	main := agent.NewMainAgent(&controlledWorkerProvider{}, "fixture", t.TempDir(), nil, 0, 1)
	if _, err := main.Chat(t.Context(), task.ID, 0, nil, store, "synthetic", "manual external action", nil, nil, nil, nil, nil); err == nil {
		t.Fatal("unadapted main agent path accepted")
	}
	if err := s.controlBackgroundAllowed(t.Context(), task.ID, "retest"); err == nil {
		t.Fatal("retest was not denied")
	}
	if err := s.controlBackgroundAllowed(t.Context(), task.ID, "enrich"); err == nil {
		t.Fatal("enrich was not denied")
	}
	var denied int
	if err := s.m.pg.QueryRow(`SELECT count(*) FROM control_outbox WHERE task_id=$1 AND event_type='control_denied'`, task.ID).Scan(&denied); err != nil || denied < 2 {
		t.Fatal("denials not audited", denied, err)
	}
}
func TestControlMissingTaskContextDenied(t *testing.T) {
	s, task, _, intent, _ := workerControlFixture(t, "http://127.0.0.1:8080/fixture")
	ctx := agent.WithRunInfo(context.Background(), agent.RunInfo{TaskID: task.ID, ExplorationID: task.ExplorationID, IntentID: intent.ID})
	tools, err := s.controlledWorkerTools(ctx, agent.RunInfoFrom(ctx))
	if err != nil || len(tools) != 1 {
		t.Fatal(tools, err)
	}
	for _, tc := range []context.Context{context.Background(), agent.WithRunInfo(context.Background(), agent.RunInfo{TaskID: task.ID + 1, ExplorationID: task.ExplorationID, IntentID: intent.ID})} {
		result, err := tools[0].Call(tc, json.RawMessage(`{}`), (*actool.ToolContext)(nil))
		if err == nil && !result.IsError {
			t.Fatal("missing/foreign task context accepted")
		}
	}
}
