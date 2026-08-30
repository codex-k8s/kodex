package controlplaneclient

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestRuntimeOperationsRegistersProviderCredentialRefresh(t *testing.T) {
	operations := RuntimeOperations()
	if operations["platform.runtime.provider-credential.refresh.commit"] != controlplanev1.RuntimeWorkService_CommitProviderCredentialRefresh_FullMethodName {
		t.Fatal("provider credential refresh operation is not registered")
	}
}
