package execution

import (
	"context"
	"time"

	"github.com/Autumn-27/artex/control"
)

type LifecycleStore interface {
	GetControlExecution(context.Context, control.TaskID, control.ExecutionID) (Record, error)
	RequestControlCancellation(context.Context, control.TaskID, control.ExecutionID) error
	ConfirmControlStopped(context.Context, control.TaskID, control.ExecutionID) error
}
type Lifecycle struct {
	Store      LifecycleStore
	Supervisor *Supervisor
}

func (l Lifecycle) Wait(ctx context.Context, task control.TaskID, id control.ExecutionID) (Record, error) {
	if l.Store == nil {
		return Record{}, control.ToolUnavailable
	}
	last := Record{ID: id, TaskID: task}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return last, control.WaitExpired
		}
		record, err := l.Store.GetControlExecution(ctx, task, id)
		if err != nil {
			if ctx.Err() != nil {
				return last, control.WaitExpired
			}
			return last, err
		}
		last = record
		switch record.Outcome.Run {
		case control.RunSucceeded, control.RunFailed, control.RunCancelled, control.RunTimedOut, control.RunLost:
			return record, nil
		}
		select {
		case <-ctx.Done():
			return last, control.WaitExpired
		case <-ticker.C:
		}
	}
}
func (l Lifecycle) Cancel(ctx context.Context, task control.TaskID, id control.ExecutionID) (Record, error) {
	if l.Store == nil {
		return Record{}, control.ToolUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := l.Store.RequestControlCancellation(ctx, task, id); err != nil {
		return Record{}, err
	}
	if l.Supervisor != nil && l.Supervisor.CancelRun(ctx, id) {
		if err := l.Store.ConfirmControlStopped(ctx, task, id); err != nil {
			return Record{}, err
		}
	}
	return l.Store.GetControlExecution(ctx, task, id)
}
