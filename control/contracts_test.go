package control

import (
	"encoding/json"
	"strings"
	"testing"
)

const proposalFixture = `{"schema_version":1,"method_id":"http.get","method_version":"1","target":{"scheme":"http","host":"localhost","port":8080,"path":"/fixture"},"purpose":"read synthetic fixture","context_version":"ctx-1","idempotency_key":"read-1"}`

func TestControlProposalRejectsAuthorityFields(t *testing.T) {
	if _, err := DecodeProposal([]byte(proposalFixture)); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"approved_by", "grant", "provenance", "task_id", "actual_cost", "confirmed", "trusted_source"} {
		t.Run(field, func(t *testing.T) {
			raw := strings.TrimSuffix(proposalFixture, "}") + `,"` + field + `":"model supplied"}`
			if _, err := DecodeProposal([]byte(raw)); err == nil {
				t.Fatalf("accepted authority field %s", field)
			}
		})
	}
	for _, raw := range []string{proposalFixture + ` {}`, `null`, strings.Replace(proposalFixture, `"path":"/fixture"`, `"path":"/fixture","grant":"forged"`, 1), strings.Repeat(" ", MaxProposalBytes) + proposalFixture} {
		if _, err := DecodeProposal([]byte(raw)); err == nil {
			t.Fatal("accepted oversized, nested authority, null or trailing input")
		}
	}
}

func TestControlIDNamespaces(t *testing.T) {
	task, err := ParseTaskID("task:42")
	if err != nil || task != TaskID(42) {
		t.Fatal(task, err)
	}
	exp, err := ParseExplorationID("exploration:42")
	if err != nil || exp != ExplorationID(42) {
		t.Fatal(exp, err)
	}
	for _, v := range []string{"exploration:42", "42", "task:0", "task:-1", "task:9223372036854775808"} {
		if _, err := ParseTaskID(v); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	if _, err := ParseExplorationID("task:42"); err == nil {
		t.Fatal("wrong ID namespace")
	}
	ctx := TaskContext{TaskID: task, ExplorationID: exp, ContractVersion: 1, ControlEpoch: 1, ContextVersion: "identity-1", CriterionVersion: "criterion-1"}
	if err := ctx.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx.TaskID = 0
	if err := ctx.Validate(); err == nil {
		t.Fatal("accepted unknown host task identity")
	}
}

func TestControlOutcomeDimensions(t *testing.T) {
	for _, effect := range []EffectStatus{EffectUnknown, EffectApplied, EffectNotApplied, EffectNotApplicable} {
		for _, cleanup := range []CleanupStatus{CleanupPending, CleanupConfirmed, CleanupUnconfirmed, CleanupFailed, CleanupNotRequired} {
			o := Outcome{Run: RunFailed, Effect: effect, Cleanup: cleanup}
			if err := o.Validate(); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(o)
			var got Outcome
			if err := json.Unmarshal(data, &got); err != nil || got != o {
				t.Fatal("dimension round trip", got, err)
			}
		}
	}
	if err := (Outcome{Run: "done", Effect: EffectUnknown, Cleanup: CleanupPending}).Validate(); err == nil {
		t.Fatal("accepted ambiguous run status")
	}
	for reason, want := range map[StopReason]string{StopComplete: "done", StopDeadline: "timeout", StopBudget: "timeout", StopUnknown: "failed", StopRevoked: "failed"} {
		if got := LegacyStatus(reason); got != want {
			t.Errorf("%s: %s != %s", reason, got, want)
		}
	}
	for _, mode := range []TaskMode{ModeDirect, ModeGoal, ModeCoverage} {
		if !mode.Valid() {
			t.Fatal(mode)
		}
	}
	if TaskMode("legacy").Valid() {
		t.Fatal("legacy must not silently enter controlled mode")
	}
}
