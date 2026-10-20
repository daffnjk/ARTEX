package agent

import (
	"context"
	"iter"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

// This provider only records the assembled request; it never dispatches tools.
type controlAssemblyProvider struct{ requests []llm.CompletionRequest }

func (p *controlAssemblyProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.requests = append(p.requests, req)
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("fixture complete")}}, "end_turn", llm.Usage{}, nil
}

func (p *controlAssemblyProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	panic("control assembly fixture requires the non-streaming path")
}

func TestControlCognitionDefaultAssembly(t *testing.T) {
	dsn := os.Getenv("ARTEX_TEST_PG_DSN")
	u, err := url.Parse(dsn)
	if err != nil || dsn == "" || dsn != os.Getenv("ARTEX_PG_DSN") ||
		(u.Scheme != "postgres" && u.Scheme != "postgresql") || u.RawQuery != "" ||
		(u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") ||
		!strings.HasPrefix(u.Path, "/artex_research_test") {
		t.Fatal("both explicit DSNs must target the same isolated localhost research database")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	task, err := d.CreateTaskWithOptions("control assembly fixture", "observe synthetic beliefs", db.TaskCreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.DeleteTask(task.ID); err != nil {
			t.Error(err)
		}
	})
	store := d.Exploration(task.ExplorationID)
	const belief = "CONTROL_ASSEMBLY_BELIEF_SENTINEL"
	if err := store.UpsertHypotheses("synthetic alternatives", []db.Hypothesis{
		{Statement: belief}, {Statement: "alternative synthetic explanation"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		configure  func(*Planner)
		tools      bool
		render     bool
		discipline bool
	}{
		{"default", func(*Planner) {}, true, true, false},
		{"discipline_on", func(p *Planner) { p.SetCognitionDiscipline(true) }, true, true, true},
		{"discipline_off", func(p *Planner) { p.SetCognitionDiscipline(true); p.SetCognitionDiscipline(false) }, true, true, false},
		{"tools_off", func(p *Planner) { p.SetCognition(false, true) }, false, true, false},
		{"render_off", func(p *Planner) { p.SetCognition(true, false); p.SetCognitionDiscipline(true) }, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &controlAssemblyProvider{}
			planner := NewPlanner(provider, "synthetic", t.TempDir(), nil, 100000, 1)
			planner.SetNonStreaming(func() bool { return true })
			tc.configure(planner)
			if _, _, err := planner.Plan(t.Context(), task.ID, nil, store, task.Goal, nil, nil); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("provider requests=%d, want 1", len(provider.requests))
			}
			req := provider.requests[0]
			for _, name := range []string{"state_hypotheses", "declare_experiment", "dispatch_experiment"} {
				count := 0
				for _, tool := range req.Tools {
					if tool.Name == name {
						count++
					}
				}
				want := 0
				if tc.tools {
					want = 1
				}
				if count != want {
					t.Errorf("assembled tool %s count=%d, want %d", name, count, want)
				}
			}
			var input strings.Builder
			for _, msg := range req.Messages {
				input.WriteString(msg.Text())
			}
			if got := strings.Contains(input.String(), belief); got != tc.render {
				t.Errorf("belief rendered=%v, want %v", got, tc.render)
			}
			if got := strings.Contains(input.String(), "【认知纪律（本任务流程要求）】"); got != tc.discipline {
				t.Errorf("explicit discipline=%v, want %v", got, tc.discipline)
			}
		})
	}
}
