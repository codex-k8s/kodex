package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestSystemAssistantImageSpecPinRequired(t *testing.T) {
	for _, kind := range []string{"CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"} {
		t.Run(kind, func(t *testing.T) {
			input := map[string]any{"systemAssistantRef": "agt_fixture0001", "scopeKind": "ORGANIZATION", "organizationRef": "org_fixture0001", "agentVersion": int64(2), "name": "Pinned image", "environmentKey": "standard", "dockerfile": "FROM registry.invalid/base@sha256:" + strings.Repeat("a", 64) + "\n", "specSha256": strings.Repeat("b", 64)}
			if kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
				input["recipeRef"], input["expectedVersion"] = "imgrec_fixture0001", int64(3)
			}
			operation := entity.AssistantPlanOperation{Type: kind, Input: input}
			result, err := systemAssistantImageCommand(operation)
			if err != nil || result.Payload.(command.SystemAssistantRoleImageInput).SpecSHA256 != input["specSha256"] {
				t.Fatal("valid server-assigned build specification pin was not materialized")
			}
			if result.Payload.(command.SystemAssistantRoleImageInput).Environment.Dockerfile != input["dockerfile"] {
				t.Fatal("command changed bytes of the confirmed Dockerfile")
			}
			for _, value := range []any{nil, "", strings.Repeat("B", 64), strings.Repeat("b", 63), "sha256:" + strings.Repeat("b", 64), " " + strings.Repeat("b", 64), strings.Repeat("b", 64) + "\n", int64(2)} {
				invalid := operation
				invalid.Input = cloneAssistantFields(input)
				invalid.Input["specSha256"] = value
				if _, err := systemAssistantImageCommand(invalid); !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("missing or malformed immutable specification pin was accepted")
				}
			}
			missing := operation
			missing.Input = cloneAssistantFields(input)
			delete(missing.Input, "specSha256")
			if _, err := systemAssistantImageCommand(missing); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("historical plan without specification pin was decoded")
			}
		})
	}
}
