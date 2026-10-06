package main

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestWorkflowLaunchClosedControllerProfile(t *testing.T) {
	const operation = "platform.runtime.execution.workflow.launch"
	method := controlplaneclient.RuntimeOperations()[operation]
	if method != controlplanev1.RuntimeWorkService_LaunchWorkflowExecution_FullMethodName {
		t.Fatal("closed Workflow launch operation missing")
	}
	want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
	if operationRequestProfile(operation, method) != want {
		t.Fatal("Workflow launch transport request profile drift")
	}
	for _, profile := range []map[string]string{controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.InteractionGatewayOperations(), controlplaneclient.IntegrationGatewayOperations()} {
		if profile[operation] != "" {
			t.Fatal("ordinary Workflow launch leaked to another workload")
		}
	}
	for _, method := range controlplaneclient.RuntimeOperations() {
		if method == controlplanev1.PlatformCommandService_LaunchRun_FullMethodName {
			t.Fatal("runtime-controller received owner LaunchRun")
		}
	}
}
