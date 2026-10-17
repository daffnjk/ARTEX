package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/harness"
)

func testBundle() bundle {
	o := harness.Observation{ID: "o", TaskID: 1, ExperimentID: "e", ExecutionID: "x", ContextVersion: "c",
		SubjectRef: "test-user", ObjectRef: "test-object", ObservedAt: time.Now().UTC(), Complete: true, Payload: []byte("abc")}
	return bundle{Request: harness.EvaluationRequest{SchemaVersion: "1", ID: "r", TaskID: 1, GoalID: 2,
		ExperimentID: "e", ContextVersion: "c", Claim: "local fixture", CriterionID: "not-installed", CriterionVersion: "1",
		Evidence: []harness.EvidenceRef{{ObservationID: "o", SHA256: harness.Digest(o.Payload)}}}, Observations: []harness.Observation{o}}
}

func TestFixtureNeverBecomesDomainProof(t *testing.T) {
	data, _ := json.Marshal(testBundle())
	var out, diag bytes.Buffer
	if code := run(bytes.NewReader(data), &out, &diag); code != 2 {
		t.Fatalf("code=%d diagnostics=%s", code, &diag)
	}
	var report harness.EvaluationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Disposition != harness.Review || report.Issues[0].Code != "criterion_unimplemented" {
		t.Fatalf("unexpected: %+v", report)
	}
}

func TestInputValidation(t *testing.T) {
	valid, _ := json.Marshal(testBundle())
	duplicate := testBundle()
	duplicate.Observations = append(duplicate.Observations, duplicate.Observations[0])
	dup, _ := json.Marshal(duplicate)
	missing := testBundle()
	missing.Observations[0].ID = ""
	missingID, _ := json.Marshal(missing)
	many := testBundle()
	many.Observations = make([]harness.Observation, harness.MaxEvidenceRefs+1)
	tooMany, _ := json.Marshal(many)
	for _, data := range [][]byte{[]byte("not json"), []byte(`{"unexpected":true}`), append(append([]byte{}, valid...), []byte(" {}")...),
		dup, missingID, tooMany, bytes.Repeat([]byte("x"), maxInputBytes+1)} {
		var out, diag bytes.Buffer
		if code := run(bytes.NewReader(data), &out, &diag); code != 1 {
			t.Fatalf("bad input returned %d", code)
		}
	}
}

func TestStaleAndMissingObservation(t *testing.T) {
	for _, stale := range []bool{false, true} {
		b := testBundle()
		want := 2
		if stale {
			b.Observations[0].ContextVersion = "old"
			want = 3
		} else {
			b.Observations = nil
		}
		data, _ := json.Marshal(b)
		if got := run(bytes.NewReader(data), io.Discard, io.Discard); got != want {
			t.Fatalf("want %d got %d", want, got)
		}
	}
}

type brokenIO struct{}

func (brokenIO) Read([]byte) (int, error)  { return 0, errors.New("broken read") }
func (brokenIO) Write([]byte) (int, error) { return 0, errors.New("broken write") }

func TestIOFailures(t *testing.T) {
	if got := run(brokenIO{}, io.Discard, io.Discard); got != 1 {
		t.Fatal(got)
	}
	if got := run(strings.NewReader(`{}`), brokenIO{}, io.Discard); got != 1 {
		t.Fatal(got)
	}
}
