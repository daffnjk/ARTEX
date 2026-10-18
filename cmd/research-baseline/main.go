// research-baseline runs ONE preregistered, metered planner round on a seeded
// isolated exploration (research M0.2). It never touches a target: the seeded
// graph has no assets, the only outbound endpoint is the configured LLM
// profile gateway, and hard budget caps fail-stop the run. Profile credentials
// are read at runtime from the operator database and never printed or stored.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"iter"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "research-baseline: "+format+"\n", args...)
	os.Exit(2)
}

// guardDSN enforces the same isolation rules as scripts/test-research-pg.sh:
// local host, database named artex_research_test*, no query parameters.
func guardDSN(dsn, what string) string {
	if strings.TrimSpace(dsn) == "" {
		fail("%s DSN is required", what)
	}
	if strings.Contains(dsn, "?") {
		fail("%s DSN must not carry query parameters", what)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		fail("%s DSN invalid: %v", what, err)
	}
	if h := u.Hostname(); h != "" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		fail("%s DSN must target localhost (got %q)", what, h)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if what == "research" && !strings.HasPrefix(name, "artex_research_test") {
		fail("research DSN must target artex_research_test* (got %q)", name)
	}
	return dsn
}

type profileRow struct {
	Format          string
	BaseURL         string
	APIKey          string
	Model           string
	ThinkingType    string
	ReasoningEffort string
}

func loadProfile(dsn, name string) profileRow {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		fail("open profile db: %v", err)
	}
	defer conn.Close()
	var p profileRow
	err = conn.QueryRow(`SELECT format, COALESCE(base_url,''), COALESCE(api_key,''), model,
		COALESCE(thinking_type,''), COALESCE(reasoning_effort,'')
		FROM llm_profiles WHERE name=$1`, name).Scan(
		&p.Format, &p.BaseURL, &p.APIKey, &p.Model, &p.ThinkingType, &p.ReasoningEffort)
	if err != nil {
		fail("read profile %q: %v", name, err)
	}
	if p.APIKey == "" {
		fail("profile %q has no api key configured", name)
	}
	return p
}

// metered wraps a provider: hard caps on request count and total tokens.
type metered struct {
	inner     llm.Provider
	maxCalls  int64
	maxTokens int64
	mu        sync.Mutex
	calls     int64
	usage     llm.Usage
	capped    bool
}

func (m *metered) snapshot() (calls int64, u llm.Usage, capped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.usage, m.capped
}

func (m *metered) allow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := int64(m.usage.InputTokens + m.usage.OutputTokens + m.usage.CacheReadTokens + m.usage.CacheWriteTokens)
	if m.calls >= m.maxCalls {
		m.capped = true
		return fmt.Errorf("budget cap: %d model requests reached", m.maxCalls)
	}
	if total >= m.maxTokens {
		m.capped = true
		return fmt.Errorf("budget cap: %d tokens reached (limit %d)", total, m.maxTokens)
	}
	m.calls++
	return nil
}

func (m *metered) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if err := m.allow(); err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	msg, s, u, err := m.inner.Complete(ctx, req)
	m.mu.Lock()
	m.usage.Add(u)
	m.mu.Unlock()
	return msg, s, u, err
}

func (m *metered) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	// Baseline runs are preregistered as non-streaming; if the SDK ever routes
	// through Stream anyway, fail loudly instead of silently losing metering.
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, fmt.Errorf("research-baseline: streaming path is disabled by preregistration (non-streaming metering only)"))
	}
}

