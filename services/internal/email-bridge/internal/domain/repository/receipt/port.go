package receipt

import (
	"context"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// Scope keys receipt storage by tenant and mailbox; the struct alone does not authenticate either identifier.
// Scope задаёт ключ хранения квитанций по tenant и ящику; структура сама не удостоверяет эти идентификаторы.
type Scope struct{ Tenant, Mailbox string }

// Audit retains the owner-resolved actor, agent, grant and configuration pins used when reserving an effect.
// Audit сохраняет разрешённые владельцем actor, agent, grant и настройки на момент резервирования эффекта.
type Audit struct {
	Actor, Agent, Grant                         string
	Operation                                   api.Operation
	ConfigurationRevision, CredentialGeneration int64
	GateApproved                                bool
}

// Valid checks required identity fields, operation membership and positive revisions, not grant authority or gate freshness.
// Valid проверяет обязательные идентификаторы, допустимость операции и положительные ревизии, не полномочия grant или свежесть gate.
func (a Audit) Valid() bool {
	return a.Actor != "" && a.Agent != "" && a.Grant != "" && a.Operation.Valid() && a.ConfigurationRevision > 0 && a.CredentialGeneration > 0
}

// Record combines immutable effect identity with mutable protocol outcome and the report journal version.
// Record объединяет неизменяемую идентичность эффекта, изменяемый результат протокола и версию журнала отчёта.
type Record struct {
	Audit                                                         Audit
	ID, Key, Digest, Status, Resource, UID, Folder, ContentDigest string
	UIDValidity                                                   uint32
	ReportVersion                                                 int64
}

// Result exposes only status, receipt reference and protocol resource coordinates, without audit or private report lineage.
// Result раскрывает только статус, ссылку на квитанцию и координаты ресурса протокола, без audit и закрытого происхождения отчёта.
func (r Record) Result() api.Result {
	return api.Result{Status: r.Status, MessageId: r.ID, Uid: r.UID, UidValidity: r.UIDValidity, Folder: r.Folder, ContentDigest: r.ContentDigest}
}

// Repository owns scoped receipt idempotency and configuration readback; protocol I/O remains with the provider port.
// Repository владеет scoped-идемпотентностью квитанций и сверкой конфигурации; сетевой протокол остаётся в порту провайдера.
type Repository interface {
	// Reserve creates a receipt or reads the matching effect-key receipt; a reused key with a different input must conflict.
	// Reserve создаёт квитанцию либо читает совпавшую по ключу эффекта; иной вход с тем же ключом должен вызвать конфликт.
	Reserve(context.Context, Scope, string, string, string, string, Audit) (Record, bool, error)
	// Complete persists an allowed protocol status and coordinates for an existing exact scoped receipt.
	// Complete сохраняет допустимый статус протокола и координаты существующей точной квитанции в заданной области.
	Complete(context.Context, Scope, Record, string) error
	// Get resolves a receipt by ID or effect key inside the supplied tenant/mailbox scope.
	// Get разрешает квитанцию по ID либо ключу эффекта внутри переданной области tenant/ящика.
	Get(context.Context, Scope, string, string) (Record, error)
	// Configuration checks the selected configuration revision and digest against the repository's durable state.
	// Configuration сверяет выбранные ревизию и дайджест конфигурации с устойчивым состоянием репозитория.
	Configuration(context.Context, api.Configuration, string) error
	// Ready checks the receipt storage prerequisite, not mailbox login, provider connectivity or delivery.
	// Ready проверяет готовность хранилища квитанций, не вход в ящик, связь с провайдером или доставку.
	Ready(context.Context) error
}
