package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/Autumn-27/artex/control"
)

// PreservePlanDependencies makes legacy whole-list writers lossless or explicit failures.
func PreservePlanDependencies(existing, incoming []PlanStep) ([]PlanStep, error) {
	byID := map[int64]int{}
	for i, s := range incoming {
		byID[s.ID] = i
	}
	for _, s := range existing {
		if s.ContextVersion == "" && len(s.RequiresStepIDs) == 0 && len(s.RequiresClaimIDs) == 0 {
			continue
		}
		i, ok := byID[s.ID]
		if !ok {
			return nil, fmt.Errorf("protected step %d requires explicit ID patch", s.ID)
		}
		n := &incoming[i]
		if n.ContextVersion != "" && n.ContextVersion != s.ContextVersion || len(n.RequiresStepIDs) > 0 && !reflect.DeepEqual(n.RequiresStepIDs, s.RequiresStepIDs) || len(n.RequiresClaimIDs) > 0 && !reflect.DeepEqual(n.RequiresClaimIDs, s.RequiresClaimIDs) {
			return nil, fmt.Errorf("dependency changes require explicit step patch")
		}
		n.ContextVersion = s.ContextVersion
		n.RequiresStepIDs = append([]int64(nil), s.RequiresStepIDs...)
		n.RequiresClaimIDs = append([]int64(nil), s.RequiresClaimIDs...)
	}
	return incoming, ValidatePlanDependencies(incoming)
}
func ValidatePlanDependencies(steps []PlanStep) error {
	if len(steps) > PlanMaxSteps {
		return fmt.Errorf("plan capacity exceeded")
	}
	byID := map[int64]PlanStep{}
	hasDependencies := false
	for _, s := range steps {
		if len(s.RequiresStepIDs) > 32 || len(s.RequiresClaimIDs) > 32 || len(s.ContextVersion) > 128 {
			return fmt.Errorf("bounded plan dependencies required")
		}
		if s.ContextVersion != "" || len(s.RequiresStepIDs)+len(s.RequiresClaimIDs) > 0 {
			hasDependencies = true
		}
		if s.ID > 0 {
			if _, ok := byID[s.ID]; ok {
				return fmt.Errorf("duplicate plan step ID")
			}
			byID[s.ID] = s
		}
		for _, id := range s.RequiresClaimIDs {
			if id <= 0 {
				return fmt.Errorf("invalid claim ID")
			}
		}
	}
	if !hasDependencies {
		return nil
	}
	for _, s := range steps {
		if s.ID <= 0 {
			return fmt.Errorf("dependency plans require stable IDs")
		}
	}
	visited := map[int64]uint8{}
	var visit func(int64) error
	visit = func(id int64) error {
		if visited[id] == 1 {
			return fmt.Errorf("plan dependency cycle")
		}
		if visited[id] == 2 {
			return nil
		}
		s, ok := byID[id]
		if !ok {
			return fmt.Errorf("missing predecessor step")
		}
		visited[id] = 1
		seen := map[int64]bool{}
		for _, prior := range s.RequiresStepIDs {
			if seen[prior] {
				return fmt.Errorf("duplicate predecessor")
			}
			seen[prior] = true
			if err := visit(prior); err != nil {
				return err
			}
		}
		visited[id] = 2
		return nil
	}
	for _, s := range steps {
		if err := visit(s.ID); err != nil {
			return err
		}
	}
	return nil
}
func loadControlPlan(ctx context.Context, w observationWriter, exp int64) (*TaskPlan, error) {
	var p TaskPlan
	var raw []byte
	err := w.QueryRowContext(ctx, `SELECT id,revision,steps,model_notes FROM task_plans WHERE exploration_id=$1 FOR UPDATE`, exp).Scan(&p.RowID, &p.Revision, &raw, &p.ModelNotes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.ExplorationID = exp
	err = json.Unmarshal(raw, &p.Steps)
	return &p, err
}
func (d *DB) SaveTaskPlan(plan TaskPlan) (TaskPlan, error) {
	ctx := context.Background()
	if err := ValidatePlanSteps(plan.Steps); err != nil {
		return plan, err
	}
	if err := ValidatePlanNotes(plan.ModelNotes); err != nil {
		return plan, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback()
	var task int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE exploration_id=$1 FOR UPDATE`, plan.ExplorationID).Scan(&task)
	if err != nil && err != sql.ErrNoRows {
		return plan, err
	}
	old, err := loadControlPlan(ctx, tx, plan.ExplorationID)
	if err != nil {
		return plan, err
	}
	if old != nil {
		plan.Steps, err = PreservePlanDependencies(old.Steps, plan.Steps)
		if err != nil {
			return plan, err
		}
	}
	if err = ValidatePlanDependencies(plan.Steps); err != nil {
		return plan, err
	}
	saved, err := saveTaskPlan(ctx, tx, plan)
	if err != nil {
		return saved, err
	}
	return saved, tx.Commit()
}
func (d *DB) PatchPlanStep(ctx context.Context, task control.TaskID, row, revision int64, step PlanStep) (TaskPlan, error) {
	var none TaskPlan
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	var exp int64
	if err = tx.QueryRowContext(ctx, `SELECT exploration_id FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, int64(task)).Scan(&exp); err != nil {
		return none, err
	}
	plan, err := loadControlPlan(ctx, tx, exp)
	if err != nil {
		return none, err
	}
	if plan == nil || plan.RowID != row || plan.Revision != revision {
		return none, &PlanConflictError{ExplorationID: exp}
	}
	found := false
	for i, s := range plan.Steps {
		if s.ID == step.ID {
			plan.Steps[i] = step
			found = true
			break
		}
	}
	if !found {
		return none, control.LineageMismatch
	}
	if err = ValidatePlanSteps(plan.Steps); err != nil {
		return none, err
	}
	if err = ValidatePlanDependencies(plan.Steps); err != nil {
		return none, err
	}
	saved, err := saveTaskPlan(ctx, tx, *plan)
	if err != nil {
		return none, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) VALUES($1,$2,'plan_changed',$3,$4)`, int64(task), fmt.Sprintf("plan:%d", row), saved.Revision, json.RawMessage(fmt.Sprintf(`{"step_id":%d}`, step.ID))); err != nil {
		return none, err
	}
	return saved, tx.Commit()
}
func checkControlClaims(ctx context.Context, q observationWriter, task int64, version string, ids []int64) error {
	if len(ids) > 32 {
		return control.UnboundedEffect
	}
	for _, id := range ids {
		var valid bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control_claims c WHERE c.id=$1 AND c.task_id=$2 AND c.context_version=$3 AND c.validity='valid' AND c.conclusion='supported' AND EXISTS(SELECT 1 FROM control_claim_dependencies d WHERE d.claim_id=c.id) AND NOT EXISTS(SELECT 1 FROM control_claim_dependencies d JOIN harness_observations o ON o.id=d.observation_id JOIN action_executions e ON e.id=o.execution_id JOIN tasks t ON t.id=c.task_id WHERE d.claim_id=c.id AND (o.capture_validity<>'valid' OR NOT o.complete OR o.context_version<>$3 OR e.control_epoch<>t.control_epoch OR EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=c.task_id AND i.observation_id=o.id))))`, id, task, version).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return control.PreconditionInvalid
		}
	}
	return nil
}
func checkControlPlanAction(ctx context.Context, q observationWriter, task, exp int64, p control.ActionProposal) error {
	if err := checkControlClaims(ctx, q, task, p.ContextVersion, p.RequiredClaimIDs); err != nil {
		return err
	}
	if p.StepID == nil {
		return nil
	}
	var raw []byte
	if err := q.QueryRowContext(ctx, `SELECT steps FROM task_plans WHERE exploration_id=$1`, exp).Scan(&raw); err != nil {
		return control.PreconditionInvalid
	}
	var steps []PlanStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return err
	}
	if err := ValidatePlanDependencies(steps); err != nil {
		return control.PreconditionInvalid
	}
	var step *PlanStep
	for i := range steps {
		if steps[i].ID == *p.StepID {
			step = &steps[i]
			break
		}
	}
	if step == nil || step.ContextVersion != p.ContextVersion {
		return control.PreconditionInvalid
	}
	if err := checkControlClaims(ctx, q, task, p.ContextVersion, step.RequiresClaimIDs); err != nil {
		return err
	}
	byID := map[int64]PlanStep{}
	for _, s := range steps {
		byID[s.ID] = s
	}
	visited := map[int64]bool{}
	var require func(int64) error
	require = func(id int64) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		predecessor, ok := byID[id]
		if !ok || predecessor.ContextVersion != p.ContextVersion {
			return control.PreconditionInvalid
		}
		if err := checkControlClaims(ctx, q, task, p.ContextVersion, predecessor.RequiresClaimIDs); err != nil {
			return err
		}
		for _, ancestor := range predecessor.RequiresStepIDs {
			if err := require(ancestor); err != nil {
				return err
			}
		}
		var valid bool
		err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM action_executions e JOIN tasks t ON t.id=e.task_id WHERE e.task_id=$1 AND e.action_json->>'step_id'=$2 AND e.state='succeeded' AND e.context_version=$3 AND e.control_epoch=t.control_epoch AND EXISTS(SELECT 1 FROM harness_observations o WHERE o.execution_id=e.id AND o.capture_validity='valid' AND o.complete AND NOT EXISTS(SELECT 1 FROM control_evidence_invalidations i WHERE i.task_id=$1 AND i.observation_id=o.id)))`, task, fmt.Sprint(id), p.ContextVersion).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return control.PreconditionInvalid
		}
		return nil
	}
	for _, id := range step.RequiresStepIDs {
		if err := require(id); err != nil {
			return err
		}
	}
	return nil
}
