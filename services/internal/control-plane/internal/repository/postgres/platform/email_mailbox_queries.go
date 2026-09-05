package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_mailbox_configuration_latest_revision.sql
var queryEmailMailboxLatestRevision string

//go:embed sql/email_mailbox_configuration_binding.sql
var queryEmailMailboxBinding string

//go:embed sql/email_mailbox_configuration_list.sql
var queryEmailMailboxList string

//go:embed sql/email_mailbox_credential_receipt.sql
var queryEmailMailboxCredentialReceipt string

//go:embed sql/email_mailbox_credentials_list.sql
var queryEmailMailboxCredentialsList string

//go:embed sql/email_mailbox_action_state.sql
var queryEmailMailboxActionState string

func (repository *Repository) PreviewEmailMailboxConfiguration(ctx context.Context, principal value.Principal, connectionRef, format, content string) (entity.EmailMailboxPreview, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailMailboxPreview{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailMailboxPreview{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.EmailMailboxPreview{}, err
	}
	specification, err := emailpolicy.DecodeSpecification(format, content)
	if err != nil {
		return entity.EmailMailboxPreview{Diagnostics: []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticSyntax)}}, nil
	}
	canonical, err := emailpolicy.CanonicalYAML(specification)
	if err != nil {
		return entity.EmailMailboxPreview{}, errs.ErrUnavailable
	}
	result := entity.EmailMailboxPreview{Specification: &specification, CanonicalYAML: canonical}
	mailboxRef, err := newMailboxRef()
	if err != nil {
		return entity.EmailMailboxPreview{}, errs.ErrUnavailable
	}
	mailbox, err := emailpolicy.MaterializeMailbox(specification, emailpolicy.MailboxBinding{Ref: mailboxRef,
		OrganizationRef: current.organizationRef, ConnectionRef: connectionRef, Revision: 1, CredentialGeneration: version})
	if err != nil {
		result.Diagnostics = []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticConfiguration)}
		return result, nil
	}
	// Preview проверяет ссылки независимо от включения доставки.
	mailbox.Enabled = true
	if _, err := emailCredentialDigests(ctx, tx, api.Configuration{Mailboxes: []api.Mailbox{mailbox}}); err != nil {
		if errors.Is(err, errs.ErrConflict) {
			result.Diagnostics = []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticCredential)}
			return result, nil
		}
		return entity.EmailMailboxPreview{}, err
	}
	result.Valid = true
	return result, nil
}

func (repository *Repository) ListEmailMailboxCredentials(ctx context.Context, principal value.Principal, connectionRef, kind string, page query.Page) ([]entity.EmailMailboxCredential, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef); err != nil {
		return nil, 0, "", err
	}
	filter := query.Filter{ResourceRef: connectionRef, Category: kind, Page: page}
	cursor, err := decodeCatalogCursor(current, "EMAIL_CREDENTIAL", filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryEmailMailboxCredentialsList, current.organizationID, connectionRef, kind, cursor, limit+1)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var total int64
	var result []entity.EmailMailboxCredential
	for rows.Next() {
		var item entity.EmailMailboxCredential
		if rows.Scan(&item.Name, &item.Generation, &item.Kind, &total) != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		if item.Name != "" {
			item.ConnectionRef, item.ConnectionVersion = connectionRef, item.Generation
			result = append(result, item)
		}
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeCatalogCursor(current, "EMAIL_CREDENTIAL", filter, fmt.Sprintf("%s.%020d", last.Name, last.Generation))
	}
	return result, total, next, nil
}

func (repository *Repository) requireEmailMailboxConnection(ctx context.Context, tx pgx.Tx, current scope, ref string) (int64, error) {
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: ref}); err != nil {
		return 0, errs.ErrNotFound
	}
	var id string
	var version int64
	if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, ref).Scan(&id, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errs.ErrNotFound
		}
		return 0, errs.ErrUnavailable
	}
	return version, nil
}

