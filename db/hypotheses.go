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
	if len(items) < 1 {
		return fmt.Errorf("at least one hypothesis per question required")
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
			err := tx.QueryRow(`SELECT n.id FROM exploration_nodes n WHERE n.id=$1 AND (
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
			if _, err := tx.Exec(`UPDATE hypotheses SET notes=$2, basis_node_ids=$3, updated_at=now() WHERE id=$1`,
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
	for hypID, exp := range e.Expectations {
		if strings.TrimSpace(exp) == "" || len(exp) > MaxExpectationBytes {
			return 0, fmt.Errorf("expectation for hypothesis %d must be 1..%d bytes", hypID, MaxExpectationBytes)
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
func (s *ExplorationStore) DispatchExperiment(experimentID, intentID int64) error {
	res, err := s.db.Exec(`UPDATE experiments SET intent_id=$2, status='dispatched', dispatched_at=now()
		WHERE id=$1 AND exploration_id=$3 AND status='declared'`, experimentID, intentID, s.expID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("experiment %d not in declared state", experimentID)
	}
	return nil
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
	out := ExperimentOutcome{Status: "inconclusive"}
	for hypID, expected := range expectations {
		if observed == expected || strings.Contains(observed, expected) || strings.Contains(expected, observed) {
			out.Matches = append(out.Matches, hypID)
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
			if _, err := tx.Exec(`UPDATE hypotheses SET status=$2, updated_at=now() WHERE id=$1 AND status='active'`, hypID, to); err != nil {
				return ExperimentOutcome{}, err
			}
			if _, err := tx.Exec(`INSERT INTO hypothesis_events(hypothesis_id, from_status, to_status, reason, experiment_id)
				VALUES ($1,'active',$2,$3,$4)`, hypID, to, "discriminated by experiment outcome", experimentID); err != nil {
				return ExperimentOutcome{}, err
			}
		}
	default:
		out.Reason = fmt.Sprintf("observation matches %d declared expectations — inconclusive, no status change", len(out.Matches))
	}
	outcomeRaw, _ := json.Marshal(map[string]any{
		"observed": observed, "fact_id": factID, "matches": out.Matches, "discriminated": out.Discriminated, "reason": out.Reason,
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
		if err := rows.Scan(&e.ID, &e.ExplorationID, &e.Question, &e.Action, &hypRaw, &expRaw, &e.IntentID, &e.Status, &outRaw, &e.StopCondition); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(hypRaw), &e.HypothesisIDs)
		_ = json.Unmarshal(expRaw, &e.Expectations)
		if len(outRaw) > 0 {
			_ = json.Unmarshal(outRaw, &e.Outcome)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
