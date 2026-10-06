package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestProjectAssistantIntegrationGrantClosedCommand(t *testing.T) {
	params := map[string]any{"projectAssistantRef": "agt_owned12345", "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION", "organizationRef": "org_owned12345", "projectRef": "prj_owned12345", "assistantProfileRef": "asstp_owned12345", "agentVersion": int64(2), "profileVersion": int64(1), "connectionRef": "int_owned12345", "capabilityKey": "github.repository.read", "enabled": true, "approvalPolicy": "NONE", "approvalScopePaths": []string{}, "definitionVersion": "2.3.1", "definitionDigest": strings.Repeat("a", 64), "grantRef": "", "grantVersion": int64(0), "defaultApprovalPolicy": "NONE", "allowedApprovalPolicies": []string{"NONE"}, "expectedVersion": int64(4)}
	op := entity.AssistantPlanOperation{Type: changeProjectAssistantIntegrationGrant, Input: params}
	result, err := assistantOperationCommand(op)
	if err != nil || result.Kind != command.ChangeProjectAssistantIntegrationGrant {
		t.Fatalf("closed command: %v", err)
	}
	for key := range params {
		bad := op
		bad.Input = cloneAssistantFields(params)
		delete(bad.Input, key)
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("missing%s accepted", key)
		}
	}
	for _, test := range []struct {
		key   string
		value any
	}{{"assistantScope", "SYSTEM"}, {"scopeKind", "PROJECT"}, {"projectRef", ""}, {"assistantProfileRef", ""}, {"agentVersion", int64(0)}, {"profileVersion", int64(0)}, {"organizationRef", ""}, {"expectedVersion", int64(0)}, {"actorRef", "caller"}, {"credentials", "caller"}, {"systemAssistantRef", "caller"}} {
		bad := op
		bad.Input = cloneAssistantFields(params)
		bad.Input[test.key] = test.value
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("malformed%s accepted", test.key)
		}
	}
	carried := carryAssistantGrantConnectionVersion(entity.AssistantPlanOperation{Type: changeProjectAssistantIntegrationGrant, Input: params, Parameters: params, Target: entity.AssistantPlanTarget{Kind: "INTEGRATION_CONNECTION", Ref: "int_owned12345"}}, 5)
	if mustAssistantInt64(carried.Input, "expectedVersion") != 5 || params["expectedVersion"] != int64(4) || carried.Input["projectAssistantRef"] != params["projectAssistantRef"] {
		t.Fatal("exact batchcarry changed owner/source")
	}
}
