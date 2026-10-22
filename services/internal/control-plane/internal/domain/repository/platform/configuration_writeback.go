package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ConfigurationWriteBackEffectInput pins one leased write-back effect to candidate commit/tree/blob, parent and base blob, content digest and optional pull-request reference/URL. The record describes an effect for owner validation; a URL alone is not confirmation that an external write succeeded.
// ConfigurationWriteBackEffectInput закрепляет один арендованный эффект обратной записи за кандидатными коммитом, деревом и blob, родителем и исходным blob, дайджестом содержимого и необязательными ссылкой и URL pull request. Структура описывает эффект для проверки владельцем; один URL не подтверждает успешную внешнюю запись.
type ConfigurationWriteBackEffectInput struct {
	Lease                                                          entity.ConfigurationWriteBackLease
	Effect, CandidateCommitSHA, CandidateTreeSHA, CandidateBlobSHA string
	ParentCommitSHA, ContentSHA256, PullRequestRef, PullRequestURL string
	BaseBlobSHA                                                    string
}

// ConfigurationWriteBackRepository separates authorized proposal reads, task claims and leases, and begin/complete receipts for external write-back effects. Persistence owns the state and replay boundary; callers do not create authority by selecting a proposal reference.
// ConfigurationWriteBackRepository разделяет разрешённое чтение предложений, захваты и аренды заданий и квитанции начала и завершения внешних эффектов обратной записи. Хранилище владеет состоянием и границей повтора; выбор ссылки предложения не создаёт полномочий вызывающего.
type ConfigurationWriteBackRepository interface {
	// GetConfigurationWriteBack reads one authorized write-back proposal with its view metadata, without beginning the external effect.
	// GetConfigurationWriteBack читает одно разрешённое предложение обратной записи с метаданными представления, не начиная внешний эффект.
	GetConfigurationWriteBack(context.Context, value.Principal, string) (entity.ConfigurationWriteBackView, error)
	// ListConfigurationWriteBacks pages proposals within the supplied resource boundary and filter, returning a continuation cursor and total count.
	// ListConfigurationWriteBacks постранично читает предложения в переданной границе ресурса с фильтром, возвращая курсор продолжения и общее число.
	ListConfigurationWriteBacks(context.Context, value.Principal, string, query.Filter) ([]entity.ConfigurationWriteBack, string, int64, error)
	// ClaimConfigurationWriteBackWork reserves a bounded batch of eligible proposals for the principal and claimant; a successful read is not an alternative claim.
	// ClaimConfigurationWriteBackWork резервирует ограниченную пачку допустимых предложений для субъекта и исполнителя; успешное чтение не заменяет захват.
	ClaimConfigurationWriteBackWork(context.Context, value.Principal, string, int32) ([]entity.ConfigurationWriteBackWork, error)
	// RenewConfigurationWriteBackWork refreshes the exact proposal lease under the principal's owner boundary while retaining its task identity.
	// RenewConfigurationWriteBackWork обновляет точную аренду предложения в границе владельца субъекта с сохранением идентичности задания.
	RenewConfigurationWriteBackWork(context.Context, value.Principal, entity.ConfigurationWriteBackLease) (entity.ConfigurationWriteBackLease, error)
	// BeginConfigurationWriteBackEffect records the fenced effect-start intent before the external action and returns the proposal plus a replay indicator. A matching prior receipt is distinct from permission to perform the action twice.
	// BeginConfigurationWriteBackEffect фиксирует намерение начала эффекта с барьером до внешнего действия и возвращает предложение и признак повтора. Соответствующая прежняя квитанция отличается от разрешения выполнить действие дважды.
	BeginConfigurationWriteBackEffect(context.Context, value.Principal, ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error)
	// CompleteConfigurationWriteBackEffect records the result of the exact begun effect using its lease and commit/blob or pull-request evidence and returns the updated proposal.
	// CompleteConfigurationWriteBackEffect фиксирует результат точного начатого эффекта по аренде и доказательствам коммита и blob либо pull request и возвращает обновлённое предложение.
	CompleteConfigurationWriteBackEffect(context.Context, value.Principal, ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error)
	// FailConfigurationWriteBackWork records a failure for the exact leased proposal; it must not be interpreted as proof that an already attempted external write was undone.
	// FailConfigurationWriteBackWork фиксирует ошибку точного арендованного предложения; её нельзя считать доказательством отмены уже предпринятой внешней записи.
	FailConfigurationWriteBackWork(context.Context, value.Principal, entity.ConfigurationWriteBackLease, string) (entity.ConfigurationWriteBack, error)
}
