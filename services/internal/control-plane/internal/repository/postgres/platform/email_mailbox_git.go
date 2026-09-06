package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_mailbox_git_receipt.sql
var queryEmailMailboxGitReceipt string

//go:embed sql/email_mailbox_git_receipt_insert.sql
var queryEmailMailboxGitReceiptInsert string

//go:embed sql/email_mailbox_git_source_insert.sql
var queryEmailMailboxGitSourceInsert string

//go:embed sql/email_mailbox_git_sources.sql
var queryEmailMailboxGitSources string

//go:embed sql/email_mailbox_git_source_update.sql
var queryEmailMailboxGitSourceUpdate string

//go:embed sql/email_mailbox_git_connection_touch.sql
var queryEmailMailboxGitConnectionTouch string

//go:embed sql/email_mailbox_publication_binding_insert.sql
var queryEmailMailboxPublicationBindingInsert string

//go:embed sql/email_mailbox_publication_bindings.sql
var queryEmailMailboxPublicationBindings string

//go:embed sql/email_mailbox_ui_binding.sql
var queryEmailMailboxUIBinding string

type mailboxGitSource struct {
	key, ref, managedBy, connectionRef, connectionID string
	version                                          int64
}
type mailboxGitEffect struct {
	connectionRef, connectionID, setID, revisionID string
	version                                        int64
}

func mailboxSpecification(mailbox api.Mailbox) entity.EmailMailboxSpecification {
	return entity.EmailMailboxSpecification{Enabled: mailbox.Enabled, ReceiveProtocol: mailbox.ReceiveProtocol,
		AllowedFolders: mailbox.AllowedFolders, ArchiveFolder: mailbox.ArchiveFolder, DraftsFolder: mailbox.DraftsFolder,
		Folder: mailbox.Folder, Sender: mailbox.Sender, ReplyTo: mailbox.ReplyTo, Recipients: mailbox.Recipients, HelloName: mailbox.HelloName,
		SMTP: mailbox.Smtp, IMAP: mailbox.Imap, POP: mailbox.Pop, Limits: mailbox.Limits, Policies: mailbox.Policies}
}

