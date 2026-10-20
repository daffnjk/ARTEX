package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ControlArchiveSnapshot is historical data, never input to live authority or dispatch.
type ControlArchiveSnapshot struct {
	SourceTaskID        int64                      `json:"source_task_id"`
	SourceExplorationID int64                      `json:"source_exploration_id"`
	TaskID              int64                      `json:"task_id"`
	ExplorationID       int64                      `json:"exploration_id"`
	Restored            bool                       `json:"restored"`
	ReferenceMap        map[string]string          `json:"reference_map,omitempty"`
	Tables              map[string]json.RawMessage `json:"tables"`
}

func snapshotControlArchive(tx *sql.Tx, task, exp int64) (*ControlArchiveSnapshot, error) {
	var mode string
	if err := tx.QueryRow(`SELECT control_mode FROM tasks WHERE id=$1`, task).Scan(&mode); err != nil {
		return nil, err
	}
	if mode == "legacy" {
		return nil, nil
	}
	out := &ControlArchiveSnapshot{SourceTaskID: task, SourceExplorationID: exp, TaskID: task, ExplorationID: exp, Tables: map[string]json.RawMessage{}}
	// Contract metadata is retained without identity references or reusable grants.
	for _, q := range []struct {
		name, sql string
		id        int64
	}{
		{"task_control_contracts", `SELECT task_id,version,body-'identity_refs' AS body,digest,approved_actor,approved_at,expires_at FROM task_control_contracts WHERE task_id=$1 ORDER BY version`, task},
		{"control_contract_events", `SELECT * FROM control_contract_events WHERE task_id=$1 ORDER BY id`, task},
		{"task_budget_accounts", `SELECT * FROM task_budget_accounts WHERE task_id=$1 ORDER BY dimension`, task},
		{"task_budget_reservations", `SELECT * FROM task_budget_reservations WHERE task_id=$1 ORDER BY request_key,dimension`, task},
		{"action_executions", `SELECT id,task_id,exploration_id,action_json,digest,context_version,state,effect_status,cleanup_status,result_json,result_digest,intent_id FROM action_executions WHERE task_id=$1 ORDER BY id`, task},
		{"execution_attempts", `SELECT a.id,a.execution_id,a.attempt_no,a.result_kind,a.effect_status,a.cleanup_status,a.intent_id FROM execution_attempts a JOIN action_executions e ON e.id=a.execution_id WHERE e.task_id=$1 ORDER BY a.id`, task},
		{"harness_observations", `SELECT * FROM harness_observations WHERE task_id=$1 ORDER BY id`, task},
		{"harness_evaluations", `SELECT * FROM harness_evaluations WHERE task_id=$1 ORDER BY id`, task},
		{"harness_acceptance", `SELECT * FROM harness_acceptance WHERE task_id=$1 ORDER BY id`, task},
		{"control_claims", `SELECT * FROM control_claims WHERE task_id=$1 ORDER BY request_id`, task},
		{"control_claim_dependencies", `SELECT d.* FROM control_claim_dependencies d JOIN control_claims c ON c.id=d.claim_id WHERE c.task_id=$1 ORDER BY d.claim_id,d.observation_id`, task},
		{"control_evidence_invalidations", `SELECT * FROM control_evidence_invalidations WHERE task_id=$1 ORDER BY observation_id`, task},
		{"task_plans", `SELECT * FROM task_plans WHERE exploration_id=$1 ORDER BY id`, exp},
		{"hypotheses", `SELECT * FROM hypotheses WHERE exploration_id=$1 ORDER BY id`, exp},
		{"experiments", `SELECT * FROM experiments WHERE exploration_id=$1 ORDER BY id`, exp},
		{"hypothesis_events", `SELECT e.* FROM hypothesis_events e JOIN hypotheses h ON h.id=e.hypothesis_id WHERE h.exploration_id=$1 ORDER BY e.id`, exp},
		{"hypothesis_reviews", `SELECT * FROM hypothesis_reviews WHERE exploration_id=$1 ORDER BY id`, exp},
	} {
		raw, _, err := queryArchiveRows(tx, q.sql, q.id)
		if err != nil {
			return nil, fmt.Errorf("control archive %s: %w", q.name, err)
		}
		out.Tables[q.name] = raw
	}
	return out, nil
}

