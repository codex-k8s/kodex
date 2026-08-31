package controlplaneclient

import (
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestRuntimeOperationsRegistersProviderCredentialRefresh(t *testing.T) {
	operations := RuntimeOperations()
	if operations["platform.runtime.provider-credential.refresh.commit"] != controlplanev1.RuntimeWorkService_CommitProviderCredentialRefresh_FullMethodName {
		t.Fatal("provider credential refresh operation is not registered")
	}
}

func TestProviderCredentialMaterializerOperationsAreExact(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"platform.provider-credentials.readiness.check":         controlplanev1.ProviderCredentialMaterializerService_CheckProviderCredentialMaterializerReadiness_FullMethodName,
		"platform.provider-credentials.device-authorize.start":  controlplanev1.ProviderCredentialMaterializerService_StartDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.device-authorize.get":    controlplanev1.ProviderCredentialMaterializerService_ObserveDeviceAuthorization_FullMethodName,
		"platform.provider-credentials.api-key.materialize":     controlplanev1.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName,
		"platform.provider-credentials.materialization.discard": controlplanev1.ProviderCredentialMaterializerService_DiscardProviderCredentialMaterialization_FullMethodName,
		"platform.provider-credentials.cleanup":                 controlplanev1.ProviderCredentialMaterializerService_CleanupProviderCredential_FullMethodName,
	}
	if got := ProviderCredentialMaterializerOperations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("provider credential materializer operations = %#v, want %#v", got, want)
	}
}
