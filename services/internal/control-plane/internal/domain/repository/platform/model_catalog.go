package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ProviderModelCatalogOperation is the exact protected operation name for observing an account's model catalog, separate from arbitrary account updates or model execution.
// ProviderModelCatalogOperation — точное имя защищённой операции наблюдения каталога моделей аккаунта, отдельное от произвольного изменения аккаунта или запуска модели.
const ProviderModelCatalogOperation = "platform.provider-accounts.model-catalog.observe"

// ProviderModelCatalogTask is an owner-claimed catalog observation pinned to organization, account version, provider, exact credential descriptor and revision, claimant, fence, generation, request digest and expiry. Credential is a descriptor, not raw authorization material.
// ProviderModelCatalogTask — захваченное владельцем наблюдение каталога, закреплённое за организацией, версией аккаунта, провайдером, точным дескриптором и ревизией реквизита, исполнителем, барьером, поколением, дайджестом запроса и сроком. Credential является дескриптором, а не исходным материалом авторизации.
type ProviderModelCatalogTask struct {
	Ref, OrganizationID, AccountRef, ProviderDefinitionKey string
	AccountVersion                                         int64
	CredentialRef                                          string
	CredentialRevision                                     int64
	Credential                                             entity.ProviderCredentialDescriptor
	AuthorizationMethod                                    string
	ClaimantID, Fence, RequestDigest                       string
	ClaimGeneration                                        int64
	ExpiresAt                                              time.Time
}

// ProviderModelCatalogRecord is the serialized model entry with its identifier, supported reasoning efforts, default effort and default-model flag. The data shape alone neither verifies provider provenance nor authorizes a model run.
// ProviderModelCatalogRecord — сериализуемая запись модели с идентификатором, поддерживаемыми степенями рассуждения, степенью по умолчанию и признаком основной модели. Сама форма данных не проверяет происхождение от провайдера и не разрешает запуск модели.
type ProviderModelCatalogRecord struct {
	ID                     string   `json:"id"`
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	ReasoningEfforts       []string `json:"reasoningEfforts"`
	IsDefault              bool     `json:"isDefault"`
}

// ProviderModelCatalogObservation reports a catalog or bounded failure for the task's account and credential, with source and observation time. The owning repository validates task pins and freshness before storing the models.
// ProviderModelCatalogObservation сообщает каталог или ограниченную ошибку для аккаунта и реквизита задания с источником и временем наблюдения. Владелец репозитория проверяет привязки задания и свежесть перед сохранением моделей.
type ProviderModelCatalogObservation struct {
	AccountRef, CredentialRef, Source, Failure string
	ObservedAt                                 time.Time
	Models                                     []ProviderModelCatalogRecord
}

// ProviderModelCatalogEncoder is supplied by composition root to encode the request digest from an owner-built task while authoritative rows are locked, before commit. Browser input must not implement this owner-side encoding boundary.
// ProviderModelCatalogEncoder передаётся корнем сборки для вычисления дайджеста запроса из задания владельца под блокировкой авторитетных строк до commit. Браузерный ввод не должен реализовывать эту границу кодирования владельца.
type ProviderModelCatalogEncoder interface {
	// ModelCatalogRequestDigest derives the protected request digest from the complete pinned catalog task; its error prevents use of an unbound request.
	// ModelCatalogRequestDigest получает дайджест защищённого запроса из полного закреплённого задания каталога; ошибка не допускает использование непривязанного запроса.
	ModelCatalogRequestDigest(ProviderModelCatalogTask) (string, error)
}

// ProviderModelCatalogRepository owns catalog-task claim and completion, keeping the account/credential snapshot and encoded request tied to the persisted claim.
// ProviderModelCatalogRepository владеет захватом и завершением задания каталога, сохраняя связь снимка аккаунта и реквизита и закодированного запроса с сохранённым захватом.
type ProviderModelCatalogRepository interface {
	// ClaimProviderModelCatalogTasks claims a bounded batch for the claimant and uses the owner encoder to bind each task's request digest before returning work.
	// ClaimProviderModelCatalogTasks захватывает ограниченную пачку для исполнителя и использует кодировщик владельца для привязки дайджеста запроса каждого задания до возврата работы.
	ClaimProviderModelCatalogTasks(context.Context, string, int32, ProviderModelCatalogEncoder) ([]ProviderModelCatalogTask, error)
	// CompleteProviderModelCatalogTask validates the observation against the exact claim, account/credential versions and freshness, then stores the catalog result or failure. Replayed completion must refer to the same stored request and receipt.
	// CompleteProviderModelCatalogTask проверяет наблюдение по точному захвату, версиям аккаунта и реквизита и свежести, затем сохраняет результат каталога либо ошибку. Повторное завершение должно относиться к тем же сохранённым запросу и квитанции.
	CompleteProviderModelCatalogTask(context.Context, ProviderModelCatalogTask, ProviderModelCatalogObservation) error
}
