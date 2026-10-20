package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/control"
)

type waitStoreFixture struct {
	gets    atomic.Int64
	cancels atomic.Int64
	record  Record
}

func (f *waitStoreFixture) GetControlExecution(context.Context, control.TaskID, control.ExecutionID) (Record, error) {
	f.gets.Add(1)
	return f.record, nil
}
func (f *waitStoreFixture) RequestControlCancellation(context.Context, control.TaskID, control.ExecutionID) error {
	f.cancels.Add(1)
	f.record.Outcome.Cleanup = control.CleanupUnconfirmed
	return nil
}
func (f *waitStoreFixture) ConfirmControlStopped(context.Context, control.TaskID, control.ExecutionID) error {
	return errors.New("fixture has no actual stop acknowledgement")
}
func TestControlWaitTimeoutDoesNotResubmit(t *testing.T) {
	f := &waitStoreFixture{record: Record{ID: "original", TaskID: 1, Outcome: control.Outcome{Run: control.RunRunning, Effect: control.EffectUnknown, Cleanup: control.CleanupPending}}}
	service := Lifecycle{Store: f, Supervisor: NewSupervisor()}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		record, err := service.Wait(ctx, 1, "original")
		cancel()
		if !errors.Is(err, control.WaitExpired) || record.ID != "original" {
			t.Fatal(record, err)
		}
	}
	if f.gets.Load() < 3 || f.cancels.Load() != 0 {
		t.Fatal("Wait changed execution instead of reading", f.gets.Load(), f.cancels.Load())
	}
}
func TestControlCancelUnconfirmed(t *testing.T) {
	f := &waitStoreFixture{record: Record{ID: "remote-unconfirmed", TaskID: 1, Outcome: control.Outcome{Run: control.RunRunning, Effect: control.EffectUnknown, Cleanup: control.CleanupPending}}}
	service := Lifecycle{Store: f, Supervisor: NewSupervisor()}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	record, err := service.Cancel(ctx, 1, "remote-unconfirmed")
	if err != nil || record.Outcome.Cleanup == control.CleanupConfirmed || record.Outcome.Effect != control.EffectUnknown || f.cancels.Load() != 1 {
		t.Fatal("ack was treated as actual stopped/cleaned", record, err)
	}
}
