package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/harness"
)

type ControlCloseRequest struct {
	Reason  control.StopReason
	Source  string
	Handoff bool
}
type ControlCloseResult struct {
	Phase, Run, Result string
	Reason             control.StopReason
	Cleanup            control.CleanupStatus
	LegacyStatus       string
	Won                bool
}
type ControlTaskSummary struct {
	TaskID                                                                               control.TaskID
	Mode                                                                                 string
	Close                                                                                ControlCloseResult
	ValidObservations, ValidClaims, UnknownClaims, UncertainEffects, UnknownReservations int
	Unknown                                                                              []string
}

func (d *DB) TryCloseTask(ctx context.Context, task control.TaskID, request ControlCloseRequest) (ControlCloseResult, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return ControlCloseResult{}, err
	}
	defer tx.Rollback()
	result, err := closeControlTaskTx(ctx, tx, int64(task), request)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func closeReason(status, source string) control.StopReason {
	switch status {
	case "done":
		return control.StopComplete
	case "timeout":
		if strings.Contains(source, "budget") {
			return control.StopBudget
		}
		return control.StopDeadline
	default:
		return control.StopUnknown
	}
}
func validCloseClaim(ctx context.Context, tx *sql.Tx, task int64, id int64, version string, epoch int64) bool {
	var reportRaw []byte
	err := tx.QueryRowContext(ctx, `SELECT e.report FROM control_claims c JOIN harness_evaluations e ON e.request_id=c.request_id WHERE c.id=$1 AND c.task_id=$2 AND c.context_version=$3 AND c.validity='valid' AND c.conclusion='supported'`, id, task, version).Scan(&reportRaw)
	if err != nil {
		return false
	}
	var report harness.EvaluationReport
	if json.Unmarshal(reportRaw, &report) != nil || len(report.CheckedEvidence) == 0 {
		return false
	}
	for _, ref := range report.CheckedEvidence {
		o, err := resolveControlObservation(ctx, tx, task, ref.ObservationID)
		if err != nil || !o.Complete || o.ContextVersion != version || harness.Digest(o.Payload) != ref.SHA256 {
			return false
		}
		var valid bool
		if err = tx.QueryRowContext(ctx, `SELECT o.capture_validity='valid' AND e.control_epoch=$3 FROM harness_observations o JOIN action_executions e ON e.id=o.execution_id WHERE o.id=$1 AND o.task_id=$2`, ref.ObservationID, task, epoch).Scan(&valid); err != nil || !valid {
			return false
		}
	}
	return true
}
func closeControlTaskTx(ctx context.Context, tx *sql.Tx, task int64, request ControlCloseRequest) (ControlCloseResult, error) {
	var out ControlCloseResult
	if strings.TrimSpace(request.Source) == "" || len(request.Source) > 255 {
		return out, control.LineageMismatch
	}
	switch request.Reason {
	case control.StopComplete, control.StopBudget, control.StopDeadline, control.StopRevoked, control.StopUnknown:
	default:
		return out, control.PreconditionInvalid
	}
	var exp, epoch int64
	var mode, phase, status, version string
	var revoked bool
	var outcome, priorRequest []byte
	if err := tx.QueryRowContext(ctx, `SELECT exploration_id,control_mode,control_phase,status,control_context_version,control_epoch,control_revoked,control_outcome,control_close_request FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, task).Scan(&exp, &mode, &phase, &status, &version, &epoch, &revoked, &outcome, &priorRequest); err != nil {
		return out, err
	}
	if mode == "legacy" {
		return out, control.ContractMissing
	}
	if phase == "closed" || IsTerminal(status) {
		if len(outcome) > 0 {
			if err := json.Unmarshal(outcome, &out); err != nil {
				return out, err
			}
		} else {
			out = ControlCloseResult{Phase: "closed", Run: "closed", Result: "inconclusive", Reason: control.StopUnknown, LegacyStatus: status, Cleanup: control.CleanupUnconfirmed}
		}
		out.Won = false
		return out, nil
	}
	var frozen struct {
		Request ControlCloseRequest
		Epoch   int64
	}
	if len(priorRequest) > 0 {
		if err := json.Unmarshal(priorRequest, &frozen); err != nil {
			return out, err
		}
		if request.Handoff {
			frozen.Request.Handoff = true
		}
		request = frozen.Request
		epoch = frozen.Epoch
	} else {
		frozen.Request = request
		frozen.Epoch = epoch
	}
	var body []byte
	var contract control.Contract
	if err := tx.QueryRowContext(ctx, `SELECT body FROM task_control_contracts c JOIN tasks t ON t.id=c.task_id AND t.contract_version=c.version WHERE t.id=$1`, task).Scan(&body); err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &contract); err != nil {
		return out, err
	}
	if len(priorRequest) == 0 {
		if request.Reason == control.StopBudget {
			var exhausted bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_budget_accounts WHERE task_id=$1 AND (overrun OR used_units+reserved_units>=limit_units-close_reserve_units))`, task).Scan(&exhausted); err != nil {
				return out, err
			}
			if !exhausted {
				return out, control.PreconditionInvalid
			}
		}
		if request.Reason == control.StopDeadline {
			var expired bool
			if err := tx.QueryRowContext(ctx, `SELECT deadline_at IS NOT NULL AND deadline_at<=now() FROM tasks WHERE id=$1`, task).Scan(&expired); err != nil {
				return out, err
			}
			if !expired {
				return out, control.PreconditionInvalid
			}
		}
		if request.Reason == control.StopRevoked && !revoked && contract.ExpiresAt.After(time.Now()) {
			return out, control.PreconditionInvalid
		}
	}
	validObservations := 0
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM harness_observations o JOIN action_executions e ON e.id=o.execution_id WHERE o.task_id=$1 AND o.context_version=$2 AND e.control_epoch=$3 AND o.capture_validity='valid' AND o.complete AND NOT EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id)`, task, version, epoch).Scan(&validObservations); err != nil {
		return out, err
	}
	if request.Reason == control.StopComplete {
		if revoked || !contract.ExpiresAt.After(time.Now()) {
			return out, control.ContractExpired
		}
		if mode == string(control.ModeDirect) {
			var total, invalid int
			if err := tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE e.state<>'succeeded' OR e.context_version<>$2 OR e.control_epoch<>$3 OR NOT EXISTS(SELECT 1 FROM harness_observations o WHERE o.execution_id=e.id AND o.capture_validity='valid' AND o.complete AND NOT EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id))) FROM action_executions e WHERE e.task_id=$1`, task, version, epoch).Scan(&total, &invalid); err != nil {
				return out, err
			}
			if total == 0 || invalid > 0 {
				return out, control.EvidenceUnavailable
			}
		} else {
			rows, err := tx.QueryContext(ctx, `SELECT a.goal_id,a.version,a.context_version,a.criterion_id,a.criterion_version,a.claim,a.experiment_id,n.id FROM harness_acceptance a LEFT JOIN exploration_nodes n ON n.id=a.goal_id AND n.exploration_id=$2 AND n.kind='goal' WHERE a.task_id=$1 AND a.is_current ORDER BY a.goal_id`, task, exp)
			if err != nil {
				return out, err
			}
			type accepted struct {
				goal                                                             int64
				version, context, criterion, criterionVersion, claim, experiment string
				node                                                             sql.NullInt64
			}
			var specs []accepted
			for rows.Next() {
				var a accepted
				if err = rows.Scan(&a.goal, &a.version, &a.context, &a.criterion, &a.criterionVersion, &a.claim, &a.experiment, &a.node); err != nil {
					rows.Close()
					return out, err
				}
				specs = append(specs, a)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, err
			}
			if len(specs) == 0 {
				return out, ErrCompletionAcceptanceUnmet
			}
			var unmatched int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM exploration_nodes n WHERE n.exploration_id=$2 AND n.kind='goal' AND NOT EXISTS(SELECT 1 FROM harness_acceptance a WHERE a.task_id=$1 AND a.goal_id=n.id AND a.is_current)`, task, exp).Scan(&unmatched); err != nil {
				return out, err
			}
			if unmatched > 0 {
				return out, ErrCompletionAcceptanceUnmet
			}
			for _, a := range specs {
				if !a.node.Valid || a.version != contract.AcceptanceVersion || a.context != version {
					return out, ErrCompletionAcceptanceUnmet
				}
				rows, err = tx.QueryContext(ctx, `SELECT c.id FROM control_claims c JOIN harness_evaluations e ON e.request_id=c.request_id JOIN hypotheses h ON h.id=c.hypothesis_id WHERE c.task_id=$1 AND e.goal_id=$2 AND e.criterion_id=$3 AND e.criterion_version=$4 AND e.experiment_id=$5 AND e.report->>'acceptance_version'=$6 AND h.statement=$7`, task, a.goal, a.criterion, a.criterionVersion, a.experiment, a.version, a.claim)
				if err != nil {
					return out, err
				}
				var ids []int64
				for rows.Next() {
					var id int64
					if err = rows.Scan(&id); err != nil {
						rows.Close()
						return out, err
					}
					ids = append(ids, id)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return out, err
				}
				valid := false
				for _, id := range ids {
					valid = valid || validCloseClaim(ctx, tx, task, id, version, epoch)
				}
				if !valid {
					return out, ErrCompletionAcceptanceUnmet
				}
			}
		}
	}
	requestRaw, _ := json.Marshal(frozen)
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET control_phase='draining',control_epoch=CASE WHEN control_close_request IS NULL THEN control_epoch+1 ELSE control_epoch END,control_close_request=COALESCE(control_close_request,$2),control_stop_reason=$3 WHERE id=$1`, task, requestRaw, string(request.Reason)); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM action_executions WHERE task_id=$1 AND state='queued' FOR UPDATE`, task)
	if err != nil {
		return out, err
	}
	var queued []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		queued = append(queued, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	zero := int64(0)
	for _, id := range queued {
		if err = settleControlBudgetTx(ctx, tx, task, id, map[string]*int64{"requests": &zero, "response_bytes": &zero, "concurrency": &zero}); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET state='cancelled',effect_status='known_not_applied',cleanup_status='not_required',cancel_requested=true WHERE id=$1`, id); err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_executions SET cancel_requested=true WHERE task_id=$1 AND state='running'`, task); err != nil {
		return out, err
	}
	var running, unconfirmed, applied int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state='running'),count(*) FILTER(WHERE cleanup_status IN ('pending','unconfirmed','failed') OR state='running'),count(*) FROM action_executions WHERE task_id=$1`, task).Scan(&running, &unconfirmed, &applied); err != nil {
		return out, err
	}
	out = ControlCloseResult{Phase: "closed", Run: "closed", Result: "inconclusive", Reason: request.Reason, Cleanup: control.CleanupNotRequired, LegacyStatus: control.LegacyStatus(request.Reason)}
	if applied > 0 {
		out.Cleanup = control.CleanupConfirmed
	}
	if unconfirmed > 0 {
		out.Cleanup = control.CleanupUnconfirmed
	}
	if running > 0 && !request.Handoff {
		out.Phase = "draining"
		out.Run = "draining"
		out.LegacyStatus = status
	} else {
		out.Won = true
	}
	if request.Reason == control.StopComplete {
		out.Result = "complete"
	} else if request.Reason == control.StopRevoked {
		out.Result = "aborted"
	} else if validObservations > 0 {
		out.Result = "partial"
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tasks SET status=CASE WHEN $2='closed' THEN $3 ELSE status END,control_phase=$2,control_outcome=$4,control_cleanup=$5,completed_at=CASE WHEN $2='closed' THEN COALESCE(completed_at,now()) ELSE completed_at END,completed_source=CASE WHEN $2='closed' THEN $6 ELSE completed_source END WHERE id=$1`, task, out.Phase, out.LegacyStatus, raw, string(out.Cleanup), request.Source); err != nil {
		return out, err
	}
	if out.Phase == "closed" && out.Result == "complete" && mode != string(control.ModeDirect) {
		if _, err = tx.ExecContext(ctx, `UPDATE exploration_nodes n SET state='met',updated_at=now() WHERE n.exploration_id=$2 AND n.kind='goal' AND EXISTS(SELECT 1 FROM harness_acceptance a WHERE a.task_id=$1 AND a.goal_id=n.id AND a.is_current)`, task, exp); err != nil {
			return out, err
		}
	}
	event := "task_draining"
	if out.Phase == "closed" {
		event = "task_closed"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,$3,1,$4) ON CONFLICT DO NOTHING`, task, fmt.Sprintf("task:%d", task), event, raw); err != nil {
		return out, err
	}
	return out, nil
}
func (d *DB) ControlTaskSummary(ctx context.Context, task control.TaskID) (ControlTaskSummary, error) {
	s := ControlTaskSummary{TaskID: task}
	var raw []byte
	err := d.QueryRowContext(ctx, `SELECT control_mode,control_outcome,control_phase,status,control_cleanup FROM tasks WHERE id=$1 AND deleted_at IS NULL`, int64(task)).Scan(&s.Mode, &raw, &s.Close.Phase, &s.Close.LegacyStatus, &s.Close.Cleanup)
	if err != nil {
		return s, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &s.Close); err != nil {
			return s, err
		}
	}
	err = d.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM harness_observations o WHERE o.task_id=$1 AND o.capture_validity='valid' AND o.complete AND NOT EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id)),(SELECT count(*) FROM control_claims WHERE task_id=$1 AND validity='valid' AND conclusion='supported'),(SELECT count(*) FROM control_claims WHERE task_id=$1 AND (validity<>'valid' OR conclusion='inconclusive')),(SELECT count(*) FROM action_executions WHERE task_id=$1 AND effect_status='unknown'),(SELECT count(*) FROM task_budget_reservations WHERE task_id=$1 AND settlement_state='unknown')`, int64(task)).Scan(&s.ValidObservations, &s.ValidClaims, &s.UnknownClaims, &s.UncertainEffects, &s.UnknownReservations)
	s.Unknown = []string{"unanswered or untested claims remain unknown; no global coverage is inferred"}
	if s.Close.Cleanup == control.CleanupUnconfirmed {
		s.Unknown = append(s.Unknown, "cleanup requires explicit host handoff")
	}
	return s, err
}
