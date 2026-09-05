package app

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestIdentityEnvironmentSecretExactAuthorityOperations(t *testing.T) {
	for operation, method := range map[string]string{
		"platform.assistant.conversations.archive":         controlplanev1.SystemAssistantService_ArchiveAssistantConversation_FullMethodName,
		"platform.query.managed-configurations.impact.get": controlplanev1.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName,
		"platform.query.interaction-identities.list":       controlplanev1.PlatformQueryService_ListInteractionIdentities_FullMethodName,
		"platform.command.interaction-identities.bind":     controlplanev1.PlatformCommandService_BindInteractionIdentity_FullMethodName,
		"platform.command.interaction-identities.revoke":   controlplanev1.PlatformCommandService_RevokeInteractionIdentity_FullMethodName,
		"platform.query.runtime-environments.impact":       controlplanev1.PlatformQueryService_GetRuntimeEnvironmentImpact_FullMethodName,
		"platform.command.runtime-environments.rebind":     controlplanev1.PlatformCommandService_RebindRuntimeEnvironment_FullMethodName,
		"platform.query.runtime-secrets.impact":            controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName,
		"platform.command.runtime-secrets.rebind":          controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName,
	} {
		if authorityProofOperations()[operation] != method || controlplaneclient.ControlAPIGatewayOperations()[operation] != method {
			t.Fatalf("exact CP method missing for %s", operation)
		}
		if _, required := authorityProjectRequiredOperations()[operation]; required {
			t.Fatalf("opaque resource operation %s incorrectly trusts a browser project locator", operation)
		}
	}
}

func TestAuthorityProofProfileIncludesDirectOrganizationScopedSTT(t *testing.T) {
	const operation = "platform.stt.transcribe"
	sttOperations := controlplaneclient.STTGatewayOperations()
	proofOperations := authorityProofOperations()
	if proofOperations[operation] != sttOperations[operation] {
		t.Fatal("STT proof operation is not registered")
	}
	if _, required := authorityProjectRequiredOperations()[operation]; required {
		t.Fatal("organization STT requires an unrelated project")
	}
	if _, routedToControlPlane := controlplaneclient.ControlAPIGatewayOperations()[operation]; routedToControlPlane {
		t.Fatal("direct STT operation must not be routed to control-plane")
	}
}
