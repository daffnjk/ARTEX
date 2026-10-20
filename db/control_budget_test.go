package db

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/artex/control"
)

func budgetInt(v int64) *int64 { return &v }
func setupControlBudget(t *testing.T) (*DB, *Task) {
	t.Helper()
	d, task, c := controlContractFixture(t)
	if _, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); err != nil {
		t.Fatal(err)
	}
	return d, task
}
func TestControlBudgetConcurrentReserve(t *testing.T) {
	d, task := setupControlBudget(t)
	var wg sync.WaitGroup
	var wins atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := d.ReserveControlBudget(t.Context(), task.ID, fmt.Sprintf("reserve-%d", i), "worker:synthetic", map[string]int64{"requests": 1}, false)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, control.BudgetExhausted) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 9 {
		t.Fatalf("reservations=%d want 9 with 1 protected close unit", wins.Load())
	}
	var reserved int64
	if err := d.QueryRow(`SELECT reserved_units FROM task_budget_accounts WHERE task_id=$1 AND dimension='requests'`, task.ID).Scan(&reserved); err != nil || reserved != 9 {
		t.Fatal(reserved, err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "partial", "worker:synthetic", map[string]int64{"requests": 1, "missing": 1}, false); err == nil {
		t.Fatal("missing dimension accepted")
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM task_budget_reservations WHERE task_id=$1 AND request_key='partial'`, task.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial reservation persisted", n, err)
	}
}
func TestControlBudgetSettlementIdempotent(t *testing.T) {
	d, task := setupControlBudget(t)
	for i := 0; i < 2; i++ {
		if err := d.ReserveControlBudget(t.Context(), task.ID, "settle", "worker:synthetic", map[string]int64{"requests": 3}, false); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := d.SettleControlBudget(t.Context(), task.ID, "settle", map[string]*int64{"requests": budgetInt(2)}); err != nil {
			t.Fatal(err)
		}
	}
	var used, reserved int64
	if err := d.QueryRow(`SELECT used_units,reserved_units FROM task_budget_accounts WHERE task_id=$1 AND dimension='requests'`, task.ID).Scan(&used, &reserved); err != nil || used != 2 || reserved != 0 {
		t.Fatal(used, reserved, err)
	}
	if err := d.SettleControlBudget(t.Context(), task.ID, "settle", map[string]*int64{"requests": budgetInt(1)}); !errors.Is(err, control.IdempotencyConflict) {
		t.Fatal("conflicting settlement", err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "settle", "worker:synthetic", map[string]int64{"requests": 1}, false); !errors.Is(err, control.IdempotencyConflict) {
		t.Fatal("changed replay", err)
	}
}
func TestControlUnknownUsageNotRefunded(t *testing.T) {
	d, task := setupControlBudget(t)
	if err := d.ReserveControlBudget(t.Context(), task.ID, "unknown", "summary:synthetic", map[string]int64{"requests": 8}, false); err != nil {
		t.Fatal(err)
	}
	if err := d.SettleControlBudget(t.Context(), task.ID, "unknown", map[string]*int64{"requests": nil}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "too-much", "planner:synthetic", map[string]int64{"requests": 2}, false); !errors.Is(err, control.BudgetExhausted) {
		t.Fatal("unknown usage refunded", err)
	}
	if err := d.SettleControlBudget(t.Context(), task.ID, "unknown", map[string]*int64{"requests": budgetInt(7)}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReserveControlBudget(t.Context(), task.ID, "after-reconcile", "planner:synthetic", map[string]int64{"requests": 2}, false); err != nil {
		t.Fatal("known usage did not reconcile", err)
	}
}
