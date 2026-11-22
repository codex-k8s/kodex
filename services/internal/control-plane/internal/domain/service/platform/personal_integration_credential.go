package platform

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// maximumIntegrationCredentialRequirements matches the existing RuntimeRevision grant bound.
// maximumIntegrationCredentialRequirements совпадает с существующим пределом grants RuntimeRevision.
const maximumIntegrationCredentialRequirements = 256

// PersonalIntegrationCredentialMaterializationRef binds an idempotent materialization to the
// resolved tenant, subject and Connection. The caller resolves those coordinates before use;
// neither another user's operation key nor ambiguous concatenation can reuse the same Secret key.
// PersonalIntegrationCredentialMaterializationRef привязывает идемпотентную материализацию к
// разрешённым tenant, subject и Connection. Вызывающий заранее разрешает эти координаты;
// operation key другого пользователя и неоднозначная конкатенация не переиспользуют Secret key.
func PersonalIntegrationCredentialMaterializationRef(initiator entity.IntegrationCredentialInitiator, connectionRef, idempotencyKey string) (string, error) {
	if initiator.SubjectKind != "USER" || !personalIntegrationCredentialCoordinate(initiator.OrganizationID) ||
		!personalIntegrationCredentialCoordinate(initiator.SubjectID) || !personalIntegrationCredentialCoordinate(connectionRef) ||
		len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
		return "", errs.ErrInvalid
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("kodex.personal-integration-credential/v1\x00"))
	for _, field := range []string{initiator.OrganizationID, initiator.SubjectID, connectionRef, idempotencyKey} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(field))
	}
	return "integration-personal-" + hex.EncodeToString(digest.Sum(nil)), nil
}

