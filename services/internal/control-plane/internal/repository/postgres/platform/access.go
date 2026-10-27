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
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// syncOIDCGroups reconciles groups only for a nonzero verified external-session revision. It
// checks existing observations before and after locking the tenant subject, then replaces
// memberships in the caller transaction; it never commits independently.
// syncOIDCGroups сверяет группы только для ненулевой проверенной ревизии внешней сессии.
// Наблюдения проверяются до и после блокировки субъекта организации, затем членства заменяются в
// транзакции вызывающего; самостоятельного commit нет.
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

// normalizedOIDCGroups requires an issuer and at most 100 input groups, trims names, rejects
// empty/control-containing or oversized names, and returns sorted unique group names without
// performing identity verification.
// normalizedOIDCGroups требует issuer и не более 100 входных групп, обрезает пробелы, отклоняет
// пустые, длинные имена и управляющие символы и возвращает отсортированные уникальные имена без
// проверки идентичности.
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

// oidcGroupsMatch compares stored ordered group names with the desired normalized list and
// requires every observation to have the supplied session revision and be recent. Query/scan
// failures close rows and return a safe availability error.
// oidcGroupsMatch сравнивает сохранённые упорядоченные имена с нормализованным списком и требует у
// каждого наблюдения заданную ревизию сессии и признак свежести. Ошибки запроса или Scan закрывают
// rows и дают безопасную ошибку доступности.
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
		var recentlyObserved bool
		if err := rows.Scan(&groupName, &currentSessionRevision, &recentlyObserved); err != nil {
			rows.Close()
			return false, errs.ErrUnavailable
		}
		currentGroups = append(currentGroups, groupName)
		if currentSessionRevision != sessionRevision || !recentlyObserved {
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

// slicesEqual compares ordered string lists element by element, treating nil and empty lists as
// equal; it neither sorts nor normalizes its inputs.
// slicesEqual поэлементно сравнивает упорядоченные списки строк, считая nil и пустой список
// равными; входы не сортируются и не нормализуются.
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

// accessScanner aliases the common positional scan contract for access projections.
// Row and cursor readers share the same method set; transaction ownership and concrete
// projection/error mapping remain with the existing callers.
// accessScanner использует общий контракт позиционного Scan для проекций доступа.
// Одиночная строка и курсор имеют прежний набор методов; владение транзакцией и конкретные
// проекции/преобразование ошибок остаются у исходных вызывающих.
type accessScanner = rowScanner

// resolvedAccessSubject pairs a tenant-resolved internal identity with the
// public subject and trusted group snapshot used by the evaluator.
// resolvedAccessSubject связывает разрешённую внутри организации внутреннюю
// identity с публичным субъектом и доверенным снимком групп для evaluator.
type resolvedAccessSubject struct {
	id string
	entity.AccessSubject
}

// resolvedAccessTarget carries owner-resolved resource/project IDs, owner ref
// and related-resource scope; public request locators do not populate it directly.
// resolvedAccessTarget хранит разрешённые владельцем ID ресурса и проекта,
// ref владельца и область связанных ресурсов; публичные входы не заполняют её напрямую.
type resolvedAccessTarget struct {
	resourceID, projectID, ownerSubjectRef string
	scope                                  entity.AccessScope
}

// ListPermissionRegistry resolves the caller, requires organization access.view in a read-only
// transaction and returns permission metadata from PostgreSQL. Catalog entries describe
// capabilities and do not confer permissions.
// ListPermissionRegistry разрешает вызывающего, требует организационное access.view в read-only
// транзакции и возвращает метаданные разрешений из PostgreSQL. Записи каталога описывают
// возможности и не предоставляют полномочий.
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

// ListAccessSubjects uses the authorized access read transaction to page tenant subjects by
// kind/query and raw ref cursor. One lookahead row determines continuation; commit errors are
// returned to the caller with the assembled page.
// ListAccessSubjects использует авторизованную транзакцию чтения для страниц субъектов организации
// по виду, запросу и прямому ref-курсору. Лишняя строка определяет продолжение; ошибка commit
// возвращается вместе с собранной страницей.
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

// ListOIDCGroups pages tenant OIDC groups, their counts and observation timestamps after
// access.view authorization. It uses a raw group-ref cursor and trims the lookahead row without
// changing membership state.
// ListOIDCGroups выдаёт страницы OIDC групп организации, их счётчики и время наблюдений после
// проверки access.view. Используется прямой ref-курсор группы; лишняя строка удаляется без
// изменения членства.
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
	return readAccessRowsPage(ctx, tx, rows, boundedPage(filter.Page), scanOIDCGroup, func(item entity.OIDCGroup) string { return item.Ref })
}

