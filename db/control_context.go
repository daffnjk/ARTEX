package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Autumn-27/artex/control"
	"github.com/google/uuid"
)

func (s *ExplorationStore) ControlledBinding(ctx context.Context, expectedTask int64) (bool, error) {
	if s == nil {
		return false, nil
	}
	var task int64
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT id,control_mode FROM tasks WHERE exploration_id=$1 AND deleted_at IS NULL`, s.expID).Scan(&task, &mode)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if mode == "legacy" {
		return false, nil
	}
	if task != expectedTask || expectedTask <= 0 {
		return true, control.LineageMismatch
	}
	return true, nil
}
func (d *DB) RecordControlDenial(ctx context.Context, taskID int64, path, reason string) error {
	if len(path) > 128 || len(reason) > 512 {
		return control.UnboundedEffect
	}
	payload, err := json.Marshal(map[string]string{"path": path, "reason": reason})
	if err != nil {
		return err
	}
	_, err = d.ExecContext(ctx, `INSERT INTO control_outbox(task_id,aggregate_id,event_type,event_version,payload) SELECT id,$2,'control_denied',1,$3 FROM tasks WHERE id=$1 AND control_mode<>'legacy'`, taskID, uuid.NewString(), payload)
	return err
}
func (s *AssetStore) LegacyEnrichmentAllowed(ctx context.Context, assetID int64) (bool, error) {
	var controlled bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control_asset_boundaries WHERE asset_id=$1) OR EXISTS(SELECT 1 FROM task_asset_links a JOIN tasks t ON t.id=a.task_id WHERE a.asset_id=$1 AND t.control_mode<>'legacy' AND t.deleted_at IS NULL)`, assetID).Scan(&controlled)
	return !controlled, err
}

func (s *ExplorationStore) AuditControlDenial(ctx context.Context, taskID int64, path string) error {
	if s == nil {
		return control.LineageMismatch
	}
	return s.db.RecordControlDenial(ctx, taskID, path, "unadapted task execution disabled")
}
