package command

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const (
	// TestPersonalIntegrationCredential queues one owner-resolved READ health probe for the self binding.
	// TestPersonalIntegrationCredential ставит одну разрешённую владельцем READ health-проверку собственной привязки в очередь.
	TestPersonalIntegrationCredential Kind = "TEST_PERSONAL_INTEGRATION_CREDENTIAL"
	// CompletePersonalIntegrationCredentialTest records one exact consumed verification lease outcome.
	// CompletePersonalIntegrationCredentialTest сохраняет результат точного израсходованного verification lease.
	CompletePersonalIntegrationCredentialTest Kind = "COMPLETE_PERSONAL_INTEGRATION_CREDENTIAL_TEST"
	// SetIntegrationCredentialPolicy changes only a Connection's explicit selection policy.
	// SetIntegrationCredentialPolicy меняет только явную политику выбора credential подключения.
	SetIntegrationCredentialPolicy Kind = "SET_INTEGRATION_CREDENTIAL_POLICY"
	// CreatePersonalIntegrationCredential creates only the authenticated user's empty binding.
	// CreatePersonalIntegrationCredential создаёт только пустую привязку аутентифицированного пользователя.
	CreatePersonalIntegrationCredential Kind = "CREATE_PERSONAL_INTEGRATION_CREDENTIAL"
	// ConfigurePersonalIntegrationCredential commits materialized metadata for that same self-owned binding.
	// ConfigurePersonalIntegrationCredential сохраняет материализованные metadata той же собственной привязки.
	ConfigurePersonalIntegrationCredential Kind = "CONFIGURE_PERSONAL_INTEGRATION_CREDENTIAL"
	// RevokePersonalIntegrationCredential fences future personal call starts without deleting history.
	// RevokePersonalIntegrationCredential ограждает будущие личные вызовы без удаления истории.
	RevokePersonalIntegrationCredential Kind = "REVOKE_PERSONAL_INTEGRATION_CREDENTIAL"
)

// IntegrationCredentialPolicyInput contains no credential or user selector. Configuration ownership,
// exact version and in-flight dependency checks remain authoritative owner decisions.
// IntegrationCredentialPolicyInput не содержит credential либо selector пользователя. Владение
// настройкой, точная версия и незавершённые зависимости проверяются авторитетным владельцем.
type IntegrationCredentialPolicyInput struct {
	ConnectionRef string
	Policy        entity.IntegrationCredentialPolicy
}

// PersonalIntegrationCredentialInput never accepts a subject or tenant selector. The specialized
// plaintext service assigns materialization metadata before issuing the owner command.
// PersonalIntegrationCredentialInput не принимает selector subject или tenant. Специализированный
// plaintext service назначает metadata материализации до команды владельца.
type PersonalIntegrationCredentialInput struct {
	ConnectionRef, MaterializationRef string
	ExpectedConnectionVersion         int64
	GitHubDiscoveryPage               int
	Credential                        *entity.IntegrationCredentialRevision
}

// PersonalIntegrationCredentialTestCompletion carries only fenced authentication/READ observations,
// with no subject, credential selector or provider payload; the owner validates its immutable set.
// PersonalIntegrationCredentialTestCompletion содержит лишь fenced authentication/READ observations
// без subject, credential selector либо provider payload; owner сверяет immutable допустимый набор.
type PersonalIntegrationCredentialTestCompletion struct {
	TestRef, LeaseRef, Fence                      string
	Generation                                    int64
	Outcome                                       entity.PersonalIntegrationCredentialTestOutcome
	ConfirmedCapabilityKeys, DeniedCapabilityKeys []string
	ObservedPermissionProfiles                    []string
	ExpiresAt                                     *time.Time
	GitHubDiscovery                               *integrationpackage.GitHubDiscoveryPage
}