// ListAccessRoles validates bounded UTF-8 search text and system-role aliases before
// authorization, then reads tenant roles and optional archived entries. Each result includes its
// current immutable version; continuation uses the last retained role ref.
// ListAccessRoles проверяет ограниченный UTF-8 запрос и псевдонимы системных ролей до авторизации,
// затем читает роли организации, при необходимости архивные. Результат включает текущую
// неизменяемую версию; продолжение задаётся ref последней возвращённой роли.
func (repository *Repository) ListAccessRoles(ctx context.Context, principal value.Principal, filter query.Filter, includeArchived bool) ([]entity.AccessRole, string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if !validAccessRoleQuery(filter.Query, filter.Aliases) {
		return nil, "", errs.ErrInvalid
	}
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(filter.Page) + 1
	rows, err := tx.Query(ctx, queryAccessListRoles, pgx.NamedArgs{
		"organization_id": current.organizationID, "include_archived": includeArchived,
		"query": filter.Query, "query_aliases": filter.Aliases,
		"cursor": filter.Page.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	return readAccessRowsPage(ctx, tx, rows, boundedPage(filter.Page), scanAccessRole, func(item entity.AccessRole) string { return item.Ref })
}

// validAccessRoleQueryAliases accepts at most ten entries from the explicit system-role
// name/description localization keys. It allows an empty list and does not impose uniqueness or
// expand arbitrary localization aliases.
// validAccessRoleQueryAliases допускает не более десяти значений из явных ключей перевода имён и
// описаний системных ролей. Пустой список разрешён; уникальность не проверяется и произвольные
// псевдонимы не расширяются.
func validAccessRoleQueryAliases(aliases []string) bool {
	if len(aliases) > 10 {
		return false
	}
	for _, alias := range aliases {
		switch alias {
		case "i18n:SYSTEM_ROLE_OWNER", "i18n:SYSTEM_ROLE_OWNER_DESCRIPTION",
			"i18n:SYSTEM_ROLE_ADMINISTRATOR", "i18n:SYSTEM_ROLE_ADMINISTRATOR_DESCRIPTION",
			"i18n:SYSTEM_ROLE_OPERATOR", "i18n:SYSTEM_ROLE_OPERATOR_DESCRIPTION",
			"i18n:SYSTEM_ROLE_MEMBER", "i18n:SYSTEM_ROLE_MEMBER_DESCRIPTION",
			"i18n:SYSTEM_ROLE_AUDITOR", "i18n:SYSTEM_ROLE_AUDITOR_DESCRIPTION":
		default:
			return false
		}
	}
	return true
}

// ListAccessRoleVersions resolves one tenant role after access.view and pages its immutable
// revisions using a positive numeric cursor. Role and versions are read in the same caller-owned
// transaction and no current-version pointer changes.
// ListAccessRoleVersions разрешает одну роль организации после access.view и выдаёт её
// неизменяемые ревизии по положительному числовому курсору. Роль и версии читаются в одной
// транзакции; указатель текущей версии не меняется.
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

// ListAccessBindings validates role-oriented search text/aliases and returns tenant bindings
// filtered by subject, role, project and revoked state. It authorizes access.view before querying
// and uses the retained binding ref as continuation.
// ListAccessBindings проверяет поиск и псевдонимы ролей и возвращает привязки организации по
// субъекту, роли, проекту и признаку отзыва. access.view проверяется до запроса; продолжение
// задаётся ref последней возвращённой привязки.
func (repository *Repository) ListAccessBindings(ctx context.Context, principal value.Principal, filter query.AccessBindingFilter) ([]entity.AccessBinding, string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if !validAccessRoleQuery(filter.Query, filter.Aliases) {
		return nil, "", errs.ErrInvalid
	}
	current, tx, err := repository.accessReadTransaction(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	limit := boundedPage(filter.Page) + 1
	rows, err := tx.Query(ctx, queryAccessListBindings, pgx.NamedArgs{
		"organization_id": current.organizationID, "include_revoked": filter.IncludeRevoked,
		"subject_kind": filter.SubjectKind, "subject_ref": filter.SubjectRef, "role_ref": filter.RoleRef,
		"project_ref": filter.ProjectRef, "query": filter.Query, "query_aliases": filter.Aliases,
		"cursor": filter.Token, "limit": limit,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	return readAccessRowsPage(ctx, tx, rows, boundedPage(filter.Page), scanAccessBinding, func(item entity.AccessBinding) string { return item.Ref })
}

// QueryEffectiveAccess evaluates one active tenant subject against an owner-resolved visible
// target in a repeatable-read snapshot. Inspecting another subject requires access.manage;
// organization image permissions additionally require organization ownership/admin and management
// access. Public target coordinates are preserved in the response.
// QueryEffectiveAccess вычисляет доступ активного субъекта организации к видимой цели, разрешённой
// владельцем, в repeatable-read снимке. Чужой субъект требует access.manage; организационные права
// образов дополнительно требуют роли владельца/администратора и управления организацией. В ответе
// сохраняются публичные координаты цели.
func (repository *Repository) QueryEffectiveAccess(ctx context.Context, principal value.Principal, subjectRef string, target entity.AccessScope, permissionKeys []string, evaluatedAt time.Time) (entity.EffectiveAccess, error) {
	current, tx, err := repository.beginScopedTransaction(ctx, principal, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.EffectiveAccess{}, err
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
	resolvedTarget, err := repository.resolveAccessQueryTarget(ctx, tx, current, target)
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
		decision := access.Evaluate(subject.AccessSubject, permissionKey, resolvedTarget.scope, resolvedTarget.ownerSubjectRef, bindings, result.EvaluatedAt)
		organizationImage := resolvedTarget.scope.ResourceKind == "ORGANIZATION" || resolvedTarget.scope.ResourceKind == "ROLE_IMAGE" && resolvedTarget.projectID == ""
		if organizationImage && (permissionKey == "image.build" || permissionKey == "image.source.view" || permissionKey == "image.source.manage") {
			var organizationID, organizationRef, actorID, actorRef, actorName, role string
			if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, subject.Ref, current.organizationRef).Scan(&organizationID, &organizationRef, &actorID, &actorRef, &actorName, &role); err != nil {
				return entity.EffectiveAccess{}, errs.ErrNotFound
			}
			organizationDecision := access.Evaluate(subject.AccessSubject, "organization.manage", organizationTarget(current.organizationRef), "", bindings, result.EvaluatedAt)
			if current.authorityProjectID != "" || role != "OWNER" && role != "ADMINISTRATOR" || !organizationDecision.Allowed {
				decision.Allowed = false
				decision.Explanation = []entity.AccessExplanationStep{{Code: "ORGANIZATION_OWNER_REQUIRED"}}
			}
		}
		// Ответ сохраняет проверенный public scope; внутренний locator нужен только evaluator.
		decision.Target = target
		result.Decisions = append(result.Decisions, decision)
	}
	return result, tx.Commit(ctx)
}

// SimulateAccess requires access.manage and target visibility, validates a hypothetical
// role/binding for the resolved subject and compares current and augmented evaluator decisions in
// a read-only snapshot. The simulation creates no role, binding, grant or persistent command
// result.
// SimulateAccess требует access.manage и видимость цели, проверяет гипотетические роль и привязку
// разрешённого субъекта и сравнивает исходное решение с дополненным в read-only снимке. Симуляция
// не создаёт роль, привязку, grant или устойчивый результат команды.
func (repository *Repository) SimulateAccess(ctx context.Context, principal value.Principal, input command.AccessSimulationInput) (entity.AccessSimulation, error) {
	current, tx, err := repository.beginScopedTransaction(ctx, principal, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef)); err != nil {
		return entity.AccessSimulation{}, err
	}
	resolvedTarget, err := repository.resolveAccessQueryTarget(ctx, tx, current, input.Target)
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
	currentDecision.Target, simulated.Target = input.Target, input.Target
	if err := tx.Commit(ctx); err != nil {
		return entity.AccessSimulation{}, errs.ErrConflict
	}
	return entity.AccessSimulation{Subject: subject.AccessSubject, Current: currentDecision, Simulated: simulated, EvaluatedAt: now}, nil
}

// accessReadTransaction resolves the principal and opens a read-only transaction with organization
// access.view already checked. Failure rolls the transaction back here; successful callers own its
// subsequent commit/rollback.
// accessReadTransaction разрешает principal и открывает read-only транзакцию с уже проверенным
// организационным access.view. При отказе rollback выполняется здесь; успешный caller отвечает за
// последующий commit/rollback.
func (repository *Repository) accessReadTransaction(ctx context.Context, principal value.Principal) (scope, pgx.Tx, error) {
	current, tx, err := repository.beginScopedTransaction(ctx, principal, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, err
	}
	if err := repository.requireAccess(ctx, tx, current, "access.view", organizationTarget(current.organizationRef)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, err
	}
	return current, tx, nil
}

// requireAccess resolves a target or accepts an internal resolved target, checks the
// authority-project boundary, loads the active actor and bindings, and evaluates the exact
// permission at current UTC time. Denial is hidden as NotFound; no binding is created.
// requireAccess разрешает цель либо принимает внутреннюю разрешённую цель, проверяет границу
// authority-project, загружает активного actor с привязками и вычисляет точное разрешение на
// текущее UTC время. Отказ скрывается как NotFound; привязка не создаётся.
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
	if current.authorityProjectID != "" && resolved.projectID != "" && current.authorityProjectID != resolved.projectID {
		return errs.ErrNotFound
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

// resolveAccessSubject loads an active tenant subject and its groups, falling back to an OIDC
// group only when the subject row is absent. Missing/inactive identities return NotFound;
// caller-provided refs alone never establish identity.
// resolveAccessSubject загружает активного субъекта организации и его группы, переходя к поиску
// OIDC группы только при отсутствии строки субъекта. Отсутствующие или неактивные идентичности
// дают NotFound; один переданный ref не доказывает identity.
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

// loadAccessBindings loads typed bindings for an already resolved tenant subject, selecting the
// separate group-ID path for OIDC groups. It scans every row before returning and does not
// independently evaluate target permissions.
// loadAccessBindings загружает типизированные привязки уже разрешённого субъекта организации,
// выбирая отдельный путь group-ID для OIDC группы. Все строки читаются до возврата; разрешения на
// цель самостоятельно не вычисляются.
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

// resolveAccessQueryTarget validates the public scope, binds organization scope to the current
// tenant and converts project scope to an owner lookup. Resource-kind scope with a project
// additionally requires project.view; concrete resources use the internal resolver.
// resolveAccessQueryTarget проверяет публичную область, связывает организационную область с
// текущим tenant и преобразует проектную в поиск владельца. Область вида ресурсов с проектом
// дополнительно требует project.view; конкретные ресурсы проходят внутренний resolver.
func (repository *Repository) resolveAccessQueryTarget(ctx context.Context, tx pgx.Tx, current scope, requested entity.AccessScope) (resolvedAccessTarget, error) {
	if err := access.ValidateScope(requested); err != nil {
		return resolvedAccessTarget{}, errs.ErrInvalid
	}
	switch requested.Kind {
	case "ORGANIZATION":
		if requested.ResourceRef != "" && requested.ResourceRef != current.organizationRef {
			return resolvedAccessTarget{}, errs.ErrNotFound
		}
		requested = organizationTarget(current.organizationRef)
	case "PROJECT":
		requested = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: requested.ProjectRef, ResourceKind: "PROJECT", ResourceRef: requested.ProjectRef}
	case "RESOURCE_KIND":
		resolved := resolvedAccessTarget{scope: requested}
		if requested.ProjectRef != "" {
			project, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: requested.ProjectRef})
			if err != nil {
				return resolvedAccessTarget{}, err
			}
			if err := repository.requireAccess(ctx, tx, current, "project.view", project); err != nil {
				return resolvedAccessTarget{}, errs.ErrNotFound
			}
			resolved.projectID = project.projectID
		}
		return resolved, nil
	}
	return repository.resolveAccessTarget(ctx, tx, current.organizationID, requested)
}

// resolveAccessTarget resolves concrete resource identity, project, owner and related refs inside
// the given tenant. It rejects missing resources and contradictory project locators; relationships
// come from SQL rather than the requested payload.
// resolveAccessTarget разрешает идентичность ресурса, проект, владельца и связанные refs внутри
// заданной организации. Отсутствующий ресурс и противоречивый проектный адресат отклоняются; связи
// приходят из SQL, а не входных данных.
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

// organizationTarget constructs the organization-shaped access target from a trusted organization
// ref. The constructor performs no lookup, validation or authorization itself.
// organizationTarget собирает организационную цель доступа по доверенному ref организации. Сам
// конструктор не выполняет поиск, валидацию или авторизацию.
func organizationTarget(ref string) entity.AccessScope {
	return entity.AccessScope{Kind: "ORGANIZATION", ResourceKind: "ORGANIZATION", ResourceRef: ref}
}

// visibilityPermission maps resource kinds to the read permission needed before exposing access
// explanations; run visibility also covers owner gates. Unmapped kinds return an empty key and
// rely on their caller-specific eligibility path.
// visibilityPermission сопоставляет видам ресурсов разрешение чтения перед выдачей объяснения
// доступа; видимость запуска также покрывает owner gate. Для отсутствующего вида возвращается
// пустой ключ и действует проверка допуска вызывающего.
func visibilityPermission(kind string) string {
	switch kind {
	case "RUNTIME_ENVIRONMENT":
		return "project.view"
	case "ROLE_IMAGE":
		return "project.view"
	case "MEMBERSHIP":
		return "access.manage"
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

// scanAccessRole scans one role with its current version, creator and binding count, then assigns
// the parent role ref to the version. No row maps to NotFound; other scan failures return
// Unavailable without partial entities.
// scanAccessRole читает роль с текущей версией, создателем и числом привязок, затем назначает
// версии ref родительской роли. Отсутствие строки даёт NotFound; прочие ошибки Scan дают
// Unavailable без частичных сущностей.
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

// scanAccessRoleVersion scans an immutable role revision and creator, attaching the role ref
// already resolved by the caller. All scan errors map to Unavailable; the helper does not resolve
// tenant ownership.
// scanAccessRoleVersion читает неизменяемую ревизию роли и создателя, назначая уже разрешённый
// вызывающим ref роли. Любая ошибка Scan даёт Unavailable; владение в организации helper не
// разрешает.
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

// scanAccessBinding scans the complete binding projection with subject, immutable role version,
// scope and conditions, discarding the internal database ID. It preserves presentation kind for
// protected membership bindings and returns no partial binding on scan failure.
// scanAccessBinding читает полную проекцию привязки с субъектом, неизменяемой версией роли,
// областью и условиями, отбрасывая внутренний ID базы. Presentation kind сохраняется для защиты
// привязок членства; при ошибке Scan частичный результат не возвращается.
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
		&item.CreatedAt, &item.UpdatedAt, &item.PresentationKind,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AccessBinding{}, errs.ErrNotFound
		}
		return entity.AccessBinding{}, errs.ErrUnavailable
	}
	item.RoleVersion.CreatedBy = creator
	return item, nil
}

