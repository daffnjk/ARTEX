package harness

import (
	"fmt"
	"strings"
)

// ChangeProposal is the slow-loop output, not an executable update. Applying a
// change requires an external review/release process. There is no AutoApply API.
type ChangeProposal struct {
	SchemaVersion         string   `json:"schema_version"`
	ID                    string   `json:"id"`
	FailureEvaluationIDs  []string `json:"failure_evaluation_ids"`
	AffectedComponents    []string `json:"affected_components"`
	ExpectedBenefit       string   `json:"expected_benefit"`
	RegressionRisks       []string `json:"regression_risks"`
	ReplayReportRefs      []string `json:"replay_report_refs"`
	RequiresHumanApproval bool     `json:"requires_human_approval"`
}

func (p ChangeProposal) Validate() error {
	if p.SchemaVersion != SchemaVersion || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.ExpectedBenefit) == "" {
		return fmt.Errorf("invalid proposal identity or benefit")
	}
	if len(p.FailureEvaluationIDs) == 0 || len(p.AffectedComponents) == 0 || len(p.RegressionRisks) == 0 || len(p.ReplayReportRefs) == 0 {
		return fmt.Errorf("proposal requires failure, component, risk and replay references")
	}
	for _, values := range [][]string{p.FailureEvaluationIDs, p.AffectedComponents, p.RegressionRisks, p.ReplayReportRefs} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("empty proposal reference")
			}
		}
	}
	if !p.RequiresHumanApproval {
		return fmt.Errorf("automatic policy changes are not supported")
	}
	return nil
}
