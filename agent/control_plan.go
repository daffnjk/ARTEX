package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

type planStepPatcher interface {
	PatchPlanStep(context.Context, control.TaskID, int64, int64, db.PlanStep) (db.TaskPlan, error)
}

func (st *plannerPlanState) patchTool(task int64) actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "PatchPlanStep", Description: "Edit an existing stable step ID using expected plan row_id and revision. Dependencies and context are explicit. Step status is a planning note, never proof or permission.",
		Schema:   map[string]any{"type": "object", "properties": map[string]any{"row_id": map[string]any{"type": "integer"}, "revision": map[string]any{"type": "integer"}, "step": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}, "content": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}, "context_version": map[string]any{"type": "string"}, "requires_step_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "requires_claim_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}}, "required": []string{"id", "content", "status", "context_version"}}}, "required": []string{"row_id", "revision", "step"}},
		ReadOnly: func(json.RawMessage) bool { return false }, Concurrent: func(json.RawMessage) bool { return false }, Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			patcher, ok := st.persister.(planStepPatcher)
			if !ok {
				return actool.Errorf("plan ID patch unavailable"), nil
			}
			var in struct {
				RowID    int64       `json:"row_id"`
				Revision int64       `json:"revision"`
				Step     db.PlanStep `json:"step"`
			}
			if len(input) > 32768 {
				return actool.Errorf("bounded plan patch required"), nil
			}
			dec := json.NewDecoder(bytes.NewReader(input))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				return actool.Errorf("invalid plan patch"), nil
			}
			if err := dec.Decode(new(any)); err != io.EOF {
				return actool.Errorf("trailing plan patch data"), nil
			}
			if in.RowID != st.plan.RowID || in.Revision != st.plan.Revision {
				return actool.Errorf("plan patch CAS conflict; reload authoritative plan"), nil
			}
			saved, err := patcher.PatchPlanStep(ctx, control.TaskID(task), in.RowID, in.Revision, in.Step)
			if err != nil {
				return actool.Errorf(fmt.Sprintf("plan patch rejected: %v", err)), nil
			}
			st.plan = saved
			raw, err := json.Marshal(saved)
			if err != nil {
				return actool.Result{}, err
			}
			return actool.Text(string(raw)), nil
		},
	})
}