// personalIntegrationCredentialCoordinate admits only bounded owner-assigned opaque references
// or UUIDs; it is a syntax guard and never resolves ownership from untrusted input.
// personalIntegrationCredentialCoordinate принимает только ограниченные owner-assigned opaque refs
// либо UUID; это проверка синтаксиса, не разрешение владельца из недоверенного ввода.
func personalIntegrationCredentialCoordinate(value string) bool {
	if len(value) < 1 || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

// ResolveIntegrationCredential applies one explicit policy to owner-resolved metadata.
// The caller must hold the authoritative binding lock through call-start commit; this function
// grants no authority, reads no secret and does not validate external permissions by itself.
// ResolveIntegrationCredential применяет явную политику к разрешённым владельцем метаданным.
// Вызывающий удерживает блокировку привязки до commit начала вызова; функция не выдаёт
// полномочий, не читает секрет и самостоятельно не проверяет внешние права.
func ResolveIntegrationCredential(
	requirement entity.IntegrationCredentialRequirement,
	initiator entity.IntegrationCredentialInitiator,
	shared *entity.IntegrationCredentialSource,
	personal *entity.PersonalIntegrationCredential,
	now time.Time,
) (entity.IntegrationCredentialSelection, error) {
	if requirement.ConnectionRef == "" || requirement.CapabilityKey == "" || now.IsZero() ||
		initiator.OrganizationID == "" || !validIntegrationCredentialDigest(requirement.DefinitionDigest) ||
		!validIntegrationCredentialDigest(requirement.ConfigurationDigest) ||
		(requirement.Policy != entity.IntegrationCredentialProjectShared && requirement.Policy != entity.IntegrationCredentialInitiatorRequired) ||
		!validIntegrationCredentialRequirementKind(requirement) {
		return entity.IntegrationCredentialSelection{}, errs.ErrInvalid
	}
	if !requirement.CredentialRequired {
		return entity.IntegrationCredentialSelection{}, nil
	}
	if requirement.Policy == entity.IntegrationCredentialProjectShared {
		return selectIntegrationCredentialSource(requirement, shared, now)
	}
	resourceContract, resourceCheck := integrationpackage.CredentialResourcePermissionContractForProfile(requirement.DefinitionKey, requirement.CapabilityKey, requirement.ResourcePermissionProfile)
	if resourceCheck && requirement.ResourcePermissionProfile != resourceContract.Profile || !resourceCheck && requirement.ResourcePermissionProfile != "" {
		return entity.IntegrationCredentialSelection{}, errs.ErrInvalid
	}
	if initiator.SubjectKind != "USER" || initiator.SubjectID == "" {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialInitiatorMissing}, nil
	}
	if personal == nil {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialMissing}, nil
	}
	if personal.OrganizationID != initiator.OrganizationID || personal.SubjectID != initiator.SubjectID || personal.ConnectionRef != requirement.ConnectionRef {
		return entity.IntegrationCredentialSelection{}, errs.ErrForbidden
	}
	if personal.Ref == "" || personal.Version < 1 || personal.Generation < 1 {
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
	var problem entity.IntegrationCredentialProblem
	switch personal.State {
	case "REVOKED":
		problem = entity.IntegrationCredentialRevoked
	case "INVALID":
		problem = entity.IntegrationCredentialInvalid
	case "NOT_CONFIGURED":
		problem = entity.IntegrationCredentialMissing
	case "CHECK_REQUIRED", "CHECKING":
		problem = entity.IntegrationCredentialCheckRequired
	case "READY":
	default:
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
	if problem != "" {
		return entity.IntegrationCredentialSelection{Problem: problem}, nil
	}
	if personal.ExpiresAt != nil && !personal.ExpiresAt.After(now) {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialExpired}, nil
	}
	if personal.Kind != entity.IntegrationCredentialToken && personal.Kind != entity.IntegrationCredentialBrokerIdentity {
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
	if personal.Kind != requirement.Kind {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialCheckRequired}, nil
	}
	if personal.Kind == entity.IntegrationCredentialToken && personal.BrokerPurpose != "" ||
		personal.Kind == entity.IntegrationCredentialBrokerIdentity && personal.BrokerPurpose != requirement.BrokerPurpose {
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
	selection, err := selectIntegrationCredentialSource(requirement, &entity.IntegrationCredentialSource{
		Kind: personal.Kind, Credential: personal.Credential, BrokerIdentity: personal.BrokerIdentity,
	}, now)
	if err != nil || selection.Problem != "" {
		return selection, err
	}
	verified := personal.Kind == entity.IntegrationCredentialToken && personal.VerifiedBrokerIdentityRef == "" &&
		personal.VerifiedCredentialRef == selection.Credential.Ref ||
		personal.Kind == entity.IntegrationCredentialBrokerIdentity && personal.VerifiedCredentialRef == "" &&
			personal.VerifiedBrokerIdentityRef == selection.BrokerIdentity.Ref
	if !verified || personal.VerifiedDefinitionDigest != requirement.DefinitionDigest ||
		personal.VerifiedConfigurationDigest != requirement.ConfigurationDigest {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialCheckRequired}, nil
	}
	if containsString(personal.DeniedCapabilities, requirement.CapabilityKey) {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialInsufficient}, nil
	}
	if resourceCheck {
		if resourceContract.RequiredObservationProfile != "" && !containsString(personal.ObservedPermissionProfiles, resourceContract.RequiredObservationProfile) {
			return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialUnconfirmed}, nil
		}
		for _, dependency := range resourceContract.ReadCapabilities {
			if containsString(personal.DeniedCapabilities, dependency) {
				return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialInsufficient}, nil
			}
			if !containsString(personal.VerifiedCapabilities, dependency) {
				return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialUnconfirmed}, nil
			}
		}
	} else if !containsString(personal.VerifiedCapabilities, requirement.CapabilityKey) {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialUnconfirmed}, nil
	}
	selection.PersonalBindingRef = personal.Ref
	selection.BindingGeneration = personal.Generation
	selection.ResourcePermissionProfile = requirement.ResourcePermissionProfile
	return selection, nil
}

// PreflightIntegrationCredentials reports every known unusable requirement in stable owner order.
// No positive decision is cached or turned into a reusable execution grant; malformed snapshots
// and duplicate requirements fail as a whole rather than returning a misleading partial list.
// PreflightIntegrationCredentials сообщает все известные непригодные требования в порядке владельца.
// Положительное решение не кэшируется и не становится execution grant; некорректный снимок
// либо повтор требования отклоняет весь результат вместо вводящего в заблуждение частичного списка.
func PreflightIntegrationCredentials(
	requirements []entity.IntegrationCredentialRequirement,
	initiator entity.IntegrationCredentialInitiator,
	shared map[string]*entity.IntegrationCredentialSource,
	personal map[string]*entity.PersonalIntegrationCredential,
	now time.Time,
) ([]entity.IntegrationCredentialPreflightIssue, error) {
	if len(requirements) > maximumIntegrationCredentialRequirements || now.IsZero() || initiator.OrganizationID == "" {
		return nil, errs.ErrInvalid
	}
	issues := make([]entity.IntegrationCredentialPreflightIssue, 0)
	seen := make(map[[2]string]struct{}, len(requirements))
	connections := make(map[string]entity.IntegrationCredentialRequirement, len(requirements))
	for _, requirement := range requirements {
		key := [2]string{requirement.ConnectionRef, requirement.CapabilityKey}
		if _, exists := seen[key]; exists {
			return nil, errs.ErrInvalid
		}
		seen[key] = struct{}{}
		if previous, exists := connections[requirement.ConnectionRef]; exists &&
			(previous.DefinitionDigest != requirement.DefinitionDigest || previous.ConfigurationDigest != requirement.ConfigurationDigest ||
				previous.Policy != requirement.Policy || previous.CredentialRequired != requirement.CredentialRequired ||
				previous.Kind != requirement.Kind || previous.BrokerPurpose != requirement.BrokerPurpose || previous.BrokerTargetRef != requirement.BrokerTargetRef) {
			return nil, errs.ErrInvalid
		}
		connections[requirement.ConnectionRef] = requirement
		selection, err := ResolveIntegrationCredential(requirement, initiator, shared[requirement.ConnectionRef], personal[requirement.ConnectionRef], now)
		if err != nil {
			return nil, err
		}
		if selection.Problem != "" {
			issues = append(issues, entity.IntegrationCredentialPreflightIssue{ConnectionRef: requirement.ConnectionRef, CapabilityKey: requirement.CapabilityKey, Problem: selection.Problem})
		}
	}
	return issues, nil
}

