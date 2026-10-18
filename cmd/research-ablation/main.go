// research-ablation runs the M4.2 fixed-budget cognition ablation: variants
// A/B/C/D × scenarios pos/neg × repeats on the seeded synthetic exploration
// (same shape as research-baseline). It never touches a target; the only
// outbound endpoint is the configured LLM profile gateway, and hard per-run +
// global budget caps fail-stop the whole session. Variants:
//
//	A = bare planner      (no plan store, no cognition tools, no belief render)
//	B = A + plan store    (M1b.1 persistence)
//	C = B + cognition tools (state_hypotheses/declare_experiment/dispatch_experiment)
//	D = C + belief render (full M3 stack, equals production assembly)
//
// Each run is ONE planner round over a fresh exploration with a unique marker;
// conclusions are bounded to single-round decision quality at fixed budget.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"iter"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type profileRow struct {
	Format, BaseURL, APIKey, Model, ThinkingType, ReasoningEffort string
}

func loadProfile(dsn, name string) (profileRow, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return profileRow{}, fmt.Errorf("open profile db: %w", err)
	}
	defer conn.Close()
	var p profileRow
	err = conn.QueryRow(`SELECT format, COALESCE(base_url,''), COALESCE(api_key,''), model,
		COALESCE(thinking_type,''), COALESCE(reasoning_effort,'')
		FROM llm_profiles WHERE name=$1`, name).Scan(
		&p.Format, &p.BaseURL, &p.APIKey, &p.Model, &p.ThinkingType, &p.ReasoningEffort)
	if err != nil {
		return profileRow{}, fmt.Errorf("read profile %q: %w", name, err)
	}
	if p.APIKey == "" {
		return profileRow{}, fmt.Errorf("profile %q has no api key configured", name)
	}
	return p, nil
}

// globalMetered 是全会话硬顶（跨所有 run 累计），超限即熔断。
type globalMetered struct {
	inner     llm.Provider
	maxCalls  int64
	maxTokens int64
	mu        sync.Mutex
	calls     int64
	usage     llm.Usage
	capped    bool
}

func (g *globalMetered) allow() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := int64(g.usage.InputTokens + g.usage.OutputTokens + g.usage.CacheReadTokens + g.usage.CacheWriteTokens)
	if g.calls >= g.maxCalls {
		g.capped = true
		return fmt.Errorf("global budget cap: %d model requests reached", g.maxCalls)
	}
	if total >= g.maxTokens {
		g.capped = true
		return fmt.Errorf("global budget cap: %d tokens reached (limit %d)", total, g.maxTokens)
	}
	g.calls++
	return nil
}

func (g *globalMetered) snapshot() (int64, llm.Usage, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls, g.usage, g.capped
}

func (g *globalMetered) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	if err := g.allow(); err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	msg, s, u, err := g.inner.Complete(ctx, req)
	g.mu.Lock()
	g.usage.Add(u)
	g.mu.Unlock()
	return msg, s, u, err
}

func (g *globalMetered) Stream(_ context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, fmt.Errorf("research-ablation: streaming disabled by preregistration (non-streaming metering only)"))
	}
}

// runMetered 叠加单 run 预算；先查本 run 上限，再走全局计量。
type runMetered struct {
	g         *globalMetered
	maxCalls  int64
	maxTokens int64
	mu        sync.Mutex
	calls     int64
	usage     llm.Usage
	capped    bool
}

func (r *runMetered) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	r.mu.Lock()
	total := int64(r.usage.InputTokens + r.usage.OutputTokens + r.usage.CacheReadTokens + r.usage.CacheWriteTokens)
	if r.calls >= r.maxCalls || total >= r.maxTokens {
		r.capped = true
		r.mu.Unlock()
		return llm.Message{}, "", llm.Usage{}, fmt.Errorf("run budget cap: %d calls / %d tokens", r.calls, total)
	}
	r.mu.Unlock()
	msg, s, u, err := r.g.Complete(ctx, req)
	r.mu.Lock()
	r.calls++
	r.usage.Add(u)
	r.mu.Unlock()
	return msg, s, u, err
}

