package report

import (
	"fmt"
	"github.com/Autumn-27/artex/db"
	"strings"
)

func ControlMarkdown(s db.ControlTaskSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Controlled task %d\n\nmode: %s\nphase: %s\nresult: %s\nreason: %s\ncleanup: %s\nlegacy status: %s\n\n", s.TaskID, s.Mode, s.Close.Phase, s.Close.Result, s.Close.Reason, s.Close.Cleanup, s.Close.LegacyStatus)
	fmt.Fprintf(&b, "Valid captured observations: %d\nSupported claims: %d\nUnknown/inconclusive claims: %d\nUnknown effects: %d\nUnknown budget settlements: %d\n\n", s.ValidObservations, s.ValidClaims, s.UnknownClaims, s.UncertainEffects, s.UnknownReservations)
	b.WriteString("## unknown / untested\n\n")
	for _, item := range s.Unknown {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	b.WriteString("\nCleanup covers local adapter resources; unknown remote effects are listed separately. Generated from host records without an LLM. Scope is the approved task and acceptance cells; no global safety or coverage conclusion.\n")
	return b.String()
}
