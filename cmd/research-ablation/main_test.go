package main

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// researchDSN 是与 db 包同款守卫的错误值版本：只接受本地 artex_research_test*。
func researchDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ARTEX_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("ARTEX_TEST_PG_DSN not set")
	}
	if !strings.HasPrefix(dsn, "postgres://") || strings.Contains(dsn, "?") {
		t.Fatalf("DSN must be a plain postgres:// URL: %q", dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN invalid: %v", err)
	}
	if h := u.Hostname(); h != "" && h != "localhost" && h != "127.0.0.1" && h != "::1" {
		t.Fatalf("DSN must be local: %q", h)
	}
	if name := strings.TrimPrefix(u.Path, "/"); !strings.HasPrefix(name, "artex_research_test") {
		t.Fatalf("DSN must target artex_research_test*: %q", name)
	}
	return dsn
}

// TestHuntEnvironmentContract 离线验证 M4.3 脚本化环境（零模型调用）：
// 探测意图→事实产出、意图状态迁移、绑定实验的期望精确匹配判定全链路。
func TestHuntEnvironmentContract(t *testing.T) {
	d, err := db.Open(researchDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("hunt env contract", "verify M4.3 env", db.TaskCreateOptions{Name: "m43-env"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = d.Exec(`DELETE FROM task_archives WHERE task_id=$1`, task.ID)
		_, _ = d.DeleteTaskCascade(task.ID, false, false)
	}()
	store := d.Exploration(task.ExplorationID)
	if _, err := store.AddGoalWithRoot(map[string]any{"text": "verify env"}, "system"); err != nil {
		t.Fatal(err)
	}
	marker := "M43-test0a1b"
	if err := store.UpsertHypotheses("which endpoint?", []db.Hypothesis{
		{Statement: "marker on E2"}, {Statement: "marker not on E2"},
	}); err != nil {
		t.Fatal(err)
	}
	hyps, _ := store.ListHypotheses()
	var hYes, hNo int64
	for _, h := range hyps {
		if strings.Contains(h.Statement, "on E2") && !strings.Contains(h.Statement, "not") {
			hYes = h.ID
		} else {
			hNo = h.ID
		}
	}
	if hYes == 0 || hNo == 0 {
		t.Fatalf("setup: hypotheses not found (%d/%d)", hYes, hNo)
	}
	intent, err := store.AddIntentWithParents(map[string]any{"summary": "probe E2"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	noise, err := store.AddIntentWithParents(map[string]any{"summary": "do something else"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	expID, err := store.DeclareExperiment(db.Experiment{
		Question: "which endpoint?", Action: "probe E2",
		HypothesisIDs: []int64{hYes, hNo},
		Expectations:  map[int64]string{hYes: marker, hNo: "M43-DEAD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DispatchExperiment(expID, intent); err != nil {
		t.Fatal(err)
	}

	// truth=2：probe E2 必须产出真实标记并唯一区分 hYes。
	executeHuntProbes(d, store, 2, marker)
	var factSum string
	if err := d.QueryRow(`SELECT payload->>'summary' FROM exploration_nodes
		WHERE exploration_id=$1 AND kind='fact' AND payload->>'summary'=$2`, task.ExplorationID, marker).Scan(&factSum); err != nil {
		t.Fatalf("probe must yield the real marker fact: %v", err)
	}
	var intentState string
	_ = d.QueryRow(`SELECT state FROM exploration_nodes WHERE id=$1`, intent).Scan(&intentState)
	if intentState != "done" {
		t.Fatalf("probe intent must end done, got %q", intentState)
	}
	var noiseState string
	_ = d.QueryRow(`SELECT state FROM exploration_nodes WHERE id=$1`, noise).Scan(&noiseState)
	if noiseState == "done" {
		t.Fatal("non-probe intent must NOT be executed by the scripted environment")
	}
	hypsAfter, _ := store.ListHypotheses()
	stat := map[int64]string{}
	for _, h := range hypsAfter {
		stat[h.ID] = h.Status
	}
	if stat[hYes] != "supported" || stat[hNo] != "refuted" {
		t.Fatalf("expectation matching must discriminate: %+v", stat)
	}

	// truth≠2：probe E1 返回 M43-DEAD，唯一区分 hNo（信念修订方向反转）。
	if err := store.UpsertHypotheses("dead probe?", []db.Hypothesis{
		{Statement: "E1 is real"}, {Statement: "E1 is dead"},
	}); err != nil {
		t.Fatal(err)
	}
	h2, _ := store.ListHypotheses()
	var d1, d2 int64
	for _, h := range h2 {
		if h.Question == "dead probe?" {
			if d1 == 0 {
				d1 = h.ID
			} else {
				d2 = h.ID
			}
		}
	}
	i2, err := store.AddIntentWithParents(map[string]any{"summary": "Probe   e1"}, 5, nil, "planner", nil)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := store.DeclareExperiment(db.Experiment{
		Question: "dead probe?", Action: "probe E1",
		HypothesisIDs: []int64{d1, d2},
		Expectations:  map[int64]string{d1: marker, d2: "M43-DEAD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DispatchExperiment(e2, i2); err != nil {
		t.Fatal(err)
	}
	executeHuntProbes(d, store, 3, marker) // truth=E3
	h3, _ := store.ListHypotheses()
	stat2 := map[int64]string{}
	for _, h := range h3 {
		stat2[h.ID] = h.Status
	}
	if stat2[d2] != "supported" || stat2[d1] != "refuted" {
		t.Fatalf("DEAD observation must discriminate the other way: %+v", stat2)
	}
}
