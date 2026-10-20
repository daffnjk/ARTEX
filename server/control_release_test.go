package server

import (
	"context"
	"fmt"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestControlReleaseAllowlist(t *testing.T) {
	var hits atomic.Int64
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("allowlisted local read"))
	}))
	defer fixture.Close()
	methods := []control.MethodRef{{ID: "http.get", Version: "2"}, {ID: "http.post", Version: "1"}, {ID: "shell.exec", Version: "1"}, {ID: "browser.navigate", Version: "1"}, {ID: "mcp.call", Version: "1"}, {ID: "custom.command", Version: "1"}}
	s, task, store, intent, p := workerControlFixture(t, fixture.URL, func(c *control.Contract) {
		c.Methods = append(c.Methods, methods...)
		c.Targets = append(c.Targets, control.ScopeRule{Kind: "exact_host", Value: "192.0.2.1", Ports: []int{80}})
	})
	e2eWorker(t, s, task, store, intent, p)
	for i, m := range methods {
		q := p
		q.MethodID = m.ID
		q.MethodVersion = m.Version
		q.IdempotencyKey = fmt.Sprintf("not-released-%d", i)
		if _, err := s.m.pg.AdmitAndReserve(t.Context(), control.TaskID(task.ID), q); err == nil {
			t.Fatal("approval expanded shipped adapter", m)
		}
	}
	q := p
	q.Target.Host = "192.0.2.1"
	q.Target.Port = 80
	q.IdempotencyKey = "non-local-target"
	if _, err := s.m.pg.AdmitAndReserve(t.Context(), control.TaskID(task.ID), q); err == nil {
		t.Fatal("public target admitted")
	}
	for _, path := range []string{"retest", "enrich", "shell", "browser", "mcp", "custom", "background"} {
		if err := s.controlBackgroundAllowed(t.Context(), task.ID, path); err == nil {
			t.Fatal("unadapted outlet enabled", path)
		}
	}
	if hits.Load() != 1 {
		t.Fatal("release changed physical effects", hits.Load())
	}
}
func TestControlFeatureRollbackKeepsSafety(t *testing.T) {
	// Rollback omits optional score/review calls; mandatory gates have no off switch.
	for _, optional := range []bool{true, false} {
		t.Run(fmt.Sprintf("optional_%v", optional), func(t *testing.T) {
			var hits atomic.Int64
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
			defer fixture.Close()
			s, task, store, intent, p := workerControlFixture(t, fixture.URL, func(c *control.Contract) { c.Mode = control.ModeGoal })
			p, _ = e2eAcceptance(t, s, task, store, intent, p, []byte("unobserved claim"))
			before, err := s.m.pg.ActiveControlContract(t.Context(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if optional {
				scores, err := s.m.pg.ScoreControlCandidates(t.Context(), control.TaskID(task.ID), []db.ControlScoreCandidate{{Proposal: p, Predictions: []string{"same", "same"}}})
				if err != nil || len(scores) != 1 || scores[0].Discrimination != 0 {
					t.Fatal(scores, err)
				}
				ctx := context.WithValue(t.Context(), controlAdminKey{}, "admin:ARTEX")
				session, packet, err := s.beginControlReview(ctx, control.TaskID(task.ID), 1)
				if err != nil || packet.Control.Independent {
					t.Fatal(packet, err)
				}
				if err = session.Archive(ctx, db.ControlReviewSuggestion{Decision: "no_issue_detected", Summary: "host advisory only, not proof"}); err != nil {
					t.Fatal(err)
				}
			}
			after, err := s.m.pg.ActiveControlContract(t.Context(), task.ID)
			if err != nil || before.Digest != after.Digest || before.Epoch != after.Epoch {
				t.Fatal("optional policies changed authority", after, err)
			}
			if response := controlRequest(t, s, task.ID, http.MethodPost, "revoke", "reviewer", map[string]any{"expected_epoch": 1, "reason": "forged"}); response.Code != 403 {
				t.Fatal("reviewer became approver", response.Code)
			}
			if closed, err := s.m.pg.TryCloseTask(t.Context(), control.TaskID(task.ID), db.ControlCloseRequest{Reason: control.StopComplete, Source: "model:PASS"}); err == nil || closed.Result == "complete" {
				t.Fatal("optional result replaced evidence", closed, err)
			}
			if err = s.m.pg.ReserveControlBudget(t.Context(), task.ID, "known-test-reservation", "worker:bounded-test", map[string]int64{"requests": 9}, false); err != nil {
				t.Fatal(err)
			}
			e2eWorker(t, s, task, store, intent, p)
			if hits.Load() != 0 {
				t.Fatal("rollback disabled budget", hits.Load())
			}
			var n int
			if err = s.m.pg.QueryRow(`SELECT count(*) FROM control_claims WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
				t.Fatal("rollback forged evidence", n, err)
			}
		})
	}
}

func TestControlHostShutdownDrainsRuntime(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := New(context.Background(), m, t.TempDir(), t.TempDir(), t.TempDir())
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if s.ctx.Err() == nil {
		t.Fatal("manager closed without cancelling owned server")
	}
	s.engine.runtimeMu.Lock()
	remaining := len(s.engine.runtimes)
	s.engine.runtimeMu.Unlock()
	if remaining != 0 {
		t.Fatal("manager left task writers after store shutdown", remaining)
	}
	if err = m.Close(); err != nil {
		t.Fatal("close is not idempotent", err)
	}
}
