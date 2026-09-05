package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_mailbox_publication_claim.sql
var queryEmailMailboxPublicationClaim string

//go:embed sql/email_mailbox_publication_stage_policy.sql
var queryEmailMailboxPublicationStagePolicy string

//go:embed sql/email_mailbox_publication_applied.sql
var queryEmailMailboxPublicationApplied string

//go:embed sql/email_mailbox_publication_callback.sql
var queryEmailMailboxPublicationCallback string

//go:embed sql/email_mailbox_publication_release.sql
var queryEmailMailboxPublicationRelease string

//go:embed sql/email_mailbox_publication_finish_lock.sql
var queryEmailMailboxPublicationFinishLock string

//go:embed sql/email_mailbox_publication_ready.sql
var queryEmailMailboxPublicationReady string

//go:embed sql/email_mailbox_binding_apply.sql
var queryEmailMailboxBindingApply string

//go:embed sql/email_mailbox_binding_remove.sql
var queryEmailMailboxBindingRemove string

//go:embed sql/email_mailbox_connection_apply.sql
var queryEmailMailboxConnectionApply string

func (repository *Repository) ClaimEmailMailboxPublication(ctx context.Context, claimant string) (entity.EmailMailboxPublicationWork, bool, error) {
	var work entity.EmailMailboxPublicationWork
	if claimant == "" || len(claimant) > 96 {
		return work, false, errs.ErrInvalid
	}
	var raw []byte
	var digest string
	err := repository.pool.QueryRow(ctx, queryEmailMailboxPublicationClaim, claimant).Scan(&work.Ref, &work.State, &work.ClaimGeneration, &raw, &digest, &work.PolicyDocument, &work.Applied, &work.Callback, &work.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return work, false, nil
	}
	if err != nil {
		return work, false, errs.ErrUnavailable
	}
	if api.Decode(raw, &work.Configuration) != nil || api.ValidateConfiguration(work.Configuration) != nil || api.Digest(work.Configuration) != digest {
		return work, false, errs.ErrUnavailable
	}
	work.Claimant = claimant
	return work, true, nil
}

