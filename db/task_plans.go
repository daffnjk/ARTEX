package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// task_plans 存储（research M1b.1）：planner 的任务计划从进程内 map 提升为权威
// PostgreSQL 记录。revision 是 CAS 计数器：写入方必须携带它读取时的 revision，
// 过期/并发写入得到显式冲突（绝不 last-write-wins）。
//
// 认知视角：计划是控制律的草稿。持久化给控制器状态可达性（Planner 重建/进程
// 重启后不回空计划）；CAS 防止过期控制器（旧 Planner 实例）静默覆盖新状态。
// 计划状态只是模型便签——任何验收/完成路径都不读这张表。

// PlanStep 是计划中的一个步骤。ID 由宿主分配且跨保存稳定（按内容精确匹配保持，
// 与展示顺序无关；旧整体写入按内容匹配，显式 PatchPlanStep 原位改文案）。
// BasisNodeIDs 是该步骤派生自的探索节点（fact/意图），ExpectedOutput
// 是预期产出的占位描述（M1b.1 只记录不绑定，绑定在 M2/M3）。
type PlanStep struct {
	ID               int64   `json:"id"`
	Content          string  `json:"content"`
	Status           string  `json:"status"`
	BasisNodeIDs     []int64 `json:"basis_node_ids,omitempty"`
	ExpectedOutput   string  `json:"expected_output,omitempty"`
	ContextVersion   string  `json:"context_version,omitempty"`
	RequiresStepIDs  []int64 `json:"requires_step_ids,omitempty"`
	RequiresClaimIDs []int64 `json:"requires_claim_ids,omitempty"`
}

// TaskPlan 是一个探索（任务，二者 1:1）的当前计划。Revision 是 CAS 期望值：
// Load 返回当前值（无记录返回 nil,nil —— 显式空计划，不臆造）；Save 用它做
// 乐观并发控制，成功后返回携带新 Revision 的落盘结果。RowID 是存储行的身份
// （BIGSERIAL 主键）：CAS 谓词同时校验 revision 与 RowID——行被删除后重建会
// 获得新 RowID，持有旧行身份的迟到写入即使 revision 撞值（如都为 1）也会被
// 拒绝（封死 delete→recreate 的 ABA 覆盖）。首次写入期望 (Revision 0, RowID 0)。
type TaskPlan struct {
	ExplorationID int64
	RowID         int64
	Revision      int64
	Steps         []PlanStep
	ModelNotes    string
}

// PlanConflictError 表示 CAS 冲突：HaveRevision 是数据库里的当前 revision；
// HaveRevision==0 表示当前无行（计划已被删除/归档清除），期望值相对"行已消失"
// 过期。两者都可恢复：调用方重新 Load 后基于新状态重试；绝不静默覆盖。
type PlanConflictError struct {
	ExplorationID int64
	HaveRevision  int64
}

func (e *PlanConflictError) Error() string {
	if e.HaveRevision == 0 {
		return fmt.Sprintf("task plan conflict: exploration %d has no stored plan row (deleted/archived); expectation is stale", e.ExplorationID)
	}
	return fmt.Sprintf("task plan conflict: exploration %d is at revision %d", e.ExplorationID, e.HaveRevision)
}

// 计划输入的边界（防畸形/超大材料，M1b.1 验收矩阵的 malformed/bounded 行）。
const (
	PlanMaxSteps         = 100
	PlanMaxContentBytes  = 8 << 10
	PlanMaxExpectBytes   = 2 << 10
	PlanMaxNotesBytes    = 16 << 10
	PlanMaxBasisPerStep  = 32
	planStatusPending    = "pending"
	planStatusInProgress = "in_progress"
	planStatusCompleted  = "completed"
)

