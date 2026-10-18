package agent

import (
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

// M4.2 消融开关的注册面契约：cogAblated 必须恰好摘除 3 个 M3 认知工具
// （假设声明/实验预注册/实验派发），其余 planner 工具一个不少；
// worker 侧的 report_experiment_outcome 不消融——观察上报是环境回路，不是认知层。
func TestCognitionLiteAblatesExactlyHypothesisTools(t *testing.T) {
	names := func(ts []actool.CoreTool) map[string]bool {
		m := make(map[string]bool, len(ts))
		for _, x := range ts {
			m[x.Name()] = true
		}
		return m
	}
	full := names(NewToolSet(nil, "planner").PlannerTools())

	liteSet := NewToolSet(nil, "planner")
	liteSet.cogAblated = true
	lite := names(liteSet.PlannerTools())

	for _, cog := range []string{"state_hypotheses", "declare_experiment", "dispatch_experiment"} {
		if !full[cog] {
			t.Errorf("full planner must register %s", cog)
		}
		if lite[cog] {
			t.Errorf("cogAblated planner must NOT register %s", cog)
		}
	}
	if want := len(full) - 3; len(lite) != want {
		t.Fatalf("lite tool count = %d, want %d (exactly the cognition trio removed)", len(lite), want)
	}
	if !lite["report_finding"] || !lite["prove_goal"] {
		t.Errorf("lite must keep the non-cognition write path (report_finding/prove_goal)")
	}

	// worker 侧不受消融影响（同一 ToolSet 工厂，避免误消融观察回路）。
	wn := names(NewToolSet(nil, "worker").WorkerTools())
	if !wn["report_experiment_outcome"] {
		t.Errorf("worker must always register report_experiment_outcome")
	}
	liteWorker := NewToolSet(nil, "worker")
	liteWorker.cogAblated = true
	if lwn := names(liteWorker.WorkerTools()); !lwn["report_experiment_outcome"] {
		t.Errorf("worker experiment reporting is the environment loop, not ablated")
	}
}
