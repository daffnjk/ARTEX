package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
)

type ControlScoreCandidate struct {
	ExperimentID                 int64
	Proposal                     control.ActionProposal
	Predictions                  []string
	GoalBenefit, CoverageBenefit int
}
type ControlShadowScore struct {
	ExperimentID                                                               int64
	Index, Discrimination, Goal, Coverage, Cost, Risk, DuplicatePenalty, Score int
	Excluded                                                                   bool
	Reason                                                                     string
}

func (d *DB) ScoreControlCandidates(ctx context.Context, task control.TaskID, candidates []ControlScoreCandidate) ([]ControlShadowScore, error) {
	if len(candidates) > 32 {
		return nil, control.UnboundedEffect
	}
	contract, err := d.ActiveControlContract(ctx, int64(task))
	if err != nil {
		return nil, err
	}
	var exp int64
	var version string
	if err = d.QueryRowContext(ctx, `SELECT exploration_id,control_context_version FROM tasks WHERE id=$1`, int64(task)).Scan(&exp, &version); err != nil {
		return nil, err
	}
	budgets, err := d.ControlBudgetAccounts(ctx, int64(task))
	if err != nil {
		return nil, err
	}
	var uncertain bool
	if err = d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM action_executions WHERE task_id=$1 AND state IN ('lost','failed','cancelled','timed_out') AND effect_status='unknown')`, int64(task)).Scan(&uncertain); err != nil {
		return nil, err
	}
	scores := make([]ControlShadowScore, 0, len(candidates))
	for i, c := range candidates {
		score := ControlShadowScore{Index: i, ExperimentID: c.ExperimentID}
		p := c.Proposal
		reason := ""
		if uncertain {
			reason = "effect_unknown"
		}
		method := false
		identity := false
		for _, m := range contract.Body.Methods {
			method = method || m.ID == p.MethodID && m.Version == p.MethodVersion
		}
		id := "anonymous"
		if p.IdentityRef != nil {
			id = *p.IdentityRef
		}
		for _, ref := range contract.Body.IdentityRefs {
			identity = identity || ref == id
		}
		normalized, normalizeErr := control.NormalizeTarget(p.Target.URL())
		if p.Validate() != nil || normalizeErr != nil || normalized != p.Target || p.ContextVersion != version {
			reason = "invalid_or_stale_proposal"
		} else if !method || p.MethodID != "http.get" || p.MethodVersion != "1" || !identity || id != "anonymous" || control.MatchTarget(contract.Body.Targets, p.Target) != nil {
			reason = "policy_denied"
		}
		if reason == "" && p.Target.Host != "localhost" {
			ip, e := netip.ParseAddr(p.Target.Host)
			if e != nil || !ip.IsLoopback() {
				reason = "local_adapter_only"
			}
		}
		if reason == "" && p.ExperimentID != nil {
			var owned bool
			if err = d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM experiments WHERE id=$1 AND exploration_id=$2)`, *p.ExperimentID, exp).Scan(&owned); err != nil {
				return nil, err
			}
			if !owned {
				reason = "experiment_lineage_mismatch"
			}
		}
		if reason == "" {
			if err = checkControlPlanAction(ctx, d, int64(task), exp, p); err != nil {
				reason = "precondition_invalid"
			}
		}
		for dimension, units := range map[string]int64{"requests": execution.HTTPMaxRequests, "response_bytes": execution.HTTPMaxRequests * execution.HTTPMaxResponseBytes, "concurrency": 1} {
			found := false
			for _, b := range budgets {
				if b.Dimension == dimension {
					found = true
					if b.Overrun || b.Limit-b.Used-b.Reserved-b.CloseReserve < units {
						reason = "budget_unavailable"
					}
				}
			}
			if !found {
				reason = "budget_unavailable"
			}
		}
		if len(c.Predictions) > 8 || c.GoalBenefit < 0 || c.GoalBenefit > 100 || c.CoverageBenefit < 0 || c.CoverageBenefit > 100 {
			reason = "invalid_proxy_input"
		}
		distinct := map[string]bool{}
		for _, prediction := range c.Predictions {
			if len(prediction) > 4096 {
				reason = "invalid_proxy_input"
			}
			if strings.TrimSpace(prediction) != "" {
				distinct[strings.TrimSpace(prediction)] = true
			}
		}
		if reason != "" {
			score.Excluded = true
			score.Reason = reason
			scores = append(scores, score)
			continue
		}
		if len(distinct) > 1 {
			score.Discrimination = len(distinct) - 1
		}
		score.Goal = c.GoalBenefit
		score.Coverage = c.CoverageBenefit
		score.Cost = 3
		score.Risk = 1
		base, e := controlBaseActionKey(p)
		if e != nil {
			return nil, e
		}
		var duplicate bool
		if err = d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM action_executions WHERE task_id=$1 AND base_key=$2)`, int64(task), base).Scan(&duplicate); err != nil {
			return nil, err
		}
		if duplicate {
			score.DuplicatePenalty = 20
		}
		score.Score = 10*score.Discrimination + score.Goal + score.Coverage - score.Cost - score.Risk - score.DuplicatePenalty
		score.Reason = "uncalibrated_proxy_shadow_only"
		scores = append(scores, score)
	}
	raw, err := json.Marshal(scores)
	if err != nil {
		return nil, err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO control_score_snapshots(task_id,context_version,scorer_version,mode,scores) VALUES($1,$2,'proxy-v1','shadow',$3)`, int64(task), version, raw)
	return scores, err
}
func (d *DB) RecordControlFeedback(ctx context.Context, task control.TaskID, id control.ExecutionID, kind control.FeedbackKind, retryAfter time.Duration) error {
	decision := control.DecideFeedback(kind, retryAfter)
	raw, err := json.Marshal(struct {
		Kind        control.FeedbackKind
		ExecutionID control.ExecutionID
		Decision    control.FeedbackDecision
	}{kind, id, decision})
	if err != nil {
		return err
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var root int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE id=$1 AND control_mode<>'legacy' AND deleted_at IS NULL FOR UPDATE`, int64(task)).Scan(&root); err != nil {
		return err
	}
	if kind == control.FeedbackRateLimited {
		if _, err = tx.ExecContext(ctx, `UPDATE tasks SET control_cooldown_until=GREATEST(control_cooldown_until,$2) WHERE id=$1`, int64(task), time.Now().UTC().Add(decision.Cooldown)); err != nil {
			return err
		}
	}
	// Generated host identity distinguishes actual feedback events; model cannot set the deadline.
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) SELECT id,'feedback:'||id::text||':'||nextval('control_feedback_seq')::text,'control_feedback',1,$2 FROM tasks WHERE id=$1`, int64(task), raw); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) PrepareControlPlanningWindow(ctx context.Context, task control.TaskID) (control.PlanningWindow, error) {
	var state control.PlanningWindow
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	var exp, epoch int64
	var version, phase string
	var revoked bool
	if err = tx.QueryRowContext(ctx, `SELECT exploration_id,control_context_version,control_epoch,control_phase,control_revoked FROM tasks WHERE id=$1 AND control_mode<>'legacy' AND deleted_at IS NULL FOR UPDATE`, int64(task)).Scan(&exp, &version, &epoch, &phase, &revoked); err != nil {
		return state, err
	}
	if revoked || phase != "accepting" {
		return state, control.ContractRevoked
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT state FROM control_planner_windows WHERE task_id=$1`, int64(task)).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return state, err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &state); err != nil {
			return state, err
		}
	}
	if state.ContextVersion != version {
		state.EmptyRounds = 0
		state.Paused = false
		state.ContextVersion = version
	}
	state.Version = "window-v1"
	state.Collect = nil
	state.Feedback = nil
	state.AllowNew = true
	state.Reason = "short_window_one_intent"
	var newest sql.NullTime
	var newCount int
	if err = tx.QueryRowContext(ctx, `SELECT max(o.created_at),count(*) FILTER (WHERE o.created_at>$4) FROM harness_observations o JOIN action_executions e ON e.id=o.execution_id WHERE o.task_id=$1 AND o.context_version=$2 AND e.control_epoch=$3 AND o.capture_validity='valid' AND o.complete AND NOT EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id)`, int64(task), version, epoch, state.ObservationWatermark).Scan(&newest, &newCount); err != nil {
		return state, err
	}
	if newCount > 0 {
		state.EmptyRounds = 0
		state.Paused = false
		state.ObservationWatermark = newest.Time
	}
	if state.EmptyRounds >= 3 {
		state.Paused = true
		state.AllowNew = false
		state.Reason = "no_new_valid_evidence_after_three_windows"
	} else {
		state.EmptyRounds++
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(used_units+reserved_units),0) FROM task_budget_accounts WHERE task_id=$1 AND dimension IN ('requests','llm_requests')`, int64(task)).Scan(&state.BudgetBurn); err != nil {
		return state, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,event_type,payload FROM control_outbox WHERE task_id=$1 AND id>$2 AND event_type<>'execution_admitted' ORDER BY id LIMIT 128`, int64(task), state.LastEventID)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var eventID int64
		var kind string
		var payload []byte
		if err = rows.Scan(&eventID, &kind, &payload); err != nil {
			rows.Close()
			return state, err
		}
		state.LastEventID = eventID
		switch kind {
		case "control_feedback":
			var f struct {
				Kind     control.FeedbackKind
				Decision control.FeedbackDecision
			}
			if err = json.Unmarshal(payload, &f); err != nil {
				rows.Close()
				return state, err
			}
			decision := control.DecideFeedback(f.Kind, f.Decision.Cooldown)
			state.Feedback = append(state.Feedback, decision)
			if !decision.AllowNew {
				state.AllowNew = false
			}
			if decision.Cooldown > 0 {
				state.CooldownUntil = time.Now().Add(decision.Cooldown)
			}
		case "evidence_invalidated", "plan_changed", "outcome_committed", "execution_finished":
			state.Feedback = append(state.Feedback, control.DecideFeedback(control.FeedbackValidResult, 0))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,state FROM action_executions WHERE task_id=$1 AND (state IN ('queued','running') OR state IN ('lost','failed','cancelled','timed_out') AND effect_status='unknown') ORDER BY created_at LIMIT 128`, int64(task))
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var id control.ExecutionID
		var run string
		if err = rows.Scan(&id, &run); err != nil {
			rows.Close()
			return state, err
		}
		state.Collect = append(state.Collect, id)
		state.AllowNew = false
		if run == "lost" {
			state.Reason = "reconcile_original_unknown_effect"
		} else {
			state.Reason = "collect_original_before_new_experiment"
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control_outbox WHERE task_id=$1 AND id>$2 AND event_type<>'execution_admitted')`, int64(task), state.LastEventID).Scan(&state.Backlog); err != nil {
		return state, err
	}
	if state.Backlog {
		state.AllowNew = false
		state.Reason = "drain_feedback_queue"
	}
	if time.Now().Before(state.CooldownUntil) {
		state.AllowNew = false
		state.Reason = "rate_limit_cooldown"
	}
	raw, err = json.Marshal(state)
	if err != nil {
		return state, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_planner_windows(task_id,state) VALUES($1,$2) ON CONFLICT(task_id) DO UPDATE SET state=excluded.state,updated_at=now()`, int64(task), raw); err != nil {
		return state, err
	}
	return state, tx.Commit()
}
func (s *ExplorationStore) PrepareControlPlanningWindow(ctx context.Context, task int64) (control.PlanningWindow, error) {
	if _, err := s.ControlledBinding(ctx, task); err != nil {
		return control.PlanningWindow{}, err
	}
	window, err := s.db.PrepareControlPlanningWindow(ctx, control.TaskID(task))
	if err != nil {
		return window, err
	}
	// Shadow snapshots never replace baseline ordering or dispatch an action.
	experiments, err := s.OpenExperiments()
	if err != nil {
		return window, err
	}
	candidates := []ControlScoreCandidate{}
	for _, e := range experiments {
		if len(candidates) >= 32 {
			break
		}
		p, decodeErr := control.DecodeProposal([]byte(e.Action))
		if decodeErr != nil {
			p = control.ActionProposal{}
		}
		predictions := []string{}
		for _, prediction := range e.Expectations {
			predictions = append(predictions, prediction)
		}
		candidates = append(candidates, ControlScoreCandidate{ExperimentID: e.ID, Proposal: p, Predictions: predictions, GoalBenefit: 1})
	}
	_, err = s.db.ScoreControlCandidates(ctx, control.TaskID(task), candidates)
	return window, err
}