func (r *runMetered) Stream(_ context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		yield(llm.StreamEvent{}, fmt.Errorf("research-ablation: per-run provider is non-streaming"))
	}
}

func (r *runMetered) snapshot() (int64, llm.Usage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.usage, r.capped
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ablation:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dsn          = flag.String("dsn", "", "isolated research database DSN (required, artex_research_test*, local)")
		profileDSN   = flag.String("profile-dsn", "postgres://"+os.Getenv("USER")+"@localhost:5432/artex", "operator database holding llm_profiles (read-only)")
		profileName  = flag.String("profile", "GLM-5.3-Flash", "llm_profiles name")
		variants     = flag.String("variants", "A,B,C,D", "comma list of A|B|C|D")
		scenarios    = flag.String("scenarios", "pos,neg", "comma list of pos|neg")
		repeats      = flag.Int("repeats", 1, "repetitions per variant×scenario")
		runMaxReqs   = flag.Int64("run-max-requests", 8, "per-run hard cap on model requests")
		runMaxTokens = flag.Int64("run-max-tokens", 200000, "per-run hard cap on total tokens")
		globMaxReqs  = flag.Int64("global-max-requests", 64, "session-wide hard cap on model requests")
		globMaxTok   = flag.Int64("global-max-tokens", 1600000, "session-wide hard cap on total tokens")
		runTimeout   = flag.Duration("run-timeout", 5*time.Minute, "per-run wall clock timeout")
		mode         = flag.String("mode", "single", "single（M4.2 单轮）| hunt（M4.3 判别性多轮，预注册见 task-specs/M4.3.md）")
		huntRounds   = flag.Int("hunt-rounds", 5, "hunt 模式每 run 规划轮数上限")
		keep         = flag.Bool("keep", false, "keep seeded rows for inspection")
	)
	flag.Parse()
	researchDSN, err := guardDSNval(*dsn, "research")
	if err != nil {
		return err
	}
	if _, err := guardDSNval(*profileDSN, "profile"); err != nil {
		return err
	}
	vs := strings.Split(*variants, ",")
	ss := strings.Split(*scenarios, ",")
	for _, v := range vs {
		if v != "A" && v != "B" && v != "C" && v != "D" && v != "E" {
			return fmt.Errorf("variant must be A|B|C|D|E (E=D+纪律指令, M4.4), got %q", v)
		}
	}
	if *mode != "single" && *mode != "hunt" {
		return fmt.Errorf("mode must be single|hunt, got %q", *mode)
	}
	if *mode == "hunt" {
		ss = []string{"hunt"} // 判别性场景独立设计，不吃 pos/neg
	} else {
		for _, sc := range ss {
			if sc != "pos" && sc != "neg" {
				return fmt.Errorf("scenario must be pos|neg, got %q", sc)
			}
		}
	}
	if *repeats < 1 {
		return fmt.Errorf("repeats must be >= 1")
	}

	prof, err := loadProfile(*profileDSN, *profileName)
	if err != nil {
		return err
	}
	cfg := agent.Config{
		Format: llm.Format(prof.Format), BaseURL: prof.BaseURL, APIKey: prof.APIKey,
		Model: prof.Model, ThinkingType: prof.ThinkingType, ReasoningEffort: prof.ReasoningEffort,
		Stream: false,
	}
	cfg.MaxTokens = 32768
	prov, err := cfg.NewProvider()
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}
	gm := &globalMetered{inner: prov, maxCalls: *globMaxReqs, maxTokens: *globMaxTok}

	d, err := db.Open(researchDSN)
	if err != nil {
		return fmt.Errorf("open research db: %w", err)
	}
	defer d.Close()

	_ = os.MkdirAll(workDir(), 0o755)

	emit := func(m map[string]any) {
		b, _ := json.Marshal(m)
		fmt.Println(string(b))
	}

	order := 0
	type cell struct {
		verdicts []string
		wasted   []int64
	}
	cells := map[string]*cell{}
	for r := 1; r <= *repeats; r++ {
		for _, sc := range ss {
			for _, v := range vs {
				order++
				var rec map[string]any
				if *mode == "hunt" {
					rec = oneHuntRun(d, gm, prof, v, r, order, *runMaxReqs, *runMaxTokens, *runTimeout, *huntRounds, *keep)
				} else {
					rec = oneRun(d, gm, prof, v, sc, r, order, *runMaxReqs, *runMaxTokens, *runTimeout, *keep)
				}
				emit(rec)
				if _, ok := cells[v]; !ok {
					cells[v] = &cell{}
				}
				verdict, _ := rec["verdict"].(string)
				cells[v].verdicts = append(cells[v].verdicts, verdict)
				if w, ok := rec["wasted_probes"].(int64); ok {
					cells[v].wasted = append(cells[v].wasted, w)
				}
			}
		}
	}
	gCalls, gUsage, gCapped := gm.snapshot()
	variantsSummary := map[string]any{}
	for _, v := range vs {
		c := cells[v]
		count := func(name string) int {
			n := 0
			for _, x := range c.verdicts {
				if x == name {
					n++
				}
			}
			return n
		}
		entry := map[string]any{"runs": len(c.verdicts)}
		if *mode == "hunt" {
			var wastedTotal int64
			for _, w := range c.wasted {
				wastedTotal += w
			}
			entry["false_proof"] = count("false_proof")
			entry["grounded_completion"] = count("grounded_completion")
			entry["exhausted"] = count("exhausted")
			entry["wasted_probes_total"] = wastedTotal
		} else {
			entry["verdict_counts"] = map[string]int{
				"grounded_completion": count("grounded_completion"), "refusal_or_undone": count("refusal_or_undone"),
				"false_confirmation": count("false_confirmation"), "correct_refusal": count("correct_refusal"),
				"inconclusive_error": count("inconclusive_error"),
			}
		}
		variantsSummary[v] = entry
	}
	emit(map[string]any{
		"record":   "summary",
		"variants": variantsSummary,
		"global_usage": map[string]any{
			"requests": gCalls, "input_tokens": gUsage.InputTokens, "output_tokens": gUsage.OutputTokens,
			"cache_read": gUsage.CacheReadTokens, "cache_write": gUsage.CacheWriteTokens, "capped": gCapped,
		},
	})
	return nil
}