func (repository *Repository) StageEmailMailboxPolicy(ctx context.Context, work entity.EmailMailboxPublicationWork, document mailpolicy.MailDocument) error {
	if document.Validate() != nil || document.ConfigurationRevision != work.Configuration.Revision || document.ConfigurationDigest != api.Digest(work.Configuration) {
		return errs.ErrInvalid
	}
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > mailpolicy.MaximumFileBytes {
		return errs.ErrInvalid
	}
	tag, err := repository.pool.Exec(ctx, queryEmailMailboxPublicationStagePolicy, work.Ref, work.Claimant, work.ClaimGeneration, raw, document.Digest(), document.ConfigurationDigest, document.ConfigurationRevision)
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
func (repository *Repository) MarkEmailMailboxApplied(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	tag, err := repository.pool.Exec(ctx, queryEmailMailboxPublicationApplied, work.Ref, work.Claimant, work.ClaimGeneration)
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
func (repository *Repository) ReleaseEmailMailboxPublication(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	_, err := repository.pool.Exec(ctx, queryEmailMailboxPublicationRelease, work.Ref, work.Claimant, work.ClaimGeneration)
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) ReportEmailConfigurationReadback(ctx context.Context, principal value.Principal, revision int64, digest string) error {
	if principal.CallerWorkload != "email-bridge" || principal.ProjectRef != "" || principal.Permission != "platform.email.configuration.report" {
		return errs.ErrForbidden
	}
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return err
	}
	if revision < 1 || !emailpolicy.ValidDigest(digest) {
		return errs.ErrInvalid
	}
	tag, err := repository.pool.Exec(ctx, queryEmailMailboxPublicationCallback, revision, digest)
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// Complete вызывается локальным reconciler только после полного Kubernetes readback.
func (repository *Repository) CompleteEmailMailboxPublication(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
		return errs.ErrUnavailable
	}
	var current scope
	var state, connectionID, connectionRef, setID, revisionID, kind, digest string
	var expectedVersion, connectionVersion int64
	var enabled, callback bool
	var expiresAt time.Time
	var raw []byte
	err = tx.QueryRow(ctx, queryEmailMailboxPublicationFinishLock, work.Ref, work.Claimant, work.ClaimGeneration).Scan(
		&state, &current.organizationID, &current.organizationRef, &connectionID, &connectionRef, &expectedVersion, &connectionVersion, &enabled,
		&setID, &revisionID, &kind, &raw, &digest, &callback, &expiresAt, &current.actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if state == "READY" {
		if _, err := tx.Exec(ctx, queryEmailMailboxPublicationRelease, work.Ref, work.Claimant, work.ClaimGeneration); err != nil {
			return errs.ErrUnavailable
		}
		return tx.Commit(ctx)
	}
	if !callback || !time.Now().Before(expiresAt) || expectedVersion != connectionVersion || !enabled && kind == "BIND" {
		return errs.ErrConflict
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil || api.Digest(configuration) != digest || configuration.Revision != work.Configuration.Revision || api.Digest(work.Configuration) != digest {
		return errs.ErrUnavailable
	}
	mailboxRef, sender := "", ""
	for _, mailbox := range configuration.Mailboxes {
		if mailbox.TenantId == current.organizationRef && mailbox.ConnectionId == connectionRef {
			mailboxRef, sender = mailbox.Id, mailbox.Sender
		}
	}
	if kind == "BIND" && (mailboxRef == "" || sender == "") {
		return errs.ErrUnavailable
	}
	if kind == "BIND" || kind == "UNBIND" {
		tag, err := tx.Exec(ctx, queryEmailMailboxConnectionApply, current.organizationID, connectionID, connectionVersion, mailboxRef, sender)
		if err != nil || tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		if kind == "BIND" {
			if _, err := tx.Exec(ctx, queryEmailMailboxBindingApply, current.organizationID, connectionID, setID, revisionID); err != nil {
				return errs.ErrUnavailable
			}
		} else if _, err := tx.Exec(ctx, queryEmailMailboxBindingRemove, current.organizationID, connectionID); err != nil {
			return errs.ErrUnavailable
		}
	}
	if kind == "GIT_SYNC" || kind == "RECOVERY" {
		if err := repository.applyMailboxPublicationBindings(ctx, tx, current, work.Ref, configuration, connectionRef); err != nil {
			return err
		}
	}
	if kind == "RECOVERY" && mailboxRef == "" {
		if _, err := tx.Exec(ctx, queryEmailMailboxBindingRemove, current.organizationID, connectionID); err != nil {
			return errs.ErrUnavailable
		}
	}
	projection, err := emailpolicy.DecodeConfiguration(raw)
	if err != nil {
		return errs.ErrUnavailable
	}
	var accepted bool
	if err := tx.QueryRow(ctx, queryEmailConfigurationAccept, projection.Revision, projection.Digest, asJSON(projection.Mailboxes)).Scan(&accepted); err != nil || !accepted {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryEmailConfigurationDocumentInsert, projection.Revision, projection.Digest, raw); err != nil {
		return errs.ErrUnavailable
	}
	var stored []byte
	var storedDigest string
	if err := tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&stored, &storedDigest); err != nil || storedDigest != digest {
		return errs.ErrConflict
	}
	var decoded api.Configuration
	if api.Decode(stored, &decoded) != nil || api.Digest(decoded) != digest {
		return errs.ErrUnavailable
	}
	canonical, _ := json.Marshal(configuration)
	readback, _ := json.Marshal(decoded)
	if !bytes.Equal(canonical, readback) {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryEmailMailboxPublicationReady, work.Ref, work.Claimant, work.ClaimGeneration)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	current.correlationRef = work.Ref
	if err := auditMailboxOwner(ctx, tx, current, connectionRef, "controlplane.email-mailbox.publication-ready", "i18n:EMAIL_MAILBOX_PUBLICATION_READY"); err != nil {
		return err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", connectionRef, "i18n:EMAIL_MAILBOX_PUBLICATION_READY", connectionVersion, "READY"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
