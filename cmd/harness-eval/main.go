// harness-eval validates a LOCAL fixture bundle. It never connects to a target.
// Fixture observations are user-supplied, so this command is not a trusted
// evidence ingestion path. No domain verifier is registered: intact bundles
// produce REVIEW, never CONFIRM, until a trusted application adapter is built.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Autumn-27/artex/harness"
)

const maxInputBytes = 8 << 20

type bundle struct {
	Request      harness.EvaluationRequest `json:"request"`
	Observations []harness.Observation     `json:"observations"`
}

type fixtureStore map[string]harness.Observation

func (s fixtureStore) Resolve(_ context.Context, id string) (harness.Observation, error) {
	o, ok := s[id]
	if !ok {
		return harness.Observation{}, errors.New("observation not found")
	}
	return o, nil
}

func run(in io.Reader, out, diagnostics io.Writer) int {
	data, err := io.ReadAll(io.LimitReader(in, maxInputBytes+1))
	if err != nil || len(data) > maxInputBytes {
		fmt.Fprintln(diagnostics, "input unreadable or exceeds 8 MiB")
		return 1
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var input bundle
	if err := dec.Decode(&input); err != nil {
		fmt.Fprintln(diagnostics, "invalid fixture JSON")
		return 1
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		fmt.Fprintln(diagnostics, "expected exactly one JSON object")
		return 1
	}
	if len(input.Observations) > harness.MaxEvidenceRefs {
		fmt.Fprintln(diagnostics, "too many fixture observations")
		return 1
	}
	store := fixtureStore{}
	for _, observation := range input.Observations {
		if observation.ID == "" {
			fmt.Fprintln(diagnostics, "fixture observation needs an ID")
			return 1
		}
		if _, exists := store[observation.ID]; exists {
			fmt.Fprintln(diagnostics, "duplicate fixture observation ID")
			return 1
		}
		store[observation.ID] = observation
	}
	report := harness.NewEvaluator(store, nil).Evaluate(context.Background(), input.Request)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(diagnostics, "cannot write report")
		return 1
	}
	switch report.Disposition {
	case harness.Confirm:
		return 0
	case harness.Retry:
		return 3
	case harness.NotSupported:
		return 4
	default:
		return 2 // REVIEW is intentional and must not look like success.
	}
}

func main() {
	path := flag.String("input", "-", "local fixture JSON path, or - for stdin")
	flag.Parse()
	input := os.Stdin
	if *path != "-" {
		f, err := os.Open(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot open fixture input")
			os.Exit(1)
		}
		defer f.Close()
		input = f
	}
	os.Exit(run(input, os.Stdout, os.Stderr))
}