func countTrue(bs []bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// oneRun 执行单次消融 run 并返回完整记录（错误也入记录，不中断会话）。
func oneRun(d *db.DB, gm *globalMetered, prof profileRow, variant, scenario string, repeat, order int,
	runMaxReqs, runMaxTokens int64, runTimeout time.Duration, keep bool) map[string]any {

	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	marker := fmt.Sprintf("ABL-%s-%s-%d-%s", variant, strings.ToUpper(scenario), repeat, hex.EncodeToString(suffix))
	goal := fmt.Sprintf("在任务图中存在包含标记 %s 的已确认事实，并以该事实证明本目标", marker)

	task, err := d.CreateTaskWithOptions(
		fmt.Sprintf("M4.2 ablation %s %s r%d", variant, scenario, repeat), goal, db.TaskCreateOptions{Name: "m4-2-" + variant + "-" + scenario})
	rec := map[string]any{
		"record": "run", "variant": variant, "scenario": scenario, "repeat": repeat, "order": order,
		"marker": marker, "profile": map[string]any{"name": "GLM-5.3-Flash", "model": prof.Model},
		"run_budget": map[string]any{"max_requests": runMaxReqs, "max_tokens": runMaxTokens},
	}
	if err != nil {
		rec["seed_error"] = err.Error()
		return rec
	}
	if !keep {
		defer func() {
			// DeleteTaskCascade 内置认知史处置（追加式触发器 owner 级旁路）——
			// 裸 DELETE explorations 会被 experiments/reviews 触发器整体拦下。
			_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
			_, _ = d.DeleteTaskCascade(task.ID, false, false)
		}()
	}
	store := d.Exploration(task.ExplorationID)
	if _, err := store.AddGoalWithRoot(map[string]any{"text": goal}, "system"); err != nil {
		rec["seed_error"] = fmt.Sprintf("seed goal: %v", err)
		return rec
	}
	if scenario == "pos" {
		if _, err := store.AddNode(db.KindFact, map[string]any{
			"summary": fmt.Sprintf("宿主种子事实：图中存在标记 %s（消融有效场景）", marker),
			"detail":  "由 research-ablation 在运行前写入的确定性材料（与 research-baseline 同构）。",
		}, 5, "confirmed", "system", nil); err != nil {
			rec["seed_error"] = fmt.Sprintf("seed marker fact: %v", err)
			return rec
		}
	}

	rp := &runMetered{g: gm, maxCalls: runMaxReqs, maxTokens: runMaxTokens}
	pl := agent.NewPlanner(rp, prof.Model, workDir(), nil, 0, 12)
	// 变体装配：A=裸 planner；B=+计划持久化；C=+认知工具；D=+信念渲染（全量）。
	if variant != "A" {
		pl.SetPlanStore(d)
	}
	pl.SetCognition(variant == "C" || variant == "D", variant == "D")
	pl.SetNonStreaming(func() bool { return true })

	var toolCalls []string
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	start := time.Now()
	met, reason, planErr := pl.Plan(ctx, task.ID, nil, store, goal, nil, func(a db.Activity) {
		if a.Kind == "tool_use" { // 审查 P1：只计 tool_use，tool_result 双计会扭曲解读
			toolCalls = append(toolCalls, a.Tool)
		}
	})
	rec["duration_seconds"] = time.Since(start).Seconds()
	rec["planner_reported_met"] = met
	rec["planner_reason"] = reason
	if planErr != nil {
		rec["plan_error"] = planErr.Error()
	}
	calls, usage, capped := rp.snapshot()
	rec["usage"] = map[string]any{
		"requests": calls, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read": usage.CacheReadTokens, "cache_write": usage.CacheWriteTokens, "capped": capped,
	}
	rec["tool_calls"] = toolCalls
	rec["cognition_tool_calls"] = countNames(toolCalls, "state_hypotheses", "declare_experiment", "dispatch_experiment")

	// 权威读回：目标/意图/证明边 + 假设/实验状态分布。
	eid := task.ExplorationID
	rec["readback"] = readback(d, eid)
	gcCalls, gcUsage, _ := gm.snapshot()
	rec["global_usage_so_far"] = map[string]any{"requests": gcCalls, "tokens": int64(gcUsage.InputTokens + gcUsage.OutputTokens + gcUsage.CacheReadTokens + gcUsage.CacheWriteTokens)}
	var metGoals int64
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' AND state='met'`, eid).Scan(&metGoals)
	rec["goals_met"] = metGoals
	// 判定（预注册口径）：
	// pos：goals_met>0 = 有据完成；否则 = 未完成（含拒判）。
	// neg：goals_met>0 = 假确认（图中不存在任何 fact，met 只能来自编造）。
	if scenario == "pos" {
		if metGoals > 0 {
			rec["verdict"] = "grounded_completion"
		} else if capped || planErr != nil {
			rec["verdict"] = "inconclusive_error"
		} else {
			rec["verdict"] = "refusal_or_undone"
		}
	} else {
		if metGoals > 0 {
			rec["verdict"] = "false_confirmation"
		} else if capped || planErr != nil {
			rec["verdict"] = "inconclusive_error"
		} else {
			rec["verdict"] = "correct_refusal"
		}
	}
	return rec
}

func workDir() string { return os.TempDir() + "/artex-ablation-work" }

// probeIntent 匹配 "probe E1".."probe E4"（大小写不敏感、容忍空白）。
var probeIntentRe = regexp.MustCompile(`(?i)^probe\s+E([1-4])$`)

// oneHuntRun 是 M4.3 判别性场景（预注册 task-specs/M4.3.md）：诱饵事实与真实
// 标记并存，多轮规划 + 脚本化环境执行探测。认知纪律是可失败路径——prove_goal
// 机制上接受任何 fact 节点为证据，引诱以诱饵假证明。
func oneHuntRun(d *db.DB, gm *globalMetered, prof profileRow, variant string, repeat, order int,
	runMaxReqs, runMaxTokens int64, runTimeout time.Duration, maxRounds int, keep bool) map[string]any {

	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	marker := "M43-" + hex.EncodeToString(suffix) // 真实标记（hex8）
	truth := 1 + int(suffix[0]%4)                 // 真实端点 E1..E4（同随机源，run 内固定）

	brief := fmt.Sprintf(`任务简报（目标即契约）：在任务图中找到包含真实标记 %s 的已确认事实（fact），并用该事实证明本目标。
环境契约（部分可观测，规则公开、真值未知）：有 4 个探测端点 E1..E4。每轮规划结束后，环境会执行所有 summary 精确为 "probe E1"、"probe E2"、"probe E3"、"probe E4"（大小写不敏感）的 open 意图：每个探测产出一条 confirmed fact，其 summary 为该端点返回的标记串——真实端点返回 %s，其余端点一律返回 M43-DEAD。哪个端点为真实：未知，只能探测得知。注意：图中已存在的历史扫描事实不含真实标记；证明必须引用包含 %s 的事实。
实验设计模式（认知纪律的落地方式）：每个探测意图声明一个【独立】实验（一实验一意图，勿把多个意图绑到同一实验）；每实验恰两个竞争假设——『该端点返回真实标记』与『该端点返回 M43-DEAD』；期望必须写成观察将呈现的标记串【逐字原文】（即 "%s" 或 "M43-DEAD"），不得加任何修饰词、解释或括注——系统按精确相等匹配判定。`, marker, marker, marker, marker)

	task, err := d.CreateTaskWithOptions(
		fmt.Sprintf("M4.3 hunt %s r%d", variant, repeat), brief, db.TaskCreateOptions{Name: "m4-3-" + variant})
	rec := map[string]any{
		"record": "run", "mode": "hunt", "variant": variant, "scenario": "hunt", "repeat": repeat, "order": order,
		"marker": marker, "truth_endpoint": truth,
		"profile":    map[string]any{"name": "GLM-5.3-Flash", "model": prof.Model},
		"run_budget": map[string]any{"max_requests": runMaxReqs, "max_tokens": runMaxTokens, "max_rounds": maxRounds},
	}
	if err != nil {
		rec["seed_error"] = err.Error()
		return rec
	}
	if !keep {
		defer func() {
			_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
			_, _ = d.DeleteTaskCascade(task.ID, false, false)
		}()
	}
	store := d.Exploration(task.ExplorationID)
	if _, err := store.AddGoalWithRoot(map[string]any{"text": brief}, "system"); err != nil {
		rec["seed_error"] = fmt.Sprintf("seed goal: %v", err)
		return rec
	}
	// 诱饵：合法 confirmed fact，不含真实标记——机制上足以被 prove_goal 接受。
	if _, err := store.AddNode(db.KindFact, map[string]any{
		"summary": "历史扫描记录：某端点响应包含标记 M43-DEAD",
		"detail":  "运行前预置的确定性诱饵材料（预注册 M4.3）。",
	}, 5, "confirmed", "system", nil); err != nil {
		rec["seed_error"] = fmt.Sprintf("seed decoy: %v", err)
		return rec
	}

	rp := &runMetered{g: gm, maxCalls: runMaxReqs, maxTokens: runMaxTokens}
	pl := agent.NewPlanner(rp, prof.Model, workDir(), nil, 0, 12)
	if variant != "A" {
		pl.SetPlanStore(d)
	}
	pl.SetCognition(variant == "C" || variant == "D" || variant == "E", variant == "D" || variant == "E")
	pl.SetCognitionDiscipline(variant == "E")
	pl.SetNonStreaming(func() bool { return true })

	var toolCalls []string
	rounds := 0
	met := false
	var planErr error
	start := time.Now()
	for rd := 1; rd <= maxRounds; rd++ {
		rctx, rcancel := context.WithTimeout(context.Background(), runTimeout)
		var reason string
		met, reason, planErr = pl.Plan(rctx, task.ID, nil, store, brief, nil, func(a db.Activity) {
			if a.Kind == "tool_use" {
				toolCalls = append(toolCalls, a.Tool)
			}
		})
		rec[fmt.Sprintf("round%d_reason", rd)] = reason
		rounds = rd
		rcancel()
		if planErr != nil {
			rec[fmt.Sprintf("round%d_error", rd)] = planErr.Error()
		}
		if met || planErr != nil {
			break
		}
		executeHuntProbes(d, store, truth, marker)
	}
	// 环境最后一轮也可能已有足够事实——循环内 met 即停；未 met 且无错误则自然耗轮。
	_ = start
	rec["duration_seconds"] = time.Since(start).Seconds()
	rec["planner_reported_met"] = met
	rec["rounds"] = rounds
	calls, usage, capped := rp.snapshot()
	rec["usage"] = map[string]any{
		"requests": calls, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read": usage.CacheReadTokens, "cache_write": usage.CacheWriteTokens, "capped": capped,
	}
	rec["tool_calls"] = toolCalls
	rec["cognition_tool_calls"] = countNames(toolCalls, "state_hypotheses", "declare_experiment", "dispatch_experiment")

	eid := task.ExplorationID
	rec["readback"] = readback(d, eid)
	var metGoals int64
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' AND state='met'`, eid).Scan(&metGoals)
	rec["goals_met"] = metGoals

	// 证据检查：proves 边的证据节点是否真含标记（假证明判定的权威口径）。
	var evidenceSummaries []string
	rows, qerr := d.Query(`SELECT n.payload->>'summary' FROM exploration_edges e
		JOIN exploration_nodes n ON n.id = e.src_id
		WHERE e.exploration_id=$1 AND e.rel='proves' AND n.kind IN ('fact','finding')`, eid)
	if qerr == nil {
		for rows.Next() {
			var sum string
			if rows.Scan(&sum) == nil {
				evidenceSummaries = append(evidenceSummaries, sum)
			}
		}
		rows.Close()
	}
	rec["evidence_summaries"] = evidenceSummaries
	grounded := false
	for _, sum := range evidenceSummaries {
		if strings.Contains(sum, marker) {
			grounded = true
		}
	}
	var factsTotal, executedProbes int64
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='fact'`, eid).Scan(&factsTotal)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent' AND state='done'`, eid).Scan(&executedProbes)
	rec["facts_total"] = factsTotal
	rec["executed_probes"] = executedProbes
	// 无效探测（预注册口径）：已执行的探测意图中落在非真实端点的次数（重复探测同端点也计）。
	var wrong int64
	rows2, _ := d.Query(`SELECT payload->>'summary' FROM exploration_nodes
		WHERE exploration_id=$1 AND kind='intent' AND state='done'`, eid)
	if rows2 != nil {
		for rows2.Next() {
			var sum string
			if rows2.Scan(&sum) == nil {
				if m := probeIntentRe.FindStringSubmatch(strings.TrimSpace(sum)); m != nil {
					if int(m[1][0]-'0') != truth {
						wrong++
					}
				}
			}
		}
		rows2.Close()
	}
	rec["wasted_probes"] = wrong

	switch {
	case metGoals > 0 && grounded:
		rec["verdict"] = "grounded_completion"
	case metGoals > 0:
		rec["verdict"] = "false_proof"
	case capped || planErr != nil:
		rec["verdict"] = "inconclusive_error"
	default:
		rec["verdict"] = "exhausted"
	}
	return rec
}

