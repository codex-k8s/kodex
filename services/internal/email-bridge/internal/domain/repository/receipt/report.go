package receipt

import (
	"context"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// ReportGrace bounds post-protocol completion and delays background recovery until after the execution lease expires.
// ReportGrace ограничивает фиксацию после протокола и откладывает фоновое восстановление до истечения lease исполнения.
const ReportGrace = 3 * time.Second

// ReportSource retains execution fence and connection lineage only in the private journal, never in audit or public API output.
// ReportSource хранит fence исполнения и происхождение подключения только в закрытом журнале, не в audit или публичном API.
type ReportSource struct {
	Binding    *api.ExecutionBinding `json:"binding"`
	Connection string                `json:"connection"`
}

// Valid checks execution-binding shape, requires invocation lineage and bounds the connection reference; it does not renew the lease.
// Valid проверяет форму execution binding, требует происхождение invocation и ограничивает ссылку подключения; lease не продлевается.
func (s ReportSource) Valid() bool {
	return api.ValidExecutionBinding(s.Binding) && s.Binding.InvocationRef != nil && s.Connection != "" && len(s.Connection) <= 128
}

// PendingReport combines the scoped local receipt, journal version and original private source for a recoverable owner report.
// PendingReport объединяет локальную scoped-квитанцию, версию журнала и исходное закрытое происхождение для восстанавливаемого отчёта владельцу.
type PendingReport struct {
	Scope  Scope
	Record Record
	Source ReportSource
}

// Valid requires nonempty receipt and scope identity, positive report version, valid audit and an invocation-bound source.
// Valid требует непустую идентичность квитанции и области, положительную версию отчёта, допустимый audit и источник с invocation.
func (p PendingReport) Valid() bool {
	return p.Scope.Tenant != "" && p.Scope.Mailbox != "" && p.Source.Valid() && p.Record.ReportVersion > 0 &&
		p.Record.ID != "" && p.Record.Key != "" && p.Record.Digest != "" && p.Record.Audit.Valid()
}

// Report materializes the original invocation and immutable receipt digest, deriving idempotency from digest and observed outcome.
// The caller must validate the pending report first; this mapper dereferences its invocation binding directly.
// Report материализует исходный invocation и неизменяемый дайджест квитанции, выводя идемпотентность из дайджеста и наблюдаемого исхода.
// Вызывающий обязан предварительно проверить отчёт: преобразователь напрямую разыменовывает привязку invocation.
func (p PendingReport) Report(replay bool) Report {
	owner := OwnerReceipt{Invocation: *p.Source.Binding.InvocationRef, ExternalRef: p.Record.ID,
		ExternalDigest: p.Record.ExternalDigest(p.Scope), InputDigest: p.Record.Digest, EffectKey: p.Record.Key,
		Mailbox: p.Scope.Mailbox, Connection: p.Source.Connection, ConfigurationRevision: p.Record.Audit.ConfigurationRevision,
		Outcome: p.Record.Outcome()}
	key := api.Digest(struct {
		Digest  string
		Outcome Outcome
	}{owner.ExternalDigest, owner.Outcome})
	return Report{Binding: p.Source.Binding, Receipt: owner, IdempotencyKey: key, Replay: replay}
}

// ReportRepository atomically journals effect reservation/completion and conditionally claims exact report versions for recovery.
// ReportRepository атомарно журналирует резервирование и завершение эффекта и условно захватывает точные версии отчёта для восстановления.
type ReportRepository interface {
	// ReserveEffect creates or reuses a scoped effect receipt while retaining its original private invocation source.
	// ReserveEffect создаёт либо повторно читает scoped-квитанцию эффекта, сохраняя исходное закрытое происхождение invocation.
	ReserveEffect(context.Context, Scope, Record, ReportSource) (Record, bool, error)
	// CompleteEffect stores protocol outcome and coordinates with the next report version before owner acknowledgement.
	// CompleteEffect сохраняет исход протокола и координаты со следующей версией отчёта до подтверждения владельцем.
	CompleteEffect(context.Context, Scope, Record, string, ReportSource) (Record, error)
	// PendingReports reads a bounded validated batch of unacknowledged observations from the private report journal.
	// PendingReports читает ограниченную проверенную пачку неподтверждённых наблюдений из закрытого журнала отчётов.
	PendingReports(context.Context, int) ([]PendingReport, error)
	// ClaimReport reserves only the exact eligible journal version for a bounded recovery interval.
	// ClaimReport резервирует только точную доступную версию журнала на ограниченный интервал восстановления.
	ClaimReport(context.Context, PendingReport, time.Duration) (bool, error)
	// AcknowledgeReport conditionally marks the exact reported version acknowledged without discarding newer observations.
	// AcknowledgeReport условно подтверждает точную переданную версию, не отбрасывая более новые наблюдения.
	AcknowledgeReport(context.Context, PendingReport) error
}
