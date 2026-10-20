package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) controlGate() *execution.Supervisor {
	s.controlInit.Do(func() {
		if s.controlSupervisor == nil {
			s.controlSupervisor = execution.NewSupervisor()
		}
	})
	return s.controlSupervisor
}
func (s *Server) controlBackgroundAllowed(ctx context.Context, taskID int64, path string) error {
	mode, err := s.m.pg.ControlMode(ctx, taskID)
	if err != nil {
		return err
	}
	if mode == "legacy" {
		return nil
	}
	if err := s.m.pg.RecordControlDenial(ctx, taskID, path, "unadapted execution path disabled"); err != nil {
		return err
	}
	return control.ToolUnavailable
}
func (s *Server) controlledWorkerTools(ctx context.Context, ri agent.RunInfo) ([]actool.CoreTool, error) {
	if ri.TaskID <= 0 || ri.ExplorationID <= 0 || ri.IntentID <= 0 {
		return nil, control.LineageMismatch
	}
	task, err := s.m.pg.GetTask(ri.TaskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.ExplorationID != ri.ExplorationID {
		return nil, control.LineageMismatch
	}
	mode, err := s.m.pg.ControlMode(ctx, ri.TaskID)
	if err != nil {
		return nil, err
	}
	if mode == "legacy" {
		return nil, control.LineageMismatch
	}
	validate := func(ctx context.Context) error {
		current := agent.RunInfoFrom(ctx)
		if current.TaskID != ri.TaskID || current.ExplorationID != ri.ExplorationID || current.IntentID != ri.IntentID {
			return control.LineageMismatch
		}
		return nil
	}
	props := map[string]any{}
	for _, key := range []string{"method_id", "method_version", "purpose", "context_version", "idempotency_key"} {
		props[key] = map[string]any{"type": "string"}
	}
	props["schema_version"] = map[string]any{"type": "integer", "enum": []int{1}}
	props["target"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"scheme", "host", "port", "path"}, "properties": map[string]any{"scheme": map[string]any{"type": "string"}, "host": map[string]any{"type": "string"}, "port": map[string]any{"type": "integer"}, "path": map[string]any{"type": "string"}}}
	for _, key := range []string{"experiment_id", "step_id"} {
		props[key] = map[string]any{"type": "integer"}
	}
	props["identity_ref"] = map[string]any{"type": "string"}
	props["required_claim_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"schema_version", "method_id", "method_version", "target", "purpose", "context_version", "idempotency_key"}, "properties": props}
	tool := actool.Build(actool.Spec{Name: "control_action", Description: "Propose a bounded synthetic localhost HTTP read under the current host contract. No other execution tools are available.", Schema: schema,
		Permissions: func(ctx context.Context, _ json.RawMessage, _ permission.Context) permission.Decision {
			if err := validate(ctx); err != nil {
				return permission.Denied(err.Error())
			}
			return permission.Allowed()
		},
		Run: func(ctx context.Context, raw json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			if err := validate(ctx); err != nil {
				_ = s.m.pg.RecordControlDenial(ctx, ri.TaskID, "control_action", "missing or conflicting host context")
				return actool.Errorf("LINEAGE_MISMATCH"), nil
			}
			proposal, err := control.DecodeProposal(raw)
			if err != nil {
				return actool.Errorf("invalid bounded proposal"), nil
			}
			record, err := s.m.pg.AdmitAndReserve(ctx, control.TaskID(ri.TaskID), proposal)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if record.Outcome.Run != control.RunQueued {
				existing, err := s.m.pg.ControlExecutionResult(ctx, record.TaskID, record.ID)
				if err != nil {
					return actool.Errorf(err.Error()), nil
				}
				if len(existing) > 0 {
					return actool.Text(string(existing)), nil
				}
				return actool.Text(fmt.Sprintf("execution %s is %s; collect the original result", record.ID, record.Outcome.Run)), nil
			}
			attempt, err := s.m.pg.ClaimControlExecution(ctx, record.TaskID, record.ID, ri.IntentID, fmt.Sprintf("worker:%d", ri.IntentID))
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			bound := ri
			bound.ExecutionID = string(record.ID)
			bound.AttemptID = string(attempt.ID)
			bound.LeaseEpoch = attempt.Epoch
			bound.ParentJobID = fmt.Sprint(ri.IntentID)
			ctx = agent.WithRunInfo(ctx, bound)
			result, runErr := execution.NewHTTPAdapter(s.m.pg, s.controlGate(), nil, nil).Run(ctx, record.TaskID, record.ID)
			if result.ContextVersion == "" {
				result.ContextVersion = proposal.ContextVersion
			}
			if err := s.m.pg.CommitControlHTTPResult(ctx, attempt, result); err != nil {
				return actool.Errorf("result commit failed; execution remains recoverable"), nil
			}
			if runErr != nil {
				return actool.Errorf(runErr.Error()), nil
			}
			output, err := json.Marshal(result)
			if err != nil {
				return actool.Errorf("result encoding failed"), nil
			}
			return actool.Text(string(output)), nil
		}})
	return []actool.CoreTool{tool}, nil
}
