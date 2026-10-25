package runtimecontract

import (
	"slices"
	"testing"
)

// TestRepositoryBundleCatalogRequiresExactProjectGrant rejects missing, unrelated, read-only, assistant and non-turn profiles without broadening the generic integration catalog.
// TestRepositoryBundleCatalogRequiresExactProjectGrant отклоняет отсутствующие, посторонние, READ, вспомогательные и неходовые профили без расширения общего каталога интеграций.
func TestRepositoryBundleCatalogRequiresExactProjectGrant(t *testing.T) {
	input := RunnerInput{Mode: RunnerModeTurn, AssistantScope: AssistantScopeNone, ProjectRef: "prj_fixture", IntegrationGrants: []RunnerIntegrationGrant{{CapabilityKey: RepositoryBundlePublishOperation, Operation: RepositoryBundlePublishOperation, Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT"}}}
	for name, change := range map[string]func(*RunnerInput){
		"valid":             func(*RunnerInput) {},
		"no-project":        func(i *RunnerInput) { i.ProjectRef = "" },
		"no-grant":          func(i *RunnerInput) { i.IntegrationGrants = nil },
		"wrong-capability":  func(i *RunnerInput) { i.IntegrationGrants[0].CapabilityKey = "github.repository.content.create" },
		"wrong-operation":   func(i *RunnerInput) { i.IntegrationGrants[0].Operation = "github.repository.content.create" },
		"read":              func(i *RunnerInput) { i.IntegrationGrants[0].Risk = "READ" },
		"no-gate":           func(i *RunnerInput) { i.IntegrationGrants[0].ApprovalPolicy = "NONE" },
		"scoped-gate":       func(i *RunnerInput) { i.IntegrationGrants[0].ApprovalPolicy = "HUMAN_SCOPED" },
		"warm":              func(i *RunnerInput) { i.Mode = RunnerModeWarm },
		"unknown-mode":      func(i *RunnerInput) { i.Mode = "UNKNOWN" },
		"system-assistant":  func(i *RunnerInput) { i.AssistantScope = AssistantScopeSystem },
		"project-assistant": func(i *RunnerInput) { i.AssistantScope = AssistantScopeProject },
		"missing-scope":     func(i *RunnerInput) { i.AssistantScope = "" },
		"unknown-scope":     func(i *RunnerInput) { i.AssistantScope = "UNKNOWN" },
	} {
		t.Run(name, func(t *testing.T) {
			selected := input
			selected.IntegrationGrants = slices.Clone(input.IntegrationGrants)
			change(&selected)
			want := name == "valid"
			if RuntimeRepositoryBundleAvailable(selected) != want || slices.Contains(RuntimeMCPToolNames(selected), RepositoryBundlePrepareTool) != want {
				t.Fatal("repository bundle catalog lost its exact project grant boundary")
			}
		})
	}
}
