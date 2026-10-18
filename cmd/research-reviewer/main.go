// research-reviewer runs ONE clean-context advisory review of a task's belief
// state (research M3.4). The reviewer sees ONLY the authoritative packet
// (hypotheses/events/experiments/integrity/facts) — no planner conversation —
// and has NO tools and NO write authority: its critique is stored append-only
// as advisory. Model usage is metered and hard-capped (preregistered).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "research-reviewer:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dsn        = flag.String("dsn", "", "isolated research DSN (required)")
		profileDSN = flag.String("profile-dsn", "postgres://"+os.Getenv("USER")+"@localhost:5432/artex", "operator db (read-only llm_profiles)")
		profile    = flag.String("profile", "GLM-5.3-Flash", "profile name")
		maxReqs    = flag.Int64("max-requests", 2, "hard cap on model requests")
		maxTokens  = flag.Int64("max-tokens", 100000, "hard cap on total tokens")
		keep       = flag.Bool("keep", false, "keep seeded rows")
	)
	flag.Parse()
	if err := guard(*dsn); err != nil {
		return err
	}
	prof, err := loadProfile(*profileDSN, *profile)
	if err != nil {
		return err
	}
	cfg := agent.Config{
		Format: llm.Format(prof.Format), BaseURL: prof.BaseURL, APIKey: prof.APIKey,
		Model: prof.Model, ThinkingType: prof.ThinkingType, ReasoningEffort: prof.ReasoningEffort,
		Stream: false, MaxTokens: 8192,
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}
	m := &meter{inner: prov, maxCalls: *maxReqs, maxTokens: *maxTokens}

	d, err := db.Open(*dsn)
	if err != nil {
		return err
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("reviewer round", "clean-context advisory review", db.TaskCreateOptions{Name: "m34-review"})
	if err != nil {
		return err
	}
	if !*keep {
		defer func() {
			_, _ = d.Exec(`DELETE FROM hypothesis_reviews WHERE exploration_id=$1`, task.ExplorationID)
			_ = d.DeleteTask(task.ID)
			_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, task.ExplorationID)
		}()
	}
	s := d.Exploration(task.ExplorationID)
	// 种子信念态：两竞争假设 + 已结区分性实验（一方 supported）+ 旁证事实。
	if err := s.UpsertHypotheses("why admin 500?", []db.Hypothesis{
		{Statement: "WAF blocks oversized payloads"}, {Statement: "app crashes on unicode"},
	}); err != nil {
		return err
	}
	hyps, _ := s.ListHypotheses()
	intent, err := s.AddIntentWithParents(map[string]any{"summary": "review seed"}, 5, nil, "planner", nil)
	if err != nil {
		return err
	}
	factID, err := s.AddFactWithYields(map[string]any{"summary": "oversized payload returns WAF block page"}, nil, "worker", intent)
	if err != nil {
		return err
	}
	expID, err := s.DeclareExperiment(db.Experiment{
		Question: "why admin 500?", Action: "send oversized payload",
		HypothesisIDs: []int64{hyps[0].ID, hyps[1].ID},
		Expectations:  map[int64]string{hyps[0].ID: "WAF block page returned", hyps[1].ID: "500 stack trace returned"},
	})
	if err != nil {
		return err
	}
	if err := s.DispatchExperiment(expID, intent); err != nil {
		return err
	}
	if _, err := s.RecordExperimentOutcome(expID, "WAF block page returned", factID); err != nil {
		return err
	}
	packet, err := s.BuildReviewPacket()
	if err != nil {
		return err
	}
	packetRaw, _ := json.Marshal(packet)
	system := "你是干净上下文的独立评审员（advisory reviewer）。你只看到给定的权威状态包（假设/事件流/实验与结果/图一致性/事实），没有规划者的对话历史。职责：找出（1）未被实验支撑却标记 supported 的断言；（2）互相矛盾或与事实冲突的假设；（3）依据已失效但未降级的假设；（4）区分度不足的实验设计。输出 JSON：{\"issues\":[{\"severity\":\"high|medium|low\",\"kind\":\"...\",\"detail\":\"...\",\"evidence\":\"包内引用\"}],\"summary\":\"一句话总评\"}。你没有工具、没有写权限——你的输出只是建议，不会改变任何状态。只输出 JSON。"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	msg, _, usage, err := m.Complete(ctx, llm.CompletionRequest{
		System:   []string{system},
		Messages: []llm.Message{llm.UserText(string(packetRaw))},
	})
	if err != nil {
		return fmt.Errorf("review call failed (packet NOT reviewed): %w", err)
	}
	raw := strings.TrimSpace(msg.Text())
	// 剥离模型常见的 markdown 代码围栏（实测首轮即遇到）。
	if strings.HasPrefix(raw, "```") {
		if i := strings.IndexByte(raw, '\n'); i > 0 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	var advisory map[string]any
	if jerr := json.Unmarshal([]byte(raw), &advisory); jerr != nil {
		advisory = map[string]any{"raw": msg.Text(), "parse_error": jerr.Error()}
	}
	usageOut := map[string]any{
		"requests": m.calls, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read": usage.CacheReadTokens,
	}
	if err := s.StoreReview(prof.Model, advisory, usageOut); err != nil {
		return err
	}
	out := map[string]any{
		"schema_version": 1, "exploration_id": task.ExplorationID,
		"model": prof.Model, "usage": usageOut,
		"duration_seconds": time.Since(start).Seconds(),
		"advisory":         advisory,
		"kept_rows":        *keep,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return nil
}

func guard(dsn string) error {
	if strings.TrimSpace(dsn) == "" || !strings.HasPrefix(dsn, "postgres") || strings.Contains(dsn, "?") {
		return fmt.Errorf("dsn must be a plain postgres:// URL")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	if h := u.Hostname(); h != "" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return fmt.Errorf("dsn must target localhost")
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "artex_research_test") {
		return fmt.Errorf("dsn must target artex_research_test*")
	}
	return nil
}

type profileRow struct {
	Format, BaseURL, APIKey, Model, ThinkingType, ReasoningEffort string
}

func loadProfile(dsn, name string) (profileRow, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return profileRow{}, err
	}
	defer conn.Close()
	var p profileRow
	err = conn.QueryRow(`SELECT format, COALESCE(base_url,''), COALESCE(api_key,''), model,
		COALESCE(thinking_type,''), COALESCE(reasoning_effort,'') FROM llm_profiles WHERE name=$1`, name).
		Scan(&p.Format, &p.BaseURL, &p.APIKey, &p.Model, &p.ThinkingType, &p.ReasoningEffort)
	if err != nil {
		return profileRow{}, err
	}
	if p.APIKey == "" {
		return profileRow{}, fmt.Errorf("profile %q has no api key", name)
	}
	return p, nil
}

type meter struct {
	inner     llm.Provider
	maxCalls  int64
	maxTokens int64
	calls     int64
	usage     llm.Usage
}

func (m *meter) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	total := int64(m.usage.InputTokens + m.usage.OutputTokens)
	if m.calls >= m.maxCalls || total >= m.maxTokens {
		return llm.Message{}, "", llm.Usage{}, fmt.Errorf("budget cap reached")
	}
	m.calls++
	msg, s, u, err := m.inner.Complete(ctx, req)
	m.usage.Add(u)
	return msg, s, u, err
}

func (m *meter) Stream(ctx context.Context, req llm.CompletionRequest) func(yield func(llm.StreamEvent, error) bool) {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, fmt.Errorf("streaming disabled by preregistration"))
	}
}
