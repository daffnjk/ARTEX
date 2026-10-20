package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/execution"
)

func (s *Server) controlStatus(w http.ResponseWriter, r *http.Request) {
	if controlAdminFrom(r.Context()) == "" {
		writeErr(w, 403, "administrator read access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid task")
		return
	}
	snapshot, err := s.m.pg.ControlStatusSnapshot(r.Context(), control.TaskID(id), r.URL.Query().Get("execution_after"), r.URL.Query().Get("evidence_after"))
	if err != nil {
		writeErr(w, 404, "task-scoped control state unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, snapshot)
}
func (s *Server) controlAdminOperation(w http.ResponseWriter, r *http.Request) {
	actor := controlAdminFrom(r.Context())
	if actor == "" {
		writeErr(w, 403, "administrator required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid task")
		return
	}
	task := control.TaskID(id)
	var in struct {
		ExpectedEpoch int64               `json:"expected_epoch"`
		ExecutionID   control.ExecutionID `json:"execution_id,omitempty"`
		Reason        string              `json:"reason,omitempty"`
		Handoff       bool                `json:"handoff,omitempty"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
		writeErr(w, 400, "invalid bounded control action")
		return
	}
	switch r.PathValue("operation") {
	case "cancel":
		if in.ExecutionID == "" || in.Handoff || in.Reason != "" {
			writeErr(w, 400, "execution_id required")
			return
		}
		err = s.m.pg.CancelControlExecutionCAS(r.Context(), task, in.ExecutionID, in.ExpectedEpoch, actor)
		if err == nil {
			ctx, done := context.WithTimeout(r.Context(), time.Second)
			if s.controlGate().CancelRun(ctx, in.ExecutionID) {
				err = s.m.pg.ConfirmControlStopped(ctx, task, in.ExecutionID)
			}
			done()
		}
	case "close":
		if in.ExecutionID != "" {
			writeErr(w, 400, "close is task-scoped")
			return
		}
		var result db.ControlCloseResult
		result, err = s.m.pg.CloseControlTaskCAS(r.Context(), task, in.ExpectedEpoch, actor, db.ControlCloseRequest{Reason: control.StopReason(in.Reason), Handoff: in.Handoff})
		if err == nil {
			if live, ok := s.m.Task(strconv.FormatInt(id, 10)); ok {
				if result.Phase == "closed" {
					live.updateLifecycle(func(state *taskLifecycleState) {
						state.Status = result.LegacyStatus
						state.CompletedAt = time.Now().Unix()
					})
				}
				live.Notify()
			}
		}
	case "retest":
		if in.Handoff || in.ExecutionID == "" {
			writeErr(w, 400, "prior execution required")
			return
		}
		var ref string
		ref, err = s.m.pg.ApproveControlRetest(r.Context(), task, in.ExecutionID, in.Reason, in.ExpectedEpoch, actor)
		if err == nil {
			writeJSON(w, 200, map[string]string{"retest_ref": ref, "notice": "one bounded host approval; no execution submitted"})
			return
		}
	default:
		writeErr(w, 404, "unknown control operation")
		return
	}
	if err != nil {
		if errors.Is(err, control.StaleContext) {
			writeErr(w, 409, "control revision conflict; refresh authoritative state")
		} else {
			writeErr(w, 409, "control action refused by current state or ownership")
		}
		return
	}
	snapshot, err := s.m.pg.ControlStatusSnapshot(r.Context(), task, "", "")
	if err != nil {
		writeErr(w, 503, "action stored; refresh state; do not resubmit execution")
		return
	}
	writeJSON(w, 200, snapshot)
}
func (s *Server) controlExecutionRead(w http.ResponseWriter, r *http.Request) {
	if controlAdminFrom(r.Context()) == "" {
		writeErr(w, 403, "administrator read access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	executionID := control.ExecutionID(r.PathValue("execution"))
	if err != nil || id <= 0 || executionID == "" || len(executionID) > 128 {
		writeErr(w, 400, "invalid execution reference")
		return
	}
	task := control.TaskID(id)
	wait := r.URL.Query().Get("wait_ms")
	if wait != "" {
		ms, err := strconv.Atoi(wait)
		if err != nil || ms < 1 || ms > 1000 {
			writeErr(w, 400, "wait_ms must be 1..1000")
			return
		}
		ctx, done := context.WithTimeout(r.Context(), time.Duration(ms)*time.Millisecond)
		record, err := (execution.Lifecycle{Store: s.m.pg}).Wait(ctx, task, executionID)
		done()
		if errors.Is(err, control.WaitExpired) {
			writeJSON(w, 200, map[string]any{"execution_id": executionID, "feedback": control.WaitExpired, "outcome": record.Outcome})
			return
		}
		if err != nil {
			writeErr(w, 404, "task-scoped execution unavailable")
			return
		}
	}
	record, err := s.m.pg.GetControlExecution(r.Context(), task, executionID)
	if err != nil {
		writeErr(w, 404, "task-scoped execution unavailable")
		return
	}
	raw, err := s.m.pg.ControlExecutionResult(r.Context(), task, executionID)
	if err != nil {
		writeErr(w, 503, "stored result unavailable")
		return
	}
	var result *execution.HTTPResult
	if len(raw) > 0 {
		var stored execution.HTTPResult
		if json.Unmarshal(raw, &stored) != nil {
			writeErr(w, 503, "invalid stored result")
			return
		}
		view := execution.RedactedHTTPResult(stored)
		result = &view
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"execution_id": record.ID, "outcome": record.Outcome, "result": result})
}

// History is a host-only download; it cannot be consumed as live control state.
func (s *Server) controlRestoredHistory(w http.ResponseWriter, r *http.Request) {
	if controlAdminFrom(r.Context()) == "" {
		http.Error(w, "administrator required", 403)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid task", 400)
		return
	}
	task, err := s.m.pg.GetTask(id)
	if err != nil || task == nil {
		http.Error(w, "task unavailable", 404)
		return
	}
	history, err := s.m.pg.ControlRestoreHistory(r.Context(), id)
	if err != nil {
		http.Error(w, "history unavailable", 404)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="restored-control-history.json"`)
	_ = json.NewEncoder(w).Encode(history)
}
