package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
)

func TestControlEffectThenCrashReconcile(t *testing.T) {
	var mu sync.Mutex
	counter := 0
	effects := map[string]bool{}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/increment" {
			counter++
			effects[r.Header.Get("X-ARTEX-Execution-ID")] = true
			_, _ = w.Write([]byte(`{"effect":"applied"}`))
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/effects/")
		_ = json.NewEncoder(w).Encode(map[string]any{"execution_id": id, "applied": effects[id], "count": counter})
	}))
	defer fixture.Close()
	s, task, _, intent, p := workerControlFixture(t, fixture.URL+"/increment", func(c *control.Contract) {
		for i := range c.Budgets {
			switch c.Budgets[i].Dimension {
			case "requests":
				c.Budgets[i].CloseReserve = 3
			case "concurrency":
				c.Budgets[i].Limit = 2
				c.Budgets[i].CloseReserve = 1
			case "response_bytes":
				c.Budgets[i].CloseReserve = 3 * execution.HTTPMaxResponseBytes
			}
		}
	})
	record, err := s.m.pg.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.m.pg.ClaimControlExecution(t.Context(), record.TaskID, record.ID, intent.ID, "crashing-worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.NewHTTPAdapter(&leasedExecutionPolicy{pg: s.m.pg, attempt: a}, s.controlGate(), nil, nil).Run(t.Context(), record.TaskID, record.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after the physical effect, before any result commit.
	if _, err := s.m.pg.RecoverControlLeases(t.Context(), time.Now().Add(time.Minute), 128); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileControlCounter(context.Background(), record.TaskID, record.ID, intent.ID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := counter
	mu.Unlock()
	if got != 1 {
		t.Fatal("reconciliation repeated effect", got)
	}
	final, err := s.m.pg.GetControlExecution(t.Context(), record.TaskID, record.ID)
	if err != nil || final.Outcome.Effect != control.EffectApplied || final.Outcome.Cleanup == control.CleanupConfirmed {
		t.Fatal("counter reconciliation lost uncertainty", final, err)
	}
}
