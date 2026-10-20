package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// M3.1（research）：条件化假设与预注册实验的存储层。
//
// 认知契约见 schema 注释与 task-specs/M3.1.md：假设状态翻转必须由区分性实验
// 结果驱动（事件流留痕）；实验期望派发后不可变（触发器强制）；结果匹配不做
// 强拟合——歧义/全不匹配 → inconclusive。

// Hypothesis 是一条竞争假设（环境的内部模型候选）。
type Hypothesis struct {
	ID            int64   `json:"id"`
	ExplorationID int64   `json:"exploration_id"`
	Question      string  `json:"question"`
	Statement     string  `json:"statement"`
	Status        string  `json:"status"`
	BasisNodeIDs  []int64 `json:"basis_node_ids"`
	Notes         string  `json:"notes"`
	CreatedBy     string  `json:"created_by"`
}

// Experiment 是一次预注册的区分性实验。
type Experiment struct {
	ID            int64            `json:"id"`
	ExplorationID int64            `json:"exploration_id"`
	Question      string           `json:"question"`
	Action        string           `json:"action"`
	HypothesisIDs []int64          `json:"hypothesis_ids"`
	Expectations  map[int64]string `json:"expectations"` // hypID → 预期观察
	IntentID      int64            `json:"intent_id"`
	Status        string           `json:"status"`
	Outcome       map[string]any   `json:"outcome"`
	StopCondition string           `json:"stop_condition"`
}

// 假设/实验输入边界。
const (
	MaxHypothesisBytes  = 8 << 10
	MaxExperimentsOpen  = 16
	MaxExpectationBytes = 4 << 10
)

// ErrHypothesisNotActive 表示目标假设不在可判定状态。
var ErrHypothesisNotActive = errors.New("hypothesis not in a decidable state")

// ValidateHypothesis 校验一条假设输入。
func ValidateHypothesis(question, statement string, basis []int64) error {
	if strings.TrimSpace(question) == "" || strings.TrimSpace(statement) == "" {
		return fmt.Errorf("hypothesis question and statement required")
	}
	if len(question) > MaxHypothesisBytes || len(statement) > MaxHypothesisBytes {
		return fmt.Errorf("hypothesis text exceeds %d bytes", MaxHypothesisBytes)
	}
	if len(basis) > 32 {
		return fmt.Errorf("hypothesis basis exceeds 32 nodes")
	}
	return nil
}

