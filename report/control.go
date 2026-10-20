package report

import (
	"fmt"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/harness"
	"net/url"
	"strings"
)

func ControlMarkdown(s db.ControlTaskSummary, packets ...*db.ReviewPacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Controlled task %d\n\nmode: %s\nphase: %s\nresult: %s\nreason: %s\ncleanup: %s\nlegacy status: %s\n\n", s.TaskID, s.Mode, s.Close.Phase, s.Close.Result, s.Close.Reason, s.Close.Cleanup, s.Close.LegacyStatus)
	fmt.Fprintf(&b, "Valid captured observations: %d\nSupported claims: %d\nUnknown/inconclusive claims: %d\nUnknown effects: %d\nUnknown budget settlements: %d\n\n", s.ValidObservations, s.ValidClaims, s.UnknownClaims, s.UncertainEffects, s.UnknownReservations)
	b.WriteString("## unknown / untested\n\n")
	for _, item := range s.Unknown {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	if len(packets) > 0 && packets[0] != nil && packets[0].Control != nil {
		c := packets[0].Control
		b.WriteString("\n## Evidence-indexed conclusions\n\n")
		for _, claim := range c.Claims {
			fmt.Fprintf(&b, "- claim %d: %s; validity=%s; criterion=%s/%s; context=%s\n", claim.ID, claim.Conclusion, claim.Validity, claim.CriterionID, claim.CriterionVersion, claim.ContextVersion)
			for _, ref := range claim.Evidence {
				fmt.Fprintf(&b, "  - [captured bytes %s](/api/tasks/%d/control/observations/%s) sha256=%s\n", ref.ObservationID, s.TaskID, url.PathEscape(ref.ObservationID), ref.SHA256)
			}
		}
		b.WriteString("\n## Raw captures and counterevidence\n\n")
		for _, o := range c.Observations {
			fmt.Fprintf(&b, "- [%s](/api/tasks/%d/control/observations/%s): validity=%s; complete=%t; attempt=%s; sha256=%s\n", o.Observation.ID, s.TaskID, url.PathEscape(o.Observation.ID), o.Validity, o.Observation.Complete, o.AttemptID, harness.Digest(o.Observation.Payload))
		}
		b.WriteString("\nReview suggestions are advisory; byte equality does not establish vulnerability impact. Complex business conclusions require method-specific human review.\n")
	}
	b.WriteString("\nCleanup covers local adapter resources; unknown remote effects are listed separately. Generated from host records without an LLM. Scope is the approved task and acceptance cells; no global safety or coverage conclusion.\n")
	return b.String()
}
