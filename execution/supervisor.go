package execution

import (
	"context"
	"sync"
	"time"

	"github.com/Autumn-27/artex/control"
)

type supervisedRun struct {
	task   control.TaskID
	cancel context.CancelFunc
}

// Supervisor serializes local request-start permission with operator revocation.
// A request already permitted before revocation may still arrive at the target.
type Supervisor struct {
	mu      sync.Mutex
	blocked map[control.TaskID]bool
	active  map[control.ExecutionID]supervisedRun
}

func NewSupervisor() *Supervisor {
	return &Supervisor{blocked: map[control.TaskID]bool{}, active: map[control.ExecutionID]supervisedRun{}}
}
func (s *Supervisor) before(ctx context.Context, source GrantChecker, task control.TaskID, id control.ExecutionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked[task] {
		return control.ContractRevoked
	}
	check, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return source.CheckExecutionGrant(check, task, id)
}
func (s *Supervisor) start(ctx context.Context, source GrantChecker, task control.TaskID, id control.ExecutionID) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked[task] {
		return ctx, func() {}, control.ContractRevoked
	}
	if _, ok := s.active[id]; ok {
		return ctx, func() {}, control.AlreadyRunning
	}
	check, cancelCheck := context.WithTimeout(ctx, time.Second)
	err := source.CheckExecutionGrant(check, task, id)
	cancelCheck()
	if err != nil {
		return ctx, func() {}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s.active[id] = supervisedRun{task, cancel}
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, done := context.WithTimeout(ctx, time.Second)
				err := source.CheckExecutionGrant(check, task, id)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	stop := func() { cancel(); s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }
	return ctx, stop, nil
}
func (s *Supervisor) RevokeTask(ctx context.Context, task control.TaskID, commit func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := commit(ctx); err != nil {
		return err
	}
	s.blocked[task] = true
	for _, run := range s.active {
		if run.task == task {
			run.cancel()
		}
	}
	return nil
}
func (s *Supervisor) ResumeTask(task control.TaskID) {
	s.mu.Lock()
	delete(s.blocked, task)
	s.mu.Unlock()
}
