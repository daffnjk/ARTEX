package agent

import (
	"context"
	"encoding/json"
	"github.com/Autumn-27/artex/control"
	actool "github.com/Autumn-27/norma/tool"
	"sync"
)

type controlIntentWindow struct {
	actool.CoreTool
	mu    sync.Mutex
	used  int
	allow bool
}

func (w *controlIntentWindow) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.allow || w.used >= 1 {
		return actool.Errorf("controlled planning window: collect existing results before proposing another intent"), nil
	}
	w.used++
	return w.CoreTool.Call(ctx, in, tc)
}
func controlWindowTools(tools []actool.CoreTool, state control.PlanningWindow) []actool.CoreTool {
	out := append([]actool.CoreTool(nil), tools...)
	for i, t := range out {
		if t.Name() == "add_intent" {
			out[i] = &controlIntentWindow{CoreTool: t, allow: state.AllowNew}
		}
	}
	return out
}
