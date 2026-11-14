package platform

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// BeginPersonalIntegrationCredentialVerification invalidates earlier verification and assigns an
// exact owner snapshot after self/OCC checks. The caller resolves the published READ/NONE probes,
// persists binding/job/receipt atomically and supplies no plaintext to this operation.
// BeginPersonalIntegrationCredentialVerification сбрасывает прежнюю проверку и назначает точный
// owner snapshot после self/OCC проверок. Вызывающий разрешает опубликованные READ/NONE probes,
// атомарно сохраняет binding/job/receipt и не передаёт сюда plaintext.
func BeginPersonalIntegrationCredentialVerification(binding entity.PersonalIntegrationCredential, initiator entity.IntegrationCredentialInitiator, expectedVersion int64, spec entity.PersonalIntegrationCredentialTestSpec) (entity.PersonalIntegrationCredential, entity.PersonalIntegrationCredentialTestSnapshot, error) {
	if err := personalIntegrationCredentialMutationAuthority(binding, initiator, expectedVersion); err != nil {
		return entity.PersonalIntegrationCredential{}, entity.PersonalIntegrationCredentialTestSnapshot{}, err
	}
	if spec.GitHubDiscoveryPage < 0 || spec.GitHubDiscoveryPage > 10000 || spec.GitHubDiscoveryPage != 0 && spec.HealthCapabilityKey != "github.identity.read" {
		return entity.PersonalIntegrationCredential{}, entity.PersonalIntegrationCredentialTestSnapshot{}, errs.ErrInvalid
	}
	if binding.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired || binding.State == "REVOKED" || binding.State == "CHECKING" || binding.State == "NOT_CONFIGURED" ||
		binding.Credential == nil || !validStoredIntegrationCredential(binding.Credential) {
		return entity.PersonalIntegrationCredential{}, entity.PersonalIntegrationCredentialTestSnapshot{}, errs.ErrConflict
	}
	if !personalIntegrationCredentialCoordinate(spec.Ref) || spec.ConnectionVersion < 1 || binding.ConnectionVersion != spec.ConnectionVersion ||
		!validIntegrationCredentialDigest(spec.DefinitionDigest) || !validIntegrationCredentialDigest(spec.ConfigurationDigest) ||
		!validPersonalCredentialTestCapabilities(spec.AllowedReadCapabilityKeys) || !containsString(spec.AllowedReadCapabilityKeys, spec.HealthCapabilityKey) || !validPersonalCredentialObservationSpec(spec) {
		return entity.PersonalIntegrationCredential{}, entity.PersonalIntegrationCredentialTestSnapshot{}, errs.ErrInvalid
	}
	if binding.Version == math.MaxInt64 {
		return entity.PersonalIntegrationCredential{}, entity.PersonalIntegrationCredentialTestSnapshot{}, errs.ErrConflict
	}
	binding.Version++
	binding.State = "CHECKING"
	clearPersonalIntegrationCredentialVerification(&binding)
	spec.AllowedReadCapabilityKeys = append([]string(nil), spec.AllowedReadCapabilityKeys...)
	spec.AllowedObservationProfiles = append([]string(nil), spec.AllowedObservationProfiles...)
	spec.AllowedDeniedWriteCapabilityKeys = append([]string(nil), spec.AllowedDeniedWriteCapabilityKeys...)
	snapshot := entity.PersonalIntegrationCredentialTestSnapshot{PersonalIntegrationCredentialTestSpec: spec,
		OrganizationID: binding.OrganizationID, SubjectID: binding.SubjectID, ConnectionRef: binding.ConnectionRef,
		BindingRef: binding.Ref, BindingVersion: binding.Version, BindingGeneration: binding.Generation, CredentialRef: binding.Credential.Ref}
	return binding, snapshot, nil
}

