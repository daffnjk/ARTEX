package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// M3.4（research）：干净上下文 Reviewer 的评议包与存档。
//
// 认知角色：外环审度。Reviewer 不携带 planner 的对话历史（干净上下文），
// 只看权威状态（假设+事件流、实验+结果、图一致性、最近事实），输出【建议性】
// 批评：未被支撑的断言、矛盾、该失效未失效的依赖、实验区分度缺陷。评议
// 只存档展示——Reviewer 无工具、无任何写权限，状态变更永远走既有规则路径。

// ReviewPacket 是评议包（干净上下文的全部输入）。
type ReviewPacket struct {
	ExplorationID   int64                  `json:"exploration_id"`
	Hypotheses      []Hypothesis           `json:"hypotheses"`
	Events          []HypothesisEventRow   `json:"events"`
	Experiments     []Experiment           `json:"experiments"`
	Integrity       *IntegrityReportScoped `json:"integrity,omitempty"`
	RecentFacts     []string               `json:"recent_facts"`
	Recommendations []ExperimentCandidate  `json:"experiment_recommendations"`
}

// HypothesisEventRow 是假设事件流的行（含假设语句，便于干净上下文自含）。
type HypothesisEventRow struct {
	HypothesisID int64  `json:"hypothesis_id"`
	Statement    string `json:"statement"`
	FromStatus   string `json:"from_status"`
	ToStatus     string `json:"to_status"`
	Reason       string `json:"reason"`
}

// BuildReviewPacket 从权威状态组装评议包（干净上下文：不含任何对话/提示词）。
func (s *ExplorationStore) BuildReviewPacket() (*ReviewPacket, error) {
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
SELECT e.hypothesis_id, h.statement, e.from_status, e.to_status, e.reason
FROM hypothesis_events e JOIN hypotheses h ON h.id=e.hypothesis_id
WHERE h.exploration_id=$1 ORDER BY e.id DESC LIMIT 100`, s.expID)
	if err != nil {
		return nil, err
	}
	events := make([]HypothesisEventRow, 0, 8)
	for rows.Next() {
		var r HypothesisEventRow
		if err := rows.Scan(&r.HypothesisID, &r.Statement, &r.FromStatus, &r.ToStatus, &r.Reason); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	exps, err := s.OpenExperiments()
	if err != nil {
		return nil, err
	}
	// 已结实验（observed/inconclusive）对评议同样关键——补充读取。
	rows, err = s.db.Query(`SELECT id, exploration_id, question, action,
		array_to_json(hypothesis_ids)::text, expectations, intent_id, status, outcome, stop_condition
		FROM experiments WHERE exploration_id=$1 AND status IN ('observed','inconclusive') ORDER BY id DESC LIMIT 20`, s.expID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Experiment
		var expRaw, outRaw []byte
		var hypRaw string
		var intentID sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ExplorationID, &e.Question, &e.Action, &hypRaw, &expRaw, &intentID, &e.Status, &outRaw, &e.StopCondition); err != nil {
			rows.Close()
			return nil, err
		}
		e.IntentID = intentID.Int64
		_ = json.Unmarshal([]byte(hypRaw), &e.HypothesisIDs)
		_ = json.Unmarshal(expRaw, &e.Expectations)
		if len(outRaw) > 0 {
			_ = json.Unmarshal(outRaw, &e.Outcome)
		}
		exps = append(exps, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	recentRows, err := s.db.Query(`SELECT payload->>'summary' FROM exploration_nodes
		WHERE exploration_id=$1 AND kind='fact' AND state='confirmed' ORDER BY id DESC LIMIT 10`, s.expID)
	if err != nil {
		return nil, err
	}
	facts := make([]string, 0, 10)
	for recentRows.Next() {
		var sum string
		if err := recentRows.Scan(&sum); err != nil {
			recentRows.Close()
			return nil, err
		}
		facts = append(facts, sum)
	}
	recentRows.Close()
	if err := recentRows.Err(); err != nil {
		return nil, err
	}
	recs, err := s.RecommendExperiments()
	if err != nil {
		return nil, err
	}
	var integrity *IntegrityReportScoped
	// 任务范围内诊断（探索级）。审查 P2：读取失败不静默省略——完整性报告是
	// reviewer 的审计输入，读不到就整个包失败（fail-closed），不能悄悄变干净。
	var taskID int64
	switch err := s.db.QueryRow(`SELECT id FROM tasks WHERE exploration_id=$1 AND deleted_at IS NULL`, s.expID).Scan(&taskID); {
	case errors.Is(err, sql.ErrNoRows): // 探索无任务：合法略过
	case err != nil:
		return nil, err
	default:
		if err := s.db.TaskIntegrityReportScoped(taskID, &integrity); err != nil {
			return nil, err
		}
	}
	return &ReviewPacket{
		ExplorationID: s.expID, Hypotheses: hyps, Events: events, Experiments: exps,
		Integrity: integrity, RecentFacts: facts, Recommendations: recs,
	}, nil
}

// StoreReview 存档一次评议（append-only；usage 记录计量）。
func (s *ExplorationStore) StoreReview(model string, advisory map[string]any, usage map[string]any) error {
	advRaw, err := json.Marshal(advisory)
	if err != nil {
		return err
	}
	useRaw, _ := json.Marshal(usage)
	_, err = s.db.Exec(`INSERT INTO hypothesis_reviews(exploration_id, model, advisory, usage)
		VALUES ($1,$2,$3,$4)`, s.expID, model, advRaw, useRaw)
	if err != nil {
		return fmt.Errorf("store review: %w", err)
	}
	return nil
}
