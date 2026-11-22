package platform

import (
	"math"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ReplacePersonalIntegrationBrokerIdentity advances the same personal owner generation used by
// token replacement and revoke, retaining only a metadata locator. The owner must first resolve
// the pinned definition, target and exact purpose-specific broker materialization receipt.
// ReplacePersonalIntegrationBrokerIdentity увеличивает то же personal owner generation, что
// token replacement и revoke, сохраняя только metadata locator. Владелец заранее разрешает
// pinned definition, target и exact purpose-specific broker materialization receipt.
func ReplacePersonalIntegrationBrokerIdentity(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64, identity entity.IntegrationBrokerIdentityRevision) (entity.PersonalIntegrationCredential, error) {
	if err := personalIntegrationCredentialMutationAuthority(binding, initiator, expectedVersion); err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if binding.Kind != entity.IntegrationCredentialBrokerIdentity || binding.Credential != nil ||
		binding.BrokerPurpose != identity.Purpose ||
		!identity.ValidMetadata() ||
		binding.BrokerIdentity != nil && (identity.Ref == binding.BrokerIdentity.Ref || identity.Revision <= binding.BrokerIdentity.Revision) {
		return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
	}
	if binding.Version == math.MaxInt64 || binding.Generation == math.MaxInt64 {
		return entity.PersonalIntegrationCredential{}, errs.ErrConflict
	}
	binding.Version++
	binding.Generation++
	binding.State = "CHECK_REQUIRED"
	copy := identity
	binding.BrokerIdentity = &copy
	clearPersonalIntegrationCredentialVerification(&binding)
	return binding, nil
}

// BeginPersonalIntegrationBrokerIdentityVerification selects only the private broker test lane.
// The same lifecycle implementation assigns its snapshot; generic token probes cannot call this
// entry through their owner adapter and cannot receive broker credentials from its metadata.
// BeginPersonalIntegrationBrokerIdentityVerification выбирает только private broker test lane.
// Общая lifecycle implementation назначает snapshot; owner adapter обычных token probes не
// вызывает этот вход и не получает broker credentials из его metadata.
func BeginPersonalIntegrationBrokerIdentityVerification(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64, spec entity.PersonalIntegrationCredentialTestSpec) (entity.PersonalIntegrationCredential, entity.PersonalIntegrationCredentialTestSnapshot, error) {
	return beginPersonalIntegrationCredentialVerification(binding, initiator, expectedVersion, spec, entity.IntegrationCredentialBrokerIdentity)
}

// personalVerificationSource validates the current immutable locator against the exact private
// purpose/target in an owner-derived probe spec. It returns no credential material or permissions.
// personalVerificationSource проверяет current immutable locator против exact private purpose/
// target из owner-derived probe spec. Credential material и permissions не возвращаются.
func personalVerificationSource(binding entity.PersonalIntegrationCredential, spec entity.PersonalIntegrationCredentialTestSpec) (string, bool) {
	switch binding.Kind {
	case entity.IntegrationCredentialToken:
		if binding.BrokerIdentity != nil || binding.BrokerPurpose != "" || spec.BrokerPurpose != "" || spec.BrokerTargetRef != "" || !validStoredIntegrationCredential(binding.Credential) {
			return "", false
		}
		return binding.Credential.Ref, true
	case entity.IntegrationCredentialBrokerIdentity:
		identity := binding.BrokerIdentity
		if binding.Credential != nil || binding.BrokerPurpose != spec.BrokerPurpose || identity == nil || !identity.ValidMetadata() ||
			identity.Purpose != spec.BrokerPurpose || identity.TargetRef != spec.BrokerTargetRef || spec.GitHubDiscoveryPage != 0 {
			return "", false
		}
		return identity.Ref, true
	default:
		return "", false
	}
}
