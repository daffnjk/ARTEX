package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Autumn-27/artex/harness"
)

// M2.1（research）：评估 harness 的可信本地采集存储。
//
// 写侧仅宿主可用（StoreHarnessObservation/RecordEvaluation）——模型没有任何
// 写观察的路径；读侧以任务范围构造 harness.Resolver（越任务引用在解析层就
// 变为 evidence_unavailable，M1a 契约的调用方/任务访问边界）。两张表
// append-only：触发器拒绝 UPDATE/DELETE，代码不提供任何修改路径。

// ErrHarnessObservationConflict 表示同 id 但内容不同的重复写入（同 id 同内容
// 为幂等成功）。
var ErrHarnessObservationConflict = errors.New("harness observation id exists with different content")

// StoreHarnessObservation 写入一条可信观察（宿主 API）。校验 harness 容量上限
// 与 digest 一致性；同 id 同内容幂等成功，同 id 异内容显式冲突。
func (d *DB) StoreHarnessObservation(o harness.Observation) error {
	if o.ID == "" || o.TaskID <= 0 || o.ExperimentID == "" || o.ContextVersion == "" {
		return fmt.Errorf("harness observation: id/task_id/experiment_id/context_version required")
	}
	if len(o.Payload) > harness.MaxArtifactBytes {
		return fmt.Errorf("harness observation %s: payload exceeds %d bytes", o.ID, harness.MaxArtifactBytes)
	}
	digest := harness.Digest(o.Payload)
	// 幂等判定必须比对完整身份（id 全局唯一）：同 id 且身份字段一致才幂等，
	// 否则显式冲突——只比 digest 会把跨任务的撞 id 误判为幂等（实测发现）。
	var (
		exTask, exExp, exExec, exCtx, exSubj, exObj string
		exDigest                                    string
		exComplete                                  bool
	)
	err := d.QueryRow(`SELECT task_id::text, experiment_id, execution_id, context_version,
		subject_ref, object_ref, payload_sha256, complete FROM harness_observations WHERE id=$1`, o.ID).Scan(
		&exTask, &exExp, &exExec, &exCtx, &exSubj, &exObj, &exDigest, &exComplete)
	switch {
	case err == nil:
		same := exDigest == digest && exExp == o.ExperimentID && exExec == o.ExecutionID &&
			exCtx == o.ContextVersion && exSubj == o.SubjectRef && exObj == o.ObjectRef &&
			exComplete == o.Complete && exTask == fmt.Sprint(o.TaskID)
		if same {
			return nil // 幂等：同 id 同身份同内容（observed_at 属元数据不参与判定）
		}
		return fmt.Errorf("%w: %s", ErrHarnessObservationConflict, o.ID)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("harness observation lookup: %w", err)
	}
	_, err = d.Exec(`
INSERT INTO harness_observations(id, task_id, experiment_id, execution_id, context_version,
    subject_ref, object_ref, observed_at, complete, payload, payload_sha256)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (id) DO NOTHING`,
		o.ID, o.TaskID, o.ExperimentID, o.ExecutionID, o.ContextVersion,
		o.SubjectRef, o.ObjectRef, o.ObservedAt, o.Complete, o.Payload, digest)
	if err != nil {
		return fmt.Errorf("store harness observation %s: %w", o.ID, err)
	}
	return nil
}

// HarnessStore 是任务范围的 harness 读写视图。
type HarnessStore struct {
	d      *DB
	taskID int64
}

// Harness 返回该任务的 harness 存储视图（读取范围 = 本任务）。
func (d *DB) Harness(taskID int64) *HarnessStore { return &HarnessStore{d: d, taskID: taskID} }

// pgResolver 把任务范围内的 harness_observations 适配为 harness.Resolver。
// 越任务/不存在的 id 一律解析失败（内核映射为 evidence_unavailable 或按
// 引用位置处理），绝不跨任务泄露材料。
type pgResolver struct {
	d      *DB
	taskID int64
}

func (r pgResolver) Resolve(_ context.Context, id string) (harness.Observation, error) {
	var o harness.Observation
	err := r.d.QueryRow(`
SELECT id, task_id, experiment_id, execution_id, context_version, subject_ref, object_ref,
       observed_at, complete, payload
FROM harness_observations WHERE id=$1 AND task_id=$2`, id, r.taskID).Scan(
		&o.ID, &o.TaskID, &o.ExperimentID, &o.ExecutionID, &o.ContextVersion,
		&o.SubjectRef, &o.ObjectRef, &o.ObservedAt, &o.Complete, &o.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return harness.Observation{}, fmt.Errorf("observation %s not found in task %d", id, r.taskID)
	}
	if err != nil {
		return harness.Observation{}, fmt.Errorf("resolve observation %s: %w", id, err)
	}
	return o, nil
}

// Resolver 返回本任务范围的 harness.Resolver。
func (s *HarnessStore) Resolver() harness.Resolver { return pgResolver{d: s.d, taskID: s.taskID} }

// RecordEvaluation 追加一条评估报告存档（append-only，不可改不可删）。
func (s *HarnessStore) RecordEvaluation(r harness.EvaluationReport) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode evaluation report: %w", err)
	}
	// request_id 唯一（审查硬化）：重试重放不重复追加，幂等成功。
	res, err := s.d.Exec(`
INSERT INTO harness_evaluations(request_id, task_id, goal_id, experiment_id, context_version,
    criterion_id, criterion_version, validity, conclusion, disposition, report)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (request_id) DO NOTHING`,
		r.RequestID, r.TaskID, r.GoalID, r.ExperimentID, r.ContextVersion,
		r.CriterionID, r.CriterionVersion, string(r.Validity), string(r.Conclusion), string(r.Disposition), raw)
	if err != nil {
		return fmt.Errorf("record evaluation %s: %w", r.RequestID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // 同 request_id 已存档——幂等
	}
	return nil
}

// LatestEvaluations 读回最近 limit 条评估报告（新→旧）。
func (s *HarnessStore) LatestEvaluations(limit int) ([]harness.EvaluationReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.d.Query(`
SELECT report FROM harness_evaluations WHERE task_id=$1 ORDER BY id DESC LIMIT $2`, s.taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list evaluations (task %d): %w", s.taskID, err)
	}
	defer rows.Close()
	out := make([]harness.EvaluationReport, 0, limit)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan evaluation: %w", err)
		}
		var r harness.EvaluationReport
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("decode evaluation: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
