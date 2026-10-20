package agent

import (
	"context"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

type ControlledToolFactory func(context.Context, RunInfo) ([]actool.CoreTool, error)

func (w *Worker) SetControlledTools(factory ControlledToolFactory) { w.controlledTools = factory }
func controlledBinding(ctx context.Context, taskID int64, store *db.ExplorationStore) (bool, error) {
	return store.ControlledBinding(ctx, taskID)
}

// ControlledPlannerTools excludes execution, global asset mutation and formal proof paths.
func ControlledPlannerTools(tools []actool.CoreTool) []actool.CoreTool {
	out := make([]actool.CoreTool, 0, len(tools))
	for _, tool := range tools {
		switch tool.Name() {
		case "graph_overview", "list_goals", "list_facts", "node_detail", "state_hypotheses", "declare_experiment", "dispatch_experiment", "add_intent":
			out = append(out, tool)
		}
	}
	return out
}
func deniedControlledRun() error { return control.ToolUnavailable }
