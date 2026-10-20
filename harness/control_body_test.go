package harness

import "testing"

func TestControlHTTPBodyHashVerifier(t *testing.T) {
	body := []byte("synthetic mechanical equality")
	request := EvaluationRequest{Claim: "body_sha256:" + Digest(body)}
	observation := Observation{ID: "fixture", Complete: true, Payload: body}
	v, err := ControlHTTPBodySHA256Verifier(t.Context(), request, []Observation{observation})
	if err != nil || v.Validity != ValidityValid || v.Conclusion != Supported || len(v.EvidenceIDs) != 1 {
		t.Fatal(v, err)
	}
	observation.Payload = []byte("different")
	v, err = ControlHTTPBodySHA256Verifier(t.Context(), request, []Observation{observation})
	if err != nil || v.Conclusion != Refuted {
		t.Fatal("scoped valid negative lost", v, err)
	}
	observation.Complete = false
	v, err = ControlHTTPBodySHA256Verifier(t.Context(), request, []Observation{observation})
	if err != nil || v.Conclusion != Inconclusive || v.Validity == ValidityValid {
		t.Fatal("incomplete became negative", v, err)
	}
}