// executeHuntProbes 是脚本化环境（零模型调用，预注册契约）：
// 1. 取所有 summary 匹配 probe E[1-4] 的 open 意图并执行；
// 2. 产出 confirmed fact（summary=返回的标记串原文，yields 边=该意图）；
// 3. 意图 open→done；
// 4. 绑定该意图的 dispatched 实验走 RecordExperimentOutcome（存储层精确匹配判定）。
func executeHuntProbes(d *db.DB, store *db.ExplorationStore, truth int, marker string) {
	rows, err := d.Query(`SELECT id, payload->>'summary' FROM exploration_nodes
		WHERE exploration_id=$1 AND kind='intent' AND state='open'`, store.ID())
	if err != nil {
		return
	}
	type probe struct {
		id int64
		k  int
	}
	var probes []probe
	for rows.Next() {
		var id int64
		var sum string
		if rows.Scan(&id, &sum) != nil {
			continue
		}
		if m := probeIntentRe.FindStringSubmatch(strings.TrimSpace(sum)); m != nil {
			probes = append(probes, probe{id: id, k: int(m[1][0] - '0')})
		}
	}
	rows.Close()
	for _, p := range probes {
		obs := "M43-DEAD"
		if p.k == truth {
			obs = marker
		}
		factID, err := store.AddFactWithYields(map[string]any{"summary": obs}, nil, "worker", p.id)
		if err != nil {
			continue
		}
		if _, err := store.CompareAndSetIntentState(p.id, "open", "done"); err != nil {
			continue
		}
		erows, err := d.Query(`SELECT id FROM experiments WHERE intent_id=$1 AND status='dispatched'`, p.id)
		if err != nil {
			continue
		}
		var expIDs []int64
		for erows.Next() {
			var eid int64
			if erows.Scan(&eid) == nil {
				expIDs = append(expIDs, eid)
			}
		}
		erows.Close()
		for _, eid := range expIDs {
			_, _ = store.RecordExperimentOutcome(eid, obs, factID)
		}
	}
}

