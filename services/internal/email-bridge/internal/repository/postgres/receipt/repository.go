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

//go:embed sql/receipt__reserve.sql
var reserveSQL string

//go:embed sql/receipt__get.sql
var getSQL string

//go:embed sql/receipt__complete.sql
var completeSQL string

//go:embed sql/configuration__accept.sql
var configurationSQL string

//go:embed sql/receipt__ready.sql
var readySQL string

type Repository struct{ Pool *pgxpool.Pool }

var _ port.Repository = (*Repository)(nil)

func (r *Repository) Reserve(ctx context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit) (port.Record, bool, error) {
	return r.reserve(ctx, s, key, digest, id, resource, audit, nil)
}

func (r *Repository) ReserveEffect(ctx context.Context, s port.Scope, record port.Record, source port.ReportSource) (port.Record, bool, error) {
	if !source.Valid() || !source.Binding.Lease.ExpiresAt.After(time.Now()) {
		return port.Record{}, false, errs.Invalid
	}
	return r.reserve(ctx, s, record.Key, record.Digest, record.ID, record.Resource, record.Audit, &source)
}

func (r *Repository) reserve(ctx context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit, source *port.ReportSource) (port.Record, bool, error) {
	var result port.Record
	if !audit.Valid() {
		return result, false, errs.Invalid
	}
	raw, sourceDigest, after, err := reportSource(source)
	if err != nil {
		return result, false, err
	}
	e := r.Pool.QueryRow(ctx, reserveSQL, pgx.StrictNamedArgs{"tenant": s.Tenant, "mailbox": s.Mailbox, "key": key, "digest": digest, "id": id, "resource": resource, "actor": audit.Actor, "agent": audit.Agent, "grant": audit.Grant, "operation": audit.Operation, "configuration": audit.ConfigurationRevision, "generation": audit.CredentialGeneration, "gate": audit.GateApproved, "source": raw, "source_digest": sourceDigest, "after": after}).Scan(&result.ID, &result.Key, &result.Digest, &result.Status, &result.Resource, &result.UID, &result.UIDValidity, &result.Folder, &result.ContentDigest, &result.Audit.Actor, &result.Audit.Agent, &result.Audit.Grant, &result.Audit.Operation, &result.Audit.ConfigurationRevision, &result.Audit.CredentialGeneration, &result.Audit.GateApproved, &result.ReportVersion)
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
func (r *Repository) Get(ctx context.Context, s port.Scope, id, key string) (port.Record, error) {
	var result port.Record
	e := r.Pool.QueryRow(ctx, getSQL, pgx.StrictNamedArgs{"tenant": s.Tenant, "mailbox": s.Mailbox, "id": id, "key": key}).Scan(&result.ID, &result.Key, &result.Digest, &result.Status, &result.Resource, &result.UID, &result.UIDValidity, &result.Folder, &result.ContentDigest, &result.Audit.Actor, &result.Audit.Agent, &result.Audit.Grant, &result.Audit.Operation, &result.Audit.ConfigurationRevision, &result.Audit.CredentialGeneration, &result.Audit.GateApproved, &result.ReportVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return result, errs.NotFound
	}
	if e != nil {
		return result, errs.Unavailable
	}
	return result, nil
}
func (r *Repository) Complete(ctx context.Context, s port.Scope, record port.Record, status string) error {
	return r.complete(ctx, s, record, status, nil)
}

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
func (r *Repository) Configuration(ctx context.Context, c api.Configuration, digest string) error {
	var revision int64
	e := r.Pool.QueryRow(ctx, configurationSQL, pgx.StrictNamedArgs{"revision": c.Revision, "digest": digest}).Scan(&revision)
	if e != nil {
		return errs.Unavailable
	}
	return nil
}
func (r *Repository) Ready(ctx context.Context) error {
	var ok bool
	if r.Pool.QueryRow(ctx, readySQL).Scan(&ok) != nil || !ok {
		return errs.Unavailable
	}
	return nil
}
