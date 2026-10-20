package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Autumn-27/artex/control"
	"github.com/Autumn-27/artex/execution"
	"github.com/Autumn-27/artex/harness"
)

type controlCaptureMetadata struct {
	ExplorationID            int64
	AttemptID                string
	Epoch                    int64
	Source, Validity, Digest string
	JSON                     []byte
}

func (d *DB) CaptureControlObservations(ctx context.Context, a execution.Attempt) ([]harness.Observation, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exploration, epoch int64
	var currentContext string
	if err = tx.QueryRowContext(ctx, `SELECT exploration_id,control_epoch,control_context_version FROM tasks WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, int64(a.TaskID)).Scan(&exploration, &epoch, &currentContext); err != nil {
		return nil, control.LineageMismatch
	}
	var raw []byte
	var storedDigest, owner, state, actionContext string
	var lease, intent, recordExploration, recordEpoch int64
	var finished sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT e.result_json,e.result_digest,e.exploration_id,e.control_epoch,e.context_version,a.lease_epoch,a.lease_owner,a.intent_id,a.result_kind,a.finished_at FROM action_executions e JOIN execution_attempts a ON a.execution_id=e.id WHERE e.id=$1 AND e.task_id=$2 AND a.id=$3`, string(a.ExecutionID), int64(a.TaskID), string(a.ID)).Scan(&raw, &storedDigest, &recordExploration, &recordEpoch, &actionContext, &lease, &owner, &intent, &state, &finished)
	if err != nil {
		return nil, control.LineageMismatch
	}
	if recordExploration != exploration || lease != a.Epoch || owner != a.Owner || intent != a.IntentID || !finished.Valid {
		return nil, control.LineageMismatch
	}
	if state != "succeeded" && state != "failed" && state != "cancelled" {
		return nil, control.EvidenceUnavailable
	}
	var result execution.HTTPResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	digest, err := actionHash(result)
	if err != nil || digest != storedDigest || result.ExecutionID != a.ExecutionID || result.ContextVersion != actionContext || len(result.Hops) > int(execution.HTTPMaxRequests) {
		return nil, control.EvidenceConflict
	}
	record, err := scanControlExecution(tx.QueryRowContext(ctx, `SELECT `+controlExecutionColumns+` FROM action_executions WHERE id=$1`, string(a.ExecutionID)))
	if err != nil {
		return nil, err
	}
	observations := make([]harness.Observation, 0, len(result.Hops))
	for i, hop := range result.Hops {
		if len(hop.Body) > int(execution.HTTPMaxResponseBytes) || harness.Digest(hop.Body) != hop.SHA256 {
			return nil, control.EvidenceConflict
		}
		canonical, targetErr := control.NormalizeTarget(hop.Target.URL())
		if targetErr != nil || canonical != hop.Target {
			return nil, control.LineageMismatch
		}
		validity := "valid"
		if !hop.Complete {
			validity = "invalid"
		} else if epoch != recordEpoch || currentContext != actionContext || hop.StatusCode == 401 || hop.StatusCode == 403 {
			validity = "unknown"
		} else if hop.StatusCode < 200 || hop.StatusCode >= 300 {
			validity = "unknown"
		}
		metadata, err := json.Marshal(struct {
			Hop           execution.HTTPHop
			ExecutionID   control.ExecutionID
			AttemptID     control.AttemptID
			ExplorationID int64
			Epoch         int64
			Source        string
		}{hop, a.ExecutionID, a.ID, exploration, a.Epoch, "host_http_v1"})
		if err != nil || len(metadata) > 256*1024 {
			return nil, control.UnboundedEffect
		}
		experiment := "control-action:" + string(a.ExecutionID)
		if record.Proposal.ExperimentID != nil {
			experiment = strconv.FormatInt(*record.Proposal.ExperimentID, 10)
		}
		subject := "anonymous"
		if record.Proposal.IdentityRef != nil {
			subject = *record.Proposal.IdentityRef
		}
		o := harness.Observation{ID: fmt.Sprintf("control:%s:%s:%d", a.ExecutionID, a.ID, i), TaskID: int64(a.TaskID), ExperimentID: experiment, ExecutionID: string(a.ExecutionID), ContextVersion: actionContext, SubjectRef: subject, ObjectRef: hop.Target.URL(), ObservedAt: finished.Time.UTC(), Complete: hop.Complete, Payload: hop.Body}
		m := controlCaptureMetadata{ExplorationID: exploration, AttemptID: string(a.ID), Epoch: a.Epoch, Source: "host_http_v1", Validity: validity, JSON: metadata, Digest: harness.Digest(metadata)}
		if err := storeHarnessObservation(ctx, tx, o, &m); err != nil {
			return nil, err
		}
		observations = append(observations, o)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return observations, nil
}

type controlObservationResolver struct {
	d    *DB
	task control.TaskID
}

func (d *DB) ControlObservationResolver(task control.TaskID) harness.Resolver {
	return controlObservationResolver{d, task}
}
func (r controlObservationResolver) Resolve(ctx context.Context, id string) (harness.Observation, error) {
	var o harness.Observation
	var digest string
	err := r.d.QueryRowContext(ctx, `SELECT o.id,o.task_id,o.experiment_id,o.execution_id,o.context_version,o.subject_ref,o.object_ref,o.observed_at,o.complete,o.payload,o.payload_sha256 FROM harness_observations o JOIN tasks t ON t.id=o.task_id AND t.exploration_id=o.exploration_id JOIN action_executions e ON e.id=o.execution_id AND e.task_id=o.task_id JOIN execution_attempts a ON a.id=o.attempt_id AND a.execution_id=e.id WHERE o.task_id=$1 AND o.id=$2 AND o.capture_source='host_http_v1' AND a.lease_epoch=o.lease_epoch AND t.deleted_at IS NULL`, int64(r.task), id).Scan(&o.ID, &o.TaskID, &o.ExperimentID, &o.ExecutionID, &o.ContextVersion, &o.SubjectRef, &o.ObjectRef, &o.ObservedAt, &o.Complete, &o.Payload, &digest)
	if err != nil {
		return o, control.EvidenceUnavailable
	}
	if len(o.Payload) > harness.MaxArtifactBytes || harness.Digest(o.Payload) != digest {
		return o, control.EvidenceConflict
	}
	return o, nil
}

func (d *DB) ControlObservationValidity(ctx context.Context, task control.TaskID, id string) (control.Validity, error) {
	var validity string
	err := d.QueryRowContext(ctx, `SELECT capture_validity FROM harness_observations WHERE task_id=$1 AND id=$2 AND capture_source='host_http_v1'`, int64(task), id).Scan(&validity)
	return control.Validity(validity), err
}
