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

//go:embed sql/email_configuration_accept.sql
var queryEmailConfigurationAccept string

//go:embed sql/email_configuration_read.sql
var queryEmailConfigurationRead string

//go:embed sql/email_configuration_document_insert.sql
var queryEmailConfigurationDocumentInsert string

//go:embed sql/email_configuration_document_read.sql
var queryEmailConfigurationDocumentRead string

// ConfigureEmail принимает только deployment-owned документ до запуска workers.
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

// EmailConfiguration возвращает внутренний документ publisher, не публичный UI view.
// Он содержит только immutable credential descriptors, никогда secret values.
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

// InitializeEmailConfiguration не заменяет сохранённое состояние пустым release seed.
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

// ReconcileConfiguredEmail повторяет deployment-owned import после прежней delivery.
func (repository *Repository) ReconcileConfiguredEmail(ctx context.Context, configuration api.Configuration) error {
	return repository.importGitMailboxes(ctx, configuration)
}

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
