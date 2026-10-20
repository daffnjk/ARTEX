package server

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strconv"
	"sync"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/llm"
	"github.com/google/uuid"
)

type controlBudgetProvider struct {
	next llm.Provider
	pg   *db.DB
}
type controlCallBudget struct {
	pg       *db.DB
	taskID   int64
	role     string
	estimate int64
	mu       sync.Mutex
	keys     []string
}

func (p *controlBudgetProvider) prepare(ctx context.Context, req llm.CompletionRequest) (context.Context, *controlCallBudget, context.CancelFunc, error) {
	noop := func() {}
	a := control.CallAttributionFrom(ctx)
	taskID := int64(a.TaskID)
	if id := llmrec.TaskIDFrom(ctx); id != "" {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 || taskID != 0 && taskID != parsed {
			return ctx, nil, noop, control.LineageMismatch
		}
		taskID = parsed
	}
	if taskID == 0 {
		taskID = agent.RunInfoFrom(ctx).TaskID
	}
	if taskID == 0 {
		return ctx, nil, noop, nil
	}
	mode, err := p.pg.ControlMode(ctx, taskID)
	if err != nil {
		return ctx, nil, noop, err
	}
	if mode == "legacy" {
		return ctx, nil, noop, nil
	}
	if !control.ValidCallRole(a.Role) {
		return ctx, nil, noop, control.LineageMismatch
	}
	active, err := p.pg.ActiveControlContract(ctx, taskID)
	if err != nil {
		return ctx, nil, noop, err
	}
	if req.MaxTokens <= 0 || req.MaxTokens > 128000 {
		return ctx, nil, noop, control.UnboundedEffect
	}
	raw, err := json.Marshal(req)
	if err != nil || len(raw) > 4*1024*1024 {
		return ctx, nil, noop, control.UnboundedEffect
	}
	ctx, cancel := context.WithDeadline(ctx, active.Body.ExpiresAt)
	state := &controlCallBudget{pg: p.pg, taskID: taskID, role: a.Role, estimate: int64(len(raw) + req.MaxTokens)}
	return agent.WithModelRequestGuard(ctx, state.before), state, cancel, nil
}
func (b *controlCallBudget) before(ctx context.Context) (context.Context, error) {
	key := uuid.NewString()
	attr := b.role + ":" + key
	if err := b.pg.ReserveControlBudget(ctx, b.taskID, key, attr, map[string]int64{"llm_requests": 1, "tokens": b.estimate}, false); err != nil {
		return ctx, err
	}
	one := int64(1)
	if err := b.pg.SettleControlBudget(ctx, b.taskID, key, map[string]*int64{"llm_requests": &one, "tokens": nil}); err != nil {
		return ctx, err
	}
	b.mu.Lock()
	b.keys = append(b.keys, key)
	b.mu.Unlock()
	return ctx, nil
}
func (b *controlCallBudget) finish(ctx context.Context, usage llm.Usage, known bool) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.keys) == 0 {
		return control.UnboundedEffect
	}
	total := int64(usage.InputTokens) + int64(usage.OutputTokens) + int64(usage.CacheReadTokens) + int64(usage.CacheWriteTokens)
	var actual *int64
	if known && total > 0 {
		actual = &total
	}
	one := int64(1)
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return b.pg.SettleControlBudget(settle, b.taskID, b.keys[len(b.keys)-1], map[string]*int64{"llm_requests": &one, "tokens": actual})
}
func (p *controlBudgetProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	ctx, b, cancel, err := p.prepare(ctx, req)
	if err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	defer cancel()
	msg, stop, usage, err := p.next.Complete(ctx, req)
	if b != nil {
		err = errors.Join(err, b.finish(ctx, usage, err == nil))
	}
	return msg, stop, usage, err
}
func (p *controlBudgetProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		ctx, b, cancel, err := p.prepare(ctx, req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		defer cancel()
		acc := llm.NewAccumulator()
		for event, err := range p.next.Stream(ctx, req) {
			if err != nil {
				if b != nil {
					err = errors.Join(err, b.finish(ctx, llm.Usage{}, false))
				}
				yield(llm.StreamEvent{}, err)
				return
			}
			acc.Add(event)
			if !yield(event, nil) {
				return
			}
		}
		if b != nil {
			if err := b.finish(ctx, acc.Usage, true); err != nil {
				yield(llm.StreamEvent{}, err)
			}
		}
	}
}
