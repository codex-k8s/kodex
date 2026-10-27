package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/jackc/pgx/v5"
)

// queryEmailConfigurationAccept embeds the authoritative database configuration-acceptance
// function call with revision, digest and mailbox projections. The returned boolean must be
// checked inside the same transaction as document insertion/readback.
// queryEmailConfigurationAccept встраивает вызов authoritative database configuration-acceptance
// function с revision, digest и mailbox projections. Возвращённый boolean требуется проверить в
// той же transaction, что document insertion/readback.
//
//go:embed sql/email_configuration_accept.sql
var queryEmailConfigurationAccept string

// queryEmailConfigurationRead embeds a share-locked current mailbox projection under
// tenant/ref/optional revision, accepted watermark and enabled email connection with exact
// mailbox/sender public configuration. It returns policy metadata and grants no execution
// authority by itself.
// queryEmailConfigurationRead встраивает share-locked current mailbox projection внутри
// tenant/ref/optional revision, accepted watermark и enabled email connection с точной
// mailbox/sender public configuration. Возвращает policy metadata и сам не выдаёт execution
// authority.
//
//go:embed sql/email_configuration_read.sql
var queryEmailConfigurationRead string

// queryEmailConfigurationDocumentInsert embeds immutable revision/digest/document insertion,
// ignoring an existing revision. The caller must verify the authoritative watermark readback
// because a conflict does not replace the stored document.
// queryEmailConfigurationDocumentInsert встраивает immutable revision/digest/document insertion с
// игнорированием существующей revision. Caller обязан сверить authoritative watermark readback,
// поскольку conflict не заменяет сохранённый document.
//
//go:embed sql/email_configuration_document_insert.sql
var queryEmailConfigurationDocumentInsert string

// queryEmailConfigurationDocumentRead embeds document/digest lookup only for the exact accepted
// watermark revision/digest. Missing matching state is no rows, not an empty or permissive
// configuration.
// queryEmailConfigurationDocumentRead встраивает document/digest lookup только для exact accepted
// watermark revision/digest. Отсутствие совпадающего состояния означает no rows, а не пустую или
// разрешающую configuration.
//
//go:embed sql/email_configuration_document_read.sql
var queryEmailConfigurationDocumentRead string

