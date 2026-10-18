package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/harness"
)

// M2.1 可信采集存储回归（真实隔离 PG）：append-only 不可变、digest 钉定、
// 同 id 幂等/异内容冲突、任务范围解析边界、超界拒绝、评估存档回读，以及
// M1a 评估内核直接消费 PG 观察的全栈回放（confirm 经冻结验收路径产生）。

func m21Observation(id string, taskID int64, payload []byte) harness.Observation {
	return harness.Observation{
		ID: id, TaskID: taskID, ExperimentID: "exp-m21", ExecutionID: "exec-m21",
		ContextVersion: "ctx-m21", SubjectRef: "probe", ObjectRef: "fixture",
		ObservedAt: time.Now().UTC().Truncate(time.Microsecond), Complete: true, Payload: payload,
	}
}

func TestHarnessObservationStoreImmutable(t *testing.T) {
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	taskID := time.Now().UnixNano() % 1e9 // 无 FK 约束（观察独立于 tasks 表）
	suffix := fmt.Sprintf("%d", taskID)
	obs := m21Observation("m21-immutable-1-"+suffix, taskID, []byte(`{"v":1}`))
	if err := d.StoreHarnessObservation(obs); err != nil {
		t.Fatal(err)
	}
	// 同 id 同内容 → 幂等成功。
	if err := d.StoreHarnessObservation(obs); err != nil {
		t.Fatalf("idempotent re-store must succeed: %v", err)
	}
	// 同 id 异内容 → 显式冲突。
	conflicting := m21Observation("m21-immutable-1-"+suffix, taskID, []byte(`{"v":2}`))
	if err := d.StoreHarnessObservation(conflicting); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("same-id different-content must conflict explicitly, got %v", err)
	}
	// 不可变：UPDATE/DELETE 被触发器拒绝。
	if _, err := d.Exec(`UPDATE harness_observations SET payload='\x00' WHERE id=$1`, obs.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE must be rejected by the immutability trigger, got %v", err)
	}
	if _, err := d.Exec(`DELETE FROM harness_observations WHERE id=$1`, obs.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE must be rejected by the immutability trigger, got %v", err)
	}
	// 超界 payload 在写入前被拒。
	if err := d.StoreHarnessObservation(m21Observation("m21-big-"+suffix, taskID, make([]byte, harness.MaxArtifactBytes+1))); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize payload must be rejected at store time, got %v", err)
	}
}

func TestHarnessResolverTaskScopeAndFullStackEvaluation(t *testing.T) {
	d, err := Open(researchPlanDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	base := time.Now().UnixNano() % 1e9
	taskA, taskB := base, base+1
	obsID := fmt.Sprintf("m21-stack-1-%d", taskA) // 每轮唯一，残留行不干扰断言
	payload := []byte(`{"fixture_value":42}`)
	if err := d.StoreHarnessObservation(m21Observation(obsID, taskA, payload)); err != nil {
		t.Fatal(err)
	}
	// 任务范围：B 的 resolver 看不到 A 的观察。
	if _, err := d.Harness(taskB).Resolver().Resolve(t.Context(), obsID); err == nil {
		t.Fatal("cross-task resolve must fail (caller/task access boundary)")
	}
	// 全栈回放：M1a 内核消费 PG 观察 → 冻结验收 → confirm。
	acceptance := harness.AcceptanceSpec{
		TaskID: taskA, GoalID: base + 2, Version: "acc-m21",
		Criterion:    harness.Criterion{ID: "fixture-value", Version: "1"},
		Claim:        "Local fixture value equals 42.",
		ExperimentID: "exp-m21", ContextVersion: "ctx-m21",
	}
	source, err := harness.NewFrozenAcceptanceSource([]harness.AcceptanceSpec{acceptance})
	if err != nil {
		t.Fatal(err)
	}
	digest := harness.Digest(payload)
	verifier := func(_ context.Context, _ harness.EvaluationRequest, obs []harness.Observation) (harness.Verdict, error) {
		ids := make([]string, 0, len(obs))
		for _, o := range obs {
			ids = append(ids, o.ID)
			if string(o.Payload) != string(payload) {
				return harness.Verdict{}, nil // 不会发生：digest 已校验
			}
		}
		return harness.Verdict{Validity: harness.ValidityValid, Conclusion: harness.Supported,
			EvidenceIDs: ids, Reason: "pg-backed payload equals the expected constant"}, nil
	}
	registry := map[harness.Criterion]harness.Verifier{acceptance.Criterion: verifier}
	req := harness.EvaluationRequest{
		SchemaVersion: harness.SchemaVersion, ID: "m21-eval-1", TaskID: taskA, GoalID: base + 2,
		ExperimentID: "exp-m21", ContextVersion: "ctx-m21", Claim: acceptance.Claim,
		CriterionID: "fixture-value", CriterionVersion: "1", AcceptanceVersion: "acc-m21",
		Evidence: []harness.EvidenceRef{{ObservationID: obsID, SHA256: digest}},
	}
	ev := harness.NewEvaluatorWithAcceptance(d.Harness(taskA).Resolver(), registry, source)
	report := ev.Evaluate(t.Context(), req)
	if report.Conclusion != harness.Supported || report.Disposition != harness.Confirm {
		t.Fatalf("full-stack PG evaluation must confirm: %+v (issues=%v)", report, report.Issues)
	}
	// 评估存档：追加 + 回读一致。
	store := d.Harness(taskA)
	if err := store.RecordEvaluation(report); err != nil {
		t.Fatal(err)
	}
	got, err := store.LatestEvaluations(10)
	if err != nil || len(got) == 0 {
		t.Fatalf("evaluation read-back failed: %v %d", err, len(got))
	}
	if got[0].RequestID != "m21-eval-1" || got[0].Disposition != harness.Confirm {
		t.Fatalf("evaluation record mismatch: %+v", got[0])
	}
	// 存档同样不可变。
	if _, err := d.Exec(`DELETE FROM harness_evaluations WHERE request_id=$1`, "m21-eval-1"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("evaluation DELETE must be rejected, got %v", err)
	}
}
