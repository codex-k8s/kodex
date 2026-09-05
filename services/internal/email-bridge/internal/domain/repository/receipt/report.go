package receipt

import (
	"context"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

const ReportGrace = 3 * time.Second

// Binding содержит fence и хранится только в закрытом журнале, не в audit/API.
type ReportSource struct {
	Binding    *api.ExecutionBinding `json:"binding"`
	Connection string                `json:"connection"`
}

func (s ReportSource) Valid() bool {
	return api.ValidExecutionBinding(s.Binding) && s.Binding.InvocationRef != nil && s.Connection != "" && len(s.Connection) <= 128
}

type PendingReport struct {
	Scope  Scope
	Record Record
	Source ReportSource
}

func (p PendingReport) Valid() bool {
	return p.Scope.Tenant != "" && p.Scope.Mailbox != "" && p.Source.Valid() && p.Record.ReportVersion > 0 &&
		p.Record.ID != "" && p.Record.Key != "" && p.Record.Digest != "" && p.Record.Audit.Valid()
}

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

type ReportRepository interface {
	ReserveEffect(context.Context, Scope, Record, ReportSource) (Record, bool, error)
	CompleteEffect(context.Context, Scope, Record, string, ReportSource) (Record, error)
	PendingReports(context.Context, int) ([]PendingReport, error)
	ClaimReport(context.Context, PendingReport, time.Duration) (bool, error)
	AcknowledgeReport(context.Context, PendingReport) error
}
