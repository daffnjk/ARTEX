package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// M1b.3b（research）：共享事务性完成操作。
//
// M1b.3a 盘点的三个 done 写入点（planner-met 非 CAS、goalless 抽干、超时收尾）
// 收敛到这一个入口：单事务内 (1) 锁任务行；(2) done 时权威复查"无 open goal"
//（消除 HasOpenGoal 检查与落库之间的竞态）；(3) 验收重校验——当前冻结验收版本
// 引用的每个 goal 节点必须已 met（harness/acceptance.go 的"最终完成必须原子性
// 重校验"语义）；(4) 终态 CAS（先到终态者胜，迟到 met 不再覆盖 timeout——
// 修复盘点 R6）；(5) completed_source 分记来源。
// 注意：本操作不新增任何"自动放行"——它验证的是图内目标状态与当前验收版本，
// 不消费 EvaluationReport（那要等 M2.3 的受控集成）；review 模式的工具级拦截
// 语义不变。

// ErrCompletionGoalOpen 表示仍有未达成目标，完成被拒绝（可恢复：达成后再试）。
var ErrCompletionGoalOpen = errors.New("completion refused: open goals remain")

// ErrCompletionAcceptanceUnmet 表示当前冻结验收版本引用的目标尚未全部 met。
var ErrCompletionAcceptanceUnmet = errors.New("completion refused: current frozen acceptance references unmet goals")

// terminalStatuses 与 tasks 状态 CHECK 一致。
var terminalStatuses = "('done','failed','timeout')"

// CompleteTaskGuarded 以单事务写入任务终态并分记来源。status 必须是终态之一；
// source 是完成来源标识（model:prove_goal / system:goalless-drain /
// system:timeout / human:... 等）。won=false 表示已有终态先到（保留原状态与
// 原来源，非错误）。done 类完成附带图一致性与验收重校验。
func (d *DB) CompleteTaskGuarded(id int64, status, source string) (won bool, err error) {
	switch status {
	case "done", "failed", "timeout":
	default:
		return false, fmt.Errorf("CompleteTaskGuarded: %q is not a terminal status", status)
	}
	tx, err := d.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var explorationID int64
	var mode string
	err = tx.QueryRow(`SELECT exploration_id,control_mode FROM tasks WHERE id=$1 FOR UPDATE`, id).Scan(&explorationID, &mode)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("CompleteTaskGuarded: task %d not found", id)
	}
	if err != nil {
		return false, err
	}
	if mode != "legacy" {
		result, closeErr := closeControlTaskTx(context.Background(), tx, id, ControlCloseRequest{Reason: closeReason(status, source), Source: source})
		if closeErr != nil {
			return false, closeErr
		}
		return result.Won, tx.Commit()
	}
	if status == "done" {
		// 图一致性：仍有 open goal 则拒绝（事务内权威读，锁下无竞态）。
		var openGoals int64
		if err := tx.QueryRow(`SELECT count(*) FROM exploration_nodes
			WHERE exploration_id=$1 AND kind='goal' AND state='open'`, explorationID).Scan(&openGoals); err != nil {
			return false, err
		}
		if openGoals > 0 {
			return false, fmt.Errorf("%w: task %d has %d open goals", ErrCompletionGoalOpen, id, openGoals)
		}
		// 验收重校验：当前冻结验收版本引用的 goal 节点必须已 met。
		var unmet int64
		if err := tx.QueryRow(`
SELECT count(*) FROM harness_acceptance a
JOIN exploration_nodes n ON n.id = a.goal_id AND n.exploration_id = $1
WHERE a.task_id = $2 AND a.is_current AND n.state <> 'met'`, explorationID, id).Scan(&unmet); err != nil {
			return false, err
		}
		if unmet > 0 {
			return false, fmt.Errorf("%w: task %d has %d unmet accepted goals", ErrCompletionAcceptanceUnmet, id, unmet)
		}
	}
	res, err := tx.Exec(`
UPDATE tasks
   SET status = $1,
       completed_at = COALESCE(completed_at, now()),
       completed_source = $2
 WHERE id = $3 AND status NOT IN `+terminalStatuses, status, source, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil // 已有终态先到：保留原状态与原来源
	}
	return true, tx.Commit()
}
