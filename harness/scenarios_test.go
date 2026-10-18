package harness

import (
	"fmt"
	"testing"
)

// M0.1 回放：登记册里每个场景的每一步都以生产同一套评估内核回放，断言实际
// 结论/处置/首个问题码与构造性真值一致。这是 M0 能力基线的种子，也是内核
// 行为的回归基线——内核语义若漂移，这里先红。

func TestScenarioRegistryLoads(t *testing.T) {
	scenarios, err := LoadScenarios()
	if err != nil {
		t.Fatalf("registry must load: %v", err)
	}
	covered := map[string]int{}
	for _, s := range scenarios {
		covered[s.SampleClass]++
	}
	for _, class := range ScenarioClasses {
		if covered[class] == 0 {
			t.Errorf("sample class %q has no scenario in the registry", class)
		}
	}
	if len(scenarios) < len(ScenarioClasses) {
		t.Errorf("registry holds %d scenarios for %d classes", len(scenarios), len(ScenarioClasses))
	}
}

func TestScenarioReplay(t *testing.T) {
	scenarios, err := LoadScenarios()
	if err != nil {
		t.Fatalf("registry must load: %v", err)
	}
	if len(scenarios) == 0 {
		t.Fatal("registry is empty")
	}
	for _, s := range scenarios {
		s := s
		t.Run(s.ID+"@"+s.Version, func(t *testing.T) {
			for i, step := range s.Steps {
				report := s.ReplayStep(step)
				if report.Conclusion != step.Expect.Conclusion {
					t.Errorf("step %d (%s): conclusion = %q, truth = %q (issues=%v reason=%q)",
						i, step.Name, report.Conclusion, step.Expect.Conclusion, report.Issues, report.Reason)
				}
				if report.Disposition != step.Expect.Disposition {
					t.Errorf("step %d (%s): disposition = %q, truth = %q (issues=%v reason=%q)",
						i, step.Name, report.Disposition, step.Expect.Disposition, report.Issues, report.Reason)
				}
				gotIssue := ""
				if len(report.Issues) > 0 {
					gotIssue = report.Issues[0].Code
				}
				if gotIssue != step.Expect.IssueCode {
					t.Errorf("step %d (%s): first issue = %q, truth = %q", i, step.Name, gotIssue, step.Expect.IssueCode)
				}
				if t.Failed() {
					t.Fatalf("step %d report: %+v", i, report)
				}
			}
		})
	}
}

// TestScenarioInventorySummary 输出登记册清单（供文档与 M0.2 预注册引用）。
func TestScenarioInventorySummary(t *testing.T) {
	scenarios, err := LoadScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, s := range scenarios {
		total += len(s.Steps)
	}
	summary := fmt.Sprintf("registry: %d scenarios / %d classes / %d replay steps", len(scenarios), len(ScenarioClasses), total)
	t.Log(summary)
	if total < len(scenarios) {
		t.Fatalf("inconsistent step count: %s", summary)
	}
}
