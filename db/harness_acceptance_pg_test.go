package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/harness"
)

// M2.2 权威验收源回归（真实隔离 PG）：版本推进/历史不可变/幂等/版本号不复用/
// 缺失 fail-closed/规格改写被拒，以及全栈（PG 观察 + PG 验收 + 前提标记验收器
// → confirm）。

func m22Spec(taskID, goalID int64, version, marker string) harness.AcceptanceSpec {
	return harness.AcceptanceSpec{
		TaskID: taskID, GoalID: goalID, Version: version,
		Criterion:    harness.Criterion{ID: "premise-contains", Version: "1"},
		Claim:        fmt.Sprintf("Captured observations contain the marker %s.", marker),
		ExperimentID: "exp-m22", ContextVersion: "ctx-m22",
	}
}

func TestHarnessAcceptanceVersions(t *testing.T) {
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	base := time.Now().UnixNano() % 1e9
	taskID, goalID := base, base+1
	if err := d.SetHarnessAcceptance(m22Spec(taskID, goalID, "v1", "M1")); err != nil {
		t.Fatal(err)
	}
	// 幂等：完全一致的当前版本 → 无操作成功。
	if err := d.SetHarnessAcceptance(m22Spec(taskID, goalID, "v1", "M1")); err != nil {
		t.Fatalf("identical current version must be idempotent: %v", err)
	}
	resolver := d.HarnessAcceptanceResolver()
	spec, err := resolver.ResolveAcceptance(t.Context(), taskID, goalID)
	if err != nil || spec.Version != "v1" {
		t.Fatalf("resolve current: %+v %v", spec, err)
	}
	// 版本推进：v2 成为当前，v1 保留为历史。
	if err := d.SetHarnessAcceptance(m22Spec(taskID, goalID, "v2", "M2")); err != nil {
		t.Fatal(err)
	}
	spec, err = resolver.ResolveAcceptance(t.Context(), taskID, goalID)
	if err != nil || spec.Version != "v2" || !strings.Contains(spec.Claim, "M2") {
		t.Fatalf("resolve must return v2: %+v %v", spec, err)
	}
	hist, cur, err := d.HarnessAcceptanceHistory(taskID, goalID)
	if err != nil || len(hist) != 2 || hist[0].Version != "v2" || hist[1].Version != "v1" || !cur[0] || cur[1] {
		t.Fatalf("history: %+v cur=%v err=%v", hist, cur, err)
	}
	// 版本号不复用。
	if err := d.SetHarnessAcceptance(m22Spec(taskID, goalID, "v1", "M3")); err == nil || !strings.Contains(err.Error(), "never reused") {
		t.Fatalf("version reuse must be rejected, got %v", err)
	}
	// 缺失 goal → fail-closed。
	if _, err := resolver.ResolveAcceptance(t.Context(), taskID, goalID+999); err == nil {
		t.Fatal("missing acceptance must fail closed")
	}
	// 规格字段改写被拒（仅 is_current 可变）。
	if _, err := d.Exec(`UPDATE harness_acceptance SET claim='forged' WHERE task_id=$1 AND goal_id=$2 AND version='v1'`, taskID, goalID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("spec rewrite must be rejected, got %v", err)
	}
	if _, err := d.Exec(`DELETE FROM harness_acceptance WHERE task_id=$1`, taskID); err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("delete must be rejected, got %v", err)
	}
}

func TestHarnessAcceptanceFullStackPremise(t *testing.T) {
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	base := time.Now().UnixNano() % 1e9
	taskID, goalID := base, base+1
	suffix := fmt.Sprintf("%d", taskID)
	marker := "PREMISE-MARK-" + suffix
	spec := m22Spec(taskID, goalID, "v1", marker)
	if err := d.SetHarnessAcceptance(spec); err != nil {
		t.Fatal(err)
	}
	obs := harness.Observation{
		ID: "m22-obs-" + suffix, TaskID: taskID, ExperimentID: "exp-m22", ExecutionID: "exec-m22",
		ContextVersion: "ctx-m22", SubjectRef: "probe", ObjectRef: "fixture",
		ObservedAt: time.Now().UTC(), Complete: true,
		Payload: []byte("capture contains " + marker + " once"),
	}
	if err := d.StoreHarnessObservation(obs); err != nil {
		t.Fatal(err)
	}
	verifier, err := harness.NewPremiseMarkerVerifier(marker)
	if err != nil {
		t.Fatal(err)
	}
	registry := map[harness.Criterion]harness.Verifier{spec.Criterion: verifier}
	ev := harness.NewEvaluatorWithAcceptance(d.Harness(taskID).Resolver(), registry, d.HarnessAcceptanceResolver())
	req := harness.EvaluationRequest{
		SchemaVersion: harness.SchemaVersion, ID: "m22-eval-" + suffix, TaskID: taskID, GoalID: goalID,
		ExperimentID: spec.ExperimentID, ContextVersion: spec.ContextVersion, Claim: spec.Claim,
		CriterionID: spec.Criterion.ID, CriterionVersion: spec.Criterion.Version, AcceptanceVersion: "v1",
		Evidence: []harness.EvidenceRef{{ObservationID: obs.ID, SHA256: harness.Digest(obs.Payload)}},
	}
	report := ev.Evaluate(t.Context(), req)
	if report.Conclusion != harness.Supported || report.Disposition != harness.Confirm {
		t.Fatalf("full-stack premise evaluation must confirm: %+v issues=%v", report, report.Issues)
	}
	if err := d.Harness(taskID).RecordEvaluation(report); err != nil {
		t.Fatal(err)
	}
	// 换标记（推 v2）后，同一请求（仍指 v1）必须 fail-closed 而非沿用旧判定。
	if err := d.SetHarnessAcceptance(m22Spec(taskID, goalID, "v2", "OTHER-MARK")); err != nil {
		t.Fatal(err)
	}
	report2 := ev.Evaluate(t.Context(), req)
	if report2.Disposition == harness.Confirm {
		t.Fatalf("request pinned to v1 must not confirm after the goal advanced to v2: %+v", report2)
	}
}
