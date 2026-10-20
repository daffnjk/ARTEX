package control

import "time"

type FeedbackKind string

const (
	FeedbackValidResult         FeedbackKind = "VALID_RESULT"
	FeedbackValidNegative       FeedbackKind = "VALID_NEGATIVE"
	FeedbackPreconditionInvalid FeedbackKind = "PRECONDITION_INVALID"
	FeedbackToolUnavailable     FeedbackKind = "TOOL_UNAVAILABLE"
	FeedbackWaitExpired         FeedbackKind = "WAIT_EXPIRED"
	FeedbackPolicyDenied        FeedbackKind = "POLICY_DENIED"
	FeedbackRateLimited         FeedbackKind = "RATE_LIMITED"
	FeedbackEffectUnknown       FeedbackKind = "EFFECT_UNKNOWN"
)

type FeedbackDecision struct {
	Action   string
	AllowNew bool
	Cooldown time.Duration
}

func DecideFeedback(kind FeedbackKind, retryAfter time.Duration) FeedbackDecision {
	switch kind {
	case FeedbackValidResult, FeedbackValidNegative:
		return FeedbackDecision{Action: "replan", AllowNew: true}
	case FeedbackPreconditionInvalid:
		return FeedbackDecision{Action: "refresh_premises"}
	case FeedbackToolUnavailable:
		return FeedbackDecision{Action: "choose_supported_method", AllowNew: true}
	case FeedbackWaitExpired:
		return FeedbackDecision{Action: "collect_original"}
	case FeedbackPolicyDenied:
		return FeedbackDecision{Action: "pause_branch"}
	case FeedbackRateLimited:
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		if retryAfter > time.Minute {
			retryAfter = time.Minute
		}
		return FeedbackDecision{Action: "cooldown", Cooldown: retryAfter}
	case FeedbackEffectUnknown:
		return FeedbackDecision{Action: "reconcile_original"}
	default:
		return FeedbackDecision{Action: "inspect_unknown_feedback"}
	}
}

const PlannerStalled ErrorCode = "PLANNER_STALLED"
const RateLimited ErrorCode = "RATE_LIMITED"

type PlanningWindow struct {
	ContextVersion       string             `json:"context_version"`
	Backlog              bool               `json:"backlog"`
	Version              string             `json:"version"`
	EmptyRounds          int                `json:"empty_rounds"`
	Paused               bool               `json:"paused"`
	Reason               string             `json:"reason"`
	ObservationWatermark time.Time          `json:"observation_watermark"`
	BudgetBurn           int64              `json:"budget_burn"`
	LastEventID          int64              `json:"last_event_id"`
	CooldownUntil        time.Time          `json:"cooldown_until"`
	Collect              []ExecutionID      `json:"collect"`
	Feedback             []FeedbackDecision `json:"feedback"`
	AllowNew             bool               `json:"allow_new"`
}
