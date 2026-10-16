package agent

import (
	"context"
	"encoding/json"
	"os"

	"github.com/Autumn-27/artex/harness"
	actool "github.com/Autumn-27/norma/tool"
)

// guardGoalCompletion is an opt-in rollout gate applied AFTER tool resolution,
// including DB overrides and fallback tools. It never reintroduces a disabled
// tool and never treats a model-provided evaluation report as authorization.
// This is not the final database-level completion gate; direct writes and other
// execution channels are outside this wrapper's guarantee.
func guardGoalCompletion(t actool.CoreTool) actool.CoreTool {
	if !harness.RequiresGoalReview(t.Name(), os.Getenv("ARTEX_GOAL_COMPLETION_MODE")) {
		return t
	}
	return &goalReviewTool{CoreTool: t}
}

type goalReviewTool struct{ actool.CoreTool }

func (g *goalReviewTool) Description() string {
	return g.CoreTool.Description() + "\n[research harness] 本实例启用了目标完成复核门。此工具不会自动标记目标或任务完成；保留证据并交付待复核结果。不要重复调用，也不要改用其他写入路径绕过复核。"
}

func (g *goalReviewTool) Call(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
	return actool.Errorf("goal_review_required: 本次未执行完成状态写入。当前尚未接入可信的领域验收器，请保留证据并交付待复核结果；停止探索不等于目标达成。"), nil
}
