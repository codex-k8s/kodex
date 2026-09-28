package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantAgentEnvironmentBindingClosedInput(t *testing.T) {
	t.Parallel()
	operation := entity.AssistantPlanOperation{
		Type: "BIND_AGENT_RUNTIME_ENVIRONMENT", Key: "bind-agent-environment", Title: "Bind environment", Summary: "Bind ready environment",
		Parameters: map[string]any{"agentRef": "agt_exact", "environmentRef": "renv_next"},
	}
	if !assistantOperationMatchesContext("AGENT", "agt_exact", operation) ||
		assistantOperationMatchesContext("AGENT", "agt_other", operation) ||
		assistantOperationMatchesContext("PROJECT", "agt_exact", operation) {
		t.Fatal("binding accepted a foreign context")
	}
	before := map[string]any{
		"agentRef": "agt_exact", "projectRef": "prj_exact", "agentName": "Employee",
		"bindingRef": "bind_exact", "environmentRef": "renv_old", "environmentName": "Old", "versionRef": "renvv_old",
	}
	after := map[string]any{
		"environmentRef": "renv_next", "environmentName": "Next", "versionRef": "renvv_next", "versionDigest": "digest-next",
	}
	hydrated, err := hydrateAssistantBindingFields(before, after, 7, operation)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(hydrated)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.BindAgentRuntimeEnvironment {
		t.Fatalf("binding command: %#v %v", mapped, err)
	}
	payload := mapped.Payload.(command.RuntimeEnvironmentBindingInput)
	if payload.AgentRef != "agt_exact" || payload.EnvironmentRef != "renv_next" || payload.VersionRef != "renvv_next" ||
		mapped.Mutation.ExpectedVersion == nil || *mapped.Mutation.ExpectedVersion != 7 {
		t.Fatalf("binding lost exact pins: %#v", mapped)
	}
	forged := normalized
	forged.Input = cloneAssistantFields(normalized.Input)
	forged.Input["secretValue"] = "forged"
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("secret accepted in binding plan: %v", err)
	}
	forged.Input = cloneAssistantFields(normalized.Input)
	forged.Input["versionRef"] = "renvv_other"
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("forged revision accepted: %v", err)
	}
	forged.Input = cloneAssistantFields(normalized.Input)
	forged.Input["agentRef"] = "agt_other"
	if _, err := assistantOperationCommand(forged); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("forged agent accepted: %v", err)
	}
	if _, err := hydrateAssistantBindingFields(before, map[string]any{
		"environmentRef": "renv_old", "versionRef": "renvv_old",
	}, 7, operation); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("no-op binding accepted: %v", err)
	}
}
