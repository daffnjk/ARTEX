package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Autumn-27/artex/harness"
)

// M2.2（research）：权威冻结验收源（M1a.1 AcceptanceResolver 的 PG 实现）。
//
// 信任边界：仅宿主可写（SetHarnessAcceptance）；解析按 (task, goal) 取当前
// 版本快照——请求永远不能选择 criterion/版本。版本历史不可变（触发器），版本
// 号不复用（UNIQUE）。评估内核在验证后重读本源：版本指针在验证期间被推进的
// 话，内核按 acceptance_changed 拒绝（M1a 语义），最终数据库完成还须在事务内
// 原子重校验（M1b.3b 接入）。

// ErrAcceptanceVersionReused 表示版本号已存在（版本号永不复用）。
var ErrAcceptanceVersionReused = errors.New("harness acceptance version already exists (versions are never reused)")

// SetHarnessAcceptance 以宿主身份写入/推进某 task+goal 的当前冻结验收版本。
// 完全相同的当前版本幂等成功；新版本在单事务内完成"旧指针降级 + 新行插入"。
func (d *DB) SetHarnessAcceptance(spec harness.AcceptanceSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 幂等：当前版本完全一致 → 无操作。
	var cur harness.AcceptanceSpec
	var curVersion string
	err = tx.QueryRow(`SELECT version, criterion_id, criterion_version, claim, experiment_id, context_version
		FROM harness_acceptance WHERE task_id=$1 AND goal_id=$2 AND is_current`, spec.TaskID, spec.GoalID).
		Scan(&curVersion, &cur.Criterion.ID, &cur.Criterion.Version, &cur.Claim, &cur.ExperimentID, &cur.ContextVersion)
	switch {
	case err == nil:
		if curVersion == spec.Version && cur.Criterion == spec.Criterion && cur.Claim == spec.Claim &&
			cur.ExperimentID == spec.ExperimentID && cur.ContextVersion == spec.ContextVersion {
			return nil
		}
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("harness acceptance lookup: %w", err)
	}
	if _, err := tx.Exec(`UPDATE harness_acceptance SET is_current=false WHERE task_id=$1 AND goal_id=$2 AND is_current`,
		spec.TaskID, spec.GoalID); err != nil {
		return fmt.Errorf("demote current acceptance: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO harness_acceptance(task_id, goal_id, version, criterion_id, criterion_version, claim, experiment_id, context_version, is_current)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true)`,
		spec.TaskID, spec.GoalID, spec.Version, spec.Criterion.ID, spec.Criterion.Version,
		spec.Claim, spec.ExperimentID, spec.ContextVersion); err != nil {
		return fmt.Errorf("%w: task %d goal %d version %s", ErrAcceptanceVersionReused, spec.TaskID, spec.GoalID, spec.Version)
	}
	return tx.Commit()
}

type pgAcceptanceResolver struct{ d *DB }

// HarnessAcceptanceResolver 返回 PG 权威验收解析器（按 task/goal 解析当前版本）。
func (d *DB) HarnessAcceptanceResolver() harness.AcceptanceResolver {
	return pgAcceptanceResolver{d: d}
}

func (r pgAcceptanceResolver) ResolveAcceptance(ctx context.Context, taskID, goalID int64) (harness.AcceptanceSpec, error) {
	if err := ctx.Err(); err != nil {
		return harness.AcceptanceSpec{}, err
	}
	var spec harness.AcceptanceSpec
	err := r.d.QueryRow(`SELECT version, criterion_id, criterion_version, claim, experiment_id, context_version
		FROM harness_acceptance WHERE task_id=$1 AND goal_id=$2 AND is_current`, taskID, goalID).
		Scan(&spec.Version, &spec.Criterion.ID, &spec.Criterion.Version, &spec.Claim, &spec.ExperimentID, &spec.ContextVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return harness.AcceptanceSpec{}, fmt.Errorf("frozen goal acceptance not found (task %d goal %d)", taskID, goalID)
	}
	if err != nil {
		return harness.AcceptanceSpec{}, fmt.Errorf("resolve acceptance: %w", err)
	}
	spec.TaskID, spec.GoalID = taskID, goalID
	return spec, nil
}

// HarnessAcceptanceHistory 返回某 task+goal 的版本历史（新→旧，含是否当前），
// 供审计/诊断读取。
func (d *DB) HarnessAcceptanceHistory(taskID, goalID int64) ([]harness.AcceptanceSpec, []bool, error) {
	rows, err := d.Query(`SELECT version, criterion_id, criterion_version, claim, experiment_id, context_version, is_current
		FROM harness_acceptance WHERE task_id=$1 AND goal_id=$2 ORDER BY id DESC LIMIT 100`, taskID, goalID)
	if err != nil {
		return nil, nil, fmt.Errorf("acceptance history: %w", err)
	}
	defer rows.Close()
	specs := make([]harness.AcceptanceSpec, 0, 4)
	current := make([]bool, 0, 4)
	for rows.Next() {
		var s harness.AcceptanceSpec
		var cur bool
		if err := rows.Scan(&s.Version, &s.Criterion.ID, &s.Criterion.Version, &s.Claim, &s.ExperimentID, &s.ContextVersion, &cur); err != nil {
			return nil, nil, err
		}
		s.TaskID, s.GoalID = taskID, goalID
		specs = append(specs, s)
		current = append(current, cur)
	}
	return specs, current, rows.Err()
}

// AcceptanceRecord 是验收历史行（含 goal 与时间戳，供只读检查面）。
type AcceptanceRecord struct {
	GoalID    int64
	Version   string
	Claim     string
	Current   bool
	CreatedAt string
}

// HarnessAcceptanceTaskHistory 返回任务全部 goal 的验收版本行（新→旧）。
func (d *DB) HarnessAcceptanceTaskHistory(taskID int64) ([]AcceptanceRecord, error) {
	rows, err := d.Query(`SELECT goal_id, version, claim, is_current,
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM harness_acceptance WHERE task_id=$1 ORDER BY id DESC LIMIT 200`, taskID)
	if err != nil {
		return nil, fmt.Errorf("acceptance task history: %w", err)
	}
	defer rows.Close()
	out := make([]AcceptanceRecord, 0, 4)
	for rows.Next() {
		var a AcceptanceRecord
		if err := rows.Scan(&a.GoalID, &a.Version, &a.Claim, &a.Current, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// EvaluationRecord 是评估存档行（含时间戳，供只读检查面）。
type EvaluationRecord struct {
	RequestID   string
	GoalID      int64
	Validity    string
	Conclusion  string
	Disposition string
	CreatedAt   string
}

// EvaluationRecords 返回任务最近的评估存档行（新→旧，只读）。
func (d *DB) EvaluationRecords(taskID int64, limit int) ([]EvaluationRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.Query(`SELECT request_id, goal_id, validity, conclusion, disposition,
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM harness_evaluations WHERE task_id=$1 ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("evaluation records: %w", err)
	}
	defer rows.Close()
	out := make([]EvaluationRecord, 0, 8)
	for rows.Next() {
		var e EvaluationRecord
		if err := rows.Scan(&e.RequestID, &e.GoalID, &e.Validity, &e.Conclusion, &e.Disposition, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