// ValidatePlanSteps 校验一组（尚无 ID 的）步骤输入：状态合法、内容非空且在界内、
// basis 数量在界内。越界/畸形返回错误，由工具层作为失败返回给模型。
func ValidatePlanSteps(steps []PlanStep) error {
	if len(steps) > PlanMaxSteps {
		return fmt.Errorf("plan exceeds max steps (%d > %d)", len(steps), PlanMaxSteps)
	}
	for i, s := range steps {
		switch s.Status {
		case planStatusPending, planStatusInProgress, planStatusCompleted:
		default:
			return fmt.Errorf("step %d: invalid status %q (pending|in_progress|completed)", i, s.Status)
		}
		if s.Content == "" {
			return fmt.Errorf("step %d: empty content", i)
		}
		if len(s.Content) > PlanMaxContentBytes {
			return fmt.Errorf("step %d: content exceeds %d bytes", i, PlanMaxContentBytes)
		}
		if len(s.ExpectedOutput) > PlanMaxExpectBytes {
			return fmt.Errorf("step %d: expected_output exceeds %d bytes", i, PlanMaxExpectBytes)
		}
		if len(s.BasisNodeIDs) > PlanMaxBasisPerStep {
			return fmt.Errorf("step %d: more than %d basis node ids", i, PlanMaxBasisPerStep)
		}
	}
	return nil
}

// ValidatePlanNotes 校验计划级模型便签的边界。
func ValidatePlanNotes(notes string) error {
	if len(notes) > PlanMaxNotesBytes {
		return fmt.Errorf("plan notes exceed %d bytes", PlanMaxNotesBytes)
	}
	return nil
}

// MergePlanSteps 把一次整体替换的输入并入现有计划，分配稳定步骤 ID：
// 内容精确匹配的步骤保留原 ID（顺序无关），新内容获得单调递增的新 ID，
// 未再出现的步骤被移除。ID 分配是宿主权威——模型只提供内容与状态。
func MergePlanSteps(existing, incoming []PlanStep) []PlanStep {
	var nextID int64 = 1
	byContent := make(map[string]int64, len(existing))
	for _, s := range existing {
		if s.ID >= nextID {
			nextID = s.ID + 1
		}
		if _, dup := byContent[s.Content]; !dup {
			byContent[s.Content] = s.ID
		}
	}
	out := make([]PlanStep, 0, len(incoming))
	seen := make(map[string]bool, len(incoming))
	for _, s := range incoming {
		id, ok := byContent[s.Content]
		if !ok || seen[s.Content] {
			id = nextID // 新步骤（或同内容重复项——第二条按新步骤计）
			nextID++
		}
		seen[s.Content] = true
		s.ID = id
		out = append(out, s)
	}
	return out
}

// LoadTaskPlan 返回探索的当前计划；无记录时返回 (nil, nil)——显式空计划。
// 读取错误原样上抛：调用方（planner 恢复路径）必须停止本轮而不是回退空内存。
func (d *DB) LoadTaskPlan(explorationID int64) (*TaskPlan, error) {
	var (
		rowID int64
		rev   int64
		steps []byte
		notes string
	)
	err := d.QueryRow(`SELECT id, revision, steps, model_notes FROM task_plans WHERE exploration_id=$1`,
		explorationID).Scan(&rowID, &rev, &steps, &notes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load task plan (exploration %d): %w", explorationID, err)
	}
	var plan TaskPlan
	plan.ExplorationID = explorationID
	plan.RowID = rowID
	plan.Revision = rev
	plan.ModelNotes = notes
	if len(steps) > 0 {
		if err := json.Unmarshal(steps, &plan.Steps); err != nil {
			return nil, fmt.Errorf("decode task plan steps (exploration %d): %w", explorationID, err)
		}
	}
	return &plan, nil
}