// getAccessRole reads a role by tenant and public role ref using the shared projection scanner. It
// neither authorizes the caller nor changes the role; the enclosing operation supplies those
// guarantees.
// getAccessRole читает роль по организации и публичному ref через общий scanner проекции. Caller
// не авторизуется и роль не меняется; эти гарантии предоставляет внешняя операция.
func (repository *Repository) getAccessRole(ctx context.Context, tx pgx.Tx, organizationID, roleRef string) (entity.AccessRole, error) {
	return scanAccessRole(tx.QueryRow(ctx, queryAccessGetRole, pgx.NamedArgs{"organization_id": organizationID, "role_ref": roleRef}))
}

// getAccessBinding reads a binding by tenant and public binding ref, including its immutable role
// version and presentation protection marker. Authorization remains with the enclosing operation.
// getAccessBinding читает привязку по организации и публичному ref, включая неизменяемую версию
// роли и маркер защиты presentation. Авторизация остаётся у внешней операции.
func (repository *Repository) getAccessBinding(ctx context.Context, tx pgx.Tx, organizationID, bindingRef string) (entity.AccessBinding, error) {
	return scanAccessBinding(tx.QueryRow(ctx, queryAccessGetBinding, pgx.NamedArgs{"organization_id": organizationID, "binding_ref": bindingRef}))
}

