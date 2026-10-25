package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryEmailMailboxPublicationLock embeds the global transaction advisory lock serializing mailbox
// snapshot publication. Its lifetime belongs to the caller transaction; acquiring it does not
// authorize a connection or apply a Secret.
// queryEmailMailboxPublicationLock встраивает глобальную transaction advisory lock сериализации
// mailbox snapshot publication. Срок блокировки принадлежит переданной транзакции; захват не
// разрешает connection и не применяет Secret.
//
//go:embed sql/email_mailbox_publication_lock.sql
var queryEmailMailboxPublicationLock string

// queryEmailMailboxPublicationNext embeds next revision above both publication history and
// accepted watermark plus the existence of any PENDING publication. Callers serialize with the
// publication lock and reject forbidden concurrency.
// queryEmailMailboxPublicationNext встраивает следующую ревизию выше publication history и
// accepted watermark и наличие любой PENDING publication. Caller сериализует работу publication
// lock и отклоняет запрещённую конкуренцию.
//
//go:embed sql/email_mailbox_publication_next.sql
var queryEmailMailboxPublicationNext string

// queryEmailMailboxPublicationInsert embeds pending publication insertion with exact
// document/digest/revision, owner connection/version, optional managed revision and creator.
// Persistence is an intent; actual apply/readback is owned by the publication worker.
// queryEmailMailboxPublicationInsert встраивает вставку pending publication с точными
// document/digest/revision, owner connection/version, optional managed revision и creator.
// Сохранение является намерением; фактические apply/readback принадлежат publication worker.
//
//go:embed sql/email_mailbox_publication_insert.sql
var queryEmailMailboxPublicationInsert string

// queryEmailMailboxPublicationView embeds latest non-RECOVERY publication metadata for the
// organization connection as anchor or bound effect, resolving its configuration revision.
// State/readiness is stored owner evidence, not a new probe or permission check.
// queryEmailMailboxPublicationView встраивает последние non-RECOVERY publication metadata для
// organization connection как anchor либо bound effect с разрешением configuration revision.
// State/readiness является сохранённым owner evidence, а не новым probe и проверкой прав.
//
//go:embed sql/email_mailbox_publication_view.sql
var queryEmailMailboxPublicationView string

