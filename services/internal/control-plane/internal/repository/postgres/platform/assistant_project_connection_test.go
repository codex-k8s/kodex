package platform

import (
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
)

func TestProjectAssistantConnectionCommandClosedPins(t *testing.T) {
	input := map[string]any{"projectAssistantRef": "agt_fixture12345", "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION", "organizationRef": "org_fixture12345", "projectRef": "prj_fixture12345", "assistantProfileRef": "asstp_fixture12345", "agentVersion": int64(2), "profileVersion": int64(1), "definitionVersion": "2.3.1", "definitionDigest": strings.Repeat("a", 64), "definitionKey": "github", "name": "Own repository", "publicConfiguration": map[string]any{"owner": "fixture", "repository": "repository"}, "expectedVersion": int64(2)}
	op := entity.AssistantPlanOperation{Type: prepareProjectAssistantConnection, Input: input}
	mapped, err := assistantOperationCommand(op)
	if err != nil || mapped.Kind != command.CreateProjectAssistantIntegrationConnection {
		t.Fatalf("closed mapper: %v", err)
	}
	for _, field := range []string{"projectRef", "organizationRef", "projectAssistantRef", "assistantProfileRef", "definitionDigest", "profileVersion", "agentVersion"} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		delete(bad.Input, field)
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("missing %s accepted", field)
		}
	}
	for _, field := range []string{"ownerID", "credentials", "scope", "agentRef"} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		bad.Input[field] = ""
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("extra %s accepted", field)
		}
	}
	for _, change := range []struct {
		field string
		value any
	}{{"profileVersion", int64(0)}, {"agentVersion", int64(3)}, {"scopeKind", "PROJECT"}, {"assistantScope", "SYSTEM"}, {"publicConfiguration", map[string]any{"token": true}}} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		bad.Input[change.field] = change.value
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("malformed %s accepted", change.field)
		}
	}
}
