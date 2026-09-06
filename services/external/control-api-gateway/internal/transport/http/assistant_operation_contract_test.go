package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func TestAssistantUpdateProjectOperationContract(t *testing.T) {
	if !generated.AssistantPlanOperationType("UPDATE_PROJECT").Valid() || !generated.AssistantContextDescriptorAllowedOperations("UPDATE_PROJECT").Valid() {
		t.Fatal("existing owner operation missing from HTTP contract")
	}
	if generated.AssistantPlanOperationType("FUTURE_OPERATION").Valid() || generated.AssistantContextDescriptorAllowedOperations("FUTURE_OPERATION").Valid() {
		t.Fatal("unknown operation accepted by closed HTTP contract")
	}
	input := assistantContextInput(&generated.AssistantContextDescriptor{Route: "/projects/prj_fixture01", EntityKind: "PROJECT", EntityRef: "prj_fixture01", AllowedOperations: []generated.AssistantContextDescriptorAllowedOperations{"UPDATE_PROJECT"}})
	if len(input.AllowedOperations) != 1 || input.AllowedOperations[0] != cp.AssistantPlanOperation_TYPE_UPDATE_PROJECT {
		t.Fatal("operation changed before owner resolution")
	}
	value, err := messageMap(&cp.AssistantConversation{Context: input})
	if err != nil {
		t.Fatal(err)
	}
	operations := value["context"].(map[string]any)["allowedOperations"].([]any)
	if len(operations) != 1 || operations[0] != "UPDATE_PROJECT" {
		t.Fatal("owner operation missing from context readback")
	}
	operation, err := messageMap(&cp.AssistantPlanOperation{Type: cp.AssistantPlanOperation_TYPE_UPDATE_PROJECT})
	if err != nil || operation["type"] != "UPDATE_PROJECT" {
		t.Fatal("owner operation missing from plan readback")
	}
}
