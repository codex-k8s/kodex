package entity

import "time"

// IntegrationCredentialPolicy is the persisted, explicit selection mode of a Connection;
// an unknown value never permits falling back to its shared credential.
// IntegrationCredentialPolicy — сохранённый явный режим выбора credential подключения;
// неизвестное значение никогда не разрешает fallback к общему ключу.
type IntegrationCredentialPolicy string

const (
	// IntegrationCredentialProjectShared selects the separately authorized Connection credential.
	// IntegrationCredentialProjectShared выбирает отдельно разрешённую credential подключения.
	IntegrationCredentialProjectShared IntegrationCredentialPolicy = "PROJECT_SHARED"
	// IntegrationCredentialInitiatorRequired selects only the persisted root initiator's own binding.
	// IntegrationCredentialInitiatorRequired выбирает только собственную привязку сохранённого инициатора корня.
	IntegrationCredentialInitiatorRequired IntegrationCredentialPolicy = "INITIATOR_REQUIRED"
)

// IntegrationCredentialProblem is a closed readiness code, never a provider error or secret value.
// IntegrationCredentialProblem — закрытый код готовности, а не ошибка провайдера или значение секрета.
type IntegrationCredentialProblem string

const (
	// IntegrationCredentialConnectionUnavailable closes unavailable configuration or current access.
	// IntegrationCredentialConnectionUnavailable закрывает недоступные configuration либо текущий доступ.
	IntegrationCredentialConnectionUnavailable IntegrationCredentialProblem = "CREDENTIAL_CONNECTION_UNAVAILABLE"
	// IntegrationCredentialMissing identifies an absent credential without suggesting shared fallback.
	// IntegrationCredentialMissing обозначает отсутствующую credential без предложения общего fallback.
	IntegrationCredentialMissing IntegrationCredentialProblem = "CREDENTIAL_MISSING"
	// IntegrationCredentialCheckRequired rejects an unverified or configuration-stale binding.
	// IntegrationCredentialCheckRequired отклоняет непроверенную либо устаревшую после настройки привязку.
	IntegrationCredentialCheckRequired IntegrationCredentialProblem = "CREDENTIAL_CHECK_REQUIRED"
	// IntegrationCredentialInvalid identifies an authoritative failed authentication observation.
	// IntegrationCredentialInvalid обозначает авторитетное наблюдение отказа аутентификации.
	IntegrationCredentialInvalid IntegrationCredentialProblem = "CREDENTIAL_INVALID"
	// IntegrationCredentialInsufficient identifies a capability explicitly rejected by verification.
	// IntegrationCredentialInsufficient обозначает capability, явно отклонённую проверкой.
	IntegrationCredentialInsufficient IntegrationCredentialProblem = "CREDENTIAL_INSUFFICIENT"
	// IntegrationCredentialUnconfirmed keeps unverified capabilities distinct from observed denial.
	// IntegrationCredentialUnconfirmed отличает непроверенную capability от наблюдаемого отказа.
	IntegrationCredentialUnconfirmed IntegrationCredentialProblem = "CREDENTIAL_PERMISSION_UNCONFIRMED"
	// IntegrationCredentialExpired rejects a known expiry at or before the owner clock.
	// IntegrationCredentialExpired отклоняет известный срок истечения не позднее часов владельца.
	IntegrationCredentialExpired IntegrationCredentialProblem = "CREDENTIAL_EXPIRED"
	// IntegrationCredentialRevoked closes new call starts while retaining earlier effect receipts.
	// IntegrationCredentialRevoked закрывает новые вызовы, сохраняя квитанции прежних эффектов.
	IntegrationCredentialRevoked IntegrationCredentialProblem = "CREDENTIAL_REVOKED"
	// IntegrationCredentialInitiatorMissing rejects noninteractive roots in the personal policy.
	// IntegrationCredentialInitiatorMissing отклоняет неинтерактивные корни в персональном режиме.
	IntegrationCredentialInitiatorMissing IntegrationCredentialProblem = "CREDENTIAL_INITIATOR_REQUIRED"
)

