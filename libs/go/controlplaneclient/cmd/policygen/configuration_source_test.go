package main

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestConfigurationSourceProfilesBindExactUnaryOwnerTuple(t *testing.T) {
	for _, operation := range []string{"platform.command.role-image-sources.configure", "platform.command.role-image-sources.refresh", "platform.command.integration-definition-sources.configure", "platform.command.integration-definition-sources.refresh"} {
		method := controlplaneclient.ControlAPIGatewayOperations()[operation]
		want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
		if method == "" || operationRequestProfile(operation, method) != want {
			t.Fatalf("source command profile differs: %s", operation)
		}
	}
	for _, action := range []string{"claim", "renew", "complete", "fail"} {
		operation := "platform.configuration-sources.work." + action
		method := controlplaneclient.IntegrationGatewayOperations()[operation]
		want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
		if method == "" || operationRequestProfile(operation, method) != want {
			t.Fatalf("source work profile differs: %s", operation)
		}
		for _, operations := range []map[string]string{controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.InteractionGatewayOperations(), controlplaneclient.RuntimeOperations()} {
			if operations[operation] != "" {
				t.Fatalf("source work leaked to another caller: %s", operation)
			}
		}
	}
}
