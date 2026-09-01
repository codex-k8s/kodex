package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) syncOIDCGroups(ctx context.Context, tx pgx.Tx, organizationID, subjectID string, input platformrepo.ProofPrincipalInput) error {
	if input.ExternalSessionRevision == 0 {
		return nil
	}
	desiredGroups, err := normalizedOIDCGroups(input)
	if err != nil {
		return err
	}
	matches, err := repository.oidcGroupsMatch(ctx, tx, organizationID, subjectID, input.ExternalSessionRevision, desiredGroups)
	if err != nil || matches {
		return err
	}
	var locked int
	if err := tx.QueryRow(ctx, queryAccessSyncLockSubject, pgx.NamedArgs{
		"organization_id": organizationID, "subject_id": subjectID,
	}).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil || locked != 1 {
		return errs.ErrUnavailable
	}
	matches, err = repository.oidcGroupsMatch(ctx, tx, organizationID, subjectID, input.ExternalSessionRevision, desiredGroups)
	if err != nil || matches {
		return err
	}
	if _, err := tx.Exec(ctx, queryAccessSyncReplaceMemberships, pgx.NamedArgs{
		"organization_id": organizationID, "subject_id": subjectID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	observedAt := time.Now().UTC()
	for _, groupName := range desiredGroups {
		digest := sha256.Sum256([]byte(input.ExternalIssuer + "\x00" + groupName))
		groupRef, err := newRef("grp")
		if err != nil {
			return err
		}
		var groupID, returnedRef string
		if err := tx.QueryRow(ctx, queryAccessSyncUpsertGroup, pgx.NamedArgs{
			"ref": groupRef, "organization_id": organizationID, "issuer": input.ExternalIssuer,
			"external_group_digest": hex.EncodeToString(digest[:]), "display_name": groupName, "observed_at": observedAt,
		}).Scan(&groupID, &returnedRef); err != nil {
			return errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryAccessSyncInsertMembership, pgx.NamedArgs{
			"organization_id": organizationID, "group_id": groupID, "subject_id": subjectID,
			"session_revision": input.ExternalSessionRevision, "observed_at": observedAt,
		}); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}

func normalizedOIDCGroups(input platformrepo.ProofPrincipalInput) ([]string, error) {
	if input.ExternalIssuer == "" || len(input.ExternalGroups) > 100 {
		return nil, errs.ErrForbidden
	}
	desiredGroups := make([]string, 0, len(input.ExternalGroups))
	seen := make(map[string]struct{}, len(input.ExternalGroups))
	for _, groupName := range input.ExternalGroups {
		groupName = strings.TrimSpace(groupName)
		if groupName == "" || len([]rune(groupName)) > 200 || strings.ContainsAny(groupName, "\r\n\x00") {
			return nil, errs.ErrForbidden
		}
		if _, duplicate := seen[groupName]; duplicate {
			continue
		}
		seen[groupName] = struct{}{}
		desiredGroups = append(desiredGroups, groupName)
	}
	sort.Strings(desiredGroups)
	return desiredGroups, nil
}

func (repository *Repository) oidcGroupsMatch(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, subjectID string,
	sessionRevision uint64,
	desiredGroups []string,
) (bool, error) {
	rows, err := tx.Query(ctx, queryAccessSyncListMemberships, pgx.NamedArgs{
		"organization_id": organizationID, "subject_id": subjectID,
	})
	if err != nil {
		return false, errs.ErrUnavailable
	}
	currentGroups := make([]string, 0, len(desiredGroups))
	currentRevision := sessionRevision
	for rows.Next() {
		var groupName string
		var currentSessionRevision uint64
		if err := rows.Scan(&groupName, &currentSessionRevision); err != nil {
			rows.Close()
			return false, errs.ErrUnavailable
		}
		currentGroups = append(currentGroups, groupName)
		if currentSessionRevision != sessionRevision {
			currentRevision = 0
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return false, errs.ErrUnavailable
	}
	rows.Close()
	return currentRevision == sessionRevision && slicesEqual(currentGroups, desiredGroups), nil
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type accessScanner interface{ Scan(...any) error }

type resolvedAccessSubject struct {
	id string
	entity.AccessSubject
}

type resolvedAccessTarget struct {
	resourceID, projectID, ownerSubjectRef string
	scope                                  entity.AccessScope
}

func (repository *Repository) ListPermissionRegistry(ctx context.Context, principal value.Principal) ([]entity.PermissionDefinition, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "access.view", organizationTarget(current.organizationRef)); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryAccessListPermissions)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.PermissionDefinition
	for rows.Next() {
		var item entity.PermissionDefinition
		if err := rows.Scan(&item.Key, &item.NameKey, &item.DescriptionKey, &item.Risk, &item.AllowedScopes, &item.ResourceKinds, &item.OwnerConditionSupported); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, tx.Commit(ctx)
}

func (repository *Repository) ListAccessSubjects(ctx context.Context, principal value.Principal, filter query.Filter, kind string) ([]entity.AccessSubject, string, error) {
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(filter.Page) + 1
	rows, err := tx.Query(ctx, queryAccessListSubjects, pgx.NamedArgs{
		"organization_id": current.organizationID, "kind": kind, "query": strings.TrimSpace(filter.Query),
		"cursor": filter.Page.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AccessSubject
	for rows.Next() {
		var item entity.AccessSubject
		if err := rows.Scan(&item.Ref, &item.Kind, &item.DisplayName, &item.Active, &item.OIDCGroupRefs); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	result, next := pageAccessSubjects(result, boundedPage(filter.Page))
	return result, next, tx.Commit(ctx)
}

func (repository *Repository) ListOIDCGroups(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.OIDCGroup, string, error) {
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(filter.Page) + 1
	rows, err := tx.Query(ctx, queryAccessListOIDCGroups, pgx.NamedArgs{
		"organization_id": current.organizationID, "query": strings.TrimSpace(filter.Query),
		"cursor": filter.Page.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.OIDCGroup
	for rows.Next() {
		var item entity.OIDCGroup
		if err := rows.Scan(&item.Ref, &item.DisplayName, &item.State, &item.MemberCount, &item.BindingCount, &item.LastSeenAt, &item.SynchronizedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if int32(len(result)) > boundedPage(filter.Page) {
		next = result[boundedPage(filter.Page)-1].Ref
		result = result[:boundedPage(filter.Page)]
	}
	return result, next, tx.Commit(ctx)
}

func (repository *Repository) ListAccessRoles(ctx context.Context, principal value.Principal, page query.Page, includeArchived bool) ([]entity.AccessRole, string, error) {
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(page) + 1
	rows, err := tx.Query(ctx, queryAccessListRoles, pgx.NamedArgs{
		"organization_id": current.organizationID, "include_archived": includeArchived,
		"cursor": page.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AccessRole
	for rows.Next() {
		item, scanErr := scanAccessRole(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if int32(len(result)) > boundedPage(page) {
		next = result[boundedPage(page)-1].Ref
		result = result[:boundedPage(page)]
	}
	return result, next, tx.Commit(ctx)
}

func (repository *Repository) ListAccessRoleVersions(ctx context.Context, principal value.Principal, roleRef string, page query.Page) (entity.AccessRole, []entity.AccessRoleVersion, string, error) {
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return entity.AccessRole{}, nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	role, err := repository.getAccessRole(ctx, tx, current.organizationID, roleRef)
	if err != nil {
		return entity.AccessRole{}, nil, "", err
	}
	cursor := int64(0)
	if page.Token != "" {
		cursor, err = strconv.ParseInt(page.Token, 10, 64)
		if err != nil || cursor < 1 {
			return entity.AccessRole{}, nil, "", errs.ErrInvalid
		}
	}
	limit := boundedPage(page) + 1
	rows, err := tx.Query(ctx, queryAccessListRoleVersions, pgx.NamedArgs{
		"organization_id": current.organizationID, "role_ref": roleRef, "cursor": cursor, "limit": limit,
	})
	if err != nil {
		return entity.AccessRole{}, nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var versions []entity.AccessRoleVersion
	for rows.Next() {
		version, scanErr := scanAccessRoleVersion(rows, roleRef)
		if scanErr != nil {
			return entity.AccessRole{}, nil, "", scanErr
		}
		versions = append(versions, version)
	}
	if rows.Err() != nil {
		return entity.AccessRole{}, nil, "", errs.ErrUnavailable
	}
	next := ""
	if int32(len(versions)) > boundedPage(page) {
		next = strconv.FormatInt(versions[boundedPage(page)-1].Revision, 10)
		versions = versions[:boundedPage(page)]
	}
	return role, versions, next, tx.Commit(ctx)
}

func (repository *Repository) ListAccessBindings(ctx context.Context, principal value.Principal, filter query.AccessBindingFilter) ([]entity.AccessBinding, string, error) {
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(filter.Page) + 1
	rows, err := tx.Query(ctx, queryAccessListBindings, pgx.NamedArgs{
		"organization_id": current.organizationID, "include_revoked": filter.IncludeRevoked,
		"subject_kind": filter.SubjectKind, "subject_ref": filter.SubjectRef, "role_ref": filter.RoleRef,
		"project_ref": filter.ProjectRef, "cursor": filter.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AccessBinding
	for rows.Next() {
		item, scanErr := scanAccessBinding(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if int32(len(result)) > boundedPage(filter.Page) {
		next = result[boundedPage(filter.Page)-1].Ref
		result = result[:boundedPage(filter.Page)]
	}
	return result, next, tx.Commit(ctx)
}

func (repository *Repository) QueryEffectiveAccess(ctx context.Context, principal value.Principal, subjectRef string, target entity.AccessScope, permissionKeys []string, evaluatedAt time.Time) (entity.EffectiveAccess, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.EffectiveAccess{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if subjectRef == "" {
		subjectRef = current.actorRef
	}
	if subjectRef != current.actorRef {
		if err := repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef)); err != nil {
			return entity.EffectiveAccess{}, err
		}
	}
	resolvedTarget, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, target)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	if visibility := visibilityPermission(resolvedTarget.scope.ResourceKind); visibility != "" {
		if err := repository.requireAccess(ctx, tx, current, visibility, resolvedTarget); err != nil {
			return entity.EffectiveAccess{}, errs.ErrNotFound
		}
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, subjectRef)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	if evaluatedAt.IsZero() {
		evaluatedAt = time.Now().UTC()
	}
	if len(permissionKeys) == 0 || len(permissionKeys) > 50 {
		return entity.EffectiveAccess{}, errs.ErrInvalid
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	result := entity.EffectiveAccess{Subject: subject.AccessSubject, EvaluatedAt: evaluatedAt.UTC()}
	for _, permissionKey := range permissionKeys {
		if _, known := access.Permission(permissionKey); !known {
			return entity.EffectiveAccess{}, errs.ErrInvalid
		}
		result.Decisions = append(result.Decisions, access.Evaluate(subject.AccessSubject, permissionKey, resolvedTarget.scope, resolvedTarget.ownerSubjectRef, bindings, result.EvaluatedAt))
	}
	return result, tx.Commit(ctx)
}

func (repository *Repository) SimulateAccess(ctx context.Context, principal value.Principal, input command.AccessSimulationInput) (entity.AccessSimulation, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.AccessSimulation{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef)); err != nil {
		return entity.AccessSimulation{}, err
	}
	resolvedTarget, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, input.Target)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	if visibility := visibilityPermission(resolvedTarget.scope.ResourceKind); visibility != "" {
		if err := repository.requireAccess(ctx, tx, current, visibility, resolvedTarget); err != nil {
			return entity.AccessSimulation{}, errs.ErrNotFound
		}
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, input.SubjectRef)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	input.Role.Name = "simulation"
	if err := access.ValidateRole(input.Role); err != nil || input.Binding.SubjectRef != subject.Ref || input.Binding.SubjectKind != subject.Kind {
		return entity.AccessSimulation{}, errs.ErrInvalid
	}
	now := time.Now().UTC()
	if input.EvaluatedAt != nil {
		now = input.EvaluatedAt.UTC()
	}
	roleVersion := entity.AccessRoleVersion{Ref: "simulation", RoleRef: "simulation", Name: input.Role.Name, PermissionKeys: input.Role.PermissionKeys, AllowedScopes: input.Role.AllowedScopes}
	if err := access.ValidateBinding(input.Binding, roleVersion, time.Now().UTC()); err != nil {
		return entity.AccessSimulation{}, errs.ErrInvalid
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	currentDecision := access.Evaluate(subject.AccessSubject, input.PermissionKey, resolvedTarget.scope, resolvedTarget.ownerSubjectRef, bindings, now)
	bindings = append(bindings, entity.AccessBinding{
		Ref: "simulation", State: "ACTIVE", Subject: subject.AccessSubject,
		RoleVersion: roleVersion, Scope: input.Binding.Scope, Conditions: input.Binding.Conditions,
	})
	simulated := access.Evaluate(subject.AccessSubject, input.PermissionKey, resolvedTarget.scope, resolvedTarget.ownerSubjectRef, bindings, now)
	if err := tx.Commit(ctx); err != nil {
		return entity.AccessSimulation{}, errs.ErrConflict
	}
	return entity.AccessSimulation{Subject: subject.AccessSubject, Current: currentDecision, Simulated: simulated, EvaluatedAt: now}, nil
}

func (repository *Repository) accessReadTransaction(ctx context.Context, principal value.Principal) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "access.view", organizationTarget(current.organizationRef)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, err
	}
	return current, tx, nil
}

func (repository *Repository) requireAccess(ctx context.Context, tx pgx.Tx, current scope, permission string, target any) error {
	var resolved resolvedAccessTarget
	switch value := target.(type) {
	case entity.AccessScope:
		if value.Kind == "RESOURCE_KIND" {
			if err := access.ValidateScope(value); err != nil {
				return errs.ErrInvalid
			}
			resolved.scope = value
		} else {
			var err error
			resolved, err = repository.resolveAccessTarget(ctx, tx, current.organizationID, value)
			if err != nil {
				return err
			}
		}
	case resolvedAccessTarget:
		resolved = value
	default:
		return errs.ErrInvalid
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return err
	}
	decision := access.Evaluate(subject.AccessSubject, permission, resolved.scope, resolved.ownerSubjectRef, bindings, time.Now().UTC())
	if !decision.Allowed {
		return errs.ErrNotFound
	}
	return nil
}

func (repository *Repository) resolveAccessSubject(ctx context.Context, tx pgx.Tx, organizationID, subjectRef string) (resolvedAccessSubject, error) {
	var result resolvedAccessSubject
	err := tx.QueryRow(ctx, queryAccessResolveSubject, pgx.NamedArgs{"organization_id": organizationID, "subject_ref": subjectRef}).Scan(
		&result.id, &result.Ref, &result.Kind, &result.DisplayName, &result.Active, &result.OIDCGroupRefs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, queryAccessResolveGroup, pgx.NamedArgs{"organization_id": organizationID, "subject_ref": subjectRef}).Scan(
			&result.id, &result.Ref, &result.DisplayName, &result.Active,
		)
		result.Kind = "OIDC_GROUP"
	}
	if errors.Is(err, pgx.ErrNoRows) || !result.Active {
		return resolvedAccessSubject{}, errs.ErrNotFound
	}
	if err != nil {
		return resolvedAccessSubject{}, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) loadAccessBindings(ctx context.Context, tx pgx.Tx, organizationID string, subject resolvedAccessSubject) ([]entity.AccessBinding, error) {
	subjectID, groupID := subject.id, ""
	if subject.Kind == "OIDC_GROUP" {
		subjectID, groupID = "", subject.id
	}
	rows, err := tx.Query(ctx, queryAccessBindingsForSubject, pgx.NamedArgs{"organization_id": organizationID, "subject_id": subjectID, "group_id": groupID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AccessBinding
	for rows.Next() {
		binding, scanErr := scanAccessBinding(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, binding)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) resolveAccessTarget(ctx context.Context, tx pgx.Tx, organizationID string, requested entity.AccessScope) (resolvedAccessTarget, error) {
	if requested.ResourceKind == "" || requested.ResourceKind != "ORGANIZATION" && strings.TrimSpace(requested.ResourceRef) == "" {
		return resolvedAccessTarget{}, errs.ErrInvalid
	}
	var result resolvedAccessTarget
	var related []byte
	err := tx.QueryRow(ctx, queryAccessResolveTarget, pgx.NamedArgs{
		"organization_id": organizationID, "resource_kind": requested.ResourceKind, "resource_ref": requested.ResourceRef,
	}).Scan(&result.resourceID, &result.projectID, &result.scope.ProjectRef, &result.ownerSubjectRef, &related)
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedAccessTarget{}, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(related, &result.scope.RelatedResourceRefs) != nil {
		return resolvedAccessTarget{}, errs.ErrUnavailable
	}
	if requested.ProjectRef != "" && requested.ProjectRef != result.scope.ProjectRef {
		return resolvedAccessTarget{}, errs.ErrNotFound
	}
	result.scope.ResourceKind = requested.ResourceKind
	result.scope.ResourceRef = requested.ResourceRef
	result.scope.Kind = "RESOURCE_INSTANCE"
	if requested.ResourceKind == "ORGANIZATION" {
		result.scope = organizationTarget(requested.ResourceRef)
	}
	return result, nil
}

func organizationTarget(ref string) entity.AccessScope {
	return entity.AccessScope{Kind: "ORGANIZATION", ResourceKind: "ORGANIZATION", ResourceRef: ref}
}

func visibilityPermission(kind string) string {
	switch kind {
	case "PROJECT":
		return "project.view"
	case "AGENT":
		return "agent.view"
	case "WORKFLOW":
		return "workflow.view"
	case "RUN", "OWNER_GATE":
		return "run.view"
	case "ARTIFACT":
		return "artifact.view"
	case "SCHEDULE":
		return "schedule.view"
	case "INTEGRATION":
		return "integration.view"
	case "SECRET":
		return "secret.view"
	case "PROVIDER_ACCOUNT":
		return "provider.account.view"
	default:
		return ""
	}
}

func scanAccessRole(scanner accessScanner) (entity.AccessRole, error) {
	var item entity.AccessRole
	var creator entity.User
	if err := scanner.Scan(
		&item.Ref, &item.Kind, &item.State, &item.Version, &item.UpdatedAt,
		&item.CurrentVersion.Ref, &item.CurrentVersion.Revision, &item.CurrentVersion.Name,
		&item.CurrentVersion.Description, &item.CurrentVersion.PermissionKeys, &item.CurrentVersion.AllowedScopes,
		&item.CurrentVersion.ChangeComment, &item.CurrentVersion.CreatedAt,
		&creator.Ref, &creator.DisplayName, &creator.EmailMasked, &item.BindingCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AccessRole{}, errs.ErrNotFound
		}
		return entity.AccessRole{}, errs.ErrUnavailable
	}
	item.CurrentVersion.RoleRef = item.Ref
	item.CurrentVersion.CreatedBy = creator
	return item, nil
}

func scanAccessRoleVersion(scanner accessScanner, roleRef string) (entity.AccessRoleVersion, error) {
	var item entity.AccessRoleVersion
	var creator entity.User
	if err := scanner.Scan(&item.Ref, &item.Revision, &item.Name, &item.Description, &item.PermissionKeys,
		&item.AllowedScopes, &item.ChangeComment, &item.CreatedAt, &creator.Ref, &creator.DisplayName, &creator.EmailMasked); err != nil {
		return entity.AccessRoleVersion{}, errs.ErrUnavailable
	}
	item.RoleRef = roleRef
	item.CreatedBy = creator
	return item, nil
}

func scanAccessBinding(scanner accessScanner) (entity.AccessBinding, error) {
	var internalID string
	var creator entity.User
	var item entity.AccessBinding
	if err := scanner.Scan(
		&internalID, &item.Ref, &item.Version, &item.State,
		&item.Subject.Kind, &item.Subject.Ref, &item.Subject.DisplayName, &item.Subject.Active,
		&item.RoleVersion.RoleRef, &item.RoleVersion.Ref, &item.RoleVersion.Revision,
		&item.RoleVersion.Name, &item.RoleVersion.Description, &item.RoleVersion.PermissionKeys,
		&item.RoleVersion.AllowedScopes, &item.RoleVersion.ChangeComment, &item.RoleVersion.CreatedAt,
		&creator.Ref, &creator.DisplayName, &creator.EmailMasked,
		&item.Scope.Kind, &item.Scope.ProjectRef, &item.Scope.ResourceKind, &item.Scope.ResourceRef,
		&item.Conditions.ValidFrom, &item.Conditions.ValidUntil, &item.Conditions.RequireOwner,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AccessBinding{}, errs.ErrNotFound
		}
		return entity.AccessBinding{}, errs.ErrUnavailable
	}
	item.RoleVersion.CreatedBy = creator
	return item, nil
}

func (repository *Repository) getAccessRole(ctx context.Context, tx pgx.Tx, organizationID, roleRef string) (entity.AccessRole, error) {
	return scanAccessRole(tx.QueryRow(ctx, queryAccessGetRole, pgx.NamedArgs{"organization_id": organizationID, "role_ref": roleRef}))
}

func (repository *Repository) getAccessBinding(ctx context.Context, tx pgx.Tx, organizationID, bindingRef string) (entity.AccessBinding, error) {
	return scanAccessBinding(tx.QueryRow(ctx, queryAccessGetBinding, pgx.NamedArgs{"organization_id": organizationID, "binding_ref": bindingRef}))
}

func pageAccessSubjects(items []entity.AccessSubject, limit int32) ([]entity.AccessSubject, string) {
	if int32(len(items)) <= limit {
		return items, ""
	}
	next := items[limit-1].Ref
	return items[:limit], next
}

func (repository *Repository) applyAccessCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	if err := repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef)); err != nil {
		return commandOutcome{}, err
	}
	switch input.Kind {
	case command.CreateAccessRole:
		return repository.createAccessRole(ctx, tx, current, input)
	case command.CreateAccessRoleVersion:
		return repository.createAccessRoleVersion(ctx, tx, current, input)
	case command.ArchiveAccessRole:
		return repository.archiveAccessRole(ctx, tx, current, input)
	case command.CreateAccessBinding, command.ChangeAccessBinding, command.RevokeAccessBinding:
		return repository.changeAccessBinding(ctx, tx, current, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) createAccessRole(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AccessRoleInput)
	if !ok || payload.RoleRef != "" || access.ValidateRole(payload) != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	roleRef, err := newRef("arole")
	if err != nil {
		return commandOutcome{}, err
	}
	var roleID string
	var ignoredRef, ignoredKind, ignoredState string
	var ignoredVersion int64
	var ignoredUpdated time.Time
	if err := tx.QueryRow(ctx, queryAccessInsertRole, pgx.NamedArgs{
		"ref": roleRef, "organization_id": current.organizationID, "stable_key": "", "kind": "CUSTOM", "created_by": current.actorID,
	}).Scan(&roleID, &ignoredRef, &ignoredKind, &ignoredState, &ignoredVersion, &ignoredUpdated); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, _, err := repository.insertAccessRoleVersion(ctx, tx, current, roleID, roleRef, 1, 0, payload); err != nil {
		return commandOutcome{}, err
	}
	role, err := repository.getAccessRole(ctx, tx, current.organizationID, roleRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return accessRoleOutcome(role), nil
}

func (repository *Repository) createAccessRoleVersion(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AccessRoleInput)
	if !ok || payload.RoleRef == "" || input.Mutation.ExpectedVersion == nil || access.ValidateRole(payload) != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var roleID, kind, state string
	var version, revision int64
	if err := tx.QueryRow(ctx, queryAccessResolveRole, pgx.NamedArgs{"organization_id": current.organizationID, "role_ref": payload.RoleRef}).Scan(&roleID, &kind, &state, &version, &revision); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if kind == "SYSTEM" {
		return commandOutcome{}, errs.ErrProtected
	}
	if state != "ACTIVE" || version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if _, _, err := repository.insertAccessRoleVersion(ctx, tx, current, roleID, payload.RoleRef, revision+1, version, payload); err != nil {
		return commandOutcome{}, err
	}
	role, err := repository.getAccessRole(ctx, tx, current.organizationID, payload.RoleRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return accessRoleOutcome(role), nil
}

func (repository *Repository) insertAccessRoleVersion(ctx context.Context, tx pgx.Tx, current scope, roleID, roleRef string, revision, expectedRoleVersion int64, payload command.AccessRoleInput) (string, string, error) {
	versionRef, err := newRef("arv")
	if err != nil {
		return "", "", err
	}
	var versionID, returnedRef, name, description, changeComment string
	var returnedRevision int64
	var permissionKeys, allowedScopes []string
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryAccessInsertRoleVersion, pgx.NamedArgs{
		"ref": versionRef, "organization_id": current.organizationID, "role_id": roleID, "revision": revision,
		"name": payload.Name, "description": payload.Description, "permission_keys": payload.PermissionKeys,
		"allowed_scopes": payload.AllowedScopes, "change_comment": payload.ChangeComment, "created_by": current.actorID,
	}).Scan(&versionID, &returnedRef, &returnedRevision, &name, &description, &permissionKeys, &allowedScopes, &changeComment, &createdAt); err != nil {
		return "", "", mapWriteError(err)
	}
	var activatedVersion int64
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, queryAccessActivateRoleVersion, pgx.NamedArgs{
		"role_version_id": versionID, "role_id": roleID, "organization_id": current.organizationID, "expected_version": expectedRoleVersion,
	}).Scan(&activatedVersion, &updatedAt); errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrVersionMismatch
	} else if err != nil {
		return "", "", errs.ErrUnavailable
	}
	return versionID, returnedRef, nil
}

func (repository *Repository) bootstrapAccess(ctx context.Context, tx pgx.Tx, organizationID, organizationRef, systemSubjectID string) error {
	for _, definition := range access.Definitions() {
		if _, err := tx.Exec(ctx, queryAccessBootstrapInsertPermission, pgx.NamedArgs{
			"permission_key": definition.Key, "name_key": definition.NameKey,
			"description_key": definition.DescriptionKey, "risk": definition.Risk,
			"allowed_scopes": definition.AllowedScopes, "resource_kinds": definition.ResourceKinds,
			"owner_condition_supported": definition.OwnerConditionSupported,
		}); err != nil {
			return fmt.Errorf("seed permission registry: %w", errs.ErrUnavailable)
		}
	}
	all := make([]string, 0, len(access.Definitions()))
	for _, definition := range access.Definitions() {
		all = append(all, definition.Key)
	}
	roles := []struct {
		key, name   string
		permissions []string
	}{
		{key: "OWNER", name: "i18n:SYSTEM_ROLE_OWNER", permissions: all},
		{key: "ADMINISTRATOR", name: "i18n:SYSTEM_ROLE_ADMINISTRATOR", permissions: all},
		{key: "OPERATOR", name: "i18n:SYSTEM_ROLE_OPERATOR", permissions: []string{"organization.view", "project.create"}},
		{key: "MEMBER", name: "i18n:SYSTEM_ROLE_MEMBER", permissions: []string{"organization.view"}},
		{key: "AUDITOR", name: "i18n:SYSTEM_ROLE_AUDITOR", permissions: []string{"organization.view", "project.view", "agent.view", "workflow.view", "run.view", "artifact.view", "schedule.view", "integration.view", "audit.view", "access.view"}},
	}
	current := scope{organizationID: organizationID, organizationRef: organizationRef, actorID: systemSubjectID}
	var ownerVersionID string
	for _, specification := range roles {
		roleRef, err := newRef("arole")
		if err != nil {
			return err
		}
		var roleID, returnedRef, kind, state string
		var version int64
		var updatedAt time.Time
		if err := tx.QueryRow(ctx, queryAccessInsertRole, pgx.NamedArgs{
			"ref": roleRef, "organization_id": organizationID, "stable_key": specification.key,
			"kind": "SYSTEM", "created_by": systemSubjectID,
		}).Scan(&roleID, &returnedRef, &kind, &state, &version, &updatedAt); err != nil {
			return fmt.Errorf("seed system access role: %w", errs.ErrUnavailable)
		}
		payload := command.AccessRoleInput{
			Name: specification.name, Description: "i18n:" + "SYSTEM_ROLE_" + specification.key + "_DESCRIPTION",
			PermissionKeys: specification.permissions, AllowedScopes: []string{"ORGANIZATION"},
			ChangeComment: "i18n:SYSTEM_ROLE_BOOTSTRAP",
		}
		versionID, _, err := repository.insertAccessRoleVersion(ctx, tx, current, roleID, roleRef, 1, 0, payload)
		if err != nil {
			return err
		}
		if specification.key == "OWNER" {
			ownerVersionID = versionID
		}
	}
	bindingRef, err := newRef("abnd")
	if err != nil {
		return err
	}
	var internalID, returnedRef, state string
	var bindingVersion int64
	var createdAt, updatedAt time.Time
	if err := tx.QueryRow(ctx, queryAccessInsertBinding, pgx.NamedArgs{
		"ref": bindingRef, "organization_id": organizationID, "subject_kind": "SERVICE",
		"subject_id": systemSubjectID, "oidc_group_id": "", "role_version_id": ownerVersionID,
		"scope_kind": "ORGANIZATION", "project_id": "", "resource_kind": "", "resource_id": "",
		"valid_from": nil, "valid_until": nil, "require_owner": false, "created_by": systemSubjectID,
	}).Scan(&internalID, &returnedRef, &bindingVersion, &state, &createdAt, &updatedAt); err != nil {
		return fmt.Errorf("seed system access binding: %w", errs.ErrUnavailable)
	}
	return nil
}

func (repository *Repository) archiveAccessRole(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AccessRoleInput)
	if !ok || payload.RoleRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var version int64
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, queryAccessArchiveRole, pgx.NamedArgs{
		"organization_id": current.organizationID, "role_ref": payload.RoleRef, "expected_version": *input.Mutation.ExpectedVersion,
	}).Scan(&version, &updatedAt); errors.Is(err, pgx.ErrNoRows) {
		var kind, state, roleID string
		var currentVersion, revision int64
		err := tx.QueryRow(ctx, queryAccessResolveRole, pgx.NamedArgs{"organization_id": current.organizationID, "role_ref": payload.RoleRef}).Scan(&roleID, &kind, &state, &currentVersion, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if kind == "SYSTEM" {
			return commandOutcome{}, errs.ErrProtected
		}
		return commandOutcome{}, errs.ErrVersionMismatch
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	role, err := repository.getAccessRole(ctx, tx, current.organizationID, payload.RoleRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return accessRoleOutcome(role), nil
}

func (repository *Repository) changeAccessBinding(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AccessBindingInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.RevokeAccessBinding {
		if payload.BindingRef == "" || input.Mutation.ExpectedVersion == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		var internalID, ref, state string
		var version int64
		var createdAt, updatedAt time.Time
		if err := tx.QueryRow(ctx, queryAccessRevokeBinding, pgx.NamedArgs{
			"organization_id": current.organizationID, "binding_ref": payload.BindingRef, "expected_version": *input.Mutation.ExpectedVersion,
		}).Scan(&internalID, &ref, &version, &state, &createdAt, &updatedAt); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, repository.mapAccessBindingMutationMiss(ctx, tx, current.organizationID, payload.BindingRef)
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		binding, err := repository.getAccessBinding(ctx, tx, current.organizationID, payload.BindingRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return accessBindingOutcome(binding), nil
	}
	if input.Kind == command.ChangeAccessBinding {
		if payload.BindingRef == "" || input.Mutation.ExpectedVersion == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		existing, err := repository.getAccessBinding(ctx, tx, current.organizationID, payload.BindingRef)
		if err != nil {
			return commandOutcome{}, err
		}
		payload.SubjectKind, payload.SubjectRef = existing.Subject.Kind, existing.Subject.Ref
	} else if payload.BindingRef != "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	roleVersionID, roleVersion, err := repository.resolveAccessRoleVersion(ctx, tx, current.organizationID, payload.RoleVersionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := access.ValidateBinding(payload, roleVersion, time.Now().UTC()); err != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	subjectID, groupID, err := repository.resolveBindingSubject(ctx, tx, current.organizationID, payload.SubjectKind, payload.SubjectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	projectID, resourceID, err := repository.resolveBindingScope(ctx, tx, current.organizationID, payload.Scope)
	if err != nil {
		return commandOutcome{}, err
	}
	args := pgx.NamedArgs{
		"organization_id": current.organizationID, "role_version_id": roleVersionID,
		"scope_kind": payload.Scope.Kind, "project_id": projectID, "resource_kind": payload.Scope.ResourceKind,
		"resource_id": resourceID, "valid_from": payload.Conditions.ValidFrom, "valid_until": payload.Conditions.ValidUntil,
		"require_owner": payload.Conditions.RequireOwner,
	}
	if input.Kind == command.CreateAccessBinding {
		ref, refErr := newRef("abnd")
		if refErr != nil {
			return commandOutcome{}, refErr
		}
		args["ref"], args["subject_kind"], args["subject_id"], args["oidc_group_id"], args["created_by"] = ref, payload.SubjectKind, subjectID, groupID, current.actorID
		var internalID, returnedRef, state string
		var version int64
		var createdAt, updatedAt time.Time
		if err := tx.QueryRow(ctx, queryAccessInsertBinding, args).Scan(&internalID, &returnedRef, &version, &state, &createdAt, &updatedAt); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		payload.BindingRef = returnedRef
	} else {
		args["binding_ref"], args["expected_version"] = payload.BindingRef, *input.Mutation.ExpectedVersion
		var internalID, returnedRef, state string
		var version int64
		var createdAt, updatedAt time.Time
		if err := tx.QueryRow(ctx, queryAccessChangeBinding, args).Scan(&internalID, &returnedRef, &version, &state, &createdAt, &updatedAt); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, repository.mapAccessBindingMutationMiss(ctx, tx, current.organizationID, payload.BindingRef)
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	binding, err := repository.getAccessBinding(ctx, tx, current.organizationID, payload.BindingRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return accessBindingOutcome(binding), nil
}

func (repository *Repository) resolveAccessRoleVersion(ctx context.Context, tx pgx.Tx, organizationID, ref string) (string, entity.AccessRoleVersion, error) {
	var id string
	var item entity.AccessRoleVersion
	var creator entity.User
	if err := tx.QueryRow(ctx, queryAccessResolveRoleVersion, pgx.NamedArgs{"organization_id": organizationID, "role_version_ref": ref}).Scan(
		&id, &item.Ref, &item.RoleRef, &item.Revision, &item.Name, &item.Description, &item.PermissionKeys,
		&item.AllowedScopes, &item.ChangeComment, &item.CreatedAt, &creator.Ref, &creator.DisplayName, &creator.EmailMasked,
	); errors.Is(err, pgx.ErrNoRows) {
		return "", entity.AccessRoleVersion{}, errs.ErrNotFound
	} else if err != nil {
		return "", entity.AccessRoleVersion{}, errs.ErrUnavailable
	}
	item.CreatedBy = creator
	return id, item, nil
}

func (repository *Repository) resolveBindingSubject(ctx context.Context, tx pgx.Tx, organizationID, kind, ref string) (string, string, error) {
	var returnedKind, id, returnedRef, displayName string
	var active bool
	if err := tx.QueryRow(ctx, queryAccessResolveBindingSubject, pgx.NamedArgs{
		"organization_id": organizationID, "subject_kind": kind, "subject_ref": ref,
	}).Scan(&returnedKind, &id, &returnedRef, &displayName, &active); errors.Is(err, pgx.ErrNoRows) || !active {
		return "", "", errs.ErrNotFound
	} else if err != nil {
		return "", "", errs.ErrUnavailable
	}
	if kind == "OIDC_GROUP" {
		return "", id, nil
	}
	return id, "", nil
}

func (repository *Repository) resolveBindingScope(ctx context.Context, tx pgx.Tx, organizationID string, scope entity.AccessScope) (string, string, error) {
	switch scope.Kind {
	case "ORGANIZATION":
		return "", "", nil
	case "PROJECT":
		resolved, err := repository.resolveAccessTarget(ctx, tx, organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: scope.ProjectRef})
		return resolved.projectID, "", err
	case "RESOURCE_KIND":
		if scope.ProjectRef == "" {
			return "", "", nil
		}
		resolved, err := repository.resolveAccessTarget(ctx, tx, organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: scope.ProjectRef})
		return resolved.projectID, "", err
	case "RESOURCE_INSTANCE":
		resolved, err := repository.resolveAccessTarget(ctx, tx, organizationID, scope)
		if err != nil {
			return "", "", err
		}
		return resolved.projectID, resolved.resourceID, nil
	default:
		return "", "", errs.ErrInvalid
	}
}

func (repository *Repository) mapAccessBindingMutationMiss(ctx context.Context, tx pgx.Tx, organizationID, ref string) error {
	_, err := repository.getAccessBinding(ctx, tx, organizationID, ref)
	if errors.Is(err, errs.ErrNotFound) {
		return errs.ErrNotFound
	}
	if err != nil {
		return err
	}
	return errs.ErrVersionMismatch
}

func accessRoleOutcome(role entity.AccessRole) commandOutcome {
	return commandOutcome{result: command.Result{AccessRole: &role}, resourceKind: "ACCESS_ROLE", resourceRef: role.Ref, summary: "i18n:ACCESS_ROLE_CHANGED", platformEvent: "MEMBERSHIP_CHANGED"}
}

func accessBindingOutcome(binding entity.AccessBinding) commandOutcome {
	return commandOutcome{result: command.Result{AccessBinding: &binding}, projectRef: binding.Scope.ProjectRef, resourceKind: "ACCESS_BINDING", resourceRef: binding.Ref, summary: "i18n:ACCESS_BINDING_CHANGED", platformEvent: "MEMBERSHIP_CHANGED"}
}
