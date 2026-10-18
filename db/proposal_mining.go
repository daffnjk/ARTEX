package db

import (
	"fmt"

	"github.com/Autumn-27/artex/harness"
)

// M4.1（research）：从错误轨迹生成改进提案（慢环）。
//
// 认知角色：复盘只生成【受审查的提案】——绝不自改授权/验收标准/发布规则
//（harness.ChangeProposal 契约：RequiresHumanApproval 恒真，无 AutoApply）。
// 挖掘源（全部来自权威轨迹）：条件失效事件流（basis 反证连锁）、不区分实验
//（inconclusive——实验设计缺陷信号）、评议存档（advisory 中的 high 问题）。

// MineChangeProposals 从探索的错误轨迹离线挖掘结构化改进提案。只读；输出
// 仅是提案（需人工审批才可能生效），不落库、不执行。
func (s *ExplorationStore) MineChangeProposals() ([]harness.ChangeProposal, error) {
	// 1) 条件失效轨迹：哪些 basis 节点引发了 supported→invalidated 连锁。
	rows, err := s.db.Query(`
SELECT e.reason, count(*) FROM hypothesis_events e
JOIN hypotheses h ON h.id=e.hypothesis_id
WHERE h.exploration_id=$1 AND e.to_status='invalidated'
GROUP BY e.reason ORDER BY count(*) DESC LIMIT 10`, s.expID)
	if err != nil {
		return nil, err
	}
	type invTraj struct {
		reason string
		n      int64
	}
	var inversions []invTraj
	for rows.Next() {
		var t invTraj
		if err := rows.Scan(&t.reason, &t.n); err != nil {
			rows.Close()
			return nil, err
		}
		inversions = append(inversions, t)
	}
	rows.Close()

	// 2) 不区分实验：设计缺陷信号（期望无法区分、层级混淆等）。
	var inconclusiveIDs []string
	rows, err = s.db.Query(`SELECT id::text FROM experiments
		WHERE exploration_id=$1 AND status='inconclusive' ORDER BY id DESC LIMIT 10`, s.expID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		inconclusiveIDs = append(inconclusiveIDs, id)
	}
	rows.Close()

	// 3) 评议 high 问题（存档 advisory 的结构化引用）。
	var reviewIDs []string
	rows, err = s.db.Query(`SELECT id::text FROM hypothesis_reviews
		WHERE exploration_id=$1 ORDER BY id DESC LIMIT 5`, s.expID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		reviewIDs = append(reviewIDs, id)
	}
	rows.Close()

	// 回放参考：优先评议存档；无评议时用可重放的轨迹引用本身（实验 id /
	// 失效事件描述都是可复演的报告来源）。
	replayRefs := reviewIDs
	if len(replayRefs) == 0 {
		replayRefs = inconclusiveIDs
	}
	var proposals []harness.ChangeProposal
	if len(inversions) > 0 {
		invRefs := replayRefs
		if len(invRefs) == 0 {
			invRefs = []string{"invalidation trajectory (hypothesis_events)"}
		}
		var evidence []string
		for _, t := range inversions {
			evidence = append(evidence, fmt.Sprintf("invalidation x%d: %s", t.n, t.reason))
		}
		p := harness.ChangeProposal{
			SchemaVersion:         harness.SchemaVersion,
			ID:                    fmt.Sprintf("exp%d-basis-hardening", s.expID),
			FailureEvaluationIDs:  evidence,
			AffectedComponents:    []string{"hypothesis basis discipline (state_hypotheses guidance)"},
			ExpectedBenefit:       "减少建立在单一可反证依据上的 supported 假设：要求关键假设给出多源 basis 或先做廉价稳固实验",
			RegressionRisks:       []string{"basis 强制可能提高声明成本、降低假设覆盖速度"},
			ReplayReportRefs:      invRefs,
			RequiresHumanApproval: true,
		}
		if err := p.Validate(); err == nil {
			proposals = append(proposals, p)
		}
	}
	if len(inconclusiveIDs) > 0 {
		p := harness.ChangeProposal{
			SchemaVersion:         harness.SchemaVersion,
			ID:                    fmt.Sprintf("exp%d-experiment-discrimination", s.expID),
			FailureEvaluationIDs:  inconclusiveIDs,
			AffectedComponents:    []string{"experiment design guidance (declare_experiment prompt)"},
			ExpectedBenefit:       "提高预注册实验的区分度：要求期望互斥且与假设层级一致（外环评议已捕获层级混淆案例）",
			RegressionRisks:       []string{"更严格的期望格式可能减少实验数量"},
			ReplayReportRefs:      replayRefs,
			RequiresHumanApproval: true,
		}
		if err := p.Validate(); err == nil {
			proposals = append(proposals, p)
		}
	}
	return proposals, nil
}
