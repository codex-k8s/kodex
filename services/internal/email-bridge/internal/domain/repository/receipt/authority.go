package receipt

import (
	"context"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// Outcome describes evidence about a reserved external effect, not whether an email reached its recipient.
// Outcome описывает свидетельство о зарезервированном внешнем эффекте, не доставку письма получателю.
type Outcome string

const (
	// Unknown preserves uncertainty and cannot authorize an automatic resend.
	// Unknown сохраняет неопределённость и не разрешает автоматическую повторную отправку.
	Unknown Outcome = "UNKNOWN_OUTCOME"
	// EffectConfirmed records positive evidence of the protocol effect, not final recipient delivery.
	// EffectConfirmed фиксирует положительное свидетельство эффекта протокола, не конечную доставку получателю.
	EffectConfirmed Outcome = "EFFECT_CONFIRMED"
	// NoEffectConfirmed records a confirmed absence of the reserved effect, distinct from an unknown transport failure.
	// NoEffectConfirmed фиксирует подтверждённое отсутствие эффекта, в отличие от неопределённого сбоя транспорта.
	NoEffectConfirmed Outcome = "NO_EFFECT_CONFIRMED"
)

// OwnerReceipt pins the authoritative owner's versioned observation to invocation, mailbox, connection and immutable effect input.
// OwnerReceipt связывает версионированное наблюдение владельца с invocation, ящиком, подключением и неизменяемым входом эффекта.
type OwnerReceipt struct {
	Ref                                                             string
	Version                                                         int64
	Invocation, ExternalRef, ExternalDigest, InputDigest, EffectKey string
	Mailbox, Connection                                             string
	ConfigurationRevision                                           int64
	Outcome                                                         Outcome
}

// Report carries private execution lineage and an idempotent owner observation; Replay distinguishes recovery from the original call.
// Report передаёт закрытое происхождение исполнения и идемпотентное наблюдение владельцу; Replay отличает восстановление от исходного вызова.
type Report struct {
	Binding        *api.ExecutionBinding
	Receipt        OwnerReceipt
	IdempotencyKey string
	Replay         bool
}

// Decision binds a time-limited owner reconciliation outcome and approving authority to one exact owner receipt.
// Decision связывает ограниченное по времени решение владельца и его полномочия с одной точной квитанцией владельца.
type Decision struct {
	Ref, Actor, Grant string
	Version           int64
	Receipt           OwnerReceipt
	Outcome           Outcome
	ExpiresAt         time.Time
}

// EffectAuthority separates authoritative reporting and human reconciliation from local protocol execution.
// EffectAuthority отделяет отчёт владельцу и сверку по решению человека от локального исполнения протокола.
type EffectAuthority interface {
	// Report submits the exact effect observation and returns its authoritative versioned receipt.
	// Report передаёт точное наблюдение эффекта и возвращает авторитетную версионированную квитанцию.
	Report(context.Context, Report) (OwnerReceipt, error)
	// Reconcile requests the owner's decision for the exact receipt and supplied idempotency key, without resending mail.
	// Reconcile запрашивает решение владельца для точной квитанции и переданного ключа идемпотентности, не отправляя письмо повторно.
	Reconcile(context.Context, OwnerReceipt, string) (Decision, error)
}

// Pending joins an unresolved local receipt with its exact owner observation for later reconciliation.
// Pending связывает неразрешённую локальную квитанцию с точным наблюдением владельца для последующей сверки.
type Pending struct {
	Scope  Scope
	Record Record
	Owner  OwnerReceipt
}

// ReconciliationRepository durably schedules and fences resolution of uncertain effects, never initiating protocol retries.
// ReconciliationRepository устойчиво планирует и ограничивает разрешение неопределённых эффектов, не запуская повторы протокола.
type ReconciliationRepository interface {
	// Remember stores a matching owner observation and its earliest reconciliation time beside the exact local receipt.
	// Remember сохраняет совпадающее наблюдение владельца и раннее время сверки рядом с точной локальной квитанцией.
	Remember(context.Context, Scope, Record, OwnerReceipt, time.Time) error
	// Pending reads a bounded batch of unresolved receipts eligible for reconciliation.
	// Pending читает ограниченную пачку неразрешённых квитанций, доступных для сверки.
	Pending(context.Context, int) ([]Pending, error)
	// ClaimReconciliation conditionally reserves the matching pending observation for the requested retry interval.
	// ClaimReconciliation условно резервирует совпадающее ожидающее наблюдение на запрошенный интервал повтора сверки.
	ClaimReconciliation(context.Context, Pending, time.Duration) (bool, error)
	// CommitReconciliation applies an exact fresh owner decision and reports whether durable state changed rather than replayed.
	// CommitReconciliation применяет точное свежее решение владельца и сообщает об изменении устойчивого состояния, а не повторе.
	CommitReconciliation(context.Context, Pending, Decision) (bool, error)
}

// Same compares every receipt field, including version and outcome, without treating a shared reference as equivalence.
// Same сравнивает все поля квитанции, включая версию и исход, не считая общую ссылку достаточным равенством.
func (a OwnerReceipt) Same(b OwnerReceipt) bool {
	return a == b
}

// Valid requires an unknown local/owner outcome and exact scope, input, audit and external-identity correlations.
// These structural correlations do not verify signatures or replace the authenticated owner client.
// Valid требует неопределённый исход у локальной и внешней квитанций и точную связь области, входа, audit и внешней идентичности.
// Структурная связность не проверяет подписи и не заменяет аутентифицированный клиент владельца.
func (p Pending) Valid() bool {
	r, o := p.Record, p.Owner
	return p.Scope.Tenant != "" && p.Scope.Mailbox != "" && r.Status == "unknown" && r.Audit.Valid() && o.Ref != "" && o.Version > 0 && o.Invocation != "" && o.Connection != "" && o.Mailbox == p.Scope.Mailbox && o.ExternalRef == r.ID && o.ExternalDigest == r.ExternalDigest(p.Scope) && o.InputDigest == r.Digest && o.EffectKey == r.Key && o.ConfigurationRevision == r.Audit.ConfigurationRevision && o.Outcome == Unknown
}

// ValidFor accepts only a conclusive decision for the complete pending receipt with actor/grant pins and over 100 ms of remaining lifetime.
// ValidFor принимает только определённое решение для полной ожидающей квитанции с actor/grant и запасом срока более 100 мс.
func (d Decision) ValidFor(p Pending, now time.Time) bool {
	return p.Valid() && d.Receipt.Same(p.Owner) && d.Ref != "" && d.Version > 0 && d.Actor != "" && d.Grant != "" && (d.Outcome == EffectConfirmed || d.Outcome == NoEffectConfirmed) && d.ExpiresAt.After(now.Add(100*time.Millisecond))
}