// ValidatePersonalIntegrationCredentialVerificationSnapshot checks only current owner/pin
// equality. Lease admission and active actor/resource eligibility remain separate owner checks.
// ValidatePersonalIntegrationCredentialVerificationSnapshot проверяет только текущие owner/pins.
// Допуск lease и активные actor/resource eligibility остаются отдельными проверками владельца.
func ValidatePersonalIntegrationCredentialVerificationSnapshot(binding entity.PersonalIntegrationCredential, snapshot entity.PersonalIntegrationCredentialTestSnapshot, currentDefinitionDigest, currentConfigurationDigest string) error {
	if binding.OrganizationID != snapshot.OrganizationID || binding.SubjectID != snapshot.SubjectID || binding.ConnectionRef != snapshot.ConnectionRef || binding.Ref != snapshot.BindingRef ||
		snapshot.OrganizationID == "" || snapshot.SubjectID == "" || snapshot.BindingRef == "" {
		return errs.ErrForbidden
	}
	if binding.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired || binding.State != "CHECKING" || binding.Version != snapshot.BindingVersion ||
		binding.Generation != snapshot.BindingGeneration || binding.Credential == nil || binding.Credential.Ref != snapshot.CredentialRef ||
		binding.ConnectionVersion != snapshot.ConnectionVersion || currentDefinitionDigest != snapshot.DefinitionDigest || currentConfigurationDigest != snapshot.ConfigurationDigest {
		return errs.ErrConflict
	}
	if binding.Version == math.MaxInt64 {
		return errs.ErrConflict
	}
	if binding.Version < 1 || binding.Generation < 1 || !validStoredIntegrationCredential(binding.Credential) ||
		!personalIntegrationCredentialCoordinate(snapshot.Ref) || !validIntegrationCredentialDigest(currentDefinitionDigest) || !validIntegrationCredentialDigest(currentConfigurationDigest) ||
		!validPersonalCredentialTestCapabilities(snapshot.AllowedReadCapabilityKeys) || !containsString(snapshot.AllowedReadCapabilityKeys, snapshot.HealthCapabilityKey) || !validPersonalCredentialObservationSpec(snapshot.PersonalIntegrationCredentialTestSpec) {
		return errs.ErrInvalid
	}
	return nil
}

// CompletePersonalIntegrationCredentialVerification applies an observation only to the exact
// still-checking generation and current definition/configuration. The caller first validates the
// live lease/fence and locks the same binding as revoke/rotation; this helper grants no worker rights.
// CompletePersonalIntegrationCredentialVerification применяет наблюдение только к точному всё ещё
// проверяемому поколению и текущим definition/configuration. Вызывающий заранее проверяет live
// lease/fence и блокирует ту же привязку, что revoke/rotation; helper не выдаёт прав worker.
func CompletePersonalIntegrationCredentialVerification(binding entity.PersonalIntegrationCredential, snapshot entity.PersonalIntegrationCredentialTestSnapshot,
	currentDefinitionDigest, currentConfigurationDigest string, observation entity.PersonalIntegrationCredentialTestObservation,
) (entity.PersonalIntegrationCredential, error) {
	if err := ValidatePersonalIntegrationCredentialVerificationSnapshot(binding, snapshot, currentDefinitionDigest, currentConfigurationDigest); err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	clearPersonalIntegrationCredentialVerification(&binding)
	switch observation.Outcome {
	case entity.PersonalIntegrationCredentialAuthenticated:
		if !validPersonalCredentialTestCapabilities(observation.ConfirmedCapabilityKeys) || !containsString(observation.ConfirmedCapabilityKeys, snapshot.HealthCapabilityKey) ||
			len(observation.DeniedCapabilityKeys) > 0 && !validPersonalCredentialTestCapabilities(observation.DeniedCapabilityKeys) {
			return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
		}
		if !validPersonalCredentialObservationResult(snapshot.PersonalIntegrationCredentialTestSpec, observation) {
			return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
		}
		for _, key := range observation.ConfirmedCapabilityKeys {
			if !containsString(snapshot.AllowedReadCapabilityKeys, key) {
				return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
			}
		}
		for _, key := range observation.DeniedCapabilityKeys {
			if containsString(observation.ConfirmedCapabilityKeys, key) {
				return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
			}
		}
		if snapshot.GitHubDiscoveryPage != 0 {
			if observation.GitHubDiscovery == nil || !observation.GitHubDiscovery.Valid() || observation.GitHubDiscovery.Page != snapshot.GitHubDiscoveryPage {
				return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
			}
			raw, _ := json.Marshal(observation.GitHubDiscovery)
			binding.GitHubDiscovery, _ = integrationpackage.ParseGitHubDiscoveryPage(raw)
		} else if observation.GitHubDiscovery != nil {
			return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
		}
		binding.State = "READY"
		binding.VerifiedCredentialRef, binding.VerifiedDefinitionDigest, binding.VerifiedConfigurationDigest = snapshot.CredentialRef, snapshot.DefinitionDigest, snapshot.ConfigurationDigest
		binding.VerifiedCapabilities = append([]string(nil), observation.ConfirmedCapabilityKeys...)
		binding.DeniedCapabilities = append([]string(nil), observation.DeniedCapabilityKeys...)
		binding.ObservedPermissionProfiles = append([]string(nil), observation.ObservedPermissionProfiles...)
		if observation.ExpiresAt != nil {
			if observation.ExpiresAt.IsZero() || observation.ExpiresAt.Year() < 1 || observation.ExpiresAt.Year() > 9999 {
				return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
			}
			copy := observation.ExpiresAt.UTC()
			binding.ExpiresAt = &copy
		}
	case entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable:
		if len(observation.ConfirmedCapabilityKeys) != 0 || len(observation.DeniedCapabilityKeys) != 0 || len(observation.ObservedPermissionProfiles) != 0 || observation.ExpiresAt != nil || observation.GitHubDiscovery != nil {
			return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
		}
		binding.State = "INVALID"
		if observation.Outcome == entity.PersonalIntegrationCredentialVerificationUnavailable {
			binding.State = "CHECK_REQUIRED"
		}
	default:
		return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
	}
	binding.Version++
	return binding, nil
}