// pageAccessSubjects trims a possible lookahead subject and returns the last retained ref as a raw
// continuation token. Callers must supply a positive bounded limit; no visibility filtering occurs
// here.
// pageAccessSubjects удаляет возможного лишнего субъекта и возвращает ref последнего оставленного
// как прямой токен продолжения. Caller передаёт положительный ограниченный limit; видимость здесь
// не фильтруется.
func pageAccessSubjects(items []entity.AccessSubject, limit int32) ([]entity.AccessSubject, string) {
	if int32(len(items)) <= limit {
		return items, ""
	}
	next := items[limit-1].Ref
	return items[:limit], next
}

// applyAccessCommand requires organization access.manage and dispatches only the explicit
// role/version/archive and binding create/change/revoke commands. The caller owns the surrounding
// idempotency, audit, event and transaction lifecycle.
// applyAccessCommand требует организационное access.manage и направляет только явные команды роли,
// версии, архива и create/change/revoke привязки. Внешний caller владеет жизненным циклом
// idempotency, audit, событий и транзакции.
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

// createAccessRole validates a new custom role without a caller-assigned ref, generates its
// identity, inserts revision one and reads the resulting role in the caller transaction. It
// returns an outcome without committing.
// createAccessRole проверяет новую пользовательскую роль без назначенного caller ref, создаёт
// identity, вставляет первую ревизию и читает итоговую роль в транзакции вызывающего. Возвращается
// outcome без commit.
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

