package receipt

import (
	"context"
	_ "embed"
	"errors"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reserveSQL inserts the initial unknown receipt and optional private report journal atomically, with scoped effect-key conflict handling.
// reserveSQL атомарно вставляет исходную unknown-квитанцию и необязательный закрытый журнал отчёта, обрабатывая scoped-конфликт ключа эффекта.
//
//go:embed sql/receipt__reserve.sql
var reserveSQL string

// getSQL reads explicit receipt/audit fields under the exact tenant and mailbox predicates using an ID or effect key.
// getSQL читает явные поля квитанции и audit с точными tenant/mailbox-предикатами по ID либо ключу эффекта.
//
//go:embed sql/receipt__get.sql
var getSQL string

// completeSQL persists outcome/coordinates only for a still-unknown receipt whose source remains locked, fencing version/source/grace when a report is supplied.
// completeSQL сохраняет исход и координаты лишь для ещё неопределённой квитанции с неразблокированным источником, проверяя version/source/grace при наличии отчёта.
//
//go:embed sql/receipt__complete.sql
var completeSQL string

// configurationSQL advances the durable singleton watermark or accepts the exact same revision/digest, rejecting rollback or same-version mutation.
// configurationSQL повышает устойчивый singleton-watermark либо допускает точные прежние revision/digest, отвергая откат и изменение той же версии.
//
//go:embed sql/configuration__accept.sql
var configurationSQL string

// readySQL checks the exact runtime login, absence of superuser/RLS bypass, forced RLS and required receipt/owner-table privileges without DELETE.
// readySQL проверяет точный runtime-login, отсутствие superuser/RLS bypass, принудительный RLS и нужные привилегии таблиц квитанций без DELETE.
//
//go:embed sql/receipt__ready.sql
var readySQL string

// Repository adapts the service-owned receipt schema using its supplied runtime pool; domain authority and protocol I/O remain outside storage.
// Repository адаптирует собственную схему квитанций через переданный runtime-пул; доменные полномочия и протокольный I/O остаются вне хранилища.
type Repository struct{ Pool *pgxpool.Pool }

var _ port.Repository = (*Repository)(nil)

// Reserve reserves the scoped effect identity without introducing a report source, returning whether this call created the receipt.
// Reserve резервирует scoped-идентичность эффекта без нового источника отчёта, возвращая признак создания квитанции этим вызовом.
func (r *Repository) Reserve(ctx context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit) (port.Record, bool, error) {
	return r.reserve(ctx, s, key, digest, id, resource, audit, nil)
}

// ReserveEffect requires a valid invocation source with a live lease before atomically reserving the initial unknown effect and report lineage.
// ReserveEffect требует допустимый источник invocation с живым lease до атомарного резервирования исходного unknown-эффекта и происхождения отчёта.
func (r *Repository) ReserveEffect(ctx context.Context, s port.Scope, record port.Record, source port.ReportSource) (port.Record, bool, error) {
	if !source.Valid() || !source.Binding.Lease.ExpiresAt.After(time.Now()) {
		return port.Record{}, false, errs.Invalid
	}
	return r.reserve(ctx, s, record.Key, record.Digest, record.ID, record.Resource, record.Audit, &source)
}

// reserve validates audit/source shape and inserts one receipt before external I/O; a replay reads the existing scoped key and requires the same input digest.
// Unique violations become conflicts, while other driver details collapse to unavailable; the caller must already own the scope and authority decision.
// reserve проверяет форму audit/source и вставляет квитанцию до внешнего I/O; повтор читает существующий scoped-ключ и требует тот же дайджест входа.
// Нарушение уникальности даёт conflict, остальные подробности драйвера сводятся к unavailable; область и полномочия заранее разрешает вызывающий.
func (r *Repository) reserve(ctx context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit, source *port.ReportSource) (port.Record, bool, error) {
	var result port.Record
	if !audit.Valid() {
		return result, false, errs.Invalid
	}
	raw, sourceDigest, after, err := reportSource(source)
	if err != nil {
		return result, false, err
	}
	result, e := scanRecord(r.Pool.QueryRow(ctx, reserveSQL, pgx.StrictNamedArgs{"tenant": s.Tenant, "mailbox": s.Mailbox, "key": key, "digest": digest, "id": id, "resource": resource, "actor": audit.Actor, "agent": audit.Agent, "grant": audit.Grant, "operation": audit.Operation, "configuration": audit.ConfigurationRevision, "generation": audit.CredentialGeneration, "gate": audit.GateApproved, "source": raw, "source_digest": sourceDigest, "after": after}))
	if e == nil {
		return result, true, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		var pgerr *pgconn.PgError
		if errors.As(e, &pgerr) && pgerr.Code == "23505" {
			return result, false, errs.Conflict
		}
		return result, false, errs.Unavailable
	}
	result, e = r.Get(ctx, s, "", key)
	if e != nil {
		return result, false, e
	}
	if result.Digest != digest {
		return port.Record{}, false, errs.Conflict
	}
	return result, false, nil
}

// Get scans one receipt under exact tenant/mailbox and ID/key predicates, mapping missing rows separately from storage failures.
// Get читает одну квитанцию с точными tenant/mailbox и ID/key-предикатами, отличая отсутствие строки от отказа хранилища.
func (r *Repository) Get(ctx context.Context, s port.Scope, id, key string) (port.Record, error) {
	result, e := scanRecord(r.Pool.QueryRow(ctx, getSQL, pgx.StrictNamedArgs{"tenant": s.Tenant, "mailbox": s.Mailbox, "id": id, "key": key}))
	if errors.Is(e, pgx.ErrNoRows) {
		return result, errs.NotFound
	}
	if e != nil {
		return result, errs.Unavailable
	}
	return result, nil
}

// Complete stores the selected protocol outcome without adding a new report-source snapshot or report-version increment.
// Complete сохраняет выбранный исход протокола без добавления нового снимка источника отчёта или повышения его версии.
func (r *Repository) Complete(ctx context.Context, s port.Scope, record port.Record, status string) error {
	return r.complete(ctx, s, record, status, nil)
}

// CompleteEffect validates the private source and positive report version, persists guarded outcome/coordinates and returns the next report version.
// CompleteEffect проверяет закрытый источник и положительную версию отчёта, сохраняет защищённые исход и координаты и возвращает следующую версию отчёта.
func (r *Repository) CompleteEffect(ctx context.Context, s port.Scope, record port.Record, status string, source port.ReportSource) (port.Record, error) {
	if !source.Valid() || record.ReportVersion < 1 {
		return port.Record{}, errs.Invalid
	}
	if err := r.complete(ctx, s, record, status, &source); err != nil {
		return port.Record{}, err
	}
	record.Status = status
	record.ReportVersion++
	return record, nil
}

// complete accepts only unknown/accepted/deleted/failed and requires exactly one guarded update of the receipt and optional pending report journal.
// A canceled or rejected update never becomes a successful local completion and never initiates a protocol retry.
// complete допускает только unknown/accepted/deleted/failed и требует ровно одно защищённое обновление квитанции и необязательного pending-журнала.
// Отменённое либо отвергнутое обновление не становится успешной локальной фиксацией и не запускает повтор протокола.
func (r *Repository) complete(ctx context.Context, s port.Scope, record port.Record, status string, source *port.ReportSource) error {
	if status != "unknown" && status != "accepted" && status != "deleted" && status != "failed" {
		return errs.Invalid
	}
	raw, digest, after, err := reportSource(source)
	if err != nil {
		return err
	}
	tag, e := r.Pool.Exec(ctx, completeSQL, pgx.StrictNamedArgs{"tenant": s.Tenant, "mailbox": s.Mailbox, "id": record.ID, "digest": record.Digest, "status": status, "uid": record.UID, "validity": record.UIDValidity, "folder": record.Folder, "content": record.ContentDigest, "source": raw, "source_digest": digest, "after": after, "version": record.ReportVersion})
	if e != nil || tag.RowsAffected() != 1 {
		return errs.Unavailable
	}
	return nil
}

// Configuration durably accepts a monotone revision or identical replay; query rejection leaves the current watermark unchanged and returns unavailable.
// Configuration устойчиво принимает возрастающую ревизию либо идентичный повтор; отказ запроса сохраняет watermark и возвращает unavailable.
func (r *Repository) Configuration(ctx context.Context, c api.Configuration, digest string) error {
	var revision int64
	e := r.Pool.QueryRow(ctx, configurationSQL, pgx.StrictNamedArgs{"revision": c.Revision, "digest": digest}).Scan(&revision)
	if e != nil {
		return errs.Unavailable
	}
	return nil
}

// Ready executes the runtime-role/RLS/schema privilege assertion and rejects false, missing or failed results without probing mail providers.
// Ready выполняет проверку runtime-роли, RLS и привилегий схемы и отвергает ложный, отсутствующий либо отказной результат без проверки почтовых провайдеров.
func (r *Repository) Ready(ctx context.Context) error {
	var ok bool
	if r.Pool.QueryRow(ctx, readySQL).Scan(&ok) != nil || !ok {
		return errs.Unavailable
	}
	return nil
}