// validPersonalCredentialTestCapabilities accepts a closed bounded set of capability identifiers,
// rejecting duplicates, whitespace and malformed keys before they can describe a permission proof.
// validPersonalCredentialTestCapabilities принимает ограниченный набор capability identifiers,
// отклоняет дубликаты, пробелы и неверные ключи до их использования как permission proof.
func validPersonalCredentialTestCapabilities(keys []string) bool {
	if len(keys) == 0 || len(keys) > maximumIntegrationCredentialRequirements {
		return false
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key == "" || len(key) > 100 || strings.TrimSpace(key) != key || seen[key] || key[0] < 'a' || key[0] > 'z' {
			return false
		}
		separator := false
		for _, character := range key {
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
				separator = false
				continue
			}
			if separator || character != '.' && character != '_' && character != '-' {
				return false
			}
			separator = true
		}
		if separator {
			return false
		}
		seen[key] = true
	}
	return true
}

// validPersonalCredentialObservationSpec bounds the owner-resolved metadata request. Empty fields
// preserve legacy health jobs; write denials can only belong to an explicit closed profile.
// validPersonalCredentialObservationSpec ограничивает owner-resolved metadata request. Пустые поля
// сохраняют legacy health jobs; write denials возможны только для явного закрытого профиля.
func validPersonalCredentialObservationSpec(spec entity.PersonalIntegrationCredentialTestSpec) bool {
	if len(spec.AllowedObservationProfiles) == 0 {
		return len(spec.AllowedDeniedWriteCapabilityKeys) == 0
	}
	if len(spec.AllowedObservationProfiles) != 1 || (spec.AllowedObservationProfiles[0] != integrationpackage.CredentialObservationGitLabProjectWriteScopeV1 && spec.AllowedObservationProfiles[0] != integrationpackage.CredentialObservationSentryScopesV1) ||
		!validPersonalCredentialTestCapabilities(spec.AllowedDeniedWriteCapabilityKeys) {
		return false
	}
	for _, key := range spec.AllowedDeniedWriteCapabilityKeys {
		if containsString(spec.AllowedReadCapabilityKeys, key) || !integrationpackage.CredentialObservationMayDeny(spec.AllowedObservationProfiles[0], key) {
			return false
		}
	}
	return true
}

// validPersonalCredentialObservationResult accepts only requested metadata and known denials;
// a checked profile never places WRITE in confirmed capabilities. Unknown facts stay absent.
// validPersonalCredentialObservationResult принимает только запрошенные metadata и известные отказы;
// проверенный профиль не помещает WRITE в confirmed capabilities. Неизвестные факты отсутствуют.
func validPersonalCredentialObservationResult(spec entity.PersonalIntegrationCredentialTestSpec, observation entity.PersonalIntegrationCredentialTestObservation) bool {
	if len(observation.ObservedPermissionProfiles) > 1 {
		return false
	}
	for _, profile := range observation.ObservedPermissionProfiles {
		if !containsString(spec.AllowedObservationProfiles, profile) {
			return false
		}
	}
	for _, key := range observation.DeniedCapabilityKeys {
		if containsString(spec.AllowedReadCapabilityKeys, key) {
			continue
		}
		if !containsString(spec.AllowedDeniedWriteCapabilityKeys, key) {
			return false
		}
		matched := false
		for _, profile := range observation.ObservedPermissionProfiles {
			matched = matched || integrationpackage.CredentialObservationMayDeny(profile, key)
		}
		if !matched {
			return false
		}
	}
	// Expiry restricts eligibility and never grants a capability.
	// Expiry ограничивает eligibility и никогда не выдаёт capability.
	return true
}
