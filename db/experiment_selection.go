package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// M3.3（research）：可解释实验候选选择。
//
// 认知角色：主动辨识的"下一步选哪个实验"。排序信号用可解释的代理指标——
// 区分潜力（实验覆盖的活跃假设数；问题仍有 ≥2 活跃假设才算未决）——不引入
// 黑盒评分。硬边界过滤：action 与任务 deny 约束冲突的候选被显式排除并给出
// 排除理由（不静默）。决策可审计：推荐与排除的理由随候选一起返回，供渲染
// 与存档。

// queryer 是 denyConflictOn 的最小执行面（*sql.DB 与 *sql.Tx 都满足）。
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// ExperimentCandidate 是一个可解释的实验候选（含推荐/排除理由）。
type ExperimentCandidate struct {
	Experiment
	ActiveHypotheses int64    `json:"active_hypotheses"` // 参试中仍活跃的假设数
	QuestionOpen     bool     `json:"question_open"`     // 问题是否未决（≥2 活跃）
	Score            int64    `json:"score"`
	Reasons          []string `json:"reasons"`
	Excluded         bool     `json:"excluded"`
	ExclusionReason  string   `json:"exclusion_reason,omitempty"`
}

// denyConflict 返回与 action 冲突的 deny 约束原文（互含判定；无冲突返回 ""）。
// 推荐排序（提示层）与 DispatchExperiment（强制层）共用同一判据——审查 P2：
// 边界不能只靠渲染提示，派发路径必须有硬拒绝。
func (s *ExplorationStore) denyConflict(action string) (string, error) {
	return denyConflictOn(s.db, s.expID, action)
}

// denyConflictOn 在指定执行器（连接或事务）上做同一判据——派发路径在单事务内
// 检查+更新，堵住跨连接 check-then-act 窗口（批审 P2）。
func denyConflictOn(q queryer, expID int64, action string) (string, error) {
	rows, err := q.Query(`SELECT text FROM task_constraints WHERE exploration_id=$1 AND kind='deny'`, expID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	actLower := strings.ToLower(action)
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return "", err
		}
		norm := strings.ToLower(strings.TrimSpace(text))
		if norm != "" && (strings.Contains(actLower, norm) || strings.Contains(norm, actLower)) {
			return text, nil
		}
	}
	return "", rows.Err()
}

// RecommendExperiments 返回开放实验的可解释排序（含被硬边界排除者，显式标注）。
// 已派发(dispatched)优先于已声明(declared)——在跑的先看结果；同级按区分潜力降序。
// deny 约束（task_constraints kind='deny'）与 action 文本互含即视为边界冲突。
func (s *ExplorationStore) RecommendExperiments() ([]ExperimentCandidate, error) {
	exps, err := s.OpenExperiments()
	if err != nil {
		return nil, err
	}
	hyps, err := s.ListHypotheses()
	if err != nil {
		return nil, err
	}
	activeByQ := map[string]int64{}
	activeByID := map[int64]bool{}
	for _, h := range hyps {
		if h.Status == "active" {
			activeByQ[h.Question]++
			activeByID[h.ID] = true
		}
	}
	denies := map[string]string{} // 约束文本 → 原文
	rows, err := s.db.Query(`SELECT text FROM task_constraints WHERE exploration_id=$1 AND kind='deny'`, s.expID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			return nil, err
		}
		denies[strings.ToLower(strings.TrimSpace(text))] = text
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ExperimentCandidate, 0, len(exps))
	for _, e := range exps {
		c := ExperimentCandidate{Experiment: e}
		for _, id := range e.HypothesisIDs {
			if activeByID[id] {
				c.ActiveHypotheses++
			}
		}
		c.QuestionOpen = activeByQ[e.Question] >= 2
		c.Score = c.ActiveHypotheses
		if e.Status == "dispatched" {
			c.Score += 1000 // 在跑的优先收结果
			c.Reasons = append(c.Reasons, "已派发——优先等待/收取结果")
		}
		if c.QuestionOpen {
			c.Reasons = append(c.Reasons, fmt.Sprintf("问题「%s」未决（≥2 活跃假设）", e.Question))
		} else {
			c.Reasons = append(c.Reasons, "问题已收敛或未保持 ≥2 活跃假设——低优先")
		}
		c.Reasons = append(c.Reasons, fmt.Sprintf("覆盖 %d 个活跃假设（区分潜力）", c.ActiveHypotheses))
		// 硬边界过滤：deny 约束与 action 互含（双向子串）即冲突——显式排除。
		actLower := strings.ToLower(e.Action)
		for norm, orig := range denies {
			if strings.Contains(actLower, norm) || strings.Contains(norm, actLower) {
				c.Excluded = true
				c.ExclusionReason = fmt.Sprintf("操作约束冲突：任务 deny 约束「%s」", orig)
				break
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Excluded != out[j].Excluded {
			return !out[i].Excluded // 未排除者在前
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}
