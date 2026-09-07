package main

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestRuntimeProjectionProjectScopeIsOperationSpecific(t *testing.T) {
	operations := controlplaneclient.RuntimeCredentialProjectionOperations()
	profile := delegatedTargetedWorker(
		"runtime-controller", "control-plane.runtime-controller", operations,
		"secret-broker", "spiffe://kodex.local/ns/kodex-system/sa/secret-broker",
		"urn:kodex:internal-rpc:secret-broker", "secret-broker.kodex-system.svc.cluster.local",
	)
	for operation := range operations {
		_, required := profile.ProjectRequired[operation]
		want := operation == "platform.runtime.credentials.materialize"
		if required != want {
			t.Fatalf("operation %s: project required=%t, want %t", operation, required, want)
		}
	}
	if len(profile.ProjectRequired) != 1 {
		t.Fatal("runtime projection profile permits an unexpected project scope")
	}
}
