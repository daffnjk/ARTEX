package server

import (
	"context"
	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/report"
	"strconv"
	"time"
)

func (s *Server) renderControlReport(ctx context.Context, task control.TaskID) (string, error) {
	summary, err := s.m.pg.ControlTaskSummary(ctx, task)
	if err != nil {
		return "", err
	}
	packet, packetErr := s.m.pg.BuildControlReviewPacket(ctx, task)
	if packetErr != nil {
		return "", packetErr
	}
	return report.ControlMarkdown(summary, packet), nil
}
func (e *Engine) closeControlledTask(ctx context.Context, t *Task, reason control.StopReason) {
	id, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil {
		return
	}
	request := db.ControlCloseRequest{Reason: reason, Source: "system:controlled-" + string(reason)}
	deadline := time.Now().Add(3 * time.Second)
	for {
		result, err := e.m.pg.TryCloseTask(ctx, control.TaskID(id), request)
		if err != nil {
			e.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: "Controlled close failed; task remains fenced or pending host review"})
			return
		}
		if result.Phase == "closed" {
			t.updateLifecycle(func(state *taskLifecycleState) {
				state.Status = result.LegacyStatus
				if state.CompletedAt == 0 {
					state.CompletedAt = time.Now().Unix()
				}
			})
			e.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: "Controlled task closed: " + result.Result + "; cleanup=" + string(result.Cleanup)})
			return
		}
		e.cancelExec(t.ID, agent.AbortSettleDrainTimeout)
		if request.Handoff {
			return
		}
		if time.Now().After(deadline) {
			request.Handoff = true
			continue
		}
		if sleepCtx(ctx, 25*time.Millisecond) {
			return
		}
	}
}
