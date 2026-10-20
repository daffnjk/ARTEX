package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/agent"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

func controlServerBudgetFixture(t *testing.T) (*db.DB, *db.Task) {
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
	task, err := d.CreateTaskWithOptions("budgeted model fixture", "synthetic local budget", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	c := control.Contract{Mode: control.ModeDirect, Methods: []control.MethodRef{{ID: "http.get", Version: "1"}}, Targets: []control.ScopeRule{{Kind: "exact_host", Value: "localhost", Ports: []int{8080}}}, DataScope: []string{"synthetic"}, Budgets: []control.BudgetLimit{{Dimension: "llm_requests", Limit: 4, CloseReserve: 1, Measurement: "exact"}, {Dimension: "tokens", Limit: 100000, CloseReserve: 100, Measurement: "estimated"}}, ExpiresAt: time.Now().Add(time.Hour), AcceptanceVersion: "fixture-1"}
	if _, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); err != nil {
		t.Fatal(err)
	}
	return d, task
}
func TestControlLLMRetryBudgeted(t *testing.T) {
	defer withZeroRetryBackoff()()
	for _, mode := range []string{"sdk", "outer", "failover", "stream"} {
		t.Run(mode, func(t *testing.T) {
			d, task := controlServerBudgetFixture(t)
			var calls atomic.Int64
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if calls.Add(1) <= 2 {
					w.Header().Set("Content-Type", "application/json")
					if mode == "failover" {
						w.WriteHeader(402)
						_, _ = w.Write([]byte(`{"error":{"message":"insufficient_quota"}}`))
					} else {
						w.WriteHeader(503)
						_, _ = w.Write([]byte(`{"error":{"message":"temporary"}}`))
					}
					return
				}
				if body["stream"] == true {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"synthetic\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"synthetic"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
			}))
			defer fixture.Close()
			retry := agent.RetryConfig{ConnectAttempts: 2, ConnectInterval: time.Millisecond, EmptyAttempts: -1}
			if mode == "outer" || mode == "failover" {
				retry.ConnectAttempts = -1
			}
			providers := []llm.Provider{}
			for i := 0; i < 3; i++ {
				raw, err := (agent.Config{Format: llm.FormatOpenAI, BaseURL: fmt.Sprintf("%s/profile%d", fixture.URL, i), APIKey: "synthetic", Model: "fixture", Retry: retry}).NewProvider()
				if err != nil {
					t.Fatal(err)
				}
				providers = append(providers, &controlBudgetProvider{next: raw, pg: d})
			}
			index := 0
			hooks := taskLLMStreamHooks{current: func() (taskLLMSelection, error) {
				outer := agent.RetryConfig{StreamAttempts: -1}
				if mode == "outer" {
					outer.StreamAttempts = 2
				}
				return taskLLMSelection{provider: providers[index], profileID: int64(index + 1), retry: outer}, nil
			}, exhaust: func(_ taskLLMSelection, _ error) (db.TaskLLMTransition, error) {
				index++
				if index >= len(providers) {
					return db.TaskLLMTransition{}, errors.New("fixture profiles exhausted")
				}
				next := int64(index + 1)
				return db.TaskLLMTransition{Advanced: true, NextProfileID: &next}, nil
			}}
			ctx := control.WithCallAttribution(context.Background(), control.TaskID(task.ID), "summary")
			req := llm.CompletionRequest{MaxTokens: 32}
			if mode == "stream" {
				for _, err := range streamTaskLLM(ctx, "synthetic", req, hooks) {
					if err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if _, _, _, err := completeTaskLLM(ctx, "synthetic", req, hooks); err != nil {
					t.Fatal(err)
				}
			}
			var used, reserved int64
			if err := d.QueryRow(`SELECT used_units,reserved_units FROM task_budget_accounts WHERE task_id=$1 AND dimension='llm_requests'`, task.ID).Scan(&used, &reserved); err != nil || used != 3 || reserved != 0 || calls.Load() != 3 {
				t.Fatal("retry accounting", used, reserved, calls.Load(), err)
			}
			if mode == "failover" && index != 2 {
				t.Fatal("did not switch three profiles", index)
			}
			if _, _, _, err := providers[index].Complete(ctx, req); !errors.Is(err, control.BudgetExhausted) {
				t.Fatal("exploration spent close reserve", err)
			}
			if _, _, _, err := providers[index].Complete(control.WithCallAttribution(context.Background(), control.TaskID(task.ID), ""), req); err == nil {
				t.Fatal("missing role accepted")
			}
		})
	}
}
func TestControlCloseReserveProtected(t *testing.T) {
	d, task := controlServerBudgetFixture(t)
	if err := d.ReserveControlBudget(t.Context(), task.ID, "all-exploration", "planner:fixture", map[string]int64{"llm_requests": 3}, false); err != nil {
		t.Fatal(err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "steal-close", "planner:fixture", map[string]int64{"llm_requests": 1}, false); !errors.Is(err, control.BudgetExhausted) {
		t.Fatal(err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "cleanup", "cleanup:fixture", map[string]int64{"llm_requests": 1}, true); err != nil {
		t.Fatal(err)
	}
	// Local accounting/report reads do not invoke a model or require exploration budget.
	accounts, err := d.ControlBudgetAccounts(t.Context(), task.ID)
	if err != nil || len(accounts) != 2 {
		t.Fatal(accounts, err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "unauthorized-close", "planner:fixture", map[string]int64{"llm_requests": 1}, true); err == nil {
		t.Fatal("planner claimed cleanup reserve")
	}
}