// ConfigureEmail decodes the deployment-owned document and validated mailbox projection before
// beginning a transaction, then requires database acceptance, document insertion and exact decoded
// digest readback before commit. Decode/encode failure is Invalid, rejected/mismatched state is
// Conflict and begin/write/commit failure is Unavailable. It preserves deferred rollback and does
// not start workers or return secret payloads.
// ConfigureEmail разбирает deployment-owned document и validated mailbox projection до начала
// transaction, затем требует database acceptance, document insertion и exact decoded digest
// readback до commit. Decode/encode failure даёт Invalid, rejected/mismatched state — Conflict,
// begin/write/commit failure — Unavailable. Сохраняет deferred rollback, не запускает workers и не
// возвращает secret payloads.
func (repository *Repository) ConfigureEmail(ctx context.Context, raw []byte) error {
	projection, err := emailpolicy.DecodeConfiguration(raw)
	if err != nil {
		return err
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil {
		return errs.ErrInvalid
	}
	document, err := json.Marshal(configuration)
	if err != nil {
		return errs.ErrInvalid
	}
	encoded, err := json.Marshal(projection.Mailboxes)
	if err != nil {
		return errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var accepted bool
	if err := tx.QueryRow(ctx, queryEmailConfigurationAccept, projection.Revision, projection.Digest, encoded).Scan(&accepted); err != nil {
		return errs.ErrUnavailable
	}
	if !accepted {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryEmailConfigurationDocumentInsert, projection.Revision, projection.Digest, document); err != nil {
		return errs.ErrUnavailable
	}
	var stored []byte
	var digest string
	if err := tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&stored, &digest); err != nil || digest != projection.Digest {
		return errs.ErrConflict
	}
	var restored api.Configuration
	if api.Decode(stored, &restored) != nil || api.Digest(restored) != projection.Digest {
		return errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// EmailConfiguration reads the internal publisher document through the accepted watermark and
// validates decode, configuration shape and exact digest. No rows is NotFound, other/corrupt reads
// are Unavailable. It returns immutable credential descriptors rather than secret values or a
// public UI projection, without opening a new transaction.
// EmailConfiguration читает internal publisher document через accepted watermark и проверяет
// decode, configuration shape и exact digest. No rows даёт NotFound, прочие/corrupt reads —
// Unavailable. Возвращает immutable credential descriptors вместо secret values или public UI
// projection без открытия новой transaction.
func (repository *Repository) EmailConfiguration(ctx context.Context) (api.Configuration, error) {
	var configuration api.Configuration
	var raw []byte
	var digest string
	err := repository.pool.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&raw, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return configuration, errs.ErrNotFound
	}
	if err != nil {
		return configuration, errs.ErrUnavailable
	}
	if api.Decode(raw, &configuration) != nil || api.ValidateConfiguration(configuration) != nil || api.Digest(configuration) != digest {
		return api.Configuration{}, errs.ErrUnavailable
	}
	return configuration, nil
}

// InitializeEmailConfiguration validates input and preserves stored state when the
// release-bootstrap revision-one empty git seed is supplied. Other deployment input ensures an
// initial seed exists, imports Git-owned mailboxes, tolerates the existing publication-pending
// result and rereads authoritative state. Initial seed marshal errors remain ignored as before;
// the whole seed/import/read sequence is not one transaction and does not replace UI-owned state
// by direct assignment.
// InitializeEmailConfiguration проверяет input и сохраняет stored state при передаче
// release-bootstrap revision-one empty git seed. Другой deployment input обеспечивает наличие
// initial seed, импортирует Git-owned mailboxes, допускает прежний publication-pending result и
// перечитывает authoritative state. Ошибки initial seed marshal по-прежнему игнорируются; вся
// seed/import/read последовательность не является одной transaction и не заменяет UI-owned state
// прямым присваиванием.
func (repository *Repository) InitializeEmailConfiguration(ctx context.Context, raw []byte) (api.Configuration, error) {
	var input api.Configuration
	if api.Decode(raw, &input) != nil || api.ValidateConfiguration(input) != nil {
		return input, errs.ErrInvalid
	}
	if input.Revision == 1 && input.ManagedBy == "git" && input.Source == "release-bootstrap" && len(input.Mailboxes) == 0 {
		stored, err := repository.EmailConfiguration(ctx)
		if err == nil {
			return stored, nil
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return api.Configuration{}, err
		}
	} else {
		if _, err := repository.EmailConfiguration(ctx); errors.Is(err, errs.ErrNotFound) {
			seed, _ := json.Marshal(api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "release-bootstrap", Mailboxes: []api.Mailbox{}})
			if err := repository.ConfigureEmail(ctx, seed); err != nil {
				return api.Configuration{}, err
			}
		} else if err != nil {
			return api.Configuration{}, err
		}
		if err := repository.importGitMailboxes(ctx, input); err != nil && !errors.Is(err, errs.ErrMailboxPublicationPending) {
			return api.Configuration{}, err
		}
		return repository.EmailConfiguration(ctx)
	}
	if err := repository.ConfigureEmail(ctx, raw); err != nil {
		return api.Configuration{}, err
	}
	return repository.EmailConfiguration(ctx)
}

// ReconcileConfiguredEmail forwards the deployment-owned configuration to the existing Git-mailbox
// import path after earlier delivery. It preserves import errors and ownership/publication policy
// without adding a retry loop, new transaction or direct worker action.
// ReconcileConfiguredEmail передаёт deployment-owned configuration существующему Git-mailbox
// import path после прежней delivery. Сохраняет import errors и ownership/publication policy без
// добавления retry loop, новой transaction или прямого worker action.
func (repository *Repository) ReconcileConfiguredEmail(ctx context.Context, configuration api.Configuration) error {
	return repository.importGitMailboxes(ctx, configuration)
}

// readEmailMailbox reads a current enabled mailbox projection through the caller transaction and
// accepted watermark/connection pins. No rows is Forbidden, backend failure or
// malformed/ref/revision mismatch is Unavailable. Revision zero omits only the explicit revision
// filter; it does not bypass current ownership, enabled state or policy checks.
// readEmailMailbox читает current enabled mailbox projection через caller transaction и accepted
// watermark/connection pins. No rows даёт Forbidden, backend failure либо malformed/ref/revision
// mismatch — Unavailable. Revision ноль убирает только explicit revision filter, не обходя current
// ownership, enabled state или policy checks.
func (repository *Repository) readEmailMailbox(ctx context.Context, tx pgx.Tx, current scope, ref string, revision int64) (emailpolicy.MailboxProjection, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, queryEmailConfigurationRead, current.organizationID, ref, revision).Scan(&raw); errors.Is(err, pgx.ErrNoRows) {
		return emailpolicy.MailboxProjection{}, errs.ErrForbidden
	} else if err != nil {
		return emailpolicy.MailboxProjection{}, errs.ErrUnavailable
	}
	var mailbox emailpolicy.MailboxProjection
	if json.Unmarshal(raw, &mailbox) != nil || mailbox.Ref != ref || revision != 0 && mailbox.Revision != revision {
		return emailpolicy.MailboxProjection{}, errs.ErrUnavailable
	}
	return mailbox, nil
}
