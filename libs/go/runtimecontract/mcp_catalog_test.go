package runtimecontract

import (
	"slices"
	"strings"
	"testing"
)

func TestRuntimeWorkflowLaunchCatalogRequiresExactOrdinaryCapability(t *testing.T) {
	input := RunnerInput{Mode: RunnerModeTurn, AssistantScope: AssistantScopeNone, ProjectRef: "prj_fixture", Capabilities: []string{"platform.run.launch"}}
	for name, change := range map[string]func(*RunnerInput){
		"valid":         func(*RunnerInput) {},
		"no-capability": func(i *RunnerInput) { i.Capabilities = nil },
		"delegation-only": func(i *RunnerInput) {
			i.Capabilities = []string{"platform.run.delegate"}
			i.DelegationTargets = []RunnerDelegationTarget{{Ref: "agt_fixture"}}
		},
		"unknown-capability": func(i *RunnerInput) { i.Capabilities = []string{"platform.run.launch.other"} },
		"warm":               func(i *RunnerInput) { i.Mode = RunnerModeWarm },
		"unknown-mode":       func(i *RunnerInput) { i.Mode = "UNKNOWN" },
		"no-project":         func(i *RunnerInput) { i.ProjectRef = "" },
		"system":             func(i *RunnerInput) { i.AssistantScope = AssistantScopeSystem },
		"project-helper": func(i *RunnerInput) {
			i.AssistantScope = AssistantScopeProject
			i.AssistantProfileRef = "asstprof_fixture123"
		},
		"missing-scope": func(i *RunnerInput) { i.AssistantScope = "" },
		"unknown-scope": func(i *RunnerInput) { i.AssistantScope = "UNKNOWN" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := input
			change(&copy)
			if RuntimeWorkflowLaunchAvailable(copy) != (name == "valid") || slices.Contains(RuntimeMCPToolNames(copy), "launch_workflow") != (name == "valid") || slices.Contains(RuntimeMCPToolNames(copy), "get_workflow_catalog") != (name == "valid") {
				t.Fatal("workflow launch catalog expanded or lost exact ordinary authority")
			}
		})
	}
}

func TestRuntimeMCPFilesRequireExactExecutionCatalog(t *testing.T) {
	input := RunnerInput{Mode: RunnerModeTurn, ProjectRef: "prj_fixture", LeaseRef: "lea_fixture", LeaseFence: "fence", LeaseGeneration: 1, FileCatalog: &RuntimeFileCatalog{Ref: "vfc_fixture1", Digest: strings.Repeat("a", 64), Purposes: []string{FilePurposeProject}}}
	for name, change := range map[string]func(*RunnerInput){
		"valid": func(*RunnerInput) {}, "warm": func(i *RunnerInput) { i.Mode = RunnerModeWarm }, "project": func(i *RunnerInput) { i.ProjectRef = "" }, "lease": func(i *RunnerInput) { i.LeaseRef = "" }, "fence": func(i *RunnerInput) { i.LeaseFence = "" }, "generation": func(i *RunnerInput) { i.LeaseGeneration = 0 }, "missing": func(i *RunnerInput) { i.FileCatalog = nil }, "digest": func(i *RunnerInput) { i.FileCatalog.Digest = "invalid" }, "purpose": func(i *RunnerInput) { i.FileCatalog.Purposes = []string{"FOREIGN"} },
	} {
		t.Run(name, func(t *testing.T) {
			copy := input
			catalog := *input.FileCatalog
			copy.FileCatalog = &catalog
			change(&copy)
			tools := RuntimeMCPToolNames(copy)
			for _, tool := range []string{FileToolSearch, FileToolMetadata, FileToolPreview, FileToolManifest, FileToolRead} {
				if slices.Contains(tools, tool) != (name == "valid") {
					t.Fatal("VFS tool profile expanded or lost")
				}
			}
		})
	}
}