func main() {
	var (
		dsn         = flag.String("dsn", "", "isolated research database DSN (required, artex_research_test*, local)")
		profileDSN  = flag.String("profile-dsn", "postgres://"+os.Getenv("USER")+"@localhost:5432/artex", "operator database holding llm_profiles (read-only)")
		profileName = flag.String("profile", "GLM-5.3-Flash", "llm_profiles name")
		scenario    = flag.String("scenario", "pos", "pos|neg")
		maxReqs     = flag.Int64("max-requests", 8, "hard cap on model requests")
		maxTokens   = flag.Int64("max-tokens", 200000, "hard cap on total tokens")
		keep        = flag.Bool("keep", false, "keep seeded rows for inspection")
	)
	flag.Parse()
	researchDSN := guardDSN(*dsn, "research")
	guardDSN(*profileDSN, "profile")

	var marker, goal string
	switch *scenario {
	case "pos":
		marker = "BASELINE-POS-7f3a"
	case "neg":
		marker = "BASELINE-NEG-0c91"
	default:
		fail("scenario must be pos or neg")
	}
	goal = fmt.Sprintf("在任务图中存在包含标记 %s 的已确认事实，并以该事实证明本目标", marker)

	prof := loadProfile(*profileDSN, *profileName)
	cfg := agent.Config{
		Format:          llm.Format(prof.Format),
		BaseURL:         prof.BaseURL,
		APIKey:          prof.APIKey,
		Model:           prof.Model,
		ThinkingType:    prof.ThinkingType,
		ReasoningEffort: prof.ReasoningEffort,
		Stream:          false,
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		fail("build provider: %v", err)
	}
	m := &metered{inner: prov, maxCalls: *maxReqs, maxTokens: *maxTokens}

	d, err := db.Open(researchDSN)
	if err != nil {
		fail("open research db: %v", err)
	}
	defer d.Close()

	task, err := d.CreateTaskWithOptions(
		fmt.Sprintf("M0.2 baseline %s", *scenario), goal, db.TaskCreateOptions{Name: "m0-2-" + *scenario})
	if err != nil {
		fail("seed task: %v", err)
	}
	if !*keep {
		defer func() {
			_, _ = d.Exec(`DELETE FROM task_plans WHERE exploration_id=$1`, task.ExplorationID)
			_ = d.DeleteTask(task.ID)
			_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
		}()
	}
	store := d.Exploration(task.ExplorationID)
	if _, err := store.AddGoalWithRoot(map[string]any{"text": goal}, "system"); err != nil {
		fail("seed goal: %v", err)
	}
	if *scenario == "pos" {
		if _, err := store.AddNode(db.KindFact, map[string]any{
			"summary": fmt.Sprintf("宿主种子事实：图中存在标记 %s（基线有效场景）", marker),
			"detail":  "由 research-baseline 在运行前写入的确定性材料，等价于 M0.1 valid-session 类的构造真值。",
		}, 5, "confirmed", "system", nil); err != nil {
			fail("seed marker fact: %v", err)
		}
	}

	workDir := os.TempDir() + "/artex-baseline-work"
	_ = os.MkdirAll(workDir, 0o755)
	pl := agent.NewPlanner(m, prof.Model, workDir, nil, 0, 12)
	pl.SetPlanStore(d)
	pl.SetNonStreaming(func() bool { return true })

	var toolCalls []string
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	start := time.Now()
	met, reason, planErr := pl.Plan(ctx, task.ID, nil, store, goal, nil, func(a db.Activity) {
		if a.Kind == "tool_use" || a.Tool != "" {
			toolCalls = append(toolCalls, a.Tool)
		}
	})
	duration := time.Since(start)

	calls, usage, capped := m.snapshot()
	// 读回权威状态。
	var metGoals, openGoals, intents, proves int64
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' AND state='met'`, task.ExplorationID).Scan(&metGoals)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' AND state='open'`, task.ExplorationID).Scan(&openGoals)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent'`, task.ExplorationID).Scan(&intents)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND rel='proves'`, task.ExplorationID).Scan(&proves)
	plan, _ := d.LoadTaskPlan(task.ExplorationID)
	planInfo := map[string]any{"present": plan != nil}
	if plan != nil {
		planInfo["revision"] = plan.Revision
		planInfo["steps"] = len(plan.Steps)
	}

	out := map[string]any{
		"schema_version":       1,
		"scenario":             *scenario,
		"marker":               marker,
		"profile":              map[string]any{"name": *profileName, "model": prof.Model, "format": prof.Format},
		"budget":               map[string]any{"max_requests": *maxReqs, "max_tokens": *maxTokens, "capped": capped},
		"usage":                map[string]any{"requests": calls, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "cache_read": usage.CacheReadTokens, "cache_write": usage.CacheWriteTokens},
		"duration_seconds":     duration.Seconds(),
		"planner_reported_met": met,
		"planner_reason":       reason,
		"plan_error":           fmt.Sprint(planErr),
		"readback":             map[string]any{"goals_met": metGoals, "goals_open": openGoals, "intents": intents, "proves_edges": proves},
		"task_plan":            planInfo,
		"exploration_id":       task.ExplorationID,
		"task_id":              task.ID,
		"tool_calls":           toolCalls,
		"kept_rows":            *keep,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if planErr != nil {
		os.Exit(1)
	}
}
