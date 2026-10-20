package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/control"
	actool "github.com/Autumn-27/norma/tool"
)

func feedbackKind(err error) control.FeedbackKind {
	switch {
	case errors.Is(err, control.WaitExpired):
		return control.FeedbackWaitExpired
	case errors.Is(err, control.PreconditionInvalid), errors.Is(err, control.StaleContext):
		return control.FeedbackPreconditionInvalid
	case errors.Is(err, control.ToolUnavailable), errors.Is(err, control.MethodUnsupported):
		return control.FeedbackToolUnavailable
	case errors.Is(err, control.ScopeDenied), errors.Is(err, control.ContractMissing), errors.Is(err, control.ContractExpired), errors.Is(err, control.ContractRevoked), errors.Is(err, control.BudgetExhausted), errors.Is(err, control.LineageMismatch):
		return control.FeedbackPolicyDenied
	case errors.Is(err, control.EffectUncertain):
		return control.FeedbackEffectUnknown
	case errors.Is(err, control.RateLimited):
		return control.FeedbackRateLimited
	default:
		return "UNKNOWN"
	}
}
func (s *Server) controlFeedbackResult(ctx context.Context, task control.TaskID, id control.ExecutionID, kind control.FeedbackKind) (actool.Result, error) {
	if err := s.m.pg.RecordControlFeedback(ctx, task, id, kind, time.Second); err != nil {
		return actool.Errorf("host feedback persistence failed; do not retry external action"), nil
	}
	if live, ok := s.m.Task(strconv.FormatInt(int64(task), 10)); ok {
		live.Notify()
	}
	raw, _ := json.Marshal(struct {
		ExecutionID control.ExecutionID
		Kind        control.FeedbackKind
		Decision    control.FeedbackDecision
	}{id, kind, control.DecideFeedback(kind, time.Second)})
	return actool.Errorf(string(raw)), nil
}
