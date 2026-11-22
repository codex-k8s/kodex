package controlplaneclient

import (
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestRuntimeOperationsRegistersProviderCredentialRefresh pins the exact runtime RPC for committing credential refresh.
// TestRuntimeOperationsRegistersProviderCredentialRefresh закрепляет точный runtime RPC фиксации обновления credentials.
func TestRuntimeOperationsRegistersProviderCredentialRefresh(t *testing.T) {
	operations := RuntimeOperations()
	if operations["platform.runtime.provider-credential.refresh.commit"] != controlplanev1.RuntimeWorkService_CommitProviderCredentialRefresh_FullMethodName {
		t.Fatal("provider credential refresh operation is not registered")
	}
}

// TestProviderCredentialMaterializerOperationsAreExact rejects additions or substitutions in the isolated materializer registry.
// TestProviderCredentialMaterializerOperationsAreExact отвергает добавления и подмены в реестре изолированного materializer.
func TestProviderCredentialMaterializerOperationsAreExact(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"platform.provider-accounts.credential.renew":           controlplanev1.ProviderCredentialMaterializerService_RenewManagedProviderCredential_FullMethodName,
		"platform.provider-accounts.credential.renewal.observe": controlplanev1.ProviderCredentialMaterializerService_ObserveManagedProviderCredentialRenewal_FullMethodName,
		"platform.provider-accounts.model-catalog.observe":      controlplanev1.ProviderCredentialMaterializerService_ObserveProviderModelCatalog_FullMethodName,
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

// TestImageSupplyChainWorkerOperationsAreExact keeps controller, admission and promotion RPC sets separate and explicit.
// TestImageSupplyChainWorkerOperationsAreExact сохраняет раздельные точные наборы RPC controller, admission и promotion.
func TestImageSupplyChainWorkerOperationsAreExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{
			name: "controller",
			got:  ImageAdmissionControllerOperations(),
			want: map[string]string{
				"platform.role-images.supply-work.get":                 controlplanev1.RoleImageService_GetImageSupplyWorkAvailability_FullMethodName,
				"platform.role-images.admission.recovery-terminal.get": controlplanev1.RoleImageService_GetImageAdmissionRecoveryTerminal_FullMethodName,
			},
		},
		{
			name: "admission",
			got:  ImageAdmissionOperations(),
			want: map[string]string{
				"platform.role-images.admission.claim":        controlplanev1.RoleImageService_ClaimImageAdmission_FullMethodName,
				"platform.role-images.admission.record":       controlplanev1.RoleImageService_RecordImageAdmission_FullMethodName,
				"platform.role-images.admission.fail":         controlplanev1.RoleImageService_FailImageAdmission_FullMethodName,
				"platform.role-images.admission.expire":       controlplanev1.RoleImageService_ExpireImageAdmissionClaim_FullMethodName,
				"platform.role-images.admission.terminal.get": controlplanev1.RoleImageService_GetImageAdmissionTerminal_FullMethodName,
			},
		},
		{
			name: "promotion",
			got:  ImagePromotionOperations(),
			want: map[string]string{
				"platform.role-images.promotion.claim":     controlplanev1.RoleImageService_ClaimImagePromotion_FullMethodName,
				"platform.role-images.promotion.authorize": controlplanev1.RoleImageService_AuthorizeImagePromotion_FullMethodName,
				"platform.role-images.promotion.complete":  controlplanev1.RoleImageService_CompleteImagePromotion_FullMethodName,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("image supply-chain operations = %#v, want %#v", test.got, test.want)
			}
		})
	}
}

// TestManagedKubernetesPurposeOperationsStayDisjoint materializes the exact broker/gateway profiles.
// Existing runtime Claim remains the sole registered worker claim, while owner result readback is private.
// TestManagedKubernetesPurposeOperationsStayDisjoint материализует exact broker/gateway profiles.
// Existing runtime Claim остаётся единственным registered worker claim; owner result readback приватен.
func TestManagedKubernetesPurposeOperationsStayDisjoint(t *testing.T) {
	broker, gateway := SecretBrokerOperations(), IntegrationGatewayOperations()
	expectedBroker := map[string]string{
		"platform.runtime.kubernetes.resolve":          controlplanev1.ManagedKubernetesOwnerService_ResolveManagedKubernetesOperation_FullMethodName,
		"platform.runtime.kubernetes.children.prepare": controlplanev1.ManagedKubernetesOwnerService_PrepareManagedKubernetesChildren_FullMethodName,
		"platform.runtime.kubernetes.effect.begin":     controlplanev1.ManagedKubernetesOwnerService_BeginManagedKubernetesEffect_FullMethodName,
		"platform.runtime.kubernetes.effect.complete":  controlplanev1.ManagedKubernetesOwnerService_CompleteManagedKubernetesEffect_FullMethodName,
		"platform.runtime.kubernetes.stream.open":      controlplanev1.ManagedKubernetesOwnerService_OpenManagedKubernetesStream_FullMethodName,
		"platform.runtime.kubernetes.stream.close":     controlplanev1.ManagedKubernetesOwnerService_CloseManagedKubernetesStream_FullMethodName,
		"platform.runtime.kubernetes.frame.begin":      controlplanev1.ManagedKubernetesOwnerService_BeginManagedKubernetesFrame_FullMethodName,
		"platform.runtime.kubernetes.frame.complete":   controlplanev1.ManagedKubernetesOwnerService_CompleteManagedKubernetesFrame_FullMethodName,
	}
	for operation, method := range expectedBroker {
		if broker[operation] != method || gateway[operation] != "" {
			t.Fatal("broker graph operation missing or widened to gateway")
		}
	}
	for operation, method := range map[string]string{
		"platform.runtime.kubernetes.renew":       controlplanev1.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName,
		"platform.runtime.kubernetes.complete":    controlplanev1.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName,
		"platform.runtime.kubernetes.read":        controlplanev1.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName,
		"platform.runtime.kubernetes.input.claim": controlplanev1.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName,
	} {
		if gateway[operation] != method || broker[operation] != "" {
			t.Fatal("gateway finalization operation missing or widened to broker")
		}
	}
	for operation, method := range gateway {
		if method == controlplanev1.ManagedKubernetesOwnerService_ClaimManagedKubernetesInvocations_FullMethodName {
			t.Fatal("competing Kubernetes worker claim registered", operation)
		}
	}
	if gateway["platform.runtime.integrations.claim"] != controlplanev1.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
		t.Fatal("single runtime claim profile lost")
	}
}
