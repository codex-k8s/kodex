package main

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

// TestPersonalIntegrationCredentialProfilesBindOnlySelfGateway checks exact request binding
// and rejects exposure through worker operation maps; the owner separately resolves the self actor.
// TestPersonalIntegrationCredentialProfilesBindOnlySelfGateway проверяет exact request binding
// и отсутствие методов в worker maps; владелец отдельно разрешает собственного actor.
func TestPersonalIntegrationCredentialProfilesBindOnlySelfGateway(t *testing.T) {
	profiles := map[string]requestProfile{
		"list":      {Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
		"get":       {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
		"create":    {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"configure": {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"revoke":    {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"test":      {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
	}
	for suffix, expected := range profiles {
		operation := "platform.personal-integrations." + suffix
		method := controlplaneclient.ControlAPIGatewayOperations()[operation]
		if method == "" || operationRequestProfile(operation, method) != expected || permissionForOperation(operation) != operation {
			t.Fatalf("personal operation profile rejected: %s", operation)
		}
		if _, required := controlplaneclient.ControlAPIGatewayProjectRequiredOperations()[operation]; required {
			t.Fatal("personal credential operation requires unrelated project authority")
		}
		for _, operations := range []map[string]string{controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.RuntimeOperations(), controlplaneclient.InteractionGatewayOperations()} {
			if operations[operation] != "" {
				t.Fatal("worker received personal credential management operation")
			}
		}
	}
}

// TestPersonalVerificationWorkProfilesDoNotExposeManagement checks exact worker methods and
// excludes browser/runtime/interaction peers from their claim, admission and completion maps.
// TestPersonalVerificationWorkProfilesDoNotExposeManagement проверяет exact worker methods и
// исключает browser/runtime/interaction peers из claim, admission и completion maps.
func TestPersonalVerificationWorkProfilesDoNotExposeManagement(t *testing.T) {
	for _, op := range []string{"platform.personal-integration-tests.claim", "platform.personal-integration-tests.begin", "platform.personal-integration-tests.complete", "platform.personal-integration-calls.begin"} {
		method := controlplaneclient.IntegrationGatewayOperations()[op]
		expected := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
		if op == "platform.personal-integration-tests.complete" {
			expected.Idempotency = "REQUIRED"
		}
		if method == "" || operationRequestProfile(op, method) != expected || permissionForOperation(op) != op {
			t.Fatal("personal worker proof profile mismatch")
		}
		for _, operations := range []map[string]string{controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.RuntimeOperations(), controlplaneclient.InteractionGatewayOperations()} {
			if operations[op] != "" {
				t.Fatal("personal worker operation escaped its workload allowlist")
			}
		}
	}
}

// TestIntegrationCredentialPolicyUsesIndependentConfigurationProof prevents self key operations
// or workers from acquiring common Connection configuration authority through the new RPC.
// TestIntegrationCredentialPolicyUsesIndependentConfigurationProof не позволяет операциям личного
// ключа либо workers получить полномочия общей настройки Connection через новый RPC.
func TestIntegrationCredentialPolicyUsesIndependentConfigurationProof(t *testing.T) {
	const operation = "platform.integrations.credential-policy.set"
	method := controlplaneclient.ControlAPIGatewayOperations()[operation]
	expected := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	if method == "" || operationRequestProfile(operation, method) != expected || permissionForOperation(operation) != operation {
		t.Fatal("credential policy operation lost independent exact proof")
	}
	if _, required := controlplaneclient.ControlAPIGatewayProjectRequiredOperations()[operation]; required {
		t.Fatal("organization Connection policy requires unrelated project authority")
	}
	for _, operations := range []map[string]string{controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.RuntimeOperations(), controlplaneclient.InteractionGatewayOperations()} {
		if operations[operation] != "" {
			t.Fatal("worker received credential policy management authority")
		}
	}
}
