package control

import "context"

type CallAttribution struct {
	TaskID TaskID
	Role   string
}
type callKey struct{}

func WithCallAttribution(ctx context.Context, task TaskID, role string) context.Context {
	return context.WithValue(ctx, callKey{}, CallAttribution{task, role})
}
func CallAttributionFrom(ctx context.Context) CallAttribution {
	v, _ := ctx.Value(callKey{}).(CallAttribution)
	return v
}
func ValidCallRole(role string) bool {
	switch role {
	case "worker", "planner", "goals", "mainagent", "summary", "reviewer", "cleanup", "reconcile":
		return true
	}
	return false
}
