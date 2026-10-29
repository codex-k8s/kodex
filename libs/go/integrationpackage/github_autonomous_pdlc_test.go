package integrationpackage

import "testing"

// TestGitHubAutonomousPDLCRequiresExplicitGrantPolicy permits the closed intermediate write set while preserving human defaults and destructive boundaries.
// TestGitHubAutonomousPDLCRequiresExplicitGrantPolicy разрешает закрытый набор промежуточных записей, сохраняя default согласования и границы разрушительных действий.
func TestGitHubAutonomousPDLCRequiresExplicitGrantPolicy(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	github := definitions["github"]
	for _, key := range []string{"github.issue.create", "github.issue.update", "github.issue.comment.create", "github.issue.comment.update", "github.pull_request.create", "github.pull_request.update", "github.pull_request.review.create", "github.repository.content.create", "github.repository.content.update"} {
		t.Run(key, func(t *testing.T) {
			capability, found := github.Capability(key)
			if !found || !github.CollaborativeWrite(capability) || !capability.AllowsApprovalPolicy("NONE") || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
				t.Fatal("intermediate write requires an explicit autonomous policy with a human default")
			}
			for _, changed := range []string{"adapter", "owner", "route", "scope", "risk", "operation"} {
				candidate, operation := github, capability
				switch changed {
				case "adapter":
					candidate.Spec.Adapter = "OPENAPI_MCP"
				case "owner":
					candidate.Spec.AdapterOwner = "runtime-controller"
				case "route":
					candidate.Spec.ExecutionRoute = "INTERACTION"
				case "scope":
					operation.ResourceScope.Kind = "GLOBAL"
				case "risk":
					operation.Risk = "DESTRUCTIVE"
				case "operation":
					operation.Operation = "github.pull_request.merge"
				}
				if candidate.CollaborativeWrite(operation) {
					t.Fatal("autonomous policy crossed the managed repository boundary")
				}
			}
		})
	}
	for _, key := range []string{"github.repository.bundle.publish", "github.pull_request.merge", "github.branch.delete", "github.repository.content.delete", "github.actions.run.cancel"} {
		capability, found := github.Capability(key)
		if !found || github.CollaborativeWrite(capability) || capability.AllowsApprovalPolicy("NONE") {
			t.Fatal("human-only operation inherited autonomous authority")
		}
	}
}