// UpsertHypotheses 同步一个问题的假设集（整体替换式声明，模型每次提交完整
// 竞争集）：新语句插入 active；不再出现的旧假设若仍 active → superseded（记
// 事件）；已 supported/refuted 的不复活也不改写。同语句幂等更新 notes/basis。
func (s *ExplorationStore) UpsertHypotheses(question string, items []Hypothesis) error {
	// 反过早收敛的结构强制（审查 P2）：未决问题的假设集重述不得少于 2 条
	//（收敛只能由区分性实验结果驱动，不能由叙事收窄）。
	if len(items) < 2 {
		return fmt.Errorf("a question under investigation keeps >=2 competing hypotheses (collapse is driven by experiment outcomes, not by restating fewer)")
	}
	if len(items) > 8 {
		return fmt.Errorf("at most 8 hypotheses per question")
	}
	for _, h := range items {
		if err := ValidateHypothesis(question, h.Statement, h.BasisNodeIDs); err != nil {
			return err
		}
	}
	for i := range items {
		if items[i].BasisNodeIDs == nil {
			items[i].BasisNodeIDs = []int64{}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// basis 归属校验：同探索或直接来源任务（与血缘边界一致）。
	for _, h := range items {
		for _, b := range h.BasisNodeIDs {
			var ok int64
			err := tx.QueryRow(`SELECT n.id FROM exploration_nodes n WHERE n.id=$1 AND n.kind IN ('fact','finding') AND (
				n.exploration_id=$2 OR EXISTS (
					SELECT 1 FROM tasks owner
					JOIN task_relations r ON r.task_id=owner.id
					JOIN tasks src ON src.id=r.source_task_id AND src.deleted_at IS NULL
					WHERE owner.exploration_id=$2 AND owner.deleted_at IS NULL
					  AND src.exploration_id=n.exploration_id))`, b, s.expID).Scan(&ok)
			if err != nil {
				return fmt.Errorf("hypothesis basis node %d not in this exploration or its direct sources", b)
			}
		}
	}
	byStmt := map[string]Hypothesis{}
	for _, h := range items {
		byStmt[h.Statement] = h
	}
	rows, err := tx.Query(`SELECT id, statement, status FROM hypotheses WHERE exploration_id=$1 AND question=$2`, s.expID, question)
	if err != nil {
		return err
	}
	type old struct {
		id     int64
		stmt   string
		status string
	}
	var olds []old
	for rows.Next() {
		var o old
		if err := rows.Scan(&o.id, &o.stmt, &o.status); err != nil {
			rows.Close()
			return err
		}
		olds = append(olds, o)
	}
	rows.Close()
	for _, o := range olds {
		if _, keep := byStmt[o.stmt]; !keep && o.status == "active" {
			if _, err := tx.Exec(`UPDATE hypotheses SET status='superseded', updated_at=now() WHERE id=$1`, o.id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO hypothesis_events(hypothesis_id, from_status, to_status, reason)
				VALUES ($1,'active','superseded',$2)`, o.id, "replaced by a restated hypothesis set"); err != nil {
				return err
			}
		}
	}
	for _, h := range items {
		var existing int64
		err := tx.QueryRow(`SELECT id FROM hypotheses WHERE exploration_id=$1 AND question=$2 AND statement=$3`,
			s.expID, question, h.Statement).Scan(&existing)
		switch {
		case err == nil:
			// M3.2：重述【invalidated】假设 = 复活为 active（条件失效后的重估入口，
			// 事件留痕）；superseded/supported/refuted 不经重述复活。
			var curStatus string
			if err := tx.QueryRow(`SELECT status FROM hypotheses WHERE id=$1`, existing).Scan(&curStatus); err != nil {
				return err
			}
			if curStatus == "invalidated" {
				if _, err := tx.Exec(`UPDATE hypotheses SET status='active', notes=$2, basis_node_ids=$3, updated_at=now() WHERE id=$1`,
					existing, h.Notes, h.BasisNodeIDs); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO hypothesis_events(hypothesis_id, from_status, to_status, reason)
					VALUES ($1,'invalidated','active',$2)`, existing, "restated after condition invalidation — re-estimation"); err != nil {
					return err
				}
			} else if _, err := tx.Exec(`UPDATE hypotheses SET notes=$2, basis_node_ids=$3, updated_at=now() WHERE id=$1`,
				existing, h.Notes, h.BasisNodeIDs); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
			by := h.CreatedBy
			if by == "" {
				by = "planner"
			}
			if _, err := tx.Exec(`INSERT INTO hypotheses(exploration_id, question, statement, basis_node_ids, notes, created_by)
				VALUES ($1,$2,$3,$4,$5,$6)`, s.expID, question, h.Statement, h.BasisNodeIDs, h.Notes, by); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return tx.Commit()
}

// ListHypotheses 返回该探索的假设（按问题分组由调用方处理；带开放实验数）。
func (s *ExplorationStore) ListHypotheses() ([]Hypothesis, error) {
	rows, err := s.db.Query(`SELECT id, exploration_id, question, statement, status,
		array_to_json(basis_node_ids)::text, notes, created_by
		FROM hypotheses WHERE exploration_id=$1 ORDER BY question, id`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Hypothesis, 0, 8)
	for rows.Next() {
		var h Hypothesis
		var basisRaw string
		if err := rows.Scan(&h.ID, &h.ExplorationID, &h.Question, &h.Statement, &h.Status, &basisRaw, &h.Notes, &h.CreatedBy); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(basisRaw), &h.BasisNodeIDs)
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeclareExperiment 预注册一个区分性实验：期望（≥2 条，每假设一条）在声明时
// 写定；开放实验数有界。
func (s *ExplorationStore) DeclareExperiment(e Experiment) (int64, error) {
	if strings.TrimSpace(e.Question) == "" || strings.TrimSpace(e.Action) == "" {
		return 0, fmt.Errorf("experiment question and action required")
	}
	if len(e.Question) > MaxHypothesisBytes || len(e.Action) > MaxHypothesisBytes {
		return 0, fmt.Errorf("experiment text exceeds %d bytes", MaxHypothesisBytes)
	}
	if len(e.Expectations) < 2 {
		return 0, fmt.Errorf("a discriminating experiment needs expectations for at least 2 hypotheses")
	}
	if len(e.HypothesisIDs) != len(e.Expectations) {
		return 0, fmt.Errorf("expectations keys must exactly cover the participating hypothesis ids (%d ids, %d expectations)", len(e.HypothesisIDs), len(e.Expectations))
	}
	// 审查 P1：键集必须与参试集一致，且全部为本探索的 active 假设——否则
	// 畸形声明可让非参试者被"supported"、参试者被批量 refuted，或跨探索腐蚀。
	partSet := map[int64]bool{}
	for _, id := range e.HypothesisIDs {
		partSet[id] = true
	}
	for hypID := range e.Expectations {
		if !partSet[hypID] {
			return 0, fmt.Errorf("expectation key %d is not among hypothesis_ids", hypID)
		}
	}
	for hypID, exp := range e.Expectations {
		if strings.TrimSpace(exp) == "" || len(exp) > MaxExpectationBytes {
			return 0, fmt.Errorf("expectation for hypothesis %d must be 1..%d bytes", hypID, MaxExpectationBytes)
		}
		var status string
		var expl int64
		err := s.db.QueryRow(`SELECT status, exploration_id FROM hypotheses WHERE id=$1`, hypID).Scan(&status, &expl)
		if err != nil {
			return 0, fmt.Errorf("hypothesis %d not found", hypID)
		}
		if expl != s.expID {
			return 0, fmt.Errorf("hypothesis %d belongs to another exploration", hypID)
		}
		if status != "active" {
			return 0, fmt.Errorf("hypothesis %d is %q, not active", hypID, status)
		}
	}
	var open int64
	if err := s.db.QueryRow(`SELECT count(*) FROM experiments WHERE exploration_id=$1 AND status IN ('declared','dispatched')`, s.expID).Scan(&open); err != nil {
		return 0, err
	}
	if open >= MaxExperimentsOpen {
		return 0, fmt.Errorf("open experiment budget reached (%d)", MaxExperimentsOpen)
	}
	if e.HypothesisIDs == nil {
		e.HypothesisIDs = []int64{}
	}
	raw, _ := json.Marshal(e.Expectations)
	var id int64
	err := s.db.QueryRow(`INSERT INTO experiments(exploration_id, question, action, hypothesis_ids, expectations, stop_condition)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		s.expID, e.Question, e.Action, e.HypothesisIDs, raw, e.StopCondition).Scan(&id)
	return id, err
}

// DispatchExperiment 把已声明实验绑定到意图并置 dispatched（期望自此不可变）。
// 意图必须属于本探索（审查 P1：防跨探索绑定）。
func (s *ExplorationStore) DispatchExperiment(experimentID, intentID int64) error {
	var intentExpl int64
	if err := s.db.QueryRow(`SELECT exploration_id FROM exploration_nodes WHERE id=$1 AND kind='intent'`, intentID).Scan(&intentExpl); err != nil {
		return fmt.Errorf("intent %d not found: %w", intentID, err)
	}
	if intentExpl != s.expID {
		return fmt.Errorf("intent %d belongs to another exploration", intentID)
	}
	// 硬边界（审查 P2 + 批审 P2）：deny 判定与状态迁移同一事务——堵住跨连接
	// check-then-act（检查后、更新前插入的 deny 约束会被漏掉）。
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var action string
	if err := tx.QueryRow(`SELECT action FROM experiments WHERE id=$1 AND exploration_id=$2 FOR UPDATE`, experimentID, s.expID).Scan(&action); err != nil {
		return fmt.Errorf("experiment %d not found: %w", experimentID, err)
	}
	if deny, err := denyConflictOn(tx, s.expID, action); err != nil {
		return err
	} else if deny != "" {
		return fmt.Errorf("实验操作与任务 deny 约束冲突，拒绝派发：「%s」", deny)
	}
	res, err := tx.Exec(`UPDATE experiments SET intent_id=$2, status='dispatched', dispatched_at=now()
		WHERE id=$1 AND exploration_id=$3 AND status='declared'`, experimentID, intentID, s.expID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("experiment %d not in declared state", experimentID)
	}
	return tx.Commit()
}

// ExperimentOutcome 是一次实验结果的判定。
type ExperimentOutcome struct {
	Status        string  // observed | inconclusive
	Discriminated int64   // 唯一匹配的假设 id（0=无）
	Matches       []int64 // 期望匹配的假设集合
	Reason        string
}

// RecordExperimentOutcome 记录实验观察并对齐假设状态：恰好一个假设的【声明
// 期望】与观察文本匹配 → 该假设 supported、其余参试假设 refuted（事件留痕）；
// 多匹配或全不匹配 → inconclusive，假设状态不动（绝不强拟合）。
func (s *ExplorationStore) RecordExperimentOutcome(experimentID int64, observed string, factID int64) (ExperimentOutcome, error) {

	if strings.TrimSpace(observed) == "" || len(observed) > MaxExpectationBytes*4 {
		return ExperimentOutcome{}, fmt.Errorf("observed summary required (bounded)")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ExperimentOutcome{}, err
	}
	defer tx.Rollback()
	var mode string
	err = tx.QueryRow(`SELECT control_mode FROM tasks WHERE exploration_id=$1 FOR UPDATE`, s.expID).Scan(&mode)
	if err != nil && err != sql.ErrNoRows {
		return ExperimentOutcome{}, err
	}
	if err == nil && mode != "legacy" {
		return ExperimentOutcome{}, fmt.Errorf("controlled tasks require validated evidence outcomes")
	}

	var (
		hypIDs []int64
		expRaw []byte
		status string
	)
	var hypRaw string
	err = tx.QueryRow(`SELECT array_to_json(hypothesis_ids)::text, expectations, status FROM experiments
		WHERE id=$1 AND exploration_id=$2 FOR UPDATE`, experimentID, s.expID).Scan(&hypRaw, &expRaw, &status)
	_ = json.Unmarshal([]byte(hypRaw), &hypIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return ExperimentOutcome{}, fmt.Errorf("experiment %d not found", experimentID)
	}
	if err != nil {
		return ExperimentOutcome{}, err
	}
	if status != "dispatched" {
		return ExperimentOutcome{}, fmt.Errorf("experiment %d is %q, not dispatched", experimentID, status)
	}
	expectations := map[int64]string{}
	if err := json.Unmarshal(expRaw, &expectations); err != nil {
		return ExperimentOutcome{}, err
	}
	// 匹配只认精确相等（审查 P1：双向 Contains 过宽——expected="200" 会匹配
	// "3200"，短串巧合可造成假唯一匹配，而 refuted 不可逆）。要求观察与预注册
	// 期望同粒度精确对写；匹配文本写入 outcome 供审计。
	out := ExperimentOutcome{Status: "inconclusive"}
	matched := map[int64]string{}
	for hypID, expected := range expectations {
		if strings.TrimSpace(observed) == strings.TrimSpace(expected) {
			out.Matches = append(out.Matches, hypID)
			matched[hypID] = expected
		}
	}
	switch {
	case len(out.Matches) == 1:
		out.Status = "observed"
		out.Discriminated = out.Matches[0]
		out.Reason = "observation matches exactly one declared expectation"
		for _, hypID := range hypIDs {
			to := "refuted"
			if hypID == out.Discriminated {
				to = "supported"
			}
			res, err := tx.Exec(`UPDATE hypotheses SET status=$2, updated_at=now() WHERE id=$1 AND status='active'`, hypID, to)
			if err != nil {
				return ExperimentOutcome{}, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				if _, err := tx.Exec(`INSERT INTO hypothesis_events(hypothesis_id, from_status, to_status, reason, experiment_id)
					VALUES ($1,'active',$2,$3,$4)`, hypID, to, "discriminated by experiment outcome", experimentID); err != nil {
					return ExperimentOutcome{}, err
				}
				// 审查 P1（M3.2–M4.1 批审）：区分性证据并入胜者 basis。失效监测
				// 按 basis_node_ids 检索——不并的话，空 basis 的 supported 假设
				// 在其真正支撑证据被撤回（false_positive/删除）后仍永久 supported。
				// 批审 P1（M4.2–M4.5）：factID 来自模型上报，并入前必须过与
				// UpsertHypotheses 相同的血缘校验（存在/fact/同探索或直接来源），
				// 且不越 basis 上限——否则跨探索嫁接/幻影 id 恰好破坏 basis 不变量。
				if to == "supported" && factID > 0 {
					var owned, card int64
					if err := tx.QueryRow(`SELECT
  (SELECT count(*) FROM exploration_nodes n WHERE n.id=$2 AND n.kind='fact' AND (
     n.exploration_id=$3 OR EXISTS (
       SELECT 1 FROM tasks owner
       JOIN task_relations r ON r.task_id=owner.id
       JOIN tasks src ON src.id=r.source_task_id AND src.deleted_at IS NULL
       WHERE owner.exploration_id=$3 AND owner.deleted_at IS NULL
         AND src.exploration_id=n.exploration_id))),
  (SELECT cardinality(basis_node_ids) FROM hypotheses WHERE id=$1)`,
						hypID, factID, s.expID).Scan(&owned, &card); err != nil {
						return ExperimentOutcome{}, err
					}
					if owned == 1 && card < 32 { // 与 ValidateHypothesis 的 basis 上限一致（32）
						if _, err := tx.Exec(`UPDATE hypotheses SET basis_node_ids = array_append(basis_node_ids, $2)
							WHERE id=$1 AND NOT ($2 = ANY(basis_node_ids))`, hypID, factID); err != nil {
							return ExperimentOutcome{}, err
						}
					} else {
						out.Reason = "discriminated, but reported fact_id failed lineage/cap validation — basis not extended"
					}
				}
			}
		}
	default:
		out.Reason = fmt.Sprintf("observation matches %d declared expectations — inconclusive, no status change", len(out.Matches))
	}
	outcomeRaw, _ := json.Marshal(map[string]any{
		"observed": observed, "fact_id": factID, "matches": out.Matches,
		"matched_text": matched, "discriminated": out.Discriminated, "reason": out.Reason,
	})
	if _, err := tx.Exec(`UPDATE experiments SET status=$2, outcome=$3, observed_at=now() WHERE id=$1`,
		experimentID, out.Status, outcomeRaw); err != nil {
		return ExperimentOutcome{}, err
	}
	return out, tx.Commit()
}

// OpenExperiments 渲染给 planner 的当前信念态（开放实验 + 活跃假设分组）。
func (s *ExplorationStore) OpenExperiments() ([]Experiment, error) {
	rows, err := s.db.Query(`SELECT id, exploration_id, question, action,
		array_to_json(hypothesis_ids)::text, expectations, intent_id, status, outcome, stop_condition
		FROM experiments WHERE exploration_id=$1 AND status IN ('declared','dispatched') ORDER BY id`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Experiment, 0, 4)
	for rows.Next() {
		var e Experiment
		var expRaw, outRaw []byte
		var hypRaw string
		var intentID sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ExplorationID, &e.Question, &e.Action, &hypRaw, &expRaw, &intentID, &e.Status, &outRaw, &e.StopCondition); err != nil {
			return nil, err
		}
		e.IntentID = intentID.Int64
		_ = json.Unmarshal([]byte(hypRaw), &e.HypothesisIDs)
		_ = json.Unmarshal(expRaw, &e.Expectations)
		if len(outRaw) > 0 {
			_ = json.Unmarshal(outRaw, &e.Outcome)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InvalidateHypothesesOnNodeTx 在调用方事务内把 basis 含 nodeID 的 supported
// 假设降级为 invalidated（M3.2 条件失效：反证时刻与状态翻转原子）。返回被
// 失效的假设 id。active/refuted/superseded 不动——只有"依据该事实成立"的
// supported 判定才被反证波及。
func InvalidateHypothesesOnNodeTx(tx *sql.Tx, nodeID int64, reason string) ([]int64, error) {
	rows, err := tx.Query(`SELECT id FROM hypotheses WHERE status='supported' AND $1=ANY(basis_node_ids)`, nodeID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	demoted := make([]int64, 0, len(ids))
	for _, id := range ids {
		res, err := tx.Exec(`UPDATE hypotheses SET status='invalidated', updated_at=now() WHERE id=$1 AND status='supported'`, id)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // 审查 P2：并发迁移过的行不再失效——不伪造 from_status='supported' 事件
		}
		if _, err := tx.Exec(`INSERT INTO hypothesis_events(hypothesis_id, from_status, to_status, reason)
			VALUES ($1,'supported','invalidated',$2)`, id, reason); err != nil {
			return nil, err
		}
		demoted = append(demoted, id)
	}
	return demoted, nil
}