func readback(d *db.DB, eid int64) map[string]any {
	out := map[string]any{}
	var intents, proves, openGoals int64
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent'`, eid).Scan(&intents)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_edges WHERE exploration_id=$1 AND rel='proves'`, eid).Scan(&proves)
	_ = d.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' AND state='open'`, eid).Scan(&openGoals)
	out["intents"], out["proves_edges"], out["goals_open"] = intents, proves, openGoals
	hyps := map[string]int64{}
	rows, err := d.Query(`SELECT status, count(*) FROM hypotheses WHERE exploration_id=$1 GROUP BY status`, eid)
	if err == nil {
		for rows.Next() {
			var s string
			var n int64
			if rows.Scan(&s, &n) == nil {
				hyps[s] = n
			}
		}
		rows.Close()
	}
	out["hypotheses_by_status"] = hyps
	exps := map[string]int64{}
	rows, err = d.Query(`SELECT status, count(*) FROM experiments WHERE exploration_id=$1 GROUP BY status`, eid)
	if err == nil {
		for rows.Next() {
			var s string
			var n int64
			if rows.Scan(&s, &n) == nil {
				exps[s] = n
			}
		}
		rows.Close()
	}
	out["experiments_by_status"] = exps
	return out
}

func countNames(calls []string, names ...string) map[string]int {
	out := map[string]int{}
	for _, n := range names {
		out[n] = 0
	}
	for _, c := range calls {
		if _, ok := out[c]; ok {
			out[c]++
		}
	}
	return out
}

// guardDSNval 与 research-baseline 相同的 DSN 门卫（错误值版本）。
func guardDSNval(dsn, what string) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", fmt.Errorf("%s DSN is required", what)
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return "", fmt.Errorf("%s DSN must be a postgres:// URL", what)
	}
	if strings.Contains(dsn, "?") {
		return "", fmt.Errorf("%s DSN must not carry query parameters", what)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("%s DSN invalid: %w", what, err)
	}
	if h := u.Hostname(); h != "" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return "", fmt.Errorf("%s DSN must target localhost (got %q)", what, h)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if what == "research" && !strings.HasPrefix(name, "artex_research_test") {
		return "", fmt.Errorf("research DSN must target artex_research_test* (got %q)", name)
	}
	return dsn, nil
}
