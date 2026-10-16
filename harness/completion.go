package harness

import "strings"

// RequiresGoalReview is a rollout gate, not an authorization system. Empty or
// "legacy" preserves existing behavior. "review" and invalid nonempty values
// block these two model-driven completion tools. Model arguments cannot opt out.
func RequiresGoalReview(toolName, mode string) bool {
	if toolName != "prove_goal" && toolName != "goal_met" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "legacy":
		return false
	default:
		return true
	}
}