func (repository *Repository) emailMailboxViewTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef string, connectionVersion int64, configurationRef, revisionRef string) (entity.EmailMailboxConfigurationView, error) {
	var boundConfiguration, boundRevision string
	err := tx.QueryRow(ctx, queryEmailMailboxBinding, current.organizationID, connectionRef).Scan(&boundConfiguration, &boundRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	if configurationRef == "" {
		configurationRef = boundConfiguration
	}
	var ownerConnection, mailboxRef string
	if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, configurationRef).Scan(&ownerConnection, &mailboxRef); err != nil || ownerConnection != connectionRef {
		return entity.EmailMailboxConfigurationView{}, errs.ErrNotFound
	}
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: configurationRef}, "EMAIL_MAILBOX", false)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	if revisionRef == "" {
		if err := tx.QueryRow(ctx, queryEmailMailboxLatestRevision, current.organizationID, set.id).Scan(&revisionRef); err != nil {
			return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
		}
	}
	revision, err := repository.lockManagedRevision(ctx, tx, current, set, revisionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	specification, err := emailpolicy.DecodeSpecification(revision.ContentFormat, revision.Content)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	view := entity.EmailMailboxConfigurationView{ConnectionRef: connectionRef, ConnectionVersion: connectionVersion,
		MailboxRef: mailboxRef, Configuration: set.ManagedConfigurationSet, Revision: revision.ManagedConfigurationRevision, Specification: specification}
	if boundConfiguration == configurationRef {
		view.BoundRevisionRef = boundRevision
	}
	for _, text := range revision.ValidationDiagnostics {
		code, _, _ := strings.Cut(text, ":")
		switch code {
		case emailpolicy.DiagnosticSyntax, emailpolicy.DiagnosticConfiguration, emailpolicy.DiagnosticCredential:
			view.Diagnostics = append(view.Diagnostics, emailpolicy.Diagnostic(code))
		default:
			return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
		}
	}
	view.Publication, err = emailMailboxPublicationView(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	actionState := emailpolicy.ActionState{ManagedBy: set.ManagedBy, RevisionState: revision.State, HasPublishedRevision: set.CurrentRevision != nil, Bound: view.BoundRevisionRef != ""}
	if err := tx.QueryRow(ctx, queryEmailMailboxActionState, current.organizationID, connectionRef, set.Ref).Scan(&actionState.ConnectionEnabled, &actionState.HasMutableDraft, &actionState.PendingDelivery); err != nil {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	view.NextActions = emailpolicy.AvailableActions(actionState)
	return view, nil
}

func (repository *Repository) GetEmailMailboxConfiguration(ctx context.Context, principal value.Principal, connectionRef, configurationRef, revisionRef string) (entity.EmailMailboxConfigurationView, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	return repository.emailMailboxViewTx(ctx, tx, current, connectionRef, version, configurationRef, revisionRef)
}

func (repository *Repository) ListEmailMailboxConfigurations(ctx context.Context, principal value.Principal, connectionRef, search string, page query.Page) (entity.EmailMailboxPage, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailMailboxPage{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	filter := query.Filter{ResourceRef: connectionRef, Query: search, Page: page}
	cursor, err := decodeCatalogCursor(current, "EMAIL_MAILBOX", filter)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryEmailMailboxList, current.organizationID, connectionRef, search, cursor, limit+1)
	if err != nil {
		return entity.EmailMailboxPage{}, errs.ErrUnavailable
	}
	result := entity.EmailMailboxPage{NextActions: []entity.EmailMailboxActionAvailability{{Action: "CREATE_DRAFT", Enabled: true, Reason: "NONE"}}}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref, &result.Total); err != nil {
			rows.Close()
			return entity.EmailMailboxPage{}, errs.ErrUnavailable
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return entity.EmailMailboxPage{}, errs.ErrUnavailable
	}
	if len(refs) > int(limit) {
		refs = refs[:limit]
		result.NextPageToken = encodeCatalogCursor(current, "EMAIL_MAILBOX", filter, refs[len(refs)-1])
	}
	for _, ref := range refs {
		view, err := repository.emailMailboxViewTx(ctx, tx, current, connectionRef, version, ref, "")
		if err != nil {
			return entity.EmailMailboxPage{}, err
		}
		result.Items = append(result.Items, view)
	}
	return result, nil
}

func (repository *Repository) GetEmailMailboxCredentialReceipt(ctx context.Context, principal value.Principal, connectionRef, key string) (entity.EmailMailboxCredential, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef); err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryEmailMailboxCredentialReceipt, current.organizationID, current.actorID, key, "controlplane."+strings.ToLower(string(command.ConfigureEmailCredential))).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.EmailMailboxCredential{}, errs.ErrNotFound
		}
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	var receipt command.Result
	if json.Unmarshal(raw, &receipt) != nil || receipt.EmailCredential == nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	credential := receipt.EmailCredential
	if credential.ConnectionRef != connectionRef {
		return entity.EmailMailboxCredential{}, errs.ErrNotFound
	}
	return entity.EmailMailboxCredential{Name: credential.Name, Generation: credential.Generation, Kind: credential.Kind,
		ConnectionRef: connectionRef, ConnectionVersion: credential.ConnectionVersion}, nil
}
