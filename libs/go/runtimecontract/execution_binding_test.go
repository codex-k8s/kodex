package runtimecontract

import "testing"

// TestExecutionBindingAllowsOnlySystemAssistantWithoutProject permits projectless system scope, separates its digests from project scope and rejects an ordinary projectless execution.
// TestExecutionBindingAllowsOnlySystemAssistantWithoutProject разрешает системную область без проекта, отделяет её дайджесты от проектных и отклоняет обычное исполнение без проекта.
func TestExecutionBindingAllowsOnlySystemAssistantWithoutProject(t *testing.T) {
	input := validRunnerInputFixture()
	input.ProjectRef = ""
	input.AssistantScope = AssistantScopeSystem
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
	input.AssistantScope = AssistantScopeNone
	if _, _, err := RuntimeExecutionBindingDigests(input); err == nil {
		t.Fatal("ordinary execution accepted without project")
	}
}