// importGitMailboxes вызывается только из configured startup source, не RPC payload.
func (repository *Repository) importGitMailboxes(ctx context.Context, input api.Configuration) error {
	if input.ManagedBy != "git" || input.Source == "release-bootstrap" || len(input.Source) == 0 || len(input.Source) > 512 || api.ValidateConfiguration(input) != nil {
		return errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
		return errs.ErrUnavailable
	}
	var actorID, organizationID, actorRef, organizationRef string
	var updated time.Time
	var organizationVersion int64
	if err := tx.QueryRow(ctx, queryResolveSystemWorkloadIdentity).Scan(&actorID, &organizationID, &updated, &organizationVersion); err != nil {
		return errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryResolveVerifiedPrincipal, actorID, organizationID).Scan(&actorRef, &organizationRef); err != nil {
		return errs.ErrForbidden
	}
	var current scope
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, actorRef, organizationRef).Scan(&current.organizationID, &current.organizationRef, &current.actorID, &current.actorRef, &current.actorName, &current.role); err != nil {
		return errs.ErrForbidden
	}
	var previousSourceRevision int64
	var previousSourceDigest string
	var previousPublicationRef string
	err = tx.QueryRow(ctx, queryEmailMailboxGitReceipt, input.Source).Scan(&previousSourceRevision, &previousSourceDigest, &previousPublicationRef)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if err == nil && input.Revision <= previousSourceRevision {
		if input.Revision == previousSourceRevision && api.Digest(input) == previousSourceDigest {
			return nil
		}
		return errs.ErrConflict
	}
	var next int64
	var pending bool
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationNext).Scan(&next, &pending); err != nil {
		return errs.ErrUnavailable
	}
	if pending {
		return errs.ErrMailboxPublicationPending
	}
	rows, err := tx.Query(ctx, queryEmailMailboxGitSources, input.Source, current.organizationID)
	if err != nil {
		return errs.ErrUnavailable
	}
	sources := map[string]mailboxGitSource{}
	for rows.Next() {
		var source mailboxGitSource
		if rows.Scan(&source.key, &source.ref, &source.managedBy, &source.connectionRef, &source.connectionID, &source.version) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		sources[source.key] = source
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	effects := map[string]mailboxGitEffect{}
	for _, source := range sources {
		if source.managedBy == "GIT" {
			var uiBound bool
			if err := tx.QueryRow(ctx, queryEmailMailboxUIBinding, current.organizationID, source.connectionRef).Scan(&uiBound); err != nil {
				return errs.ErrUnavailable
			}
			if uiBound {
				continue
			}
			effects[source.connectionRef] = mailboxGitEffect{connectionRef: source.connectionRef, connectionID: source.connectionID, version: source.version}
		}
	}
	configuration := api.Configuration{Version: "email-bridge/v1", Revision: next, ManagedBy: "git", Source: input.Source, Mailboxes: []api.Mailbox{}}
	for _, mailbox := range input.Mailboxes {
		if mailbox.TenantId != current.organizationRef {
			return errs.ErrForbidden
		}
		source, exists := sources[mailbox.Id]
		if exists && source.connectionRef != mailbox.ConnectionId {
			return errs.ErrConflict
		}
		if exists && source.managedBy != "GIT" {
			continue
		}
		var connectionID string
		var connectionVersion int64
		if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, mailbox.ConnectionId).Scan(&connectionID, &connectionVersion); err != nil {
			return errs.ErrNotFound
		}
		var uiBound bool
		if err := tx.QueryRow(ctx, queryEmailMailboxUIBinding, current.organizationID, mailbox.ConnectionId).Scan(&uiBound); err != nil {
			return errs.ErrUnavailable
		}
		if uiBound {
			continue
		}
		set, revision, mailboxRef, err := repository.importGitMailboxRevision(ctx, tx, current, input, mailbox, source.ref)
		if err != nil {
			return err
		}
		materialized, err := emailpolicy.MaterializeMailbox(mailboxSpecification(mailbox), emailpolicy.MailboxBinding{Ref: mailboxRef, OrganizationRef: current.organizationRef, ConnectionRef: mailbox.ConnectionId, Revision: next, CredentialGeneration: connectionVersion + 1})
		if err != nil {
			return err
		}
		configuration.Mailboxes = append(configuration.Mailboxes, materialized)
		effects[mailbox.ConnectionId] = mailboxGitEffect{connectionRef: mailbox.ConnectionId, connectionID: connectionID, version: connectionVersion, setID: set.id, revisionID: revision.RefID}
	}
	var acceptedRaw []byte
	var acceptedDigest string
	if err := tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&acceptedRaw, &acceptedDigest); err != nil {
		return errs.ErrUnavailable
	}
	var accepted api.Configuration
	if api.Decode(acceptedRaw, &accepted) != nil || api.Digest(accepted) != acceptedDigest {
		return errs.ErrUnavailable
	}
	for _, mailbox := range accepted.Mailboxes {
		if _, replaced := effects[mailbox.ConnectionId]; mailbox.TenantId != current.organizationRef || !replaced {
			configuration.Mailboxes = append(configuration.Mailboxes, mailbox)
		}
	}
	if len(effects) == 0 {
		if _, err := tx.Exec(ctx, queryEmailMailboxGitReceiptInsert, input.Source, input.Revision, api.Digest(input), previousPublicationRef); err != nil {
			return errs.ErrUnavailable
		}
		current.correlationRef = previousPublicationRef
		if err := auditMailboxOwner(ctx, tx, current, "", "controlplane.email-mailbox.git-import", "i18n:EMAIL_MAILBOX_GIT_SOURCE_RECORDED"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	sort.Slice(configuration.Mailboxes, func(i, j int) bool { return configuration.Mailboxes[i].Id < configuration.Mailboxes[j].Id })
	if api.ValidateConfiguration(configuration) != nil {
		return errs.ErrInvalid
	}
	if _, err := emailCredentialDigests(ctx, tx, configuration); err != nil {
		return err
	}
	keys := make([]string, 0, len(effects))
	for key := range effects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		effect := effects[key]
		if err := tx.QueryRow(ctx, queryEmailMailboxGitConnectionTouch, current.organizationID, effect.connectionID, effect.version).Scan(&effect.version); err != nil {
			return errs.ErrVersionMismatch
		}
		effects[key] = effect
	}
	anchor := effects[keys[0]]
	ref, err := newRef("mailpub")
	if err != nil {
		return errs.ErrUnavailable
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return errs.ErrUnavailable
	}
	var created time.Time
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationInsert, pgx.StrictNamedArgs{"ref": ref, "revision": next, "digest": api.Digest(configuration), "document": raw, "organization_id": current.organizationID, "connection_id": anchor.connectionID, "connection_version": anchor.version, "configuration_set_id": anchor.setID, "configuration_revision_id": anchor.revisionID, "actor_id": current.actorID, "kind": "GIT_SYNC"}).Scan(&created); err != nil {
		return errs.ErrUnavailable
	}
	for _, key := range keys {
		effect := effects[key]
		if _, err := tx.Exec(ctx, queryEmailMailboxPublicationBindingInsert, ref, current.organizationID, effect.connectionID, effect.version, effect.setID, effect.revisionID); err != nil {
			return errs.ErrUnavailable
		}
	}
	if _, err := tx.Exec(ctx, queryEmailMailboxGitReceiptInsert, input.Source, input.Revision, api.Digest(input), ref); err != nil {
		return errs.ErrUnavailable
	}
	current.correlationRef = ref
	if err := auditMailboxOwner(ctx, tx, current, "", "controlplane.email-mailbox.git-import", "i18n:EMAIL_MAILBOX_GIT_PUBLICATION_PENDING"); err != nil {
		return err
	}
	for _, key := range keys {
		effect := effects[key]
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", effect.connectionRef, "i18n:EMAIL_MAILBOX_GIT_PUBLICATION_PENDING", effect.version, "PENDING"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (repository *Repository) importGitMailboxRevision(ctx context.Context, tx pgx.Tx, current scope, input api.Configuration, mailbox api.Mailbox, existingRef string) (managedSet, lockedManagedRevision, string, error) {
	var set managedSet
	var err error
	sourceRevision := strconv.FormatInt(input.Revision, 10) + ":" + api.Digest(input)
	if existingRef != "" {
		set, err = repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: existingRef}, "EMAIL_MAILBOX", false)
	} else {
		ref, refErr := newRef("mcfg")
		if refErr != nil {
			return set, lockedManagedRevision{}, "", errs.ErrUnavailable
		}
		set, err = scanManagedSet(tx.QueryRow(ctx, queryManagedConfigurationInsertSet, pgx.StrictNamedArgs{"configuration_ref": ref, "organization_id": current.organizationID, "project_ref": "", "kind": "EMAIL_MAILBOX", "name": mailbox.Id, "managed_by": "GIT", "source": input.Source, "source_revision": sourceRevision, "actor_id": current.actorID}))
	}
	if err != nil {
		return set, lockedManagedRevision{}, "", err
	}
	var connectionRef, mailboxRef string
	if existingRef == "" {
		mailboxRef, err = newMailboxRef()
		if err != nil {
			return set, lockedManagedRevision{}, "", err
		}
		if tag, err := tx.Exec(ctx, queryEmailMailboxConfigurationInsertOwner, current.organizationID, set.Ref, mailbox.ConnectionId, mailboxRef); err != nil || tag.RowsAffected() != 1 {
			return set, lockedManagedRevision{}, "", errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryEmailMailboxGitSourceInsert, input.Source, mailbox.Id, set.id); err != nil {
			return set, lockedManagedRevision{}, "", errs.ErrUnavailable
		}
	} else if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, set.Ref).Scan(&connectionRef, &mailboxRef); err != nil || connectionRef != mailbox.ConnectionId {
		return set, lockedManagedRevision{}, "", errs.ErrConflict
	}
	specification := mailboxSpecification(mailbox)
	if emailpolicy.BoundSpecification(specification) != nil {
		return set, lockedManagedRevision{}, "", errs.ErrInvalid
	}
	raw, err := json.Marshal(specification)
	if err != nil {
		return set, lockedManagedRevision{}, "", errs.ErrInvalid
	}
	digest := sha256.Sum256(raw)
	ref, err := newRef("mrev")
	if err != nil {
		return set, lockedManagedRevision{}, "", errs.ErrUnavailable
	}
	created, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationInsertRevision, pgx.StrictNamedArgs{"revision_ref": ref, "organization_id": current.organizationID, "configuration_set_id": set.id, "content_format": "JSON", "content": string(raw), "digest": hex.EncodeToString(digest[:]), "parent_revision_id": set.currentRevisionID, "actor_id": current.actorID}))
	if err != nil {
		return set, lockedManagedRevision{}, "", err
	}
	if _, err := repository.validateEmailMailboxRevision(ctx, tx, current, set, created.ManagedConfigurationRevision); err != nil {
		return set, lockedManagedRevision{}, "", err
	}
	if _, err := scanManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationValidateRevision, pgx.StrictNamedArgs{"revision_id": created.internalID, "state": "VALID", "diagnostics": "[]"})); err != nil {
		return set, lockedManagedRevision{}, "", err
	}
	published, version, updated, err := scanPublishedManagedRevision(tx.QueryRow(ctx, queryManagedConfigurationPublishRevision, pgx.StrictNamedArgs{"configuration_set_id": set.id, "revision_id": created.internalID, "expected_version": set.Version}))
	if err != nil {
		return set, lockedManagedRevision{}, "", err
	}
	if tag, err := tx.Exec(ctx, queryEmailMailboxGitSourceUpdate, set.id, input.Source, sourceRevision); err != nil || tag.RowsAffected() != 1 {
		return set, lockedManagedRevision{}, "", errs.ErrConflict
	}
	set.Version, set.UpdatedAt = version, updated
	return set, lockedManagedRevision{ManagedConfigurationRevision: published.ManagedConfigurationRevision, RefID: published.internalID}, mailboxRef, nil
}