// IntegrationCredentialInitiator is read from the authoritative root, not from prompt or approver.
// IDs remain internal owner coordinates and do not independently establish authorization.
// IntegrationCredentialInitiator читается из авторитетного корня, а не prompt или approver.
// IDs остаются внутренними координатами владельца и сами не подтверждают полномочия.
type IntegrationCredentialInitiator struct {
	OrganizationID, SubjectID, SubjectKind string
}

// PersonalIntegrationCredential stores self-owned metadata and verification pins only.
// A READY observation is meaningful only for its exact descriptor, definition and configuration.
// PersonalIntegrationCredential хранит только собственные метаданные и привязки проверки.
// Наблюдение READY применимо лишь к точным descriptor, definition и configuration.
type PersonalIntegrationCredential struct {
	Ref, OrganizationID, SubjectID, ConnectionRef string
	// CredentialPolicy is read from the current Connection; changing it never transfers this binding.
	// CredentialPolicy читается из текущего Connection; его смена не передаёт эту привязку другому владельцу.
	CredentialPolicy IntegrationCredentialPolicy
	// ConnectionVersion is current owner metadata for configuration OCC, never the binding version.
	// ConnectionVersion — текущие owner metadata для configuration OCC, не версия личной привязки.
	ConnectionVersion                        int64
	Version, Generation                      int64
	State                                    string
	Credential                               *IntegrationCredentialRevision
	VerifiedCredentialRef                    string
	VerifiedDefinitionDigest                 string
	VerifiedConfigurationDigest              string
	VerifiedCapabilities, DeniedCapabilities []string
	ObservedPermissionProfiles               []string
	ExpiresAt                                *time.Time
}

// PersonalIntegrationConnection contains visible Connection metadata and an optional self binding.
// An absent binding is not an empty credential or permission to select a shared key. Internal
// binding metadata is excluded from incidental JSON serialization and requires a safe transport view.
// PersonalIntegrationConnection содержит metadata видимого Connection и необязательную личную привязку.
// Отсутствие привязки не означает пустой credential либо право выбрать общий ключ. Внутренние
// metadata привязки исключены из случайной JSON-сериализации и требуют безопасной transport-проекции.
type PersonalIntegrationConnection struct {
	Available                bool
	Ref, Name, DefinitionKey string
	Version                  int64
	Enabled                  bool
	Policy                   IntegrationCredentialPolicy
	Credential               *PersonalIntegrationCredential `json:"-"`
}

// IntegrationCredentialRequirement is resolved from published grants and current Connection state;
// browser-provided selectors cannot substitute for that owner-side resolution.
// IntegrationCredentialRequirement разрешается из опубликованных grants и текущего подключения;
// browser selectors не заменяют это разрешение у владельца.
type IntegrationCredentialRequirement struct {
	ConnectionRef, CapabilityKey, DefinitionDigest, ConfigurationDigest string
	DefinitionKey, ResourcePermissionProfile                            string
	Policy                                                              IntegrationCredentialPolicy
	CredentialRequired                                                  bool
}

// IntegrationCredentialSelection is internal owner-to-gateway material, excluded from JSON views.
// Personal generation and revision pins must be revalidated when a call-start lease is consumed.
// IntegrationCredentialSelection — внутренние данные owner→gateway, исключённые из JSON views.
// Personal generation и revision повторно проверяются при расходовании допуска начала вызова.
type IntegrationCredentialSelection struct {
	ResourcePermissionProfile string                         `json:"-"`
	Credential                *IntegrationCredentialRevision `json:"-"`
	PersonalBindingRef        string                         `json:"-"`
	BindingGeneration         int64                          `json:"-"`
	Problem                   IntegrationCredentialProblem   `json:"-"`
}

// IntegrationCredentialPreflightIssue exposes only the addressed requirement and a safe code.
// It carries no Secret descriptor, owner identity, raw authentication error or credential bytes.
// IntegrationCredentialPreflightIssue раскрывает только требование и безопасный код.
// Secret descriptor, identity владельца, исходной ошибки аутентификации и байтов ключа нет.
type IntegrationCredentialPreflightIssue struct {
	ConnectionRef, CapabilityKey string
	ConnectionName               string
	Problem                      IntegrationCredentialProblem
}
