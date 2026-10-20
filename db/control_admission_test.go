package db

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
)

func admissionFixture(t *testing.T) (*DB, *Task, control.ActionProposal) {
	t.Helper()
	d, task, c := controlContractFixture(t)
	c.IdentityRefs = []string{"anonymous"}
	c.Budgets = append(c.Budgets, control.BudgetLimit{Dimension: "response_bytes", Limit: 1048576, Measurement: "exact"}, control.BudgetLimit{Dimension: "concurrency", Limit: 1, Measurement: "exact"})
	if _, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); err != nil {
		t.Fatal(err)
	}
	p := control.ActionProposal{SchemaVersion: 1, MethodID: "http.get", MethodVersion: "1", Target: control.Target{Scheme: "http", Host: "localhost", Port: 8080, Path: "/fixture"}, Purpose: "synthetic bounded fixture", ContextVersion: "initial", IdempotencyKey: "fixture-key"}
	return d, task, p
}
func TestControlAdmissionRollback(t *testing.T) {
	for _, table := range []string{"task_budget_reservations", "action_executions", "control_outbox"} {
		t.Run(table, func(t *testing.T) {
			d, task, p := admissionFixture(t)
			name := fmt.Sprintf("eng05_fault_%d", task.ID)
			if _, err := d.Exec(`CREATE FUNCTION ` + name + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic ENG05 failure'; END $$; CREATE TRIGGER ` + name + ` BEFORE INSERT ON ` + table + ` FOR EACH ROW WHEN (NEW.task_id=` + fmt.Sprint(task.ID) + `) EXECUTE FUNCTION ` + name + `()`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := d.Exec(`DROP TRIGGER ` + name + ` ON ` + table + `; DROP FUNCTION ` + name + `() `); err != nil {
					t.Error(err)
				}
			})
			if _, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); err == nil {
				t.Fatal("injected failure ignored")
			}
			for _, table := range []string{"task_budget_reservations", "action_executions", "control_outbox"} {
				var n int
				if err := d.QueryRow(`SELECT count(*) FROM `+table+` WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
					t.Fatal("partial commit", table, n, err)
				}
			}
			var reserved int64
			if err := d.QueryRow(`SELECT COALESCE(sum(reserved_units),0) FROM task_budget_accounts WHERE task_id=$1`, task.ID).Scan(&reserved); err != nil || reserved != 0 {
				t.Fatal("reservation leaked", reserved, err)
			}
		})
	}
}
func TestControlAdmissionReplay(t *testing.T) {
	d, task, p := admissionFixture(t)
	a, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil || a.ID != b.ID || !a.GrantExpires.Equal(b.GrantExpires) {
		t.Fatal(a, b, err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var reserved int64
	if err := d.QueryRow(`SELECT reserved_units FROM task_budget_accounts WHERE task_id=$1 AND dimension='requests'`, task.ID).Scan(&reserved); err != nil || reserved != execution.HTTPMaxRequests {
		t.Fatal(reserved, err)
	}
	if _, err := d.RevokeControlContract(t.Context(), task.ID, a.ControlEpoch, "admin:fixture", "stop"); err != nil {
		t.Fatal(err)
	}
	replay, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil || replay.ID != a.ID || replay.ControlEpoch != a.ControlEpoch {
		t.Fatal("replay renewed authority", replay, err)
	}
	if err := d.CheckExecutionGrant(t.Context(), control.TaskID(task.ID), a.ID); !errors.Is(err, control.ContractRevoked) {
		t.Fatal("revoked grant usable", err)
	}
}
func TestControlIdempotencyConflict(t *testing.T) {
	d, task, p := admissionFixture(t)
	if _, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); err != nil {
		t.Fatal(err)
	}
	p.Target.Path = "/changed"
	if _, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); !errors.Is(err, control.IdempotencyConflict) {
		t.Fatal(err)
	}
	p.Target.Path = "/fixture"
	p.IdempotencyKey = "nonce-bypass"
	p.Purpose = "rewritten wording"
	if _, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); !errors.Is(err, control.AlreadyRunning) {
		t.Fatal("in-flight semantic duplication", err)
	}
}
func controlAdmissionCommitRace(t *testing.T, revoke bool) {
	t.Helper()
	d, task, p := admissionFixture(t)
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT id FROM tasks WHERE id=$1 FOR UPDATE`, task.ID); err != nil {
		t.Fatal(err)
	}
	if revoke {
		_, err = tx.Exec(`UPDATE tasks SET control_revoked=true,control_epoch=control_epoch+1 WHERE id=$1`, task.ID)
	} else {
		_, err = tx.Exec(`UPDATE tasks SET control_phase='closed',control_epoch=control_epoch+1 WHERE id=$1`, task.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p); result <- err }()
	select {
	case err := <-result:
		t.Fatal("admission did not wait for task root lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, control.ContractRevoked) {
			t.Fatal("post-commit admission", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admission deadlock")
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM action_executions WHERE task_id=$1`, task.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("granted after fence", n, err)
	}
}
func TestControlCloseAdmissionRace(t *testing.T)  { controlAdmissionCommitRace(t, false) }
func TestControlRevokeAdmissionRace(t *testing.T) { controlAdmissionCommitRace(t, true) }

func TestControlGrantRejectsTerminalTask(t *testing.T) {
	d, task, p := admissionFixture(t)
	action, err := d.AdmitAndReserve(t.Context(), control.TaskID(task.ID), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetStatus(task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckExecutionGrant(t.Context(), control.TaskID(task.ID), action.ID); !errors.Is(err, control.ContractRevoked) {
		t.Fatal("terminal compatibility writer left grant usable", err)
	}
}
