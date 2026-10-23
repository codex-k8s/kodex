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

// pendingReportsSQL reads a bounded ordered batch of due private reports with their exact receipt status, source digest and report version.
// pendingReportsSQL читает ограниченную упорядоченную пачку доступных закрытых отчётов с точными статусом квитанции, дайджестом источника и версией отчёта.
//
//go:embed sql/report__pending.sql
var pendingReportsSQL string

// claimReportSQL reschedules only a still-pending exact input/version/source/status tuple that is already due.
// claimReportSQL переносит только ещё ожидающий точный кортеж input/version/source/status, срок которого уже наступил.
//
//go:embed sql/report__claim.sql
var claimReportSQL string

// acknowledgeReportSQL clears the exact pending version and retires its private source only after protocol completion or the source grace deadline.
// acknowledgeReportSQL снимает ожидание точной версии и убирает её закрытый источник лишь после завершения протокола либо grace-срока источника.
//
//go:embed sql/report__acknowledge.sql
var acknowledgeReportSQL string

// retireReportSQL clears due private sources of already-acknowledged reports in a bounded SKIP LOCKED batch, preserving pending recovery input.
// retireReportSQL очищает доступные закрытые источники уже подтверждённых отчётов ограниченной SKIP LOCKED пачкой, сохраняя вход ожидающего восстановления.
//
//go:embed sql/report__retire.sql
var retireReportSQL string

var _ port.ReportRepository = (*Repository)(nil)

// reportSource encodes a valid private invocation source within 8 KiB and derives its correlation digest and lease-plus-grace recovery time.
// A nil source means no new journal input; returned bytes include the fence and must remain inside private receipt storage.
// reportSource кодирует допустимый закрытый источник invocation в пределах 8 КиБ, выводя дайджест связи и время восстановления lease-plus-grace.
// Nil означает отсутствие нового входа журнала; байты содержат fence и должны оставаться внутри закрытого хранилища квитанций.
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

// PendingReports first retires due acknowledged source data, then reads 1..64 pending entries and validates bounded JSON, trailing EOF, shape and source digest.
// Unknown JSON fields, malformed/partial rows or any iteration failure reject the whole returned batch; decoding is not a signature verification.
// PendingReports сначала убирает доступные подтверждённые источники, затем читает 1..64 ожидающих записи и проверяет ограниченный JSON, конечный EOF, форму и source digest.
// Неизвестные JSON-поля, неверные или частичные строки и любой отказ iteration отклоняют всю возвращаемую пачку; разбор не проверяет подпись.
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

// reportArgs builds the exact tenant/mailbox/receipt/input/version/source/status guard shared by report claim and acknowledgement queries.
// reportArgs формирует точную защиту tenant/mailbox/receipt/input/version/source/status для запросов claim и подтверждения отчёта.
func reportArgs(p port.PendingReport) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID,
		"input": p.Record.Digest, "version": p.Record.ReportVersion, "source_digest": api.Digest(p.Source), "status": p.Record.Status}
}

// ClaimReport requires a valid original source past lease-plus-grace and a 5-second..5-minute interval before conditional exact-version rescheduling.
// Zero affected rows indicate no claim, not a successfully reported outcome or permission to repeat the mailbox effect.
// ClaimReport требует допустимый исходный источник после lease-plus-grace и интервал 5 секунд..5 минут до условного переноса точной версии.
// Ноль изменённых строк означает отсутствие claim, не успешный отчёт или право повторить эффект в ящике.
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

// AcknowledgeReport validates the pending report and clears only its exact guarded version, returning conflict rather than acknowledging a newer observation.
// AcknowledgeReport проверяет ожидающий отчёт и снимает только его точную защищённую версию, возвращая conflict вместо подтверждения нового наблюдения.
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
