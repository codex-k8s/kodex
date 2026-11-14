package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// PersonalIntegrationCredentialTestSpec is resolved from the published definition/configuration
// by the owner. Allowed probes must be READ/NONE capabilities; listing a key here is not authority.
// PersonalIntegrationCredentialTestSpec разрешается владельцем из опубликованных definition/configuration.
// Допустимые probes обязаны быть READ/NONE capabilities; само имя ключа не является полномочием.
type PersonalIntegrationCredentialTestSpec struct {
	Ref, DefinitionDigest, ConfigurationDigest, HealthCapabilityKey string
	ConnectionVersion                                               int64
	GitHubDiscoveryPage                                             int
	AllowedReadCapabilityKeys                                       []string
	AllowedObservationProfiles, AllowedDeniedWriteCapabilityKeys    []string
}

// PersonalIntegrationCredentialTestSnapshot pins one verification to self ownership, exact binding
// generation/version, immutable credential and configuration. Lease authority and receipt storage
// belong to the owner transaction, not to serialized caller-provided snapshot coordinates.
// PersonalIntegrationCredentialTestSnapshot закрепляет проверку за собственником, точными binding
// generation/version, immutable credential и configuration. Полномочия lease и хранение receipt
// принадлежат owner-транзакции, не сериализованным координатам snapshot из caller payload.
type PersonalIntegrationCredentialTestSnapshot struct {
	PersonalIntegrationCredentialTestSpec
	OrganizationID, SubjectID, ConnectionRef, BindingRef, CredentialRef string
	BindingVersion, BindingGeneration                                   int64
}

// PersonalIntegrationCredentialTestOutcome separates observed authentication rejection from a
// transient/unobserved transport failure, which cannot establish that the key itself is invalid.
// PersonalIntegrationCredentialTestOutcome отделяет наблюдённый отказ аутентификации от временной
// либо ненаблюдённой transport-ошибки, не доказывающей невалидность самого ключа.
type PersonalIntegrationCredentialTestOutcome string

const (
	// PersonalIntegrationCredentialAuthenticated records a successful approved health probe.
	// PersonalIntegrationCredentialAuthenticated фиксирует успешный разрешённый health probe.
	PersonalIntegrationCredentialAuthenticated PersonalIntegrationCredentialTestOutcome = "AUTHENTICATED"
	// PersonalIntegrationCredentialAuthenticationRejected records a provider authentication rejection.
	// PersonalIntegrationCredentialAuthenticationRejected фиксирует отказ provider authentication.
	PersonalIntegrationCredentialAuthenticationRejected PersonalIntegrationCredentialTestOutcome = "AUTH_REJECTED"
	// PersonalIntegrationCredentialVerificationUnavailable leaves verification outstanding.
	// PersonalIntegrationCredentialVerificationUnavailable сохраняет необходимость проверки.
	PersonalIntegrationCredentialVerificationUnavailable PersonalIntegrationCredentialTestOutcome = "UNAVAILABLE"
)

// PersonalIntegrationCredentialTestObservation carries bounded verified/denied operations only;
// unknown operations stay unconfirmed and expiry stays absent unless actually observed.
// PersonalIntegrationCredentialTestObservation переносит только ограниченные проверенные либо
// отклонённые операции; остальные остаются неподтверждёнными, expiry без наблюдения отсутствует.
type PersonalIntegrationCredentialTestObservation struct {
	Outcome                                       PersonalIntegrationCredentialTestOutcome
	ConfirmedCapabilityKeys, DeniedCapabilityKeys []string
	ObservedPermissionProfiles                    []string
	ExpiresAt                                     *time.Time
	GitHubDiscovery                               *integrationpackage.GitHubDiscoveryPage
}

// PersonalIntegrationCredentialTestReceipt reports whether a fenced observation was applied or
// safely discarded; it does not claim that other permissions or credentials were verified.
// PersonalIntegrationCredentialTestReceipt сообщает о применении либо безопасном отклонении
// ограждённого наблюдения; проверка других прав либо credentials не подразумевается.
type PersonalIntegrationCredentialTestReceipt struct {
	TestRef, State string
	Applied        bool
}
