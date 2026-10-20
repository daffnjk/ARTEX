package server

import (
	"context"
	"errors"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"testing"
	"time"
)

func TestControlTypedFailureHandling(t *testing.T) {
	cases := []struct {
		kind       control.FeedbackKind
		action     string
		newAllowed bool
	}{{control.FeedbackWaitExpired, "collect_original", false}, {control.FeedbackPolicyDenied, "pause_branch", false}, {control.FeedbackRateLimited, "cooldown", false}, {control.FeedbackEffectUnknown, "reconcile_original", false}, {control.FeedbackPreconditionInvalid, "refresh_premises", false}, {control.FeedbackValidNegative, "replan", true}, {control.FeedbackToolUnavailable, "choose_supported_method", true}}
	for _, tc := range cases {
		d := control.DecideFeedback(tc.kind, time.Second)
		if d.Action != tc.action || d.AllowNew != tc.newAllowed {
			t.Fatal(tc, d)
		}
		if tc.kind == control.FeedbackRateLimited && d.Cooldown != time.Second {
			t.Fatal("rate limit not cooled")
		}
	}
}

func TestControlTypedFeedbackUsesOriginalExecution(t *testing.T) {
	s, task, _, _, p := workerControlFixture(t, "http://127.0.0.1:8080/feedback")
	r, err := s.m.pg.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	waited, err := (execution.Lifecycle{Store: s.m.pg}).Wait(ctx, control.TaskID(task.ID), r.ID)
	if !errors.Is(err, control.WaitExpired) || waited.ID != r.ID {
		t.Fatal("wait lost original execution", waited, err)
	}
	for _, kind := range []control.FeedbackKind{feedbackKind(err), feedbackKind(control.ScopeDenied), control.FeedbackRateLimited} {
		result, e := s.controlFeedbackResult(t.Context(), control.TaskID(task.ID), r.ID, kind)
		if e != nil || !result.IsError {
			t.Fatal("typed feedback not reported", result, e)
		}
	}
	snapshot, err := s.m.pg.PrepareControlPlanningWindow(t.Context(), control.TaskID(task.ID))
	if err != nil || snapshot.AllowNew || len(snapshot.Collect) != 1 || snapshot.Collect[0] != r.ID || !snapshot.CooldownUntil.After(time.Now()) {
		t.Fatal("feedback queue/cooldown not enforced", snapshot, err)
	}
	var n int
	if err = s.m.pg.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("wait/denial/cooldown resubmitted", n, err)
	}
}
