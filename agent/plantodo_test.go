package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// M1b.1 持久化 TodoWrite 工具单元测试：先落盘后确认、CAS 冲突显式化、输入
// 校验拒绝、hydration 失败传播（停止规划轮）、稳定步骤 ID 跨调用保留。
// 真实 PostgreSQL CAS/恢复/归档矩阵在 db 包专用套件（scripts/test-research-pg.sh）。

type fakePlanPersister struct {
	loaded    *db.TaskPlan
	loadErr   error
	saveErr   error // 非 nil 时保存失败（可为 *db.PlanConflictError）
	saved     []db.TaskPlan
	saveCalls int
}

func (f *fakePlanPersister) LoadTaskPlan(int64) (*db.TaskPlan, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.loaded == nil {
		return nil, nil
	}
	cp := *f.loaded
	return &cp, nil
}

func (f *fakePlanPersister) SaveTaskPlan(p db.TaskPlan) (db.TaskPlan, error) {
	f.saveCalls++
	f.saved = append(f.saved, p)
	if f.saveErr != nil {
		return p, f.saveErr
	}
	p.Revision++
	f.loaded = &p
	return p, nil
}

func todoWriteInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPlannerTodoToolPersistsBeforeAck(t *testing.T) {
	fp := &fakePlanPersister{}
	st := &plannerPlanState{persister: fp, plan: db.TaskPlan{ExplorationID: 42, Revision: 3}}
	tool := st.todoTool(nil)
	res, err := tool.Call(context.Background(), todoWriteInput(t, map[string]any{
		"todos": []map[string]any{
			{"content": "step A", "status": "pending", "basis_node_ids": []int{1, 2}, "expect": "admin cookie"},
		},
		"note": "chain reasoning",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fp.saveCalls != 1 {
		t.Fatalf("successful call must persist exactly once, got %d", fp.saveCalls)
	}
	if fp.saved[0].Revision != 3 || fp.saved[0].ModelNotes != "chain reasoning" {
		t.Fatalf("CAS expectation/notes not carried: %+v", fp.saved[0])
	}
	if len(fp.saved[0].Steps) != 1 || fp.saved[0].Steps[0].BasisNodeIDs[0] != 1 ||
		fp.saved[0].Steps[0].ExpectedOutput != "admin cookie" {
		t.Fatalf("structured fields lost: %+v", fp.saved[0].Steps)
	}
	// 确认里带 revision；内存镜像已更新到落盘值（先持久化后确认的可见面）。
	if !strings.Contains(fmt.Sprint(res), "revision 4") {
		t.Fatalf("ack should report the persisted revision: %v", res)
	}
	if st.plan.Revision != 4 || len(st.plan.Steps) != 1 {
		t.Fatalf("run state not advanced to the persisted plan: %+v", st.plan)
	}
	// 第二次调用以新 revision 为 CAS 期望值，且同内容步骤保留稳定 ID。
	id1 := st.plan.Steps[0].ID
	if _, err := tool.Call(context.Background(), todoWriteInput(t, map[string]any{
		"todos": []map[string]any{{"content": "step A", "status": "in_progress"}},
	}), nil); err != nil {
		t.Fatal(err)
	}
	if fp.saved[1].Revision != 4 {
		t.Fatalf("second save must expect revision 4, got %d", fp.saved[1].Revision)
	}
	if st.plan.Steps[0].ID != id1 {
		t.Fatalf("stable step id changed across calls: %d -> %d", id1, st.plan.Steps[0].ID)
	}
}

func TestPlannerTodoToolConflictIsExplicit(t *testing.T) {
	fp := &fakePlanPersister{saveErr: &db.PlanConflictError{ExplorationID: 42, HaveRevision: 9}}
	st := &plannerPlanState{persister: fp, plan: db.TaskPlan{ExplorationID: 42, Revision: 5}}
	tool := st.todoTool(nil)
	res, err := tool.Call(context.Background(), todoWriteInput(t, map[string]any{
		"todos": []map[string]any{{"content": "stale", "status": "pending"}},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(res), "conflict") || !strings.Contains(fmt.Sprint(res), "revision 9") {
		t.Fatalf("conflict must be explicit with current revision: %v", res)
	}
	if st.plan.Revision != 5 {
		t.Fatalf("conflict must not advance the run state: %+v", st.plan)
	}
}

func TestPlannerTodoToolValidationRejects(t *testing.T) {
	st := &plannerPlanState{persister: &fakePlanPersister{}, plan: db.TaskPlan{ExplorationID: 1}}
	tool := st.todoTool(nil)
	cases := []map[string]any{
		{"todos": []map[string]any{{"content": "x", "status": "done"}}},                                                         // 非法状态
		{"todos": []map[string]any{{"content": "", "status": "pending"}}},                                                       // 空内容
		{"todos": []map[string]any{{"content": strings.Repeat("y", db.PlanMaxContentBytes+1), "status": "pending"}}},            // 内容超界
		{"todos": []map[string]any{{"content": "x", "status": "pending"}}, "note": strings.Repeat("n", db.PlanMaxNotesBytes+1)}, // 便签超界
	}
	for i, in := range cases {
		res, err := tool.Call(context.Background(), todoWriteInput(t, in), nil)
		if err != nil {
			t.Fatalf("case %d: validation rejection is a tool result, not an error: %v", i, err)
		}
		if !strings.Contains(fmt.Sprint(res), "rejected") {
			t.Fatalf("case %d: expected explicit rejection, got %v", i, res)
		}
	}
	if st.persister.(*fakePlanPersister).saveCalls != 0 {
		t.Fatal("rejected input must never reach persistence")
	}
}

func TestPlannerTodoToolStoreErrorStopsNotAcks(t *testing.T) {
	fp := &fakePlanPersister{saveErr: errors.New("connection refused")}
	st := &plannerPlanState{persister: fp, plan: db.TaskPlan{ExplorationID: 1}}
	tool := st.todoTool(nil)
	if _, err := tool.Call(context.Background(), todoWriteInput(t, map[string]any{
		"todos": []map[string]any{{"content": "x", "status": "pending"}},
	}), nil); err == nil || !strings.Contains(err.Error(), "NOT saved") {
		t.Fatalf("store failure must surface as an error stating the plan was not saved: %v", err)
	}
}

func TestLoadPlannerPlanStateStopsOnError(t *testing.T) {
	if _, err := loadPlannerPlanState(&fakePlanPersister{loadErr: errors.New("db down")}, 7); err == nil {
		t.Fatal("hydration failure must propagate (planning round must stop, never fall back to empty memory)")
	}
	st, err := loadPlannerPlanState(&fakePlanPersister{}, 7)
	if err != nil || st.plan.Revision != 0 || len(st.plan.Steps) != 0 {
		t.Fatalf("absent plan must hydrate as explicit empty: %+v %v", st, err)
	}
	pre := &db.TaskPlan{ExplorationID: 7, Revision: 2, Steps: []db.PlanStep{{ID: 1, Content: "kept", Status: "pending"}}}
	st, err = loadPlannerPlanState(&fakePlanPersister{loaded: pre}, 7)
	if err != nil || st.plan.Revision != 2 || st.plan.Steps[0].Content != "kept" {
		t.Fatalf("stored plan must hydrate verbatim: %+v %v", st, err)
	}
}

func TestPlannerTodosProjection(t *testing.T) {
	st := &plannerPlanState{plan: db.TaskPlan{Steps: []db.PlanStep{
		{Content: "a", Status: "pending"},
		{Content: "b", Status: "in_progress"},
	}}}
	got := st.todos()
	if len(got) != 2 || got[0].Content != "a" || got[1].Status != "in_progress" {
		t.Fatalf("projection mismatch: %+v", got)
	}
}