// validIntegrationCredentialRequirementKind accepts one closed transport selected by the pinned
// definition and current owner target; neither generic tokens nor unknown purposes become broker identities.
// validIntegrationCredentialRequirementKind принимает один closed transport из pinned definition
// и current owner target; generic tokens и неизвестные purposes не становятся broker identities.
func validIntegrationCredentialRequirementKind(requirement entity.IntegrationCredentialRequirement) bool {
	switch requirement.Kind {
	case entity.IntegrationCredentialToken:
		return requirement.BrokerPurpose == "" && requirement.BrokerTargetRef == ""
	case entity.IntegrationCredentialBrokerIdentity:
		return requirement.BrokerPurpose.Valid() && personalIntegrationCredentialCoordinate(requirement.BrokerTargetRef)
	default:
		return false
	}
}

// selectIntegrationCredentialSource validates and copies a single representation without reading a
// Secret. Broker metadata can never satisfy the generic token branch or select a different target.
// selectIntegrationCredentialSource проверяет и копирует одно представление без чтения Secret.
// Broker metadata не удовлетворяют generic token branch и не выбирают другой target.
func selectIntegrationCredentialSource(requirement entity.IntegrationCredentialRequirement, source *entity.IntegrationCredentialSource, now time.Time) (entity.IntegrationCredentialSelection, error) {
	if source == nil {
		return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialMissing}, nil
	}
	if source.Kind != requirement.Kind || source.Credential != nil && source.BrokerIdentity != nil {
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
	switch source.Kind {
	case entity.IntegrationCredentialToken:
		if source.BrokerIdentity != nil {
			return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
		}
		if source.Credential == nil {
			return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialMissing}, nil
		}
		if !validStoredIntegrationCredential(source.Credential) {
			return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
		}
		copy := *source.Credential
		return entity.IntegrationCredentialSelection{Kind: source.Kind, Credential: &copy}, nil
	case entity.IntegrationCredentialBrokerIdentity:
		if source.Credential != nil {
			return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
		}
		if source.BrokerIdentity == nil {
			return entity.IntegrationCredentialSelection{Problem: entity.IntegrationCredentialMissing}, nil
		}
		identity := source.BrokerIdentity
		if !identity.ValidMetadata() || identity.CreatedAt.After(now) ||
			identity.Purpose != requirement.BrokerPurpose || identity.TargetRef != requirement.BrokerTargetRef {
			return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
		}
		copy := *identity
		return entity.IntegrationCredentialSelection{Kind: source.Kind, BrokerIdentity: &copy}, nil
	default:
		return entity.IntegrationCredentialSelection{}, errs.ErrUnavailable
	}
}

// validIntegrationCredentialDigest accepts only canonical SHA256 pins, never normalized input.
// validIntegrationCredentialDigest принимает только канонические SHA256 pins без нормализации входа.
func validIntegrationCredentialDigest(digest string) bool {
	if len(digest) != 64 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

// validStoredIntegrationCredential checks committed metadata without treating a descriptor as authority.
// validStoredIntegrationCredential проверяет сохранённые метаданные, не считая descriptor полномочием.
func validStoredIntegrationCredential(credential *entity.IntegrationCredentialRevision) bool {
	if credential == nil || credential.Ref == "" || credential.Revision < 1 || credential.CreatedAt.IsZero() {
		return false
	}
	return credential.ValidDescriptor()
}