// RestoreControlTaskArchive quarantines history under a fresh, paused identity.
// The cold package remains retained: raw traffic/files are not silently discarded.
func (d *DB) RestoreControlTaskArchive(ctx context.Context, archive int64, snap *TaskArchiveSnapshot) (int64, error) {
	if snap == nil || snap.Control == nil || snap.FormatVersion != TaskArchiveFormatVersion || snap.Control.SourceTaskID != snap.TaskID || snap.Control.SourceExplorationID != snap.ExplorationID {
		return 0, ErrTaskArchiveFormatMismatch
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = coordinateWithSchemaMigration(tx); err != nil {
		return 0, err
	}
	var source int64
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT task_id,state FROM task_archives WHERE id=$1 FOR UPDATE`, archive).Scan(&source, &state); err != nil {
		return 0, err
	}
	if source != snap.TaskID {
		return 0, ErrTaskArchiveState
	}
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT task_id FROM control_restored_history WHERE archive_id=$1`, archive).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	if state != Restoring {
		return 0, ErrTaskArchiveState
	}
	rows, err := decodeArchiveRows(snap.Tables["tasks"])
	if err != nil || len(rows) != 1 {
		return 0, ErrTaskArchiveFormatMismatch
	}
	row := rows[0]
	mode, _ := row["control_mode"].(string)
	if mode != "direct" && mode != "goal" && mode != "coverage" {
		return 0, ErrTaskArchiveFormatMismatch
	}
	var exp, task int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO explorations(description,goal) VALUES($1,$2) RETURNING id`, row["description"], row["goal"]).Scan(&exp); err != nil {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO tasks(name,description,goal,exploration_id,status,paused,queued,coverage_enabled,control_mode,control_revoked,control_context_version) VALUES($1,$2,$3,$4,'paused',true,false,false,$5,true,'restored') RETURNING id`, row["name"], row["description"], row["goal"], exp, mode).Scan(&task); err != nil {
		return 0, err
	}
	// Stable historical IDs stay namespaced to their source; only this map relates them
	// to the new identity. No live claim/acceptance/plan/action is inserted.
	history := *snap.Control
	history.TaskID = task
	history.ExplorationID = exp
	history.Restored = true
	history.ReferenceMap = map[string]string{fmt.Sprintf("task:%d", source): fmt.Sprintf("task:%d", task), fmt.Sprintf("exploration:%d", snap.ExplorationID): fmt.Sprintf("exploration:%d", exp)}
	history.Tables = make(map[string]json.RawMessage, len(snap.Control.Tables)+3)
	for k, v := range snap.Control.Tables {
		history.Tables[k] = v
	}
	for _, k := range []string{"exploration_nodes", "exploration_edges", "exploration_anchors"} {
		history.Tables[k] = snap.Tables[k]
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_restored_history(task_id,archive_id,source_task_id,history) VALUES($1,$2,$3,$4)`, task, archive, source, raw); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_archives SET state='ready',phase='restored_read_only',progress=100,error='',warnings=jsonb_build_array($2::text) WHERE id=$1`, archive, fmt.Sprintf("Restored read-only history to task %d; fresh approval and plan required; cold files retained", task)); err != nil {
		return 0, err
	}
	return task, tx.Commit()
}
func (d *DB) ControlRestoreHistory(ctx context.Context, task int64) (ControlArchiveSnapshot, error) {
	var h ControlArchiveSnapshot
	var raw []byte
	err := d.QueryRowContext(ctx, `SELECT history FROM control_restored_history WHERE task_id=$1`, task).Scan(&raw)
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(raw, &h)
	return h, err
}