// createAccessRoleVersion resolves the tenant role before version checks, rejects system roles and
// inactive/stale custom roles, and appends the next immutable revision before reading the updated
// role. The caller transaction owns atomicity.
// createAccessRoleVersion разрешает роль организации до проверки версии, отклоняет системные,
// неактивные и устаревшие пользовательские роли и добавляет следующую неизменяемую ревизию перед
// чтением обновлённой роли. Атомарностью владеет транзакция caller.
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

// insertAccessRoleVersion inserts an immutable revision under a server-generated ref and activates
// it with the expected parent-role version. A missing activation row becomes VersionMismatch;
// insertion and activation remain in the same supplied transaction.
// insertAccessRoleVersion вставляет неизменяемую ревизию с серверным ref и активирует её по
// ожидаемой версии родительской роли. Отсутствие строки активации даёт VersionMismatch; вставка и
// активация остаются в одной переданной транзакции.
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

// bootstrapAccess seeds the closed permission catalog, five system roles and their first
// revisions, then an organization owner binding for the supplied system service subject. This is
// bootstrap inside an existing transaction, without an independent commit or runtime self-grant
// path.
// bootstrapAccess заполняет закрытый каталог разрешений, пять системных ролей с первыми ревизиями
// и организационную owner-привязку переданного системного сервиса. Это bootstrap внутри
// существующей транзакции, без самостоятельного commit или runtime пути самовыдачи полномочий.
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

