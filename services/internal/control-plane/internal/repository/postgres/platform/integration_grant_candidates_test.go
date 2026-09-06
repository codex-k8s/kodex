package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"testing"
)

func TestIntegrationCandidateContextClosed(t *testing.T) {
	valid := query.IntegrationCandidates{Purpose: "USE", Context: query.IntegrationCandidateContext{ProjectRef: "project_example", RecipientKind: "AGENT", RecipientRef: "agent_example", CapabilityKey: "synthetic.journal.read"}}
	if err := validateIntegrationCandidates("CONNECTION", valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*query.IntegrationCandidates){
		func(q *query.IntegrationCandidates) { q.Purpose = "" },
		func(q *query.IntegrationCandidates) { q.Context.RecipientKind = "WORKFLOW" },
		func(q *query.IntegrationCandidates) { q.Context.RecipientKind = "UNKNOWN" },
		func(q *query.IntegrationCandidates) { q.Context.RecipientRef = "" },
		func(q *query.IntegrationCandidates) { q.Context.ProjectRef = "" },
		func(q *query.IntegrationCandidates) { q.Context.CapabilityKey = "" },
		func(q *query.IntegrationCandidates) { q.Context.WorkflowRef = "workflow_example" },
		func(q *query.IntegrationCandidates) { q.Context.StepKey = "orphan" },
		func(q *query.IntegrationCandidates) { q.Filter.Query = "\x00" },
	} {
		input := valid
		mutate(&input)
		if validateIntegrationCandidates("CONNECTION", input) == nil {
			t.Fatalf("invalid USE accepted: %#v", input)
		}
	}
	if validateIntegrationCandidates("CONNECTION", query.IntegrationCandidates{Purpose: "GRANT"}) != nil {
		t.Fatal("grant requires an existing grant")
	}
}
