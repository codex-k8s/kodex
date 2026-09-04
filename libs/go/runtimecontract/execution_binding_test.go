package runtimecontract

import "testing"

func TestExecutionBindingAllowsOnlySystemAssistantWithoutProject(t *testing.T) {
	input := validRunnerInputFixture()
	input.ProjectRef = ""
	input.SystemAssistant = true
	execution, mcp, err := RuntimeExecutionBindingDigests(input)
	if err != nil || execution == "" || mcp == "" {
		t.Fatalf("global assistant binding rejected: %v", err)
	}
	input.ProjectRef = "prj_abcdefgh"
	projectExecution, projectMCP, err := RuntimeExecutionBindingDigests(input)
	if err != nil || execution == projectExecution || mcp == projectMCP {
		t.Fatal("project and global assistant bindings were not isolated")
	}
	input.ProjectRef = ""
	input.SystemAssistant = false
	if _, _, err := RuntimeExecutionBindingDigests(input); err == nil {
		t.Fatal("ordinary execution accepted without project")
	}
}
