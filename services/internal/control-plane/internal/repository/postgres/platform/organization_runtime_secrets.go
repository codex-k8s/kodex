package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// Организационные runtime-ресурсы не наследуют project authority или grants.
func (r *Repository) requireOrganizationRuntimeResourceAccess(ctx context.Context, tx pgx.Tx, s scope, permission string) error {
	if s.authorityProjectID != "" || s.role != "OWNER" && s.role != "ADMINISTRATOR" {
		return errs.ErrNotFound
	}
	target, err := r.resolveAccessTarget(ctx, tx, s.organizationID, organizationTarget(s.organizationRef))
	if err != nil || r.requireAccess(ctx, tx, s, "organization.manage", target) != nil || r.requireAccess(ctx, tx, s, permission, target) != nil {
		return errs.ErrNotFound
	}
	return nil
}

func (r *Repository) authorizeRuntimeSecretOperationOwner(ctx context.Context, tx pgx.Tx, worker scope, operation lockedRuntimeSecretOperation) error {
	owner, err := r.secretDraftOwnerScope(ctx, tx, worker, operation.actorID)
	if err != nil || operation.organizationRef != owner.organizationRef {
		return errs.ErrNotFound
	}
	permission := map[string]string{"CREATE": "secret.create", "ROTATE": "secret.rotate", "REVEAL": "secret.reveal", "REVOKE": "secret.revoke"}[operation.kind]
	if permission == "" {
		return errs.ErrNotFound
	}
	if operation.scopeKind == "ORGANIZATION" {
		if operation.projectID != "" || operation.projectRef != "" {
			return errs.ErrNotFound
		}
		return r.requireOrganizationRuntimeResourceAccess(ctx, tx, owner, permission)
	}
	if operation.scopeKind != "PROJECT" || operation.projectID == "" || operation.projectRef == "" {
		return errs.ErrNotFound
	}
	kind, ref := "SECRET", operation.secretRef
	if operation.kind == "CREATE" {
		kind, ref = "PROJECT", operation.projectRef
	}
	target, err := r.resolveAccessTarget(ctx, tx, owner.organizationID, entity.AccessScope{ResourceKind: kind, ResourceRef: ref})
	if err != nil || r.requireAccess(ctx, tx, owner, permission, target) != nil {
		return errs.ErrNotFound
	}
	return nil
}

func (r *Repository) listOrganizationRuntimeSecrets(ctx context.Context, s scope, filter query.Filter) ([]entity.RuntimeSecret, string, error) {
	if filter.ProjectRef != "" || len([]rune(filter.Query)) > 200 {
		return nil, "", errs.ErrInvalid
	}
	limit := filter.Page.Size
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, "", errs.ErrInvalid
	}
	anchor := "ORGANIZATION:" + s.organizationRef + ":" + s.actorRef
	cursor, err := decodeRuntimeSecretListCursor(filter.Page.Token, anchor, filter.Query)
	if err != nil {
		return nil, "", err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := r.requireOrganizationRuntimeResourceAccess(ctx, tx, s, "secret.view"); err != nil {
		return nil, "", err
	}
	rows, err := tx.Query(ctx, queryOrganizationRuntimeSecretsList, pgx.StrictNamedArgs{
		"organization_id": s.organizationID, "query": strings.TrimSpace(filter.Query), "cursor_ref": cursor.Ref, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	var items []entity.RuntimeSecret
	for rows.Next() {
		item, err := scanRuntimeSecret(rows)
		if err != nil {
			rows.Close()
			return nil, "", err
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	// Проверки прав выполняются после закрытия cursor, не в занятом соединении.
	for i := range items {
		items[i].NextActions = runtimeSecretActions(items[i], func(permission string) bool {
			return r.requireOrganizationRuntimeResourceAccess(ctx, tx, s, permission) == nil
		})
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next, err = encodeRuntimeSecretListCursor(anchor, filter.Query, items[len(items)-1].Ref)
		if err != nil {
			return nil, "", err
		}
	}
	if tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

func (r *Repository) prepareOrganizationRuntimeSecretTarget(ctx context.Context, tx pgx.Tx, s scope, input platformrepo.RuntimeSecretPrepareInput) (lockedRuntimeSecret, error) {
	if input.Kind != "CREATE" || input.ScopeKind != "ORGANIZATION" || input.ProjectRef != "" || input.SecretRef != "" {
		return lockedRuntimeSecret{}, errs.ErrInvalid
	}
	if err := r.requireOrganizationRuntimeResourceAccess(ctx, tx, s, "secret.create"); err != nil {
		return lockedRuntimeSecret{}, err
	}
	secret, err := r.lockRuntimeSecretByName(ctx, tx, s.organizationID, "", input.Name)
	if err == nil {
		return secret, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return lockedRuntimeSecret{}, err
	}
	secret = lockedRuntimeSecret{scopeKind: "ORGANIZATION", organizationRef: s.organizationRef, name: input.Name,
		description: input.Description, valueType: input.ValueType, state: "PROVISIONING", namespace: r.runtimeSecretNamespace}
	secret.ref, err = newRef("sec")
	if err != nil {
		return lockedRuntimeSecret{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, queryRuntimeSecretInsert, pgx.StrictNamedArgs{
		"ref": secret.ref, "organization_id": s.organizationID, "project_id": "", "scope_kind": secret.scopeKind,
		"namespace": secret.namespace, "name": secret.name, "description": secret.description,
		"value_type": secret.valueType, "actor_id": s.actorID,
	}).Scan(&secret.id, &secret.version, &secret.currentRevision, &secret.createdAt, &secret.updatedAt); err != nil {
		return lockedRuntimeSecret{}, mapWriteError(err)
	}
	return secret, nil
}
