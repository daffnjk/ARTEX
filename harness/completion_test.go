package harness

import "testing"

func TestRequiresGoalReview(t *testing.T) {
	for _, name := range []string{"prove_goal", "goal_met", "record_fact", "report_finding", ""} {
		for _, mode := range []string{"", "legacy", " LEGACY ", "review", " REVIEW ", "typo"} {
			want := (name == "prove_goal" || name == "goal_met") && (mode == "review" || mode == " REVIEW " || mode == "typo")
			if got := RequiresGoalReview(name, mode); got != want {
				t.Fatalf("%q/%q: want %v got %v", name, mode, want, got)
			}
		}
	}
}

func TestChangeProposalRequiresReviewAndReplay(t *testing.T) {
	valid := ChangeProposal{SchemaVersion: SchemaVersion, ID: "proposal-1", FailureEvaluationIDs: []string{"eval-1"},
		AffectedComponents: []string{"method-card"}, ExpectedBenefit: "avoid invalid controls",
		RegressionRisks: []string{"unnecessary review"}, ReplayReportRefs: []string{"replay-1"}, RequiresHumanApproval: true}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ChangeProposal){
		func(p *ChangeProposal) { p.SchemaVersion = "unknown" },
		func(p *ChangeProposal) { p.ID = "" },
		func(p *ChangeProposal) { p.RequiresHumanApproval = false },
		func(p *ChangeProposal) { p.ReplayReportRefs = nil },
		func(p *ChangeProposal) { p.FailureEvaluationIDs = []string{""} },
	} {
		p := valid
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid proposal accepted")
		}
	}
}
