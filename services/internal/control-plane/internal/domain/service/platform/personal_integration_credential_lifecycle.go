package platform

import (
	"math"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ReplacePersonalIntegrationCredential advances the owned binding generation and invalidates all
// previous verification, including replacement after revoke. Receipt replay and persistence belong
// to the owner's transaction; a new descriptor cannot silently reuse an old immutable revision.
// ReplacePersonalIntegrationCredential увеличивает поколение собственной привязки и сбрасывает
// прежнюю проверку, включая замену после отзыва. Replay receipt и сохранение принадлежат
// транзакции владельца; новый descriptor не переиспользует прежнюю immutable revision.
func ReplacePersonalIntegrationCredential(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64, credential entity.IntegrationCredentialRevision) (entity.PersonalIntegrationCredential, error) {
	if err := personalIntegrationCredentialMutationAuthority(binding, initiator, expectedVersion); err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if !validStoredIntegrationCredential(&credential) || binding.Credential != nil &&
		(credential.Ref == binding.Credential.Ref || credential.Revision <= binding.Credential.Revision) {
		return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
	}
	if binding.Version == math.MaxInt64 || binding.Generation == math.MaxInt64 {
		return entity.PersonalIntegrationCredential{}, errs.ErrConflict
	}
	binding.Version++
	binding.Generation++
	binding.State = "CHECK_REQUIRED"
	copy := credential
	binding.Credential = &copy
	clearPersonalIntegrationCredentialVerification(&binding)
	return binding, nil
}

// RevokePersonalIntegrationCredential closes future selections by advancing the same generation
// serialized with call starts. It retains the immutable descriptor for earlier receipt/recovery
// and does not claim to undo an external effect; repeated current-state revoke is a no-op.
// RevokePersonalIntegrationCredential закрывает будущий выбор увеличением того же поколения,
// которое сериализуется с началом вызова. Immutable descriptor сохраняется для прежнего receipt/
// recovery; внешний эффект не отменяется задним числом, повтор текущего revoke ничего не меняет.
func RevokePersonalIntegrationCredential(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64) (entity.PersonalIntegrationCredential, error) {
	if err := personalIntegrationCredentialMutationAuthority(binding, initiator, expectedVersion); err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if binding.State == "REVOKED" {
		return binding, nil
	}
	if binding.Version == math.MaxInt64 || binding.Generation == math.MaxInt64 {
		return entity.PersonalIntegrationCredential{}, errs.ErrConflict
	}
	binding.Version++
	binding.Generation++
	binding.State = "REVOKED"
	clearPersonalIntegrationCredentialVerification(&binding)
	return binding, nil
}

// personalIntegrationCredentialMutationAuthority requires the self-owned actor before OCC,
// rejects unknown stored state and bounds counters before any state-changing calculation.
// The caller separately rechecks current principal eligibility and exact Connection access.
// personalIntegrationCredentialMutationAuthority проверяет собственного actor до OCC,
// отклоняет неизвестное сохранённое состояние и ограничивает counters до изменения.
// Вызывающий отдельно проверяет текущую eligibility principal и доступ к точному Connection.
func personalIntegrationCredentialMutationAuthority(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64) error {
	if initiator.SubjectKind != "USER" || initiator.OrganizationID == "" || initiator.SubjectID == "" ||
		binding.OrganizationID != initiator.OrganizationID || binding.SubjectID != initiator.SubjectID {
		return errs.ErrForbidden
	}
	if binding.Ref == "" || binding.ConnectionRef == "" || binding.Version < 1 || binding.Generation < 1 {
		return errs.ErrUnavailable
	}
	if binding.Version != expectedVersion {
		return errs.ErrVersionMismatch
	}
	switch binding.State {
	case "NOT_CONFIGURED", "CHECK_REQUIRED", "CHECKING", "READY", "INVALID", "REVOKED":
		return nil
	default:
		return errs.ErrUnavailable
	}
}

// clearPersonalIntegrationCredentialVerification removes only current eligibility observations;
// it never deletes historical test/effect records, a Secret or the retained credential descriptor.
// clearPersonalIntegrationCredentialVerification удаляет только текущие наблюдения eligibility;
// исторические test/effect records, Secret и сохранённый credential descriptor не удаляются.
func clearPersonalIntegrationCredentialVerification(binding *entity.PersonalIntegrationCredential) {
	binding.GitHubDiscovery = nil
	binding.VerifiedCredentialRef = ""
	binding.VerifiedDefinitionDigest = ""
	binding.VerifiedConfigurationDigest = ""
	binding.VerifiedCapabilities = nil
	binding.DeniedCapabilities = nil
	binding.ObservedPermissionProfiles = nil
	binding.ExpiresAt = nil
}
