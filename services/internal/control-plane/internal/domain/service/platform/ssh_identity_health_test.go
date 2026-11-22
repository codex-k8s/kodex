package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestSSHIdentityHealthKeepsPurposeAndReadBoundary verifies both SSH health operations use the shared metadata-only verification lifecycle without granting arbitrary execution or managed writes.
// TestSSHIdentityHealthKeepsPurposeAndReadBoundary проверяет оба SSH health operation через общий metadata-only verification lifecycle без выдачи arbitrary execution либо managed writes.
func TestSSHIdentityHealthKeepsPurposeAndReadBoundary(t *testing.T) {
	for _, health := range []string{"ssh.target.inspect", "ssh.identity.inspect"} {
		t.Run(health, func(t *testing.T) {
			requirement, initiator, binding, now := personalBrokerIdentityFixture()
			requirement.BrokerPurpose = entity.IntegrationBrokerIdentitySSH
			requirement.CapabilityKey = health
			binding.CredentialPolicy = entity.IntegrationCredentialInitiatorRequired
			binding.ConnectionVersion = 7
			binding.BrokerPurpose = entity.IntegrationBrokerIdentitySSH
			binding.BrokerIdentity.Purpose = entity.IntegrationBrokerIdentitySSH
			spec := entity.PersonalIntegrationCredentialTestSpec{Ref: "pct_ssh_fixture", ConnectionVersion: binding.ConnectionVersion,
				DefinitionDigest: requirement.DefinitionDigest, ConfigurationDigest: requirement.ConfigurationDigest,
				HealthCapabilityKey: health, AllowedReadCapabilityKeys: []string{health}, BrokerPurpose: requirement.BrokerPurpose, BrokerTargetRef: requirement.BrokerTargetRef}
			checking, snapshot, err := BeginPersonalIntegrationBrokerIdentityVerification(binding, initiator, binding.Version, spec)
			if err != nil || snapshot.BrokerPurpose != entity.IntegrationBrokerIdentitySSH || snapshot.BrokerIdentityRef != binding.BrokerIdentity.Ref || snapshot.CredentialRef != "" {
				t.Fatal("SSH health lost purpose or became TOKEN", err)
			}
			ready, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest,
				entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{health}})
			if err != nil || ready.State != "READY" || ready.Credential != nil || ready.VerifiedBrokerIdentityRef != binding.BrokerIdentity.Ref {
				t.Fatal("SSH health failed metadata-only completion", err)
			}
			selected, err := ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
			if err != nil || selected.Problem != "" || selected.BrokerIdentity == nil || selected.Credential != nil {
				t.Fatal("current SSH health was unavailable", err)
			}
			for _, capability := range []string{"ssh.execute", "ssh.file.create", "ssh.file.replace", "ssh.service.restart"} {
				write := requirement
				write.CapabilityKey = capability
				selected, err := ResolveIntegrationCredential(write, initiator, nil, &ready, now)
				if err != nil || selected.Problem != entity.IntegrationCredentialUnconfirmed || selected.BrokerIdentity != nil || selected.Credential != nil {
					t.Fatal("health fabricated unobserved effect permission", err)
				}
			}
			wrong := snapshot
			wrong.BrokerPurpose = entity.IntegrationBrokerIdentityKubernetes
			if _, err := CompletePersonalIntegrationCredentialVerification(checking, wrong, requirement.DefinitionDigest, requirement.ConfigurationDigest,
				entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{health}}); err == nil {
				t.Fatal("foreign broker purpose completed SSH health")
			}
		})
	}
}

// TestSSHIdentityHealthRequiresFreshResourceProfile uses the same selector: health enables identity only, and a registered per-operation contract remains mandatory for effects.
// No action, grant or external permission is created by this metadata-only test.
// TestSSHIdentityHealthRequiresFreshResourceProfile использует тот же selector: health подтверждает лишь identity; effects требуют registered per-operation contract.
// Metadata-only test не создаёт action, grant либо external permission.
func TestSSHIdentityHealthRequiresFreshResourceProfile(t *testing.T) {
	for _, sample := range []struct{ definition, health, operation string }{
		{integrationpackage.SSHManagedDefinitionKey, "ssh.target.inspect", "ssh.file.create"},
		{integrationpackage.SSHManagedDefinitionKey, "ssh.target.inspect", "ssh.file.delete"},
		{integrationpackage.SSHExecuteDefinitionKey, "ssh.identity.inspect", "ssh.execute"},
	} {
		requirement, initiator, binding, now := personalBrokerIdentityFixture()
		requirement.DefinitionKey = sample.definition
		requirement.BrokerPurpose, binding.BrokerPurpose, binding.BrokerIdentity.Purpose = entity.IntegrationBrokerIdentitySSH, entity.IntegrationBrokerIdentitySSH, entity.IntegrationBrokerIdentitySSH
		binding.VerifiedCapabilities = []string{sample.health}
		binding.DeniedCapabilities = nil
		requirement.CapabilityKey = sample.operation
		selection, err := ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
		if err != nil || selection.Problem != entity.IntegrationCredentialUnconfirmed || selection.BrokerIdentity != nil {
			t.Fatal("identity health proved host-wide effect permission")
		}
		requirement.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionSSHScopedV1
		selection, err = ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
		if err != nil || selection.Problem != "" || selection.BrokerIdentity == nil || selection.Credential != nil || selection.ResourcePermissionProfile != requirement.ResourcePermissionProfile {
			t.Fatal("registered fresh per-operation boundary lost current identity metadata", err)
		}
		binding.VerifiedCapabilities = nil
		selection, err = ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
		if err != nil || selection.Problem != entity.IntegrationCredentialUnconfirmed || selection.BrokerIdentity != nil {
			t.Fatal("resource profile bypassed required identity verification")
		}
	}
}
