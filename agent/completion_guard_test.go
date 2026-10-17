package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

type completionProbe struct {
	actool.CoreTool
	name  string
	calls int
}

func (p *completionProbe) Name() string        { return p.name }
func (p *completionProbe) Description() string { return "probe" }
func (p *completionProbe) Call(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
	p.calls++
	return actool.Result{}, nil
}

func TestGoalCompletionGateCannotBeOverriddenByArguments(t *testing.T) {
	for _, mode := range []string{"review", "misspelled-mode"} {
		t.Setenv("ARTEX_GOAL_COMPLETION_MODE", mode)
		for _, name := range []string{"prove_goal", "goal_met"} {
			probe := &completionProbe{name: name}
			tool := guardGoalCompletion(probe)
			res, err := tool.Call(context.Background(), json.RawMessage(`{"approved":true,"disposition":"confirm","mode":"legacy"}`), nil)
			b, _ := json.Marshal(res)
			if err != nil || probe.calls != 0 || !strings.Contains(string(b), "goal_review_required") {
				t.Fatalf("completion escaped gate: calls=%d err=%v result=%s", probe.calls, err, b)
			}
			if !strings.Contains(tool.Description(), "research harness") {
				t.Fatal("missing gate guidance")
			}
		}
	}
}

func TestGoalCompletionLegacyAndUnrelatedTools(t *testing.T) {
	for _, mode := range []string{"", "legacy", "review"} {
		t.Setenv("ARTEX_GOAL_COMPLETION_MODE", mode)
		for _, name := range []string{"prove_goal", "record_fact"} {
			probe := &completionProbe{name: name}
			_, err := guardGoalCompletion(probe).Call(context.Background(), nil, nil)
			want := 1
			if mode == "review" && name == "prove_goal" {
				want = 0
			}
			if err != nil || probe.calls != want {
				t.Fatalf("mode=%s tool=%s calls=%d want=%d", mode, name, probe.calls, want)
			}
		}
	}
}

func TestGoalCompletionAssemblyAfterResolve(t *testing.T) {
	t.Setenv("ARTEX_GOAL_COMPLETION_MODE", "review")
	oldResolve, oldAugment := ToolResolve, ToolAugment
	t.Cleanup(func() { ToolResolve, ToolAugment = oldResolve, oldAugment })
	ToolAugment = nil
	probe := &completionProbe{name: "prove_goal"}
	ToolResolve = func(context.Context, string, []actool.CoreTool) []actool.CoreTool { return []actool.CoreTool{probe} }
	tools, _, cleanup := AugmentTools(context.Background(), "planner", nil)
	defer cleanup()
	if len(tools) != 1 {
		t.Fatal("resolved tool lost")
	}
	if _, err := tools[0].Call(context.Background(), nil, nil); err != nil || probe.calls != 0 {
		t.Fatalf("resolved tool bypassed gate: %v", err)
	}
	ToolResolve = func(context.Context, string, []actool.CoreTool) []actool.CoreTool { return nil }
	tools, _, cleanup2 := AugmentTools(context.Background(), "planner", []actool.CoreTool{probe})
	defer cleanup2()
	if len(tools) != 0 {
		t.Fatal("disabled tool reintroduced")
	}
}
