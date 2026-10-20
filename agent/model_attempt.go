package agent

import "context"

type modelRequestGuardKey struct{}

// ModelRequestGuard runs at every actual HTTP attempt, including SDK retries.
// It is host context, not a model-editable request property.
type ModelRequestGuard func(context.Context) (context.Context, error)

func WithModelRequestGuard(ctx context.Context, guard ModelRequestGuard) context.Context {
	return context.WithValue(ctx, modelRequestGuardKey{}, guard)
}
