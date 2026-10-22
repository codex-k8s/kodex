package entity

import "time"

const (
	// ConfigurationSourceQueued marks Git-source synchronization waiting for an owner-issued work claim.
	// Being queued does not itself authorize remote access.
	// ConfigurationSourceQueued обозначает синхронизацию Git-источника, ожидающую захвата работы
	// владельцем. Ожидание само по себе не разрешает удалённый доступ.
	ConfigurationSourceQueued = "QUEUED"
	// ConfigurationSourceClaimed marks synchronization claimed under a source generation and fenced lease.
	// Completion still requires matching current work and eligibility.
	// ConfigurationSourceClaimed обозначает захваченную синхронизацию в рамках поколения источника и
	// ограждённой аренды. Завершение требует совпадения текущей работы и допустимости.
	ConfigurationSourceClaimed = "CLAIMED"
	// ConfigurationSourceReady marks a successfully accepted source snapshot. It does not imply that a
	// downstream image is built, admitted, or promoted.
	// ConfigurationSourceReady обозначает успешно принятый снимок источника. Это не означает, что
	// зависимый образ собран, допущен или опубликован.
	ConfigurationSourceReady = "READY"
	// ConfigurationSourceBlocked marks source synchronization blocked by a recorded failure. Previously
	// accepted pins remain distinguishable from a newly accepted revision.
	// ConfigurationSourceBlocked обозначает синхронизацию источника, заблокированную зафиксированной
	// ошибкой. Ранее принятые привязки отличаются от новой принятой ревизии.
	ConfigurationSourceBlocked = "SYNC_BLOCKED"
	// ConfigurationSourceDetached marks a configuration disconnected from its managed Git source. The
	// value names a lifecycle state and does not authorize detachment.
	// ConfigurationSourceDetached обозначает конфигурацию, отключённую от управляемого Git-источника.
	// Значение называет состояние жизненного цикла, но не разрешает отключение.
	ConfigurationSourceDetached = "DETACHED"
	// ConfigurationSourceUnavailable classifies a temporary source-service failure. The owner may retry
	// only within current eligibility, attempt, and deadline limits; the code is not an unconditional
	// retry instruction.
	// ConfigurationSourceUnavailable классифицирует временную недоступность сервиса источника. Владелец
	// допускает повтор только в пределах текущей допустимости, попыток и срока; код не является
	// безусловной инструкцией повтора.
	ConfigurationSourceUnavailable = "UNAVAILABLE"
	// ConfigurationSourceCredential classifies rejected credentials during source synchronization without
	// exposing credential values.
	// ConfigurationSourceCredential классифицирует отказ реквизитов при синхронизации источника без
	// раскрытия их значений.
	ConfigurationSourceCredential = "CREDENTIAL_REJECTED"
	// ConfigurationSourceAccess classifies denied or lost eligibility to access the configured Git source.
	// It must not be converted into an empty successful synchronization.
	// ConfigurationSourceAccess классифицирует запрещённый или утраченный доступ к настроенному
	// Git-источнику. Его нельзя преобразовывать в пустую успешную синхронизацию.
	ConfigurationSourceAccess = "ACCESS_DENIED"
	// ConfigurationSourceNotFound classifies a missing configured source resource. It differs from
	// successful retrieval of an empty configuration.
	// ConfigurationSourceNotFound классифицирует отсутствие настроенного ресурса источника. Это отличается
	// от успешного получения пустой конфигурации.
	ConfigurationSourceNotFound = "NOT_FOUND"
	// ConfigurationSourceDiverged classifies divergence from the expected source history or content
	// relationship. The owner must resolve the conflict rather than silently accepting a different
	// predecessor.
	// ConfigurationSourceDiverged классифицирует расхождение с ожидаемой историей источника или связью
	// содержимого. Владелец разрешает конфликт, а не незаметно принимает другого предшественника.
	ConfigurationSourceDiverged = "DIVERGED"
	// ConfigurationSourceContent classifies source content rejected by validation. Retrieval alone does
	// not make that content an accepted managed revision.
	// ConfigurationSourceContent классифицирует содержимое источника, отклонённое проверкой. Само
	// получение не делает это содержимое принятой управляемой ревизией.
	ConfigurationSourceContent = "CONTENT_INVALID"
	// ConfigurationSourceResponse classifies an invalid source response separately from invalid
	// configuration content. The owner must not treat a malformed response as a successful empty result.
	// ConfigurationSourceResponse классифицирует некорректный ответ источника отдельно от некорректного
	// содержимого конфигурации. Владелец не должен считать повреждённый ответ успешным пустым результатом.
	ConfigurationSourceResponse = "RESPONSE_INVALID"
)

// ManagedConfigurationGitSource presents configured repository selectors, accepted commit and content
// pins, synchronization state, and aggregate and source generations. This public projection excludes
// private credential descriptors; it does not itself verify source provenance.
// ManagedConfigurationGitSource представляет настроенные признаки репозитория, принятые привязки
// коммита и содержимого, состояние синхронизации, версию агрегата и поколение источника. Публичная
// проекция исключает закрытые дескрипторы реквизитов, но сама не проверяет происхождение источника.
type ManagedConfigurationGitSource struct {
	Ref, ConnectionRef, ProviderKey, RepositoryRef, RefName, Path string
	State, AcceptedCommitSHA, AcceptedContentSHA256               string
	AcceptedRevisionRef, FailureCode                              string
	Version, Generation                                           int64
	SyncedAt                                                      *time.Time
}

// ManagedConfigurationSourceLease pins source work to its claimant, fence, source generation, attempt,
// claim generation, and expiry. The owner compares the complete tuple against stored work; possessing
// copied fields is not authorization.
// ManagedConfigurationSourceLease закрепляет работу источника за исполнителем, барьером, поколением
// источника, попыткой, поколением захвата и сроком. Владелец сравнивает полный набор с сохранённой
// работой; наличие копии полей не предоставляет полномочия.
type ManagedConfigurationSourceLease struct {
	WorkRef, Claimant, Fence                   string
	SourceGeneration, Attempt, ClaimGeneration int64
	ExpiresAt                                  time.Time
}

// ManagedConfigurationSourceWork is the private synchronization envelope issued to the exact
// integration gateway, combining lease, definition package, credential descriptor, source pins, byte
// limit, and deadline. It is not a public catalogue entry or permission to fetch arbitrary URLs.
// ManagedConfigurationSourceWork является закрытым заданием синхронизации для точного шлюза интеграций
// и объединяет аренду, пакет определения, дескриптор реквизитов, привязки источника, лимит байтов и
// срок. Это не публичный элемент каталога и не разрешение загружать произвольные URL.
type ManagedConfigurationSourceWork struct {
	Lease                                                          ManagedConfigurationSourceLease
	SourceRef, ConfigurationRef, Kind, ConnectionRef               string
	DefinitionKey, DefinitionVersion, DefinitionDigest             string
	RepositoryRef, RefName, Path, PreviousCommitSHA, ContentFormat string
	ConnectionVersion                                              int64
	MaximumContentBytes                                            int32
	Deadline                                                       time.Time
	PublicConfiguration                                            map[string]any
	CredentialRevision                                             IntegrationCredentialRevision
	DefinitionPackage                                              []byte
}
