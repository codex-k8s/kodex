package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_configuration_accept.sql
var queryEmailConfigurationAccept string

//go:embed sql/email_configuration_read.sql
var queryEmailConfigurationRead string

// ConfigureEmail принимает только deployment-owned документ до запуска workers.
func (repository *Repository) ConfigureEmail(ctx context.Context, raw []byte) error {
	projection, err := emailpolicy.DecodeConfiguration(raw)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(projection.Mailboxes)
	if err != nil {
		return errs.ErrInvalid
	}
	var accepted bool
	if err := repository.pool.QueryRow(ctx, queryEmailConfigurationAccept, projection.Revision, projection.Digest, encoded).Scan(&accepted); err != nil {
		return errs.ErrUnavailable
	}
	if !accepted {
		return errs.ErrConflict
	}
	repository.emailConfigurationRevision = projection.Revision
	repository.emailConfigurationDigest = projection.Digest
	return nil
}

func (repository *Repository) readEmailMailbox(ctx context.Context, tx pgx.Tx, current scope, ref string, revision int64) (emailpolicy.MailboxProjection, error) {
	if repository.emailConfigurationRevision == 0 {
		return emailpolicy.MailboxProjection{}, errs.ErrForbidden
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryEmailConfigurationRead, current.organizationID, ref, revision,
		repository.emailConfigurationRevision, repository.emailConfigurationDigest).Scan(&raw); errors.Is(err, pgx.ErrNoRows) {
		return emailpolicy.MailboxProjection{}, errs.ErrForbidden
	} else if err != nil {
		return emailpolicy.MailboxProjection{}, errs.ErrUnavailable
	}
	var mailbox emailpolicy.MailboxProjection
	if json.Unmarshal(raw, &mailbox) != nil || mailbox.Ref != ref || mailbox.Revision != revision {
		return emailpolicy.MailboxProjection{}, errs.ErrUnavailable
	}
	return mailbox, nil
}
