// Package execution defines controlled execution ports; it never imports db or server.
package execution

import (
	"context"
	"time"

	"github.com/Autumn-27/artex/control"
)

const HTTPMaxRequests int64 = 3
const HTTPMaxResponseBytes int64 = 65536
const HTTPTimeout = 3 * time.Second

type Record struct {
	ID              control.ExecutionID    `json:"id"`
	TaskID          control.TaskID         `json:"task_id"`
	ExplorationID   control.ExplorationID  `json:"exploration_id"`
	Proposal        control.ActionProposal `json:"proposal"`
	Digest          string                 `json:"digest"`
	SemanticKey     string                 `json:"semantic_key"`
	ContractVersion int64                  `json:"contract_version"`
	ControlEpoch    int64                  `json:"control_epoch"`
	GrantExpires    time.Time              `json:"grant_expires"`
	Outcome         control.Outcome        `json:"outcome"`
}
type Admission interface {
	AdmitAndReserve(context.Context, control.TaskID, control.ActionProposal) (Record, error)
}
type GrantChecker interface {
	CheckExecutionGrant(context.Context, control.TaskID, control.ExecutionID) error
}
type OutboxEvent struct {
	ID          int64
	TaskID      control.TaskID
	ExecutionID control.ExecutionID
	Version     int64
}
