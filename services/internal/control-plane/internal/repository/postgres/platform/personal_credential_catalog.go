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

// queryPersonalCredentialCatalog reads one eligible lookahead page with self bindings in one query.
// queryPersonalCredentialCatalog читает одну допустимую lookahead-страницу с личными bindings одним запросом.
//
//go:embed sql/personal_credential__catalog.sql
var queryPersonalCredentialCatalog string

// ListPersonalIntegrationConnections enforces the exact browser operation, bounded search/page
// and actor/filter-bound cursor, then reads fresh visibility and self metadata in one snapshot.
// Missing bindings stay absent; no per-item queries, credential writes or external calls are made.
// ListPersonalIntegrationConnections проверяет exact browser operation, пределы поиска/страницы и
// actor/filter-bound cursor, затем читает текущую видимость и личные metadata в одном снимке.
// Отсутствующая привязка остаётся отсутствующей; per-item запросов, записи ключей и внешних вызовов нет.
func (repository *Repository) ListPersonalIntegrationConnections(ctx context.Context, principal value.Principal, filter query.PersonalIntegrationConnectionFilter) ([]entity.PersonalIntegrationConnection, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != "platform.personal-integrations.list" {
		return nil, "", errs.ErrForbidden
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, 0) || filter.Page.Size < 0 || filter.Page.Size > 100 {
		return nil, "", errs.ErrInvalid
	}
	if filter.Page.Size == 0 {
		filter.Page.Size = 50
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if current.authorityProjectID != "" {
		return nil, "", errs.ErrForbidden
	}
	const kind = "PERSONAL_INTEGRATION_CONNECTION"
	cursorFilter := query.Filter{Query: filter.Query, Page: filter.Page}
	after, err := decodeCatalogCursor(current, kind, cursorFilter)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryPersonalCredentialCatalog, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "subject_id": current.actorID, "query": filter.Query,
		"after": after, "limit": filter.Page.Size + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.PersonalIntegrationConnection, 0, filter.Page.Size)
	for rows.Next() {
		var item entity.PersonalIntegrationConnection
		var binding personalCredentialRow
		targets := []any{&item.Ref, &item.Name, &item.DefinitionKey, &item.Version, &item.Enabled, &item.Policy, &item.Available}
		if err := rows.Scan(append(targets, binding.targets()...)...); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		credential, err := binding.decode()
		if err != nil || item.Ref == "" || item.Version < 1 || (item.Policy != entity.IntegrationCredentialProjectShared && item.Policy != entity.IntegrationCredentialInitiatorRequired) {
			return nil, "", errs.ErrUnavailable
		}
		if credential.Ref != "" {
			if credential.OrganizationID != current.organizationID || credential.SubjectID != current.actorID || credential.ConnectionRef != item.Ref ||
				credential.ConnectionVersion != item.Version || credential.CredentialPolicy != item.Policy || credential.Version < 1 || credential.Generation < 1 {
				return nil, "", errs.ErrUnavailable
			}
			item.Credential = &credential
		} else if credential.OrganizationID != "" || credential.SubjectID != "" || credential.Version != 0 || credential.Generation != 0 || credential.State != "" ||
			credential.Credential != nil || credential.VerifiedCredentialRef != "" || credential.VerifiedDefinitionDigest != "" || credential.VerifiedConfigurationDigest != "" ||
			len(credential.VerifiedCapabilities) != 0 || len(credential.DeniedCapabilities) != 0 || credential.ExpiresAt != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	rows.Close()
	items, next := catalogPageWithCursor(current, kind, cursorFilter, items, filter.Page.Size, func(item entity.PersonalIntegrationConnection) string { return item.Ref })
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}
