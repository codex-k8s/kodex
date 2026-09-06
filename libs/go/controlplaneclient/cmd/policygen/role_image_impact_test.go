package main

import (
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"testing"
)

func TestRoleImageImpactProfilesBindExactOwnerResource(t *testing.T) {
	for operation, want := range map[string]requestProfile{
		"platform.command.role-image-impact-plans.prepare": {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
		"platform.query.role-image-impact-plans.get":       {Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"},
	} {
		method := controlplaneclient.ControlAPIGatewayOperations()[operation]
		if method == "" {
			t.Fatal("operation registration missing")
		}
		if got := operationRequestProfile(operation, method); got != want {
			t.Fatalf("%s: got=%+v want=%+v", operation, got, want)
		}
	}
}
