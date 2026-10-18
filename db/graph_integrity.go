package db

import (
	"fmt"
)

// M1b.2b（research）：图一致性只读诊断。
//
// M1b.2a 之前节点与血缘边是分离的 autocommit 语句且吞错，历史数据可能存在
// 脱链节点；M1b.2b 盘点还确认了 CancelIntent 并发竞态可产生新的无边节点。
// 本文件提供只读"对账"：列出病灶（不含修复——修复决策属人/后续里程碑），
// 并检查 worker 中断留下的悬挂工具调用（tool_use 无 tool_result 配对——
// 重跑时其外部效果未知，应作为"效果未知"上下文注入而非盲目重放）。
// 挂载面（API/UI）属 M2.3 只读检查面；当前经 db 层 + 测试/脚本使用。

// IntegrityCheck 是一类一致性检查的标识。
type IntegrityCheck string

const (
	CheckFactNoYields      IntegrityCheck = "fact_without_yields"         // 非 origin fact 无 yields 入边
	CheckIntentNoDerived   IntegrityCheck = "intent_without_derived_from" // intent 无 derived_from 入边（legacy human 直投为已知来源）
	CheckGoalNoSpawns      IntegrityCheck = "goal_without_spawns"         // goal 无 spawns 入边
	CheckFindingNoYields   IntegrityCheck = "finding_without_yields"      // finding 节点无 yields 入边
	CheckMetGoalNoProves   IntegrityCheck = "met_goal_without_proves"     // met 目标无 proves 边（M1b.2a 前 prove_goal 吞错）
	CheckFindingNodeBroken IntegrityCheck = "finding_node_broken"         // findings 行指向悬空/类型不符节点
	CheckDanglingToolUse   IntegrityCheck = "dangling_tool_use"           // tool_use 活动无 tool_result 配对（效果未知）
)

// IntegrityFinding 是一条病灶记录（只读诊断输出，非修复指令）。
type IntegrityFinding struct {
	Check         IntegrityCheck `json:"check"`
	ExplorationID int64          `json:"exploration_id"`
	NodeID        int64          `json:"node_id,omitempty"`
	ActivityID    int64          `json:"activity_id,omitempty"`
	Origin        string         `json:"origin,omitempty"` // intent 类携带 origin，便于区分 legacy human 直投
}

// integrityQueries 每类检查一条只读 SQL（LIMIT 有界）。跨探索血缘边是继承
// 语义的合法形态，不作为病灶判据；判据只看"应有入边的节点缺入边"。
var integrityQueries = map[IntegrityCheck]string{
	CheckFactNoYields: `
SELECT n.exploration_id, n.id, '' FROM exploration_nodes n
WHERE n.kind='fact' AND n.state <> 'origin'
  AND NOT EXISTS (SELECT 1 FROM exploration_edges e WHERE e.dst_id=n.id AND e.rel='yields')
LIMIT 200`,
	CheckIntentNoDerived: `
SELECT n.exploration_id, n.id, n.origin FROM exploration_nodes n
WHERE n.kind='intent'
  AND NOT EXISTS (SELECT 1 FROM exploration_edges e WHERE e.dst_id=n.id AND e.rel='derived_from')
LIMIT 200`,
	CheckGoalNoSpawns: `
SELECT n.exploration_id, n.id, '' FROM exploration_nodes n
WHERE n.kind='goal'
  AND NOT EXISTS (SELECT 1 FROM exploration_edges e WHERE e.dst_id=n.id AND e.rel='spawns')
LIMIT 200`,
	CheckFindingNoYields: `
SELECT n.exploration_id, n.id, '' FROM exploration_nodes n
WHERE n.kind='finding'
  AND NOT EXISTS (SELECT 1 FROM exploration_edges e WHERE e.dst_id=n.id AND e.rel='yields')
LIMIT 200`,
	CheckMetGoalNoProves: `
SELECT n.exploration_id, n.id, '' FROM exploration_nodes n
WHERE n.kind='goal' AND n.state='met'
  AND NOT EXISTS (SELECT 1 FROM exploration_edges e WHERE e.dst_id=n.id AND e.rel='proves')
LIMIT 200`,
	CheckFindingNodeBroken: `
SELECT COALESCE(n.exploration_id, 0), COALESCE(f.node_id, 0), ''
FROM findings f LEFT JOIN exploration_nodes n ON n.id=f.node_id
WHERE f.node_id IS NOT NULL AND (n.id IS NULL OR n.kind <> 'finding')
LIMIT 200`,
	CheckDanglingToolUse: `
SELECT u.exploration_id, 0, u.id
FROM activity u
LEFT JOIN activity r ON r.exploration_id=u.exploration_id
  AND r.tool_use_id=u.tool_use_id AND r.kind='tool_result'
WHERE u.kind='tool_use' AND r.id IS NULL
LIMIT 200`,
}

var integrityOrder = []IntegrityCheck{
	CheckFactNoYields, CheckIntentNoDerived, CheckGoalNoSpawns, CheckFindingNoYields,
	CheckMetGoalNoProves, CheckFindingNodeBroken, CheckDanglingToolUse,
}

// GraphIntegrityReport 执行全部只读一致性检查，返回（限量）病灶清单与各类
// 计数（计数不受 LIMIT 影响，单独 COUNT 查询）。只报不修。
func (d *DB) GraphIntegrityReport() (findings []IntegrityFinding, counts map[IntegrityCheck]int64, err error) {
	counts = map[IntegrityCheck]int64{}
	findings = make([]IntegrityFinding, 0, 32)
	for _, check := range integrityOrder {
		query := integrityQueries[check]
		// 计数：把 LIMIT 去掉换成 COUNT(*) 包裹。
		countQuery := "SELECT count(*) FROM (" + query[:len(query)-len("LIMIT 200")] + ") c"
		var cnt int64
		if qerr := d.QueryRow(countQuery).Scan(&cnt); qerr != nil {
			return nil, nil, fmt.Errorf("integrity count %s: %w", check, qerr)
		}
		counts[check] = cnt
		if cnt == 0 {
			continue
		}
		rows, qerr := d.Query(query)
		if qerr != nil {
			return nil, nil, fmt.Errorf("integrity list %s: %w", check, qerr)
		}
		for rows.Next() {
			var f IntegrityFinding
			f.Check = check
			var nodeID, actID int64
			var origin string
			switch check {
			case CheckDanglingToolUse:
				if err := rows.Scan(&f.ExplorationID, &nodeID, &actID); err != nil {
					rows.Close()
					return nil, nil, fmt.Errorf("integrity scan %s: %w", check, err)
				}
				f.ActivityID = actID
			default:
				if err := rows.Scan(&f.ExplorationID, &nodeID, &origin); err != nil {
					rows.Close()
					return nil, nil, fmt.Errorf("integrity scan %s: %w", check, err)
				}
				f.NodeID = nodeID
				f.Origin = origin
			}
			findings = append(findings, f)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("integrity iterate %s: %w", check, err)
		}
		rows.Close()
	}
	return findings, counts, nil
}
