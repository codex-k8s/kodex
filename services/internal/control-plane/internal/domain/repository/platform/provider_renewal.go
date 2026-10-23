package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	// ProviderCredentialRenewalOperation identifies the protected credential-renewal operation, distinct from observing recovery state and owner authorization checks.
	// ProviderCredentialRenewalOperation обозначает защищённую операцию обновления реквизита отдельно от наблюдения восстановления и проверок разрешения владельца.
	ProviderCredentialRenewalOperation = "platform.provider-accounts.credential.renew"
	// ProviderCredentialRenewalObserveOperation identifies readback of credential-renewal state for recovery; observing an outcome is not a second renewal attempt.
	// ProviderCredentialRenewalObserveOperation обозначает контрольное чтение состояния обновления реквизита для восстановления; наблюдение исхода не является второй попыткой обновления.
	ProviderCredentialRenewalObserveOperation = "platform.provider-accounts.credential.renewal.observe"
	// ProviderCredentialRenewalAuthorizeOperation is the exact owner authorization operation checked for secret-broker renewal before the protected write path.
	// ProviderCredentialRenewalAuthorizeOperation — точная операция разрешения владельца, проверяемая для обновления secret-broker перед защищённым путём записи.
	ProviderCredentialRenewalAuthorizeOperation = "platform.provider-accounts.credential.renewal.authorize"
	// ProviderCredentialRenewalValidateOperation identifies validation of a recovered renewal observation separately from starting or authorizing a new renewal.
	// ProviderCredentialRenewalValidateOperation обозначает проверку восстановленного наблюдения обновления отдельно от начала или разрешения нового обновления.
	ProviderCredentialRenewalValidateOperation = "platform.provider-accounts.credential.renewal.validate"
)

// ProviderCredentialRenewalTask binds the owner-locked organization, account and credential versions and previous descriptor to a claimant, generation, fence, expiry and two request digests. The external caller does not choose the organization or replace these pins.
// ProviderCredentialRenewalTask связывает закреплённые блокировкой владельца организацию, версии аккаунта и реквизита и прежний дескриптор с исполнителем, поколением, барьером, сроком и двумя дайджестами запроса. Внешний вызывающий не выбирает организацию и не заменяет эти привязки.
type ProviderCredentialRenewalTask struct {
	Ref, OrganizationID, AccountRef, CredentialRef, ClaimantID, Fence string
	AccountVersion, CredentialRevision, Generation                    int64
	Previous                                                          entity.ProviderCredentialDescriptor
	ExpiresAt                                                         time.Time
	State, RequestDigest, OwnerRequestDigest                          string
}

// ProviderCredentialRenewalResult carries renewal state, receipt identity/version and an optional replacement credential descriptor. Recovered is adapter-assigned and excluded from JSON, so a payload cannot claim that readback recovery occurred merely by setting this field.
// ProviderCredentialRenewalResult передаёт состояние обновления, идентичность и версию квитанции и необязательный заменяющий дескриптор реквизита. Recovered назначается адаптером и исключён из JSON, поэтому payload не может заявить о восстановлении контрольным чтением простой установкой поля.
type ProviderCredentialRenewalResult struct {
	// Recovered is assigned by the READ client adapter, not accepted as an RPC or JSON payload field.
	// Recovered назначается READ client adapter, а не принимается как поле RPC или JSON payload.
	Recovered                         bool `json:"-"`
	State, ReceiptUID, ReceiptVersion string
	Credential                        *entity.ProviderCredentialDescriptor
}

// ProviderCredentialRenewalEncoder creates the worker and owner request digests from one pinned task; task state alone is not an authorization token.
// ProviderCredentialRenewalEncoder создаёт дайджесты запроса исполнителя и владельца из одного закреплённого задания; само состояние задания не является токеном авторизации.
type ProviderCredentialRenewalEncoder interface {
	// CredentialRenewalRequestDigests returns the two request digests for the renewal task or an error, keeping both protected routes bound to the same immutable input.
	// CredentialRenewalRequestDigests возвращает два дайджеста запроса задания обновления либо ошибку, связывая оба защищённых маршрута с одним неизменяемым входом.
	CredentialRenewalRequestDigests(ProviderCredentialRenewalTask) (string, string, error)
}

// ProviderCredentialRenewalRepository separates claiming, recovery reads, owner authorization, observation validation, completion and readiness for account-credential renewal. Recovery is an explicit path rather than a blind retry of an uncertain external write.
// ProviderCredentialRenewalRepository разделяет захват, чтение восстановления, разрешение владельца, проверку наблюдения, завершение и готовность обновления реквизита аккаунта. Восстановление является явным путём, а не слепым повтором внешней записи с неопределённым исходом.
type ProviderCredentialRenewalRepository interface {
	// ClaimProviderCredentialRenewalTasks claims eligible account work for the claimant and encodes request digests from owner-pinned tasks under account locking.
	// ClaimProviderCredentialRenewalTasks захватывает допустимую работу аккаунтов для исполнителя и кодирует дайджесты запросов из закреплённых владельцем заданий под блокировкой аккаунтов.
	ClaimProviderCredentialRenewalTasks(context.Context, string, ProviderCredentialRenewalEncoder) ([]ProviderCredentialRenewalTask, error)
	// ListProviderCredentialRenewalRecoveryTasks reads persisted recovery work in the server-resolved organization without creating a fresh renewal claim.
	// ListProviderCredentialRenewalRecoveryTasks читает сохранённую работу восстановления в разрешённой сервером организации без создания нового захвата обновления.
	ListProviderCredentialRenewalRecoveryTasks(context.Context) ([]ProviderCredentialRenewalTask, error)
	// AuthorizeProviderCredentialRenewal checks the principal and exact task against the owner's current account/credential boundary before the renewal write.
	// AuthorizeProviderCredentialRenewal проверяет субъекта и точное задание по актуальной границе аккаунта и реквизита владельца до записи обновления.
	AuthorizeProviderCredentialRenewal(context.Context, value.Principal, ProviderCredentialRenewalTask) error
	// ValidateProviderCredentialRenewalObservation checks whether the principal's readback observation is still bound to the exact owner renewal task; it is not completion or a new write grant.
	// ValidateProviderCredentialRenewalObservation проверяет сохранение привязки наблюдения контрольного чтения субъекта к точному заданию обновления владельца; завершением или новым разрешением записи она не является.
	ValidateProviderCredentialRenewalObservation(context.Context, value.Principal, ProviderCredentialRenewalTask) error
	// CompleteProviderCredentialRenewalTask reconciles the supplied result with the persisted task, receipt and credential pins and records the corresponding owner outcome.
	// CompleteProviderCredentialRenewalTask сопоставляет переданный результат с сохранённым заданием, квитанцией и привязками реквизита и фиксирует соответствующий исход владельца.
	CompleteProviderCredentialRenewalTask(context.Context, ProviderCredentialRenewalTask, ProviderCredentialRenewalResult) error
	// CheckProviderCredentialRenewalReadiness checks the repository's renewal-readiness predicate; it is distinct from performing an OAuth refresh or validating provider credentials remotely.
	// CheckProviderCredentialRenewalReadiness проверяет предикат готовности обновления в репозитории отдельно от OAuth refresh или удалённой проверки реквизитов провайдера.
	CheckProviderCredentialRenewalReadiness(context.Context) error
}
