package entity

import (
	"encoding/json"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// EmailMailboxDiagnostic locates a mailbox configuration issue by code, field path, message, and
// optional source line and column. It is diagnostic output, not configuration content or an
// instruction to bypass validation.
// EmailMailboxDiagnostic определяет проблему конфигурации ящика кодом, путём поля, сообщением и
// необязательными строкой и столбцом источника. Это диагностический результат, а не конфигурация или
// указание обойти проверку.
type EmailMailboxDiagnostic struct {
	Code, Path, Message string
	Line, Column        int32
}

// EmailMailboxPublication records a mailbox publication revision, digest, configuration revision
// reference, state, failure code, and readiness time. Creation of the publication record is distinct
// from successful application and readiness.
// EmailMailboxPublication фиксирует ревизию публикации ящика, дайджест, ссылку на ревизию
// конфигурации, состояние, код ошибки и время готовности. Создание записи публикации отличается от
// успешного применения и готовности.
type EmailMailboxPublication struct {
	Ref, Digest, State, ConfigurationRevisionRef, FailureCode string
	Revision                                                  int64
	CreatedAt, ReadyAt                                        time.Time
}

// EmailMailboxConfigurationView combines the exact connection version and mailbox binding with managed
// configuration, revision, specification, optional publication, diagnostics, and available actions. It
// is a configuration read model, not approval of an outgoing message.
// EmailMailboxConfigurationView объединяет точную версию подключения и привязку ящика с управляемой
// конфигурацией, ревизией, спецификацией, необязательной публикацией, диагностикой и действиями. Это
// модель чтения конфигурации, а не подтверждение исходящего сообщения.
type EmailMailboxConfigurationView struct {
	ConnectionRef, MailboxRef, BoundRevisionRef string
	ConnectionVersion                           int64
	Configuration                               ManagedConfigurationSet
	Revision                                    ManagedConfigurationRevision
	Specification                               EmailMailboxSpecification
	Publication                                 *EmailMailboxPublication
	Diagnostics                                 []EmailMailboxDiagnostic
	NextActions                                 []EmailMailboxActionAvailability
}

// EmailMailboxPage returns mailbox configuration views with total, opaque pagination, and page-level
// action availability. Pagination retains the owner's eligible set; advertised actions still require
// authorization when invoked.
// EmailMailboxPage возвращает представления конфигураций ящиков с количеством, непрозрачной пагинацией
// и доступностью действий страницы. Пагинация сохраняет допустимый набор владельца; объявленные
// действия проверяются при вызове.
type EmailMailboxPage struct {
	Items         []EmailMailboxConfigurationView
	Total         int64
	NextPageToken string
	NextActions   []EmailMailboxActionAvailability
}

// EmailMailboxActionAvailability describes whether a mailbox action is currently available and why it
// may be disabled. The flag is presentation state and does not replace the owner's command checks.
// EmailMailboxActionAvailability описывает текущую доступность действия ящика и причину запрета.
// Признак является состоянием представления и не заменяет проверки команды владельцем.
type EmailMailboxActionAvailability struct {
	Action, Reason string
	Enabled        bool
}

// EmailMailboxPreview returns an optional parsed specification, canonical YAML, diagnostics, and
// validity result without publishing it. Valid describes preview validation, not a configured live
// mailbox or approval to send.
// EmailMailboxPreview возвращает необязательную разобранную спецификацию, канонический YAML,
// диагностику и результат проверки без публикации. Valid описывает проверку предпросмотра, а не
// настроенный действующий ящик или разрешение отправки.
type EmailMailboxPreview struct {
	Specification *EmailMailboxSpecification
	CanonicalYAML string
	Diagnostics   []EmailMailboxDiagnostic
	Valid         bool
}

// EmailMailboxPublicationWork is internal work for the local control-plane reconciler, carrying claim
// generation, exact configuration and policy bytes, application and callback progress, and deadline.
// It is not a public API projection; progress flags must be accepted through the owner's lifecycle.
// EmailMailboxPublicationWork является внутренней работой локального reconciler control-plane и
// передаёт поколение захвата, точные байты конфигурации и политики, ход применения и обратного вызова
// и срок. Это не публичная проекция API; признаки хода принимаются через жизненный цикл владельца.
type EmailMailboxPublicationWork struct {
	Ref, Claimant, State string
	ClaimGeneration      int64
	Configuration        api.Configuration
	PolicyDocument       json.RawMessage
	Applied, Callback    bool
	ExpiresAt            time.Time
}
