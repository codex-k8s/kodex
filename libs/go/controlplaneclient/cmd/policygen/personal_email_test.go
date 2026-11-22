package main

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

// TestPersonalEmailProfilesPinOnlyOwnerSetup binds all six typed mailbox methods to the browser gateway and keeps worker maps and unrelated project authority closed.
// TestPersonalEmailProfilesPinOnlyOwnerSetup связывает все шесть typed mailbox методов только с browser gateway, сохраняя закрытыми worker maps и несвязанные project authority.
func TestPersonalEmailProfilesPinOnlyOwnerSetup(t *testing.T) {
	methods := map[string]string{
		"receipts.read":       cp.PersonalEmailMailboxService_GetPersonalEmailMailboxMutationReceipt_FullMethodName,
		"mine.create":         cp.PersonalEmailMailboxService_CreatePersonalEmailMailboxConnection_FullMethodName,
		"mine.list":           cp.PersonalEmailMailboxService_ListMyEmailMailboxes_FullMethodName,
		"ownership.read":      cp.PersonalEmailMailboxService_GetEmailMailboxOwnership_FullMethodName,
		"ownership.configure": cp.PersonalEmailMailboxService_ConfigureEmailMailboxOwnership_FullMethodName,
		"ownership.revoke":    cp.PersonalEmailMailboxService_RevokeEmailMailboxOwnership_FullMethodName,
	}
	profiles := map[string]requestProfile{
		"receipts.read":       {Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
		"mine.create":         {Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"mine.list":           {Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
		"ownership.read":      {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
		"ownership.configure": {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"ownership.revoke":    {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
	}
	descriptor := cp.PersonalEmailMailboxService_ServiceDesc
	if len(descriptor.Methods) != len(methods) || len(descriptor.Streams) != 0 {
		t.Fatal("mailbox owner contract changed without exact operation coverage")
	}
	for _, declared := range descriptor.Methods {
		fullMethod := "/" + descriptor.ServiceName + "/" + declared.MethodName
		found := false
		for _, registered := range methods {
			if registered == fullMethod {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("mailbox owner RPC is missing from the closed operation registry")
		}
	}
	for suffix, method := range methods {
		operation := "platform.email-mailboxes." + suffix
		if controlplaneclient.ControlAPIGatewayOperations()[operation] != method || operationRequestProfile(operation, method) != profiles[suffix] || permissionForOperation(operation) != operation {
			t.Fatalf("mailbox owner operation profile rejected: %s", operation)
		}
		if _, required := controlplaneclient.ControlAPIGatewayProjectRequiredOperations()[operation]; required {
			t.Fatal("mailbox owner setup requires unrelated project authority")
		}
		for _, operations := range []map[string]string{controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.RuntimeOperations(), controlplaneclient.InteractionGatewayOperations(), controlplaneclient.SecretBrokerOperations()} {
			if operations[operation] != "" {
				t.Fatal("worker received mailbox owner setup authority")
			}
		}
	}
}
