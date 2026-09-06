package main

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestRuntimeFileProfilesRequireExactControllerTuple(t *testing.T) {
	for _, operation := range []string{"search", "metadata", "preview", "manifest"} {
		id := "platform.runtime.files." + operation
		method := controlplaneclient.RuntimeOperations()[id]
		want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
		if method == "" || operationRequestProfile(id, method) != want {
			t.Fatalf("runtime file request profile is invalid: %s", operation)
		}
		for _, profile := range []map[string]string{controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.InteractionGatewayOperations()} {
			if profile[id] != "" {
				t.Fatalf("runtime file grant leaked to another workload: %s", operation)
			}
		}
	}
}
