package harness

import (
	"context"
	"encoding/hex"
	"strings"
)

var ControlHTTPBodyCriterion = Criterion{ID: "http.body.sha256", Version: "1"}

// Only byte equality is decided here; vulnerability/impact claims require a separate method.
func ControlHTTPBodySHA256Verifier(ctx context.Context, req EvaluationRequest, observations []Observation) (Verdict, error) {
	v := Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive, Reason: "unsupported or incomplete byte-equality criterion"}
	expected, ok := strings.CutPrefix(req.Claim, "body_sha256:")
	decoded, err := hex.DecodeString(expected)
	if !ok || err != nil || len(decoded) != 32 || strings.ToLower(expected) != expected || len(observations) == 0 {
		return v, nil
	}
	v.Validity = ValidityValid
	v.Conclusion = Supported
	v.Reason = "exact captured response byte digest comparison; no impact inference"
	for _, o := range observations {
		if ctx.Err() != nil {
			return Verdict{}, ctx.Err()
		}
		if !o.Complete {
			return Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive, Reason: "incomplete capture"}, nil
		}
		v.EvidenceIDs = append(v.EvidenceIDs, o.ID)
		if Digest(o.Payload) != expected {
			v.Conclusion = Refuted
		}
	}
	return v, nil
}
