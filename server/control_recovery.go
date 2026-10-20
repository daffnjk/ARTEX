package server

import (
	"context"
	"log"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/execution"
)

type leasedExecutionPolicy struct {
	pg      *db.DB
	attempt execution.Attempt
}

func (p *leasedExecutionPolicy) CheckExecutionGrant(ctx context.Context, task control.TaskID, id control.ExecutionID) error {
	if p.attempt.TaskID != task || p.attempt.ExecutionID != id {
		return control.LineageMismatch
	}
	return p.pg.CheckControlAttempt(ctx, p.attempt)
}
func (p *leasedExecutionPolicy) ExecutionPolicy(ctx context.Context, task control.TaskID, id control.ExecutionID) (execution.Record, control.Contract, error) {
	if err := p.CheckExecutionGrant(ctx, task, id); err != nil {
		return execution.Record{}, control.Contract{}, err
	}
	return p.pg.ExecutionPolicy(ctx, task, id)
}

// reconcileControlCounter is a host-only synthetic fixture protocol, not an agent tool.
// Unregistered remote effects stay unknown and require manual reconciliation.
func (s *Server) reconcileControlCounter(ctx context.Context, task control.TaskID, id control.ExecutionID, intentID int64) error {
	original, err := s.m.pg.GetControlExecution(ctx, task, id)
	if err != nil {
		return err
	}
	if original.Outcome.Run != control.RunLost || original.Proposal.Target.Path != "/increment" {
		return control.ToolUnavailable
	}
	p := original.Proposal
	p.Target.Path = "/effects/" + string(id)
	p.Purpose = "read synthetic effect receipt without replay"
	p.IdempotencyKey = "reconcile:" + string(id)
	ctx = control.WithCallAttribution(ctx, task, "reconcile")
	probe, err := s.m.pg.AdmitAndReserve(ctx, task, p)
	if err != nil {
		return err
	}
	if probe.Outcome.Run == control.RunQueued {
		attempt, err := s.m.pg.ClaimControlExecution(ctx, task, probe.ID, intentID, "reconcile:host")
		if err != nil {
			return err
		}
		result, err := execution.NewHTTPAdapter(&leasedExecutionPolicy{pg: s.m.pg, attempt: attempt}, s.controlGate(), nil, nil).Run(ctx, task, probe.ID)
		if err != nil {
			return err
		}
		if err := s.m.pg.CommitControlHTTPResult(ctx, attempt, result); err != nil {
			return err
		}
	} else if probe.Outcome.Run != control.RunSucceeded {
		return control.EffectUncertain
	}
	return s.m.pg.ReconcileControlEffect(ctx, task, id, probe.ID)
}

// Recovery only fences records; it never resubmits network work or refunds unknown costs.
func (s *Server) startControlRecovery(ctx context.Context) {
	if ctx.Done() == nil {
		return
	}
	s.controlRecoveryOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					pass, done := context.WithTimeout(ctx, 2*time.Second)
					count, err := s.m.pg.RecoverControlLeases(pass, time.Now(), 128)
					done()
					if err != nil {
						log.Printf("[control] bounded recovery failed; unknown reservations retained")
					} else if count > 0 {
						log.Printf("[control] fenced %d expired executions; no replay or refund", count)
					}
				}
			}
		}()
	})
}
