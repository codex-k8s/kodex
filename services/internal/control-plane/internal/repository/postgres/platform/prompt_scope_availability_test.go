package platform

import (
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"testing"
)

func TestDeclaredScopeRejectsIncompleteWithoutExecutionError(t *testing.T) {
	for _, code := range []string{"CAPABILITY_REQUIRED", "FILES_REQUIRED", "RUNTIME_CONTEXT_REQUIRED"} {
		result := promptservice.Materialization{Diagnostics: []promptservice.Diagnostic{{Code: code, Severity: "ERROR", VariableName: "run.ref"}}}
		if validatePromptAvailability(result, false) == nil {
			t.Fatalf("published unavailable variable: %s", code)
		}
		if got := validatePromptAvailability(result, true); (got == nil) != (code == "RUNTIME_CONTEXT_REQUIRED") {
			t.Fatalf("continuation deferred scope widened: %s", code)
		}
	}
	if validatePromptAvailability(promptservice.Materialization{}, true) == nil {
		t.Fatal("incomplete context without diagnostic accepted")
	}
	if validatePromptAvailability(promptservice.Materialization{Diagnostics: []promptservice.Diagnostic{{Code: "RUNTIME_CONTEXT_REQUIRED", VariableName: "project.files"}}}, true) == nil {
		t.Fatal("runtime reason widened unavailable file scope")
	}
	if validatePromptAvailability(promptservice.Materialization{Complete: true}, false) != nil {
		t.Fatal("complete scope rejected")
	}
}
