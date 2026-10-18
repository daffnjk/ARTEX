package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// M3.1（research）：假设/实验工具——条件化认知进入真实调用链。
//
// state_hypotheses（planner）：声明一个问题的竞争假设集（整体替换式）。
// declare_experiment（planner）：预注册区分性实验（期望在派发前写定）。
// report_experiment_outcome（worker）：上报实验观察 → 存储层做期望匹配与
// 假设状态判定（唯一匹配才区分；歧义不判定——模型不能自证假设）。
// 假设不是证据：它们不进 prove_goal 的 evidence（fact/finding 边界不变）。

func (t *ToolSet) stateHypotheses() actool.CoreTool {
	return t.writeExpTool("state_hypotheses",
		"为一个悬而未决的问题声明【竞争假设集】（每问题 2-8 条，整体替换：本次未列出的旧 active 假设会被 superseded）。"+
			"认知纪律：只要问题未定，保持 ≥2 条互斥假设是常态——禁止过早收敛到单一叙事；每条给出可区分的表述（能被某个实验观察区分开）。"+
			"basis_node_ids 填该假设依据的 fact/finding 节点 id。",
		obj(map[string]any{
			"question": str("问题（同一问题的假设集会被整体替换）"),
			"hypotheses": map[string]any{"type": "array", "description": "竞争假设数组", "items": map[string]any{
				"type": "object", "properties": map[string]any{
					"statement":      str("假设表述（要可区分：不同假设应对同一实验有不同预期观察）"),
					"basis_node_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "依据的 fact/finding 节点 id（可选）"},
					"notes":          str("备注（可选）"),
				}, "required": []any{"statement"}}},
		}, "question", "hypotheses"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Question   string `json:"question"`
				Hypotheses []struct {
					Statement    string  `json:"statement"`
					BasisNodeIDs []int64 `json:"basis_node_ids"`
					Notes        string  `json:"notes"`
				} `json:"hypotheses"`
			}
			_ = json.Unmarshal(in, &a)
			q := strings.TrimSpace(a.Question)
			if q == "" {
				return actool.Errorf("question 必填"), nil
			}
			items := make([]db.Hypothesis, 0, len(a.Hypotheses))
			for _, h := range a.Hypotheses {
				items = append(items, db.Hypothesis{Statement: h.Statement, BasisNodeIDs: h.BasisNodeIDs, Notes: h.Notes, CreatedBy: "planner"})
			}
			if err := t.ts.UpsertHypotheses(q, items); err != nil {
				return actool.Errorf(fmt.Sprintf("state_hypotheses 落库失败：%v", err)), nil
			}
			hyps, _ := t.ts.ListHypotheses()
			var b strings.Builder
			fmt.Fprintf(&b, "已登记问题「%s」的 %d 条假设。当前假设态：\n", q, len(items))
			for _, h := range hyps {
				if h.Question == q {
					fmt.Fprintf(&b, "  #%d [%s] %s\n", h.ID, h.Status, h.Statement)
				}
			}
			b.WriteString("用 declare_experiment 为该问题预注册区分性实验（每假设一条预期观察）。")
			return actool.Text(b.String()), nil
		})
}

func (t *ToolSet) declareExperiment() actool.CoreTool {
	return t.writeExpTool("declare_experiment",
		"预注册一个【区分性实验】：action 要做的事 + 每个参试假设的预期观察（expectations，≥2 条，派发后不可改）。"+
			"这是结构性预注册——期望必须在实际执行前声明，事后不可拟合。声明后把 experiment_id 记入对应意图（下一步 add_intent 时在 summary 标注 experiment #id，或直接在此后派发）。",
		obj(map[string]any{
			"question":       str("实验要区分的问题"),
			"action":         str("实验动作：具体做什么（将作为意图派发执行）"),
			"hypothesis_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "参试假设 id（≥2）"},
			"expectations":   map[string]any{"type": "object", "description": "假设 id（字符串）→ 该假设成立时预期观察到的现象", "additionalProperties": map[string]any{"type": "string"}},
			"stop_condition": str("停止条件（可选）"),
		}, "question", "action", "hypothesis_ids", "expectations"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Question      string            `json:"question"`
				Action        string            `json:"action"`
				HypothesisIDs []int64           `json:"hypothesis_ids"`
				Expectations  map[string]string `json:"expectations"`
				StopCondition string            `json:"stop_condition"`
			}
			_ = json.Unmarshal(in, &a)
			if len(a.HypothesisIDs) < 2 {
				return actool.Errorf("区分性实验至少需要 2 个 hypothesis_ids"), nil
			}
			exp := make(map[int64]string, len(a.Expectations))
			for k, v := range a.Expectations {
				var id int64
				if _, err := fmt.Sscanf(k, "%d", &id); err != nil || id <= 0 {
					return actool.Errorf(fmt.Sprintf("expectations 的键必须是假设 id，got %q", k)), nil
				}
				exp[id] = v
			}
			id, err := t.ts.DeclareExperiment(db.Experiment{
				Question: a.Question, Action: a.Action, HypothesisIDs: a.HypothesisIDs,
				Expectations: exp, StopCondition: a.StopCondition,
			})
			if err != nil {
				return actool.Errorf(fmt.Sprintf("declare_experiment 落库失败：%v", err)), nil
			}
			return actool.Text(fmt.Sprintf("实验 #%d 已预注册（期望已固化，派发后不可改）。下一步：add_intent 执行该实验（summary 注明 experiment #%d），随后用 dispatch_experiment 绑定。", id, id)), nil
		})
}

