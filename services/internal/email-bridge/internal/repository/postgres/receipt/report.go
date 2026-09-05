package receipt

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/report__pending.sql
var pendingReportsSQL string

//go:embed sql/report__claim.sql
var claimReportSQL string

//go:embed sql/report__acknowledge.sql
var acknowledgeReportSQL string

//go:embed sql/report__retire.sql
var retireReportSQL string

var _ port.ReportRepository = (*Repository)(nil)

func reportSource(source *port.ReportSource) ([]byte, string, *time.Time, error) {
	if source == nil {
		return nil, "", nil, nil
	}
	if !source.Valid() {
		return nil, "", nil, errs.Invalid
	}
	raw, err := json.Marshal(source)
	if err != nil || len(raw) > 8192 {
		return nil, "", nil, errs.Invalid
	}
	after := source.Binding.Lease.ExpiresAt.Add(port.ReportGrace)
	return raw, api.Digest(source), &after, nil
}

func (r *Repository) PendingReports(ctx context.Context, batch int) ([]port.PendingReport, error) {
	if batch < 1 || batch > 64 {
		return nil, errs.Invalid
	}
	if _, err := r.Pool.Exec(ctx, retireReportSQL, pgx.StrictNamedArgs{"batch": batch}); err != nil {
		return nil, errs.Unavailable
	}
	rows, err := r.Pool.Query(ctx, pendingReportsSQL, pgx.StrictNamedArgs{"batch": batch})
	if err != nil {
		return nil, errs.Unavailable
	}
	defer rows.Close()
	result := make([]port.PendingReport, 0, batch)
	for rows.Next() {
		var p port.PendingReport
		var raw []byte
		var digest string
		x := &p.Record
		if err := rows.Scan(&p.Scope.Tenant, &p.Scope.Mailbox, &x.ID, &x.Key, &x.Digest, &x.Status, &x.Resource,
			&x.Audit.Actor, &x.Audit.Agent, &x.Audit.Grant, &x.Audit.Operation, &x.Audit.ConfigurationRevision,
			&x.Audit.CredentialGeneration, &x.Audit.GateApproved, &x.ReportVersion, &raw, &digest); err != nil {
			return nil, errs.Unavailable
		}
		if len(raw) > 8192 {
			return nil, errs.Unavailable
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&p.Source) != nil || decoder.Decode(new(any)) != io.EOF || !p.Valid() || api.Digest(p.Source) != digest {
			return nil, errs.Unavailable
		}
		result = append(result, p)
	}
	if rows.Err() != nil {
		return nil, errs.Unavailable
	}
	return result, nil
}

func reportArgs(p port.PendingReport) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID,
		"input": p.Record.Digest, "version": p.Record.ReportVersion, "source_digest": api.Digest(p.Source), "status": p.Record.Status}
}

func (r *Repository) ClaimReport(ctx context.Context, p port.PendingReport, interval time.Duration) (bool, error) {
	if !p.Valid() || p.Source.Binding.Lease.ExpiresAt.Add(port.ReportGrace).After(time.Now()) || interval < 5*time.Second || interval > 5*time.Minute {
		return false, errs.Invalid
	}
	args := reportArgs(p)
	args["seconds"] = interval.Seconds()
	tag, err := r.Pool.Exec(ctx, claimReportSQL, args)
	if err != nil {
		return false, errs.Unavailable
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) AcknowledgeReport(ctx context.Context, p port.PendingReport) error {
	if !p.Valid() {
		return errs.Invalid
	}
	args := reportArgs(p)
	args["after"] = p.Source.Binding.Lease.ExpiresAt.Add(port.ReportGrace)
	tag, err := r.Pool.Exec(ctx, acknowledgeReportSQL, args)
	if err != nil {
		return errs.Unavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.Conflict
	}
	return nil
}
