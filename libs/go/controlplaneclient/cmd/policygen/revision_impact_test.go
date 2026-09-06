package main

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestRevisionImpactRequestProfiles(t *testing.T) {
	prepare := operationRequestProfile("platform.command.environment-draft-impact.prepare", cp.PlatformCommandService_PrepareEnvironmentDraftImpact_FullMethodName)
	if prepare != (requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}) {
		t.Fatalf("prepare profile: %#v", prepare)
	}
	get := operationRequestProfile("platform.query.revision-impact-plans.get", cp.PlatformQueryService_GetRevisionImpactPlan_FullMethodName)
	if get != (requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}) {
		t.Fatalf("read profile: %#v", get)
	}
}