// SaveTaskPlan 以 CAS 语义落盘：plan.Revision/plan.RowID 是期望的当前值
// （0/0 = 期望尚无记录，首次写入）。三段式（每段各自原子，谓词保证语义）：
//  1. CAS 更新：行存在且 (revision, row id) 与期望完全匹配 → revision+1；
//  2. 首写插入：仅当期望为 (0,0)；并发首写恰一方成功（ON CONFLICT DO NOTHING）；
//  3. 显式冲突：期望过期（行在但不匹配）、行已消失（计划被删除/归档清除，
//     HaveRevision 0，绝不复活）、或首写撞车 → *PlanConflictError。
//
// 行身份参与谓词封死 delete→recreate 的 revision 撞值 ABA。成功返回新 revision
// 的落盘计划；落盘成功才返回——先持久化，后确认。
func saveTaskPlan(ctx context.Context, w observationWriter, plan TaskPlan) (TaskPlan, error) {
	stepsJSON, err := json.Marshal(plan.Steps)
	if err != nil {
		return plan, fmt.Errorf("encode task plan steps (exploration %d): %w", plan.ExplorationID, err)
	}
	var newRev, newRowID int64
	err = w.QueryRowContext(ctx, `
UPDATE task_plans
   SET revision = revision + 1, steps = $2, model_notes = $3, updated_at = now()
 WHERE exploration_id = $1 AND revision = $4 AND id = $5
RETURNING revision, id`,
		plan.ExplorationID, stepsJSON, plan.ModelNotes, plan.Revision, plan.RowID).Scan(&newRev, &newRowID)
	if err == nil {
		plan.Revision, plan.RowID = newRev, newRowID
		return plan, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return plan, fmt.Errorf("save task plan (exploration %d): %w", plan.ExplorationID, err)
	}
	if plan.Revision == 0 && plan.RowID == 0 {
		err = w.QueryRowContext(ctx, `
INSERT INTO task_plans (exploration_id, revision, steps, model_notes)
VALUES ($1, 1, $2, $3)
ON CONFLICT (exploration_id) DO NOTHING
RETURNING revision, id`,
			plan.ExplorationID, stepsJSON, plan.ModelNotes).Scan(&newRev, &newRowID)
		if err == nil {
			plan.Revision, plan.RowID = newRev, newRowID
			return plan, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return plan, fmt.Errorf("save task plan (exploration %d): %w", plan.ExplorationID, err)
		}
	}
	// 显式冲突：查当前状态构造可恢复错误；行不存在 ⇒ HaveRevision 0。
	var have int64
	qErr := w.QueryRowContext(ctx, `SELECT revision FROM task_plans WHERE exploration_id=$1`,
		plan.ExplorationID).Scan(&have)
	switch {
	case qErr == nil:
		return plan, &PlanConflictError{ExplorationID: plan.ExplorationID, HaveRevision: have}
	case errors.Is(qErr, sql.ErrNoRows):
		return plan, &PlanConflictError{ExplorationID: plan.ExplorationID, HaveRevision: 0}
	default:
		return plan, fmt.Errorf("save task plan (exploration %d): revision %d rejected and current state unavailable: %w",
			plan.ExplorationID, plan.Revision, qErr)
	}
}

// PlanBasisNodesOwned 校验 basis 节点确实属于该探索（防止跨任务引用/臆造 id）。
// 返回缺失的节点 id；全部存在返回空切片。查询失败原样上抛。
func (d *DB) PlanBasisNodesOwned(explorationID int64, nodeIDs []int64) ([]int64, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id FROM exploration_nodes WHERE exploration_id=$1 AND id=ANY($2)`,
		explorationID, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("check plan basis nodes (exploration %d): %w", explorationID, err)
	}
	defer rows.Close()
	have := make(map[int64]bool, len(nodeIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan plan basis node (exploration %d): %w", explorationID, err)
		}
		have[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan basis nodes (exploration %d): %w", explorationID, err)
	}
	missing := make([]int64, 0)
	for _, id := range nodeIDs {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// PlanBasisNodesOwned 是探索视图的便捷封装：按本探索校验 basis 节点归属。
func (s *ExplorationStore) PlanBasisNodesOwned(nodeIDs []int64) ([]int64, error) {
	return s.db.PlanBasisNodesOwned(s.expID, nodeIDs)
}
