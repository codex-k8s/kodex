package runtimecontract

import (
	"slices"
	"strings"
	"testing"
)

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
			for _, tool := range []string{FileToolSearch, FileToolMetadata, FileToolPreview, FileToolManifest} {
				if slices.Contains(tools, tool) != (name == "valid") {
					t.Fatal("VFS tool profile expanded or lost")
				}
			}
		})
	}
}
