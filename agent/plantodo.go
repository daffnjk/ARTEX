package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// M1b.1（research）：planner 计划的持久化接入。
//
// 背景（norma v0.4.3 实测）：SDK TodoStore 只导出 List()，set() 私有；agentcore
// 在 Options.Todos != nil 时无条件追加 SDK 自带的 TodoWrite（注册表同名后到者
// 胜出，无法经 Options 包装）。因此持久化模式改为：Options.Todos 置 nil
// （harness 的 todo reminder 有 nil 保护，自动关闭），由本文件提供一个同名
// TodoWrite 工具注册进 planner 的基础工具列表——走 AugmentTools/ToolResolve 的
// 真实注册路径（tools 表无 TodoWrite 行，原样放行）。每次成功调用先经 CAS
// 落盘 PostgreSQL、后返回确认（persist-before-ack）。
//
// 认知视角：计划是控制律草稿。恢复（hydrate）失败必须停止本轮规划——绝不回退
// 空内存假装无事；CAS 冲突意味着另一个写入方（新 Planner 实例）已接管，本实例
// 是过期控制器，其计划不得覆盖新状态。

// PlanPersister 是 planner 依赖的计划持久化后端（*db.DB 实现）。接口让
// agent 包测试可以注入假持久层；生产装配见 server/task_llm.go。
type PlanPersister interface {
	LoadTaskPlan(explorationID int64) (*db.TaskPlan, error)
	SaveTaskPlan(plan db.TaskPlan) (db.TaskPlan, error)
}

// plannerPlanState 是一轮 Plan() 的计划工作状态：从权威存储 hydrate 而来，
// TodoWrite 每次成功落盘后更新。非并发安全——一个探索同一时刻只有一个规划轮
// （引擎去抖保证；跨实例并发由存储层 CAS 显式拒绝）。
type plannerPlanState struct {
	persister PlanPersister
	plan      db.TaskPlan
}

// loadPlannerPlanState 从权威存储恢复计划。读取失败原样上抛（调用方必须停止
// 本轮）；无记录=显式空计划（revision 0，首次保存创建）。
func loadPlannerPlanState(p PlanPersister, explorationID int64) (*plannerPlanState, error) {
	plan, err := p.LoadTaskPlan(explorationID)
	if err != nil {
		return nil, fmt.Errorf("hydrate task plan (exploration %d): %w", explorationID, err)
	}
	if plan == nil {
		plan = &db.TaskPlan{ExplorationID: explorationID, Revision: 0}
	}
	return &plannerPlanState{persister: p, plan: *plan}, nil
}

// todos 把计划步骤投影成 SDK 的 Todo 形状（提示渲染复用 renderPlannerTodos）。
func (st *plannerPlanState) todos() []actool.Todo {
	out := make([]actool.Todo, 0, len(st.plan.Steps))
	for _, s := range st.plan.Steps {
		out = append(out, actool.Todo{Content: s.Content, Status: actool.TodoStatus(s.Status)})
	}
	return out
}

// todoWriteAckText 与 SDK TodoWrite 的确认文案一致（提示兼容）；追加 revision
// 信息，让模型知道计划已权威落盘。
const todoWriteAckText = "Todos have been modified successfully. Keep the todo list current and continue with the tasks. (saved, revision %d)"

// todoTool 构造持久化 TodoWrite 工具。exp 用于 basis 节点归属校验（可为 nil
// 的仅测试场景：跳过归属校验）。
func (st *plannerPlanState) todoTool(exp *db.ExplorationStore) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "TodoWrite",
		Description: "Records and updates a structured todo list for the current task. Use it to plan multi-step work and to mark progress — replace the whole list each call, marking exactly one item in_progress while working. The list persists across planning rounds in the task's authoritative storage.",
		Prompt: "status is one of: pending, in_progress, completed. Keep the list current: mark a task completed as soon as it's done; don't batch completions. " +
			"Optional per item: basis_node_ids (exploration node ids this step derives from — facts/intents already in the graph) and expect (what output this step should produce). Optional plan-level note records overall reasoning.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content":        map[string]any{"type": "string"},
							"status":         map[string]any{"type": "string", "enum": []any{"pending", "in_progress", "completed"}},
							"basis_node_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
							"expect":         map[string]any{"type": "string"},
						},
						"required": []any{"content", "status"},
					},
				},
				"note": map[string]any{"type": "string"},
			},
			"required": []any{"todos"},
		},
		ReadOnly:   func(json.RawMessage) bool { return false },
		Concurrent: func(json.RawMessage) bool { return false },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(_ context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var in struct {
				Todos []struct {
					Content      string  `json:"content"`
					Status       string  `json:"status"`
					BasisNodeIDs []int64 `json:"basis_node_ids"`
					Expect       string  `json:"expect"`
				} `json:"todos"`
				Note string `json:"note"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return actool.Result{}, err
			}
			incoming := make([]db.PlanStep, 0, len(in.Todos))
			for _, it := range in.Todos {
				incoming = append(incoming, db.PlanStep{
					Content:        it.Content,
					Status:         it.Status,
					BasisNodeIDs:   it.BasisNodeIDs,
					ExpectedOutput: it.Expect,
				})
			}
			if err := db.ValidatePlanSteps(incoming); err != nil {
				return actool.Errorf(fmt.Sprintf("TodoWrite rejected: %v", err)), nil
			}
			if err := db.ValidatePlanNotes(in.Note); err != nil {
				return actool.Errorf(fmt.Sprintf("TodoWrite rejected: %v", err)), nil
			}
			// basis 归属校验：引用的节点必须属于本探索（防跨任务引用/臆造 id）。
			if exp != nil {
				var allBasis []int64
				for _, s := range incoming {
					allBasis = append(allBasis, s.BasisNodeIDs...)
				}
				if len(allBasis) > 0 {
					missing, err := exp.PlanBasisNodesOwned(allBasis)
					if err != nil {
						return actool.Result{}, fmt.Errorf("TodoWrite basis check: %w", err)
					}
					if len(missing) > 0 {
						return actool.Errorf(fmt.Sprintf(
							"TodoWrite rejected: basis_node_ids %v do not belong to this task's exploration graph; use node ids from list_facts/graph_overview",
							missing)), nil
					}
				}
			}
			merged := db.MergePlanSteps(st.plan.Steps, incoming)
			saved, err := st.persister.SaveTaskPlan(db.TaskPlan{
				ExplorationID: st.plan.ExplorationID,
				Revision:      st.plan.Revision, // CAS 期望值：本轮已知的权威 revision
				Steps:         merged,
				ModelNotes:    in.Note,
			})
			if err != nil {
				var conflict *db.PlanConflictError
				if errors.As(err, &conflict) {
					return actool.Errorf(fmt.Sprintf(
						"TodoWrite conflict: this plan was updated by another writer (storage is at revision %d; yours was %d). Your list is stale — stop editing the plan this round; the next planning round re-reads it.",
						conflict.HaveRevision, st.plan.Revision)), nil
				}
				// 存储读写失败：不确认、不静默丢计划——把失败作为工具错误上抛。
				return actool.Result{}, fmt.Errorf("TodoWrite persistence failed (plan NOT saved): %w", err)
			}
			// 先持久化（上面已成功）后确认；内存镜像更新为落盘结果。
			st.plan = saved
			return actool.Text(fmt.Sprintf(todoWriteAckText, saved.Revision)), nil
		},
	})
}
