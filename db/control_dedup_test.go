package db

import (
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"sync"
	"testing"
)

func TestControlSemanticDedupConcurrent(t *testing.T) {
	d, task, p := admissionFixture(t)
	var wg sync.WaitGroup
	ids := make(chan control.ExecutionID, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := p
			q.IdempotencyKey = fmt.Sprint("nonce", i)
			q.Purpose = fmt.Sprint("different wording", i)
			r, e := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), q)
			if e != nil && !errors.Is(e, control.AlreadyRunning) {
				errs <- e
			}
			ids <- r.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first control.ExecutionID
	for id := range ids {
		if id == "" {
			t.Fatal("missing original execution reference")
		}
		if first == "" {
			first = id
		} else if first != id {
			t.Fatal("multiple semantic executions")
		}
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestControlRunningActionReused(t *testing.T) {
	d, task, p := admissionFixture(t)
	r, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	p.IdempotencyKey = "another"
	p.Purpose = "another description"
	again, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if !errors.Is(err, control.AlreadyRunning) || again.ID != r.ID {
		t.Fatal("original execution not returned", again, err)
	}
}
func TestControlRetestRequiresReason(t *testing.T) {
	d, task, a, _ := capturedControlFixture(t)
	record, err := d.GetControlExecution(t.Context(), control.TaskID(task.ID), a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	p := record.Proposal
	p.IdempotencyKey = "repeat-without-proof"
	prior, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if !errors.Is(err, control.ResultAvailable) || prior.ID != record.ID {
		t.Fatal("valid result not reused", prior, err)
	}
	fake := "model-approved"
	p.RetestRef = &fake
	if _, err = d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); err == nil {
		t.Fatal("model fabricated retest grant")
	}
	if _, err = d.ApproveControlRetest(t.Context(), control.TaskID(task.ID), a.ExecutionID, "evidence_invalidated", record.ControlEpoch, "admin:fixture"); err == nil {
		t.Fatal("invented evidence invalidation accepted")
	}
	if _, err = d.ApproveControlRetest(t.Context(), control.TaskID(task.ID), a.ExecutionID, "independent_confirmation", record.ControlEpoch, "model:fixture"); err == nil {
		t.Fatal("model approved retest")
	}
}
func TestControlContextChangeAllowsBoundedRetest(t *testing.T) {
	d, task, a, _ := capturedControlFixture(t)
	record, err := d.GetControlExecution(t.Context(), control.TaskID(task.ID), a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec(`UPDATE tasks SET control_context_version='deployment-v2' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	ref, err := d.ApproveControlRetest(t.Context(), control.TaskID(task.ID), a.ExecutionID, "environment_changed", record.ControlEpoch, "admin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	p := record.Proposal
	p.ContextVersion = "deployment-v2"
	p.IdempotencyKey = "versioned-retest"
	p.RetestRef = &ref
	next, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil || next.ID == record.ID {
		t.Fatal("valid bounded retest failed", next, err)
	}
	var reserved int64
	if err = d.QueryRow(`SELECT reserved_units FROM task_budget_accounts WHERE task_id=$1 AND dimension='requests'`, task.ID).Scan(&reserved); err != nil || reserved != execution.HTTPMaxRequests {
		t.Fatal("retest skipped budget", reserved, err)
	}
	p.IdempotencyKey = "consume-twice"
	again, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if !errors.Is(err, control.AlreadyRunning) || again.ID != next.ID {
		t.Fatal("inflight retest not reused", again, err)
	}
}