func (t *ToolSet) dispatchExperiment() actool.CoreTool {
	return t.writeExpTool("dispatch_experiment",
		"把已预注册的实验绑定到刚创建的执行意图（experiment → dispatched；期望自此不可变）。",
		obj(map[string]any{
			"experiment_id": idp("实验 id"),
			"intent_id":     idp("执行该实验的意图 id"),
		}, "experiment_id", "intent_id"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				ExperimentID int64 `json:"experiment_id"`
				IntentID     int64 `json:"intent_id"`
			}
			_ = json.Unmarshal(in, &a)
			if a.ExperimentID <= 0 || a.IntentID <= 0 {
				return actool.Errorf("experiment_id 和 intent_id 必填"), nil
			}
			if err := t.ts.DispatchExperiment(a.ExperimentID, a.IntentID); err != nil {
				return actool.Errorf(fmt.Sprintf("dispatch_experiment 失败：%v", err)), nil
			}
			return actool.Text(fmt.Sprintf("实验 #%d 已派发（意图 #%d）；期望已锁定。worker 完成后用 report_experiment_outcome 上报观察。", a.ExperimentID, a.IntentID)), nil
		})
}

func (t *ToolSet) reportExperimentOutcome() actool.CoreTool {
	return t.writeExpTool("report_experiment_outcome",
		"上报一个已派发实验的【实际观察】。系统会把观察与各假设的预注册期望做匹配：恰好一个匹配 → 该假设 supported、其余参试假设 refuted；"+
			"多匹配或全不匹配 → inconclusive（假设状态不变）。只上报你在工具输出里真实看到的观察，不要解读。",
		obj(map[string]any{
			"experiment_id": idp("实验 id"),
			"observed":      str("实际观察到的现象（与预注册期望同粒度的一句描述）"),
			"fact_id":       idp("支撑观察的 fact 节点 id（先 record_fact 再上报，可选）"),
		}, "experiment_id", "observed"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				ExperimentID int64  `json:"experiment_id"`
				Observed     string `json:"observed"`
				FactID       int64  `json:"fact_id"`
			}
			_ = json.Unmarshal(in, &a)
			if a.ExperimentID <= 0 || strings.TrimSpace(a.Observed) == "" {
				return actool.Errorf("experiment_id 和 observed 必填"), nil
			}
			out, err := t.ts.RecordExperimentOutcome(a.ExperimentID, a.Observed, a.FactID)
			if err != nil {
				return actool.Errorf(fmt.Sprintf("report_experiment_outcome 失败：%v", err)), nil
			}
			switch out.Status {
			case "observed":
				return actool.Text(fmt.Sprintf("实验结果已判定：观察唯一匹配假设 #%d 的预注册期望 → 该假设 supported，其余参试假设 refuted。", out.Discriminated)), nil
			default:
				return actool.Text(fmt.Sprintf("实验结果不判定（inconclusive）：%s。假设状态未变——考虑设计区分度更高的实验。", out.Reason)), nil
			}
		})
}

// renderHypothesesBlock 把当前信念态渲染进 planner 唤醒提示（与
// renderPlannerTodos 同一模式）：活跃假设（按问题分组）+ 开放实验。
func renderHypothesesBlock(hyps []db.Hypothesis, experiments []db.Experiment) string {
	var active []db.Hypothesis
	for _, h := range hyps {
		if h.Status == "active" {
			active = append(active, h)
		}
	}
	if len(active) == 0 && len(experiments) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【当前假设态（竞争解释，条件化持有）】：\n")
	byQ := map[string][]db.Hypothesis{}
	var order []string
	for _, h := range active {
		if _, ok := byQ[h.Question]; !ok {
			order = append(order, h.Question)
		}
		byQ[h.Question] = append(byQ[h.Question], h)
	}
	for _, q := range order {
		b.WriteString(fmt.Sprintf("  问题「%s」：\n", q))
		for _, h := range byQ[q] {
			b.WriteString(fmt.Sprintf("    #%d %s\n", h.ID, h.Statement))
		}
	}
	if len(experiments) > 0 {
		b.WriteString("  开放实验：\n")
		for _, e := range experiments {
			b.WriteString(fmt.Sprintf("    #%d [%s] %s（区分 %d 个假设）\n", e.ID, e.Status, e.Action, len(e.HypothesisIDs)))
		}
	}
	b.WriteString("纪律：问题未定时保持 ≥2 条可区分假设（state_hypotheses 整体重述）；为待区分问题用 declare_experiment 预注册实验（期望=与观察同粒度的【精确一行】，系统按精确相等匹配）；单一匹配才判定，歧义不判定。假设不是证据——prove_goal 仍只认 fact/finding。")
	// 体积上限（审查 P2）：截断不静默。
	if b.Len() > 16<<10 {
		return b.String()[:16<<10] + "\n…（假设态渲染超上限已截断——用 state_hypotheses 收敛问题集）"
	}
	return b.String()
}