// archiveAccessRole archives one tenant role with OCC and distinguishes missing, protected system
// and stale-role failures by authoritative readback. Successful mutation reads the final role and
// returns its outcome without committing.
// archiveAccessRole архивирует роль организации с OCC и различает отсутствие, защищённую системную
// роль и устаревшую версию по авторитетному чтению. После успешного изменения читается итоговая
// роль и возвращается outcome без commit.
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

// changeAccessBinding handles explicit binding create/change/revoke in the caller transaction.
// Existing bindings are tenant-resolved before OCC, presentation bindings are protected, and
// change preserves the original subject. New scopes, subjects and role versions are resolved
// before persistence.
// changeAccessBinding обрабатывает явные create/change/revoke привязки в транзакции caller.
// Существующая привязка разрешается внутри организации до OCC, presentation-привязки защищены, а
// изменение сохраняет прежнего субъекта. Новые области, субъекты и версии ролей разрешаются до
// записи.
func (repository *Repository) changeAccessBinding(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AccessBindingInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var existing entity.AccessBinding
	if input.Kind == command.ChangeAccessBinding || input.Kind == command.RevokeAccessBinding {
		if payload.BindingRef == "" || input.Mutation.ExpectedVersion == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		var err error
		existing, err = repository.getAccessBinding(ctx, tx, current.organizationID, payload.BindingRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if existing.PresentationKind != "NONE" {
			return commandOutcome{}, errs.ErrProtected
		}
	}
	if input.Kind == command.RevokeAccessBinding {
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

// resolveAccessRoleVersion loads an immutable tenant role revision and its creator by public
// version ref, returning the internal version ID separately for persistence. Missing rows remain
// distinct from driver failures.
// resolveAccessRoleVersion загружает неизменяемую ревизию роли организации и создателя по
// публичному ref версии, отдельно возвращая внутренний ID для записи. Отсутствие строки
// различается с ошибкой драйвера.
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

// resolveBindingSubject resolves an active subject of the requested kind inside the tenant and
// returns either subject-ID or group-ID for binding storage. Inactive or missing results are
// hidden as NotFound.
// resolveBindingSubject разрешает активного субъекта заданного вида внутри организации и
// возвращает либо subject-ID, либо group-ID для хранения привязки. Неактивный или отсутствующий
// результат скрывается как NotFound.
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

// resolveBindingScope converts a previously validated binding scope to tenant-owned
// project/resource IDs. Organization-wide and unprojected kind scopes use empty IDs; concrete
// project/resource scopes go through authoritative lookup.
// resolveBindingScope преобразует ранее проверенную область привязки во внутренние
// project/resource ID организации. Для всей организации и внепроектного вида ID пусты; конкретные
// проекты и ресурсы проходят авторитетный поиск.
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

// mapAccessBindingMutationMiss reads the tenant binding after an unsuccessful guarded mutation to
// preserve NotFound versus VersionMismatch. Driver/read errors propagate without a blind retry or
// another mutation.
// mapAccessBindingMutationMiss читает привязку организации после неудачного защищённого изменения,
// различая NotFound и VersionMismatch. Ошибки чтения передаются без слепого повтора или новой
// мутации.
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

// accessRoleOutcome builds the role command result and audit/realtime metadata, identifying
// ACCESS_ROLE and MEMBERSHIP_CHANGED. It does not persist the role or publish the event itself.
// accessRoleOutcome собирает результат команды роли и метаданные audit/realtime с ACCESS_ROLE и
// MEMBERSHIP_CHANGED. Сам helper не сохраняет роль и не публикует событие.
func accessRoleOutcome(role entity.AccessRole) commandOutcome {
	return commandOutcome{result: command.Result{AccessRole: &role}, resourceKind: "ACCESS_ROLE", resourceRef: role.Ref, summary: "i18n:ACCESS_ROLE_CHANGED", platformEvent: "MEMBERSHIP_CHANGED"}
}

// accessBindingOutcome builds the binding command result and project-scoped audit/realtime
// metadata from the stored binding. The outer command transaction persists effects and publishes
// via its existing outbox path.
// accessBindingOutcome собирает результат команды привязки и проектные метаданные audit/realtime
// из сохранённой привязки. Внешняя транзакция команды сохраняет эффекты и использует существующий
// outbox путь публикации.
func accessBindingOutcome(binding entity.AccessBinding) commandOutcome {
	return commandOutcome{result: command.Result{AccessBinding: &binding}, projectRef: binding.Scope.ProjectRef, resourceKind: "ACCESS_BINDING", resourceRef: binding.Ref, summary: "i18n:ACCESS_BINDING_CHANGED", platformEvent: "MEMBERSHIP_CHANGED"}
}

// validAccessRoleQuery validates already-trimmed search text and the existing closed alias list.
// It preserves the original UTF-8, 200-rune, NUL and alias checks in their short-circuit order;
// trimming and authorization still belong to each role/binding query.
// validAccessRoleQuery проверяет уже обрезанный поисковый текст и прежний закрытый список aliases.
// Сохраняет исходный порядок UTF-8, предела 200 рун, NUL и aliases; обрезка пробелов и авторизация
// по-прежнему принадлежат отдельным запросам ролей и привязок.
func validAccessRoleQuery(text string, aliases []string) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= 200 && !strings.ContainsRune(text, 0) && validAccessRoleQueryAliases(aliases)
}

// rawAccessPage preserves a bounded access page's backing array, nil state and raw-ref cursor.
// Only a lookahead row invokes position, once on the last retained item before trimming, as in
// the native readers. The caller supplies a positive limit and owns errors, Close and Commit.
// rawAccessPage сохраняет backing array, nil-состояние и raw-ref cursor ограниченной страницы.
// Только запасная строка вызывает position, один раз для последнего видимого элемента до обрезки,
// как в исходных readers. Вызывающий задаёт положительный limit и отвечает за ошибки, Close и Commit.
func rawAccessPage[T any](items []T, limit int32, position func(T) string) ([]T, string) {
	next := ""
	if int32(len(items)) > limit {
		next = position(items[limit-1])
		items = items[:limit]
	}
	return items, next
}

// scanOIDCGroup decodes the existing seven-column organization group projection without changing
// its order, timestamps or counts. A scan failure discards the row as Unavailable; no ownership,
// membership refresh, row closure or transaction completion is performed here.
// scanOIDCGroup декодирует прежнюю проекцию группы организации из семи колонок без изменения
// порядка, времён и счётчиков. Ошибка Scan отбрасывает строку с Unavailable; helper не разрешает
// владение, не обновляет членство, не закрывает rows и не завершает транзакцию.
func scanOIDCGroup(scanner rowScanner) (entity.OIDCGroup, error) {
	var item entity.OIDCGroup
	if err := scanner.Scan(&item.Ref, &item.DisplayName, &item.State, &item.MemberCount, &item.BindingCount, &item.LastSeenAt, &item.SynchronizedAt); err != nil {
		return entity.OIDCGroup{}, errs.ErrUnavailable
	}
	return item, nil
}

// readAccessRowsPage applies the native access-reader order: collect with a nil initial slice,
// pass scan errors through, map iteration failure before pagination, then commit once with the
// exact caller context. A commit failure preserves items/cursor and its original error. Caller
// authorization, query, SQL arguments, row Close and deferred rollback stay outside.
// readAccessRowsPage сохраняет порядок access readers: сбор с nil initial slice, исходную scan
// error, преобразование iteration failure до пагинации и один Commit с точным caller context.
// Ошибка Commit сохраняет items/cursor и исходную error. Авторизация, query, SQL arguments,
// Close rows и отложенный rollback остаются у вызывающего.
func readAccessRowsPage[T any](ctx context.Context, tx pgx.Tx, rows pgx.Rows, limit int32, scan func(rowScanner) (T, error), position func(T) string) ([]T, string, error) {
	items, err := collectRowsInto(rows, []T(nil), scan)
	if err != nil {
		return nil, "", err
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	items, next := rawAccessPage(items, limit, position)
	return items, next, tx.Commit(ctx)
}
