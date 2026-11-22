package platform

import (
	"context"
	_ "embed"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryEmailCredentialOwnerCatalog pages the current configuring user's exact visible email connections without inspecting generic personal token bindings or mailbox contents.
// queryEmailCredentialOwnerCatalog выдаёт страницы точных видимых email connections настроившего пользователя без просмотра generic personal token bindings и содержимого mailbox.
//
//go:embed sql/email_credential_owner__catalog.sql
var queryEmailCredentialOwnerCatalog string

// emailMailboxOwnershipView builds the dedicated safe view, omitting a personal subject locator and every credential descriptor.
// emailMailboxOwnershipView строит отдельное безопасное представление без locator личного subject и любых credential descriptors.
func emailMailboxOwnershipView(connectionRef string, row emailCredentialOwnerRow) entity.EmailMailboxOwnershipView {
	view := entity.EmailMailboxOwnershipView{ConnectionRef: connectionRef, ConnectionVersion: row.connectionVersion, DeliveryKind: "MAILBOX_CONFIGURATION", Policy: row.owner.Policy, State: row.state, Generation: row.owner.Generation,
		OwnerEligible: row.state == "ACTIVE" && row.initiatorActive && row.owner.SubjectActive}
	if row.owner.Policy == entity.IntegrationCredentialProjectShared && row.state != "UNCONFIGURED" {
		view.ServiceSubjectRef = row.subjectRef
	}
	return view
}

// GetEmailMailboxOwnership resolves only the authenticated configuring user; unconfigured metadata permits explicit setup but never returns historical mailbox secrets or grants their use.
// GetEmailMailboxOwnership разрешает только аутентифицированного настроившего пользователя; unconfigured metadata допускает явную настройку, не выдавая прежних секретов mailbox и права их использования.
func (repository *Repository) GetEmailMailboxOwnership(ctx context.Context, principal value.Principal, connectionRef string) (entity.EmailMailboxOwnershipView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != "platform.email-mailboxes.ownership.read" && principal.Permission != "platform.command.email-mailbox.configure-credential" {
		return entity.EmailMailboxOwnershipView{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailMailboxOwnershipView{}, err
	}
	if current.authorityProjectID != "" {
		return entity.EmailMailboxOwnershipView{}, errs.ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailMailboxOwnershipView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}); err != nil {
		return entity.EmailMailboxOwnershipView{}, errs.ErrNotFound
	}
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, connectionRef, current.actorRef)
	if err != nil {
		return entity.EmailMailboxOwnershipView{}, err
	}
	if !row.initiatorActive || row.initiator.SubjectKind != "USER" || row.state != "UNCONFIGURED" && row.configuredBy != row.initiator.SubjectID {
		return entity.EmailMailboxOwnershipView{}, errs.ErrNotFound
	}
	return emailMailboxOwnershipView(connectionRef, row), nil
}

// ListMyEmailMailboxes returns one bounded snapshot page filtered by current self ownership and visibility before pagination; opaque cursors cannot widen that eligibility.
// ListMyEmailMailboxes возвращает одну ограниченную страницу снимка с фильтром текущего собственного владения и видимости до пагинации; непрозрачный курсор не расширяет допустимый набор.
func (repository *Repository) ListMyEmailMailboxes(ctx context.Context, principal value.Principal, search string, page query.Page) ([]entity.MyEmailMailbox, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != "platform.email-mailboxes.mine.list" {
		return nil, "", errs.ErrForbidden
	}
	search = strings.TrimSpace(search)
	if len([]rune(search)) > 200 || strings.ContainsRune(search, 0) || page.Size < 0 || page.Size > 100 {
		return nil, "", errs.ErrInvalid
	}
	if page.Size == 0 {
		page.Size = 50
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if current.authorityProjectID != "" {
		return nil, "", errs.ErrForbidden
	}
	filter := query.Filter{Query: search, Page: page}
	const kind = "PERSONAL_EMAIL_MAILBOX"
	after, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryEmailCredentialOwnerCatalog, pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID, "query": search, "after": after, "limit": page.Size + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.MyEmailMailbox, 0, page.Size)
	for rows.Next() {
		var item entity.MyEmailMailbox
		var subjectRef string
		if rows.Scan(&item.Ownership.ConnectionRef, &item.Name, &item.Ownership.ConnectionVersion, &item.Enabled, &item.Ownership.Policy, &item.Ownership.State, &item.Ownership.Generation, &subjectRef, &item.Ownership.OwnerEligible, &item.Available) != nil {
			return nil, "", errs.ErrUnavailable
		}
		if item.Ownership.Policy != entity.IntegrationCredentialInitiatorRequired && item.Ownership.Policy != entity.IntegrationCredentialProjectShared || item.Ownership.State != "UNCONFIGURED" && item.Ownership.State != "ACTIVE" && item.Ownership.State != "REVOKED" {
			return nil, "", errs.ErrUnavailable
		}
		item.Ownership.DeliveryKind = "MAILBOX_CONFIGURATION"
		if item.Ownership.Policy == entity.IntegrationCredentialProjectShared && item.Ownership.State != "UNCONFIGURED" {
			item.Ownership.ServiceSubjectRef = subjectRef
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	rows.Close()
	items, next := catalogPageWithCursor(current, kind, filter, items, page.Size, func(item entity.MyEmailMailbox) string { return item.Ownership.ConnectionRef })
	if tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}