// bindEmailMailbox serializes publication, rejects pending work and checks connection plus
// configuration OCC and allowed BIND/UNBIND actions. It verifies the previous accepted snapshot
// digest, preserves other mailboxes, validates a published bound revision and credentials, sorts
// the new bounded document and advances connection version. It persists PENDING forward-only
// intent in the caller transaction; Secret application and READY readback happen later.
// bindEmailMailbox сериализует публикацию, отклоняет pending работу и проверяет
// connection/configuration OCC и допустимые BIND/UNBIND. Сверяет дайджест прежнего accepted
// snapshot, сохраняет прочие mailboxes, проверяет published bound revision и credentials,
// сортирует новый ограниченный document и продвигает connection version. Сохраняет PENDING
// forward-only intent в переданной транзакции; применение Secret и READY readback выполняются
// позднее.
func (repository *Repository) bindEmailMailbox(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.EmailMailboxInput)
	if !ok || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var nextRevision int64
	var pending bool
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationNext).Scan(&nextRevision, &pending); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if pending {
		return commandOutcome{}, errs.ErrConflict
	}
	var connectionID string
	var connectionVersion int64
	if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, payload.ConnectionRef).Scan(&connectionID, &connectionVersion); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	unbinding := input.Kind == command.UnbindEmailMailboxConfiguration
	expectedConnectionVersion := payload.ExpectedConnectionVersion
	if unbinding {
		expectedConnectionVersion = *input.Mutation.ExpectedVersion
	}
	if expectedConnectionVersion != connectionVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if unbinding {
		view, err := repository.emailMailboxViewTx(ctx, tx, current, payload.ConnectionRef, connectionVersion, "", "")
		if err != nil {
			return commandOutcome{}, err
		}
		if !emailpolicy.ActionAllowed(view.NextActions, "UNBIND") {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	configuration := api.Configuration{Version: "email-bridge/v1", Revision: nextRevision, ManagedBy: "ui", Source: "control-plane", Mailboxes: []api.Mailbox{}}
	var previous []byte
	var previousDigest string
	err := tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&previous, &previousDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err == nil {
		var accepted api.Configuration
		if api.Decode(previous, &accepted) != nil || api.Digest(accepted) != previousDigest {
			return commandOutcome{}, errs.ErrUnavailable
		}
		for _, mailbox := range accepted.Mailboxes {
			if mailbox.TenantId != current.organizationRef || mailbox.ConnectionId != payload.ConnectionRef {
				configuration.Mailboxes = append(configuration.Mailboxes, mailbox)
			}
		}
	}
	var set managedSet
	var revision lockedManagedRevision
	if !unbinding {
		var ownerConnection, mailboxRef string
		if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, payload.Managed.ConfigurationRef).Scan(&ownerConnection, &mailboxRef); err != nil || ownerConnection != payload.ConnectionRef {
			return commandOutcome{}, errs.ErrNotFound
		}
		set, err = repository.resolveManagedSet(ctx, tx, current, payload.Managed, "EMAIL_MAILBOX", false)
		if err != nil {
			return commandOutcome{}, err
		}
		if set.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		revision, err = repository.lockManagedRevision(ctx, tx, current, set, payload.Managed.RevisionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if revision.State != "PUBLISHED" {
			return commandOutcome{}, errs.ErrConflict
		}
		view, err := repository.emailMailboxViewTx(ctx, tx, current, payload.ConnectionRef, connectionVersion, set.Ref, revision.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		if !emailpolicy.ActionAllowed(view.NextActions, "BIND") {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := repository.validateEmailMailboxRevision(ctx, tx, current, set, revision.ManagedConfigurationRevision); err != nil {
			return commandOutcome{}, err
		}
		specification, err := emailpolicy.DecodeSpecification(revision.ContentFormat, revision.Content)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		mailbox, err := emailpolicy.MaterializeMailbox(specification, emailpolicy.MailboxBinding{Ref: mailboxRef, OrganizationRef: current.organizationRef,
			ConnectionRef: payload.ConnectionRef, Revision: nextRevision, CredentialGeneration: connectionVersion + 1})
		if err != nil {
			return commandOutcome{}, err
		}
		configuration.Mailboxes = append(configuration.Mailboxes, mailbox)
		if err := tx.QueryRow(ctx, queryManagedConfigurationTouchSet, pgx.StrictNamedArgs{"configuration_set_id": set.id, "expected_version": set.Version}).Scan(&set.Version, &set.UpdatedAt); err != nil {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	}
	sort.Slice(configuration.Mailboxes, func(i, j int) bool { return configuration.Mailboxes[i].Id < configuration.Mailboxes[j].Id })
	if api.ValidateConfiguration(configuration) != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := emailCredentialDigests(ctx, tx, configuration); err != nil {
		return commandOutcome{}, err
	}
	if err := tx.QueryRow(ctx, queryEmailCredentialAdvanceConnection, connectionID, current.organizationID, connectionVersion).Scan(&connectionVersion); err != nil {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	ref, err := newRef("mailpub")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	raw, err := json.Marshal(configuration)
	if err != nil || len(raw) > 900<<10 {
		return commandOutcome{}, errs.ErrInvalid
	}
	kind := "BIND"
	if unbinding {
		kind = "UNBIND"
	}
	publication := entity.EmailMailboxPublication{Ref: ref, Revision: nextRevision, Digest: api.Digest(configuration), State: "PENDING", ConfigurationRevisionRef: revision.Ref}
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationInsert, pgx.StrictNamedArgs{
		"ref": ref, "revision": nextRevision, "digest": publication.Digest, "document": raw, "organization_id": current.organizationID,
		"connection_id": connectionID, "connection_version": connectionVersion, "configuration_set_id": set.id, "configuration_revision_id": revision.RefID, "actor_id": current.actorID, "kind": kind,
	}).Scan(&publication.CreatedAt); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	outcome := commandOutcome{resourceKind: "INTEGRATION_CONNECTION", resourceRef: payload.ConnectionRef, summary: "i18n:EMAIL_MAILBOX_PUBLICATION_PENDING", platformEvent: "INTEGRATION_CONNECTION_CHANGED",
		result: command.Result{EmailPublication: &publication, EmailConnectionVersion: connectionVersion}}
	if !unbinding {
		view, err := repository.emailMailboxViewTx(ctx, tx, current, payload.ConnectionRef, connectionVersion, set.Ref, revision.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		outcome.result.EmailMailbox = &view
	}
	return outcome, nil
}

// emailMailboxPublicationView reads latest owner publication metadata for the exact
// organization/connection and copies nullable ready time when present. Missing rows return
// nil,nil; backend failure is Unavailable. This internal transaction helper neither checks caller
// access nor executes a probe.
// emailMailboxPublicationView читает последние owner publication metadata точных
// organization/connection и копирует nullable ready time при наличии. Отсутствие строки даёт
// nil,nil, backend failure — Unavailable. Внутренний transaction helper сам не проверяет доступ
// caller и не выполняет probe.
func emailMailboxPublicationView(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (*entity.EmailMailboxPublication, error) {
	var result entity.EmailMailboxPublication
	var readyAt *time.Time
	err := tx.QueryRow(ctx, queryEmailMailboxPublicationView, current.organizationID, connectionRef).Scan(&result.Ref, &result.Revision, &result.Digest, &result.State, &result.ConfigurationRevisionRef, &result.CreatedAt, &readyAt, &result.FailureCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if readyAt != nil {
		result.ReadyAt = *readyAt
	}
	return &result, nil
}
