package app

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

// TestIdentityEnvironmentSecretExactAuthorityOperations pins selected identity/environment/configuration/mail/conversation methods in both routing/proof registries and checks their opaque resource scope does not require a browser project locator. It invokes no RPC.
// TestIdentityEnvironmentSecretExactAuthorityOperations закрепляет выбранные identity, environment, configuration, mail и conversation methods в routing и proof registries и проверяет отсутствие требования browser project locator для opaque resource scope. RPC не вызывается.
func TestIdentityEnvironmentSecretExactAuthorityOperations(t *testing.T) {
	for operation, method := range map[string]string{
		"platform.query.projects.purge-recovery.get":          controlplanev1.PlatformQueryService_GetProjectPurgeRecovery_FullMethodName,
		"platform.command.projects.purge-recovery.resolve":    controlplanev1.PlatformCommandService_ResolveProjectPurgeRecovery_FullMethodName,
		"platform.command.role-images.copy":                   controlplanev1.PlatformCommandService_CopyRoleImageConfiguration_FullMethodName,
		"platform.command.integration-definitions.copy":       controlplanev1.PlatformCommandService_CopyIntegrationDefinitionConfiguration_FullMethodName,
		"platform.command.role-images.archive-configuration":  controlplanev1.PlatformCommandService_ArchiveRoleImageConfiguration_FullMethodName,
		"platform.command.integration-definitions.archive":    controlplanev1.PlatformCommandService_ArchiveIntegrationDefinitionConfiguration_FullMethodName,
		"platform.command.email-mailbox.configure-credential": controlplanev1.PlatformCommandService_ConfigureEmailMailboxCredential_FullMethodName,
		"platform.assistant.conversations.archive":            controlplanev1.SystemAssistantService_ArchiveAssistantConversation_FullMethodName,
		"platform.assistant.conversations.restore":            controlplanev1.SystemAssistantService_RestoreAssistantConversation_FullMethodName,
		"platform.assistant.conversations.purge":              controlplanev1.SystemAssistantService_PurgeAssistantConversation_FullMethodName,
		"platform.assistant.conversations.project.move":       controlplanev1.SystemAssistantService_MoveAssistantConversationToProject_FullMethodName,
		"platform.query.managed-configurations.impact.get":    controlplanev1.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName,
		"platform.query.interaction-identities.list":          controlplanev1.PlatformQueryService_ListInteractionIdentities_FullMethodName,
		"platform.command.interaction-identities.bind":        controlplanev1.PlatformCommandService_BindInteractionIdentity_FullMethodName,
		"platform.command.interaction-identities.revoke":      controlplanev1.PlatformCommandService_RevokeInteractionIdentity_FullMethodName,
		"platform.query.runtime-environments.impact":          controlplanev1.PlatformQueryService_GetRuntimeEnvironmentImpact_FullMethodName,
		"platform.command.runtime-environments.rebind":        controlplanev1.PlatformCommandService_RebindRuntimeEnvironment_FullMethodName,
		"platform.query.runtime-secrets.impact":               controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName,
		"platform.command.runtime-secrets.rebind":             controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName,
	} {
		if authorityProofOperations()[operation] != method || controlplaneclient.ControlAPIGatewayOperations()[operation] != method {
			t.Fatalf("exact CP method missing for %s", operation)
		}
		if _, required := authorityProjectRequiredOperations()[operation]; required {
			t.Fatalf("opaque resource operation %s incorrectly trusts a browser project locator", operation)
		}
	}
}

// TestAuthorityProofProfileIncludesDirectOrganizationScopedSTT requires direct speech proof registration without unrelated project scope or control-plane routing, using only local operation maps.
// TestAuthorityProofProfileIncludesDirectOrganizationScopedSTT требует регистрации direct speech proof без несвязанного project scope и control-plane routing, используя лишь локальные operation maps.
func TestAuthorityProofProfileIncludesDirectOrganizationScopedSTT(t *testing.T) {
	// operation pins the direct organization-scoped speech command checked across independent routing maps.
	// operation закрепляет прямую organization-scoped speech command для сверки независимых routing maps.
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

// TestSecretDraftProofUsesDedicatedBrokerTarget checks every secret-draft operation appears in proof registration and is absent from control-plane routing and caller-project-required scope; it does not exercise a broker connection.
// TestSecretDraftProofUsesDedicatedBrokerTarget проверяет наличие каждой secret-draft operation в proof registration и отсутствие в control-plane routing и caller-project-required scope без подключения к broker.
func TestSecretDraftProofUsesDedicatedBrokerTarget(t *testing.T) {
	for operation, method := range controlplaneclient.SecretDraftGatewayOperations() {
		if authorityProofOperations()[operation] != method {
			t.Fatal("draft broker proof operation is missing")
		}
		if _, routed := controlplaneclient.ControlAPIGatewayOperations()[operation]; routed {
			t.Fatal("broker operation routed to control-plane")
		}
		if _, required := authorityProjectRequiredOperations()[operation]; required {
			t.Fatal("draft broker operation trusts caller project")
		}
	}
}
