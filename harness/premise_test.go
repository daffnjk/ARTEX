package harness

import (
	"context"
	"strings"
	"testing"
)

// M2.2 前提标记验收器（stdlib 单测）：全含→supported、全无→refuted、混合→
// 不裁决；空 marker 拒绝构造；空观察报错。
func TestPremiseMarkerVerifier(t *testing.T) {
	v, err := NewPremiseMarkerVerifier("MARK-42")
	if err != nil {
		t.Fatal(err)
	}
	obs := func(ss ...string) []Observation {
		out := make([]Observation, 0, len(ss))
		for i, s := range ss {
			out = append(out, Observation{ID: string(rune('a' + i)), Payload: []byte(s)})
		}
		return out
	}
	verd, err := v(context.Background(), EvaluationRequest{}, obs("xMARK-42y", "MARK-42"))
	if err != nil || verd.Conclusion != Supported || verd.Validity != ValidityValid {
		t.Fatalf("all-contain must be supported: %+v %v", verd, err)
	}
	verd, err = v(context.Background(), EvaluationRequest{}, obs("plain", "other"))
	if err != nil || verd.Conclusion != Refuted {
		t.Fatalf("none-contain must be refuted: %+v %v", verd, err)
	}
	if !strings.Contains(verd.Reason, "this premise only") {
		t.Fatalf("refuted reason must bound the claim to this premise: %q", verd.Reason)
	}
	verd, err = v(context.Background(), EvaluationRequest{}, obs("has MARK-42", "none"))
	if err != nil || verd.Conclusion != Inconclusive || verd.Validity != ValidityUnknown {
		t.Fatalf("mixed must be undecided: %+v %v", verd, err)
	}
	if _, err := NewPremiseMarkerVerifier("  "); err == nil {
		t.Fatal("empty marker must be rejected at construction")
	}
	if _, err := v(context.Background(), EvaluationRequest{}, nil); err == nil {
		t.Fatal("no observations must be an error")
	}
}
