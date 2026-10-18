package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// M1b.2a（research）：图领域原子写操作。
//
// 盘点结论（task-specs/M1b.2a.md）：四个高危缺口同病根——节点 INSERT 与血缘边
// INSERT 是两条独立 autocommit 语句且 `_ = Link(...)` 吞错，连边失败留下脱链
// 节点（fact 无 yields、intent 无 derived_from、goal 无 spawns、prove_goal 无
// proves 边甚至假报成功）。本文件把"节点+它的血缘边"收敛为单个事务：任何边
// 失败整体回滚，节点不落库；边保持 ON CONFLICT DO NOTHING 幂等。

// nodeEdge 是随节点原子创建的一条血缘边。FromNew=true 时 src=新节点、
// dst=Other；否则 src=Other、dst=新节点。
type nodeEdge struct {
	Rel     string
	Other   int64
	FromNew bool
}

var errNodeBoundary = errors.New("lineage edge target does not belong to this exploration")

// addNodeLinked 在单个事务内写入节点、锚与全部血缘边；边的对端必须属于本探索
// （任务边界检查），否则整体回滚。
func (s *ExplorationStore) addNodeLinked(kind string, payload map[string]any, priority int, state, origin string, anchors []int64, edges []nodeEdge) (int64, error) {
	raw, _ := json.Marshal(payload)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`
INSERT INTO exploration_nodes(exploration_id, kind, payload, priority, state, origin)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		s.expID, kind, string(raw), priority, state, origin).Scan(&id); err != nil {
		return 0, err
	}
	for _, a := range anchors {
		if _, err := tx.Exec(`INSERT INTO exploration_anchors(node_id, asset_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, a); err != nil {
			return 0, err
		}
	}
	for _, e := range edges {
		if e.Other <= 0 {
			continue
		}
		// 任务边界：血缘对端必须在本探索内（防跨任务误连）。
		var belong int64
		if err := tx.QueryRow(`SELECT id FROM exploration_nodes WHERE id=$1 AND exploration_id=$2`, e.Other, s.expID).Scan(&belong); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, fmt.Errorf("%w: node %d (rel %s)", errNodeBoundary, e.Other, e.Rel)
			}
			return 0, err
		}
		src, dst := e.Other, id
		if e.FromNew {
			src, dst = id, e.Other
		}
		if _, err := tx.Exec(`
INSERT INTO exploration_edges(exploration_id, src_id, rel, dst_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (exploration_id, src_id, rel, dst_id) DO NOTHING`, s.expID, src, e.Rel, dst); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// AddIntentWithParents 原子写入意图节点与全部 derived_from 血缘边（parent→
// 意图）；无显式 parent 时回连 origin fact（任务根），与既有语义一致。
func (s *ExplorationStore) AddIntentWithParents(payload map[string]any, priority int, anchors []int64, origin string, parentIDs []int64) (int64, error) {
	if origin == "" {
		origin = "planner"
	}
	edges := make([]nodeEdge, 0, len(parentIDs)+1)
	for _, p := range parentIDs {
		if p > 0 {
			edges = append(edges, nodeEdge{Rel: RelDerivedFrom, Other: p})
		}
	}
	if len(edges) == 0 {
		if of, err := s.OriginFactID(); err == nil && of > 0 {
			edges = append(edges, nodeEdge{Rel: RelDerivedFrom, Other: of})
		}
	}
	return s.addNodeLinked(KindIntent, payload, priority, "open", origin, anchors, edges)
}

// AddFactWithYields 原子写入 fact 节点与 yields 边（intent→fact）。intentID<=0
// 时不连边（与旧行为一致——无意图的全局事实）。
func (s *ExplorationStore) AddFactWithYields(payload map[string]any, anchors []int64, origin string, intentID int64) (int64, error) {
	var edges []nodeEdge
	if intentID > 0 {
		edges = append(edges, nodeEdge{Rel: RelYields, Other: intentID})
	}
	return s.addNodeLinked(KindFact, payload, 5, "confirmed", origin, anchors, edges)
}

// AddGoalWithRoot 原子写入 goal 节点与 spawns 边（origin fact→goal）。origin
// fact 是任务根（创建时落库、从不删除），用 OriginFactID 解析即与既有语义一致。
func (s *ExplorationStore) AddGoalWithRoot(payload map[string]any, origin string) (int64, error) {
	if origin == "" {
		origin = "system"
	}
	var edges []nodeEdge
	if of, err := s.OriginFactID(); err == nil && of > 0 {
		edges = append(edges, nodeEdge{Rel: RelSpawns, Other: of})
	}
	return s.addNodeLinked(KindGoal, payload, 0, "open", origin, nil, edges)
}

// ErrGoalNotOpen 表示目标不在可证明状态（不存在/非 goal/已 met 之外的异常态）。
var ErrGoalNotOpen = errors.New("goal node not in provable state")

// ProveGoal 在单个事务内完成：锁定并校验目标节点 → 写 proves 边（证据→目标，
// 幂等）→ 谓词翻转 open→met。目标已 met 时为幂等成功（边仍补齐）；目标不存在
// 或非本探索 goal 返回显式错误。证据节点允许来自直接关联任务（与既有
// GetNodeWithSources 继承语义一致）。
func (s *ExplorationStore) ProveGoal(goalID, evidenceID int64) error {
	if goalID <= 0 || evidenceID <= 0 {
		return fmt.Errorf("prove goal: goal and evidence ids must be positive")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind, state string
	err = tx.QueryRow(`SELECT kind, state FROM exploration_nodes WHERE id=$1 AND exploration_id=$2 FOR UPDATE`,
		goalID, s.expID).Scan(&kind, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("prove goal: node %d not found in this exploration", goalID)
	}
	if err != nil {
		return err
	}
	if kind != KindGoal {
		return fmt.Errorf("prove goal: node %d is %q, not a goal", goalID, kind)
	}
	if _, err := tx.Exec(`
INSERT INTO exploration_edges(exploration_id, src_id, rel, dst_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (exploration_id, src_id, rel, dst_id) DO NOTHING`, s.expID, evidenceID, RelProves, goalID); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE exploration_nodes SET state='met', blocked_reason=NULL, content_version=content_version+1
WHERE id=$1 AND exploration_id=$2 AND kind='goal' AND state='open'`, goalID, s.expID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 && state != "met" {
		return fmt.Errorf("%w: goal %d state=%q", ErrGoalNotOpen, goalID, state)
	}
	// n==0 且 state=='met'：已被证明（可能正是本证据）——幂等成功。
	return tx.Commit()
}
