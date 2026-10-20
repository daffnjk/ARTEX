package db

import (
	"errors"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
)

func controlContractFixture(t *testing.T) (*DB, *Task, control.Contract) {
	t.Helper()
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	task, err := d.CreateTaskWithOptions("control contract synthetic", "read a local fixture", TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	c := control.Contract{Mode: control.ModeDirect, Methods: []control.MethodRef{{ID: "http.get", Version: "1"}}, Targets: []control.ScopeRule{{Kind: "exact_host", Value: "localhost", Ports: []int{8080}}}, IdentityRefs: []string{"synthetic-anonymous"}, DataScope: []string{"synthetic"}, Budgets: []control.BudgetLimit{{Dimension: "requests", Limit: 10, CloseReserve: 1, Measurement: "exact"}}, ExpiresAt: time.Now().UTC().Add(time.Hour), AcceptanceVersion: "fixture-v1"}
	return d, task, c
}
func TestControlContractVersionImmutable(t *testing.T) {
	d, task, c := controlContractFixture(t)
	a, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != 1 || a.Epoch != 1 {
		t.Fatal(a)
	}
	if _, err := d.Exec(`UPDATE task_control_contracts SET body='{}' WHERE task_id=$1 AND version=1`, task.ID); err == nil {
		t.Fatal("approved body was mutable")
	}
	if _, err := d.Exec(`DELETE FROM task_control_contracts WHERE task_id=$1`, task.ID); err == nil {
		t.Fatal("approved history was deletable")
	}
	c.DataScope = []string{"synthetic-v2"}
	b, err := d.ApproveControlContract(t.Context(), task.ID, a.Epoch, c, "admin:fixture")
	if err != nil || b.Version != 2 || b.Epoch <= a.Epoch {
		t.Fatal(b, err)
	}
	var count int
	if err := d.QueryRow(`SELECT count(*) FROM task_control_contracts WHERE task_id=$1`, task.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if _, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture"); !errors.Is(err, control.StaleContext) {
		t.Fatal("stale update", err)
	}
}
func TestControlCoverageDoesNotAuthorize(t *testing.T) {
	d, task, c := controlContractFixture(t)
	if _, err := d.ActiveControlContract(t.Context(), task.ID); !errors.Is(err, control.ContractMissing) {
		t.Fatal(err)
	}
	if err := d.Assets().AddAutoScope(task.ID, "root_domain", "synthetic.invalid", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ActiveControlContract(t.Context(), task.ID); !errors.Is(err, control.ContractMissing) {
		t.Fatal("coverage authorized an action", err)
	}
	a, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Assets().AddAgentScope(task.ID, "root_domain", "more.invalid", "synthetic", "agent"); err != nil {
		t.Fatal(err)
	}
	b, err := d.ActiveControlContract(t.Context(), task.ID)
	if err != nil || b.Digest != a.Digest || b.Epoch != a.Epoch {
		t.Fatal("coverage changed authority", b, err)
	}
}
func TestControlRevocationEpoch(t *testing.T) {
	d, task, c := controlContractFixture(t)
	a, err := d.ApproveControlContract(t.Context(), task.ID, 0, c, "admin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := d.RevokeControlContract(t.Context(), task.ID, a.Epoch, "admin:fixture", "operator stop")
	if err != nil || epoch <= a.Epoch {
		t.Fatal(epoch, err)
	}
	if _, err := d.ActiveControlContract(t.Context(), task.ID); !errors.Is(err, control.ContractRevoked) {
		t.Fatal("old contract still active", err)
	}
	if _, err := d.RevokeControlContract(t.Context(), task.ID, a.Epoch, "admin:fixture", "stale"); !errors.Is(err, control.StaleContext) {
		t.Fatal(err)
	}
	expired := c
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := d.ApproveControlContract(t.Context(), task.ID, epoch, expired, "admin:fixture"); err == nil {
		t.Fatal("expired authorization accepted")
	}
}
