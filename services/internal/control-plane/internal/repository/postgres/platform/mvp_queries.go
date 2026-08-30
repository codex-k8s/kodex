package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ListProviderDefinitions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ProviderDefinition, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "organization.view", func(current scope) entity.AccessScope {
		return organizationTarget(current.organizationRef)
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!validStableKey(cursor) || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListProviderDefinitions, pgx.StrictNamedArgs{
		"query": strings.TrimSpace(filter.Query), "cursor_key": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ProviderDefinition, 0, limit+1)
	for rows.Next() {
		var item entity.ProviderDefinition
		var capabilities []byte
		if err := rows.Scan(&item.Key, &item.Name, &capabilities, &item.Available); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		var capabilityMap map[string]any
		if json.Unmarshal(capabilities, &capabilityMap) != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.Description = item.Name
		item.AuthorizationMethods = []string{"API_KEY"}
		if enabled, _ := capabilityMap["deviceAuthorization"].(bool); enabled {
			item.AuthorizationMethods = append([]string{"DEVICE_CODE"}, item.AuthorizationMethods...)
		}
		item.DefaultModelID = repository.defaultRuntimeModel
		item.ModelIDs = []string{repository.defaultRuntimeModel}
		item.Ready = item.Available
		if !item.Ready {
			item.ReadinessBlockers = []string{"PROVIDER_DISABLED"}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = items[len(items)-1].Key
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrConflict
	}
	_ = current
	return items, next, nil
}

func (repository *Repository) ListProviderAccounts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ProviderAccount, string, []string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "provider.account.view", func(current scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"}
	})
	if err != nil {
		return nil, "", nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursorTime, cursorRef, err := decodeMVPCursor("provider", filter.Page.Token)
	if err != nil {
		return nil, "", nil, err
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListProviderAccounts, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "query": strings.TrimSpace(filter.Query),
		"state": strings.TrimSpace(filter.State), "definition_key": strings.TrimSpace(filter.DefinitionKey),
		"cursor_time": cursorTime, "cursor_ref": cursorRef,
		"page_size": limit + 1,
	})
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ProviderAccount, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanProviderAccount(rows)
		if scanErr != nil {
			return nil, "", nil, scanErr
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeMVPCursor("provider", last.UpdatedAt, last.Ref)
	}
	items, collectionActions, err := repository.authorizeProviderAccountActions(ctx, tx, current, items)
	if err != nil {
		return nil, "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", nil, errs.ErrConflict
	}
	return items, next, collectionActions, nil
}

func (repository *Repository) GetProviderAccount(ctx context.Context, principal value.Principal, ref string) (entity.ProviderAccount, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "provider.account.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: ref}
	})
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanProviderAccount(tx.QueryRow(ctx, queryMVPGetProviderAccount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": ref,
	}))
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	items, _, err := repository.authorizeProviderAccountActions(ctx, tx, current, []entity.ProviderAccount{item})
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProviderAccount{}, errs.ErrConflict
	}
	return items[0], nil
}

func scanProviderAccount(row rowScanner) (entity.ProviderAccount, error) {
	var item entity.ProviderAccount
	var authorization entity.ProviderAuthorization
	var expiresAt *time.Time
	if err := row.Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.ExternalAccountMasked,
		&item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt,
		&authorization.Ref, &authorization.Method, &authorization.State, &authorization.VerificationURI,
		&authorization.UserCode, &expiresAt, &authorization.SafeFailureCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ProviderAccount{}, errs.ErrNotFound
		}
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	if authorization.Ref != "" {
		authorization.ExpiresAt = expiresAt
		item.Authorization = &authorization
	}
	item.Ready = item.Enabled && item.State == "AUTHORIZED"
	return item, nil
}

func providerAccountActions(item entity.ProviderAccount, canManage, canAuthorize, canRevoke bool) []string {
	actions := []string{"OPEN"}
	if item.State == "PENDING_AUTHORIZATION" {
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		return actions
	}
	if item.State == "AUTHORIZED" {
		if canManage {
			actions = append(actions, "TEST")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
	} else if canAuthorize {
		actions = append(actions, "CONFIGURE_CREDENTIAL")
	}
	if !canManage {
		return actions
	}
	if item.Enabled {
		return append(actions, "DISABLE")
	}
	return append(actions, "ENABLE")
}

func (repository *Repository) authorizeProviderAccountActions(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	items []entity.ProviderAccount,
) ([]entity.ProviderAccount, []string, error) {
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, nil, err
	}
	at := time.Now().UTC()
	collectionTarget := entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"}
	collectionActions := []string{}
	if accessservice.Evaluate(subject.AccessSubject, "provider.account.manage", collectionTarget, "", bindings, at).Allowed {
		collectionActions = append(collectionActions, "CREATE_CONNECTION")
	}
	for index := range items {
		target := entity.AccessScope{
			Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: items[index].Ref,
		}
		canManage := accessservice.Evaluate(subject.AccessSubject, "provider.account.manage", target, "", bindings, at).Allowed
		canAuthorize := accessservice.Evaluate(subject.AccessSubject, "provider.account.authorize", target, "", bindings, at).Allowed
		canRevoke := accessservice.Evaluate(subject.AccessSubject, "provider.account.revoke", target, "", bindings, at).Allowed
		items[index].NextActions = providerAccountActions(items[index], canManage, canAuthorize, canRevoke)
	}
	return items, collectionActions, nil
}

func (repository *Repository) ListRoleImageRecipeRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RoleImageRecipeRevision, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "project.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ROLE_IMAGE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := versionCursor(filter.Page.Token)
	if err != nil || strings.TrimSpace(filter.ResourceRef) == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListRoleImageRevisions, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "recipe_ref": filter.ResourceRef,
		"before_revision": before, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RoleImageRecipeRevision, 0, limit+1)
	for rows.Next() {
		var item entity.RoleImageRecipeRevision
		if err := rows.Scan(&item.Ref, &item.RecipeRef, &item.Revision, &item.RecipeVersion,
			&item.RecipeGeneration, &item.SpecSHA256, &item.ImageArtifactRef, &item.ProvenanceSHA256,
			&item.SourceSHA256, &item.ImmutableBuildSHA256, &item.ManifestDigest,
			&item.PromotedReference, &item.PromotionReceiptSHA256, &item.CreatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatUint(items[len(items)-1].Revision, 10)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

func (repository *Repository) ListScheduleRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ScheduleRevision, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "schedule.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := versionCursor(filter.Page.Token)
	if err != nil || strings.TrimSpace(filter.ResourceRef) == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListScheduleRevisions, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "schedule_ref": filter.ResourceRef,
		"before_revision": before, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ScheduleRevision, 0, limit+1)
	for rows.Next() {
		var item entity.ScheduleRevision
		var input []byte
		if err := rows.Scan(&item.Ref, &item.Revision, &item.Digest, &item.Name, &item.Target.Type,
			&item.Target.Ref, &item.Preset, &item.CronExpression, &item.Timezone, &input,
			&item.SessionPolicy, &item.NotificationPolicy, &item.CreatedAt); err != nil || json.Unmarshal(input, &item.Input) != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatInt(items[len(items)-1].Revision, 10)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

func (repository *Repository) ListScheduleRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ScheduleRunOccurrence, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "schedule.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!strings.HasPrefix(cursor, "run_") || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListScheduleRuns, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "schedule_ref": filter.ResourceRef,
		"cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ScheduleRunOccurrence, 0, limit+1)
	for rows.Next() {
		var item entity.ScheduleRunOccurrence
		run, scanErr := scanRunWithPrefix(rows, false, &item.ScheduleRef, &item.ScheduleRevisionRef, &item.ScheduleRevision)
		if scanErr != nil {
			return nil, "", scanErr
		}
		item.Run = run
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = items[len(items)-1].Run.Ref
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

func (repository *Repository) GetRuntimeEnvironmentReadiness(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentReadiness, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "project.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUNTIME_ENVIRONMENT", ResourceRef: ref}
	})
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanRuntimeEnvironment(tx.QueryRow(ctx, queryRuntimeConfigurationGetEnvironment,
		current.organizationID, ref, current.role, current.actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrUnavailable
	}
	result := runtimeEnvironmentReadiness(item)
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrConflict
	}
	return result, nil
}

func runtimeEnvironmentReadiness(item entity.RuntimeEnvironmentSet) entity.RuntimeEnvironmentReadiness {
	result := entity.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.Ref, EnvironmentVersion: item.Version,
		PublishedVersionRef: item.CurrentVersion.Ref, PublishedVersionDigest: item.CurrentVersion.Digest,
		ObservedAt: time.Now().UTC(),
	}
	if item.State != "ACTIVE" {
		result.Blockers = append(result.Blockers, "ENVIRONMENT_NOT_ACTIVE")
	}
	if item.CurrentVersion.Ref == "" || item.CurrentVersion.Digest == "" {
		result.Blockers = append(result.Blockers, "PUBLISHED_VERSION_MISSING")
	}
	if item.CurrentVersion.Image.Reference == "" || item.CurrentVersion.Image.Digest == "" {
		result.Blockers = append(result.Blockers, "PROMOTED_IMAGE_MISSING")
	}
	result.Ready = len(result.Blockers) == 0
	return result
}

func (repository *Repository) ListRuntimeEnvironmentAgents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "project.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUNTIME_ENVIRONMENT", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!strings.HasPrefix(cursor, "agt_") || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPRuntimeEnvironmentAgents, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "environment_ref": filter.ResourceRef,
		"cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	refs := make([]string, 0, limit+1)
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, "", errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(refs) > int(limit) {
		refs = refs[:limit]
		next = refs[len(refs)-1]
	}
	items := make([]entity.Agent, 0, len(refs))
	for _, ref := range refs {
		var item entity.Agent
		var canManage, canLaunch bool
		scanErr := tx.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey,
			current.organizationID, ref, current.role, current.actorID).Scan(
			&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey,
			&item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled,
			&item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model,
			&item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt,
			&item.UpdatedAt, &canManage, &canLaunch)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.System = item.SystemKey != ""
		item.NextActions = agentActions(item, canManage, canLaunch)
		items = append(items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrConflict
	}
	return items, next, nil
}

func (repository *Repository) authorizedRead(
	ctx context.Context,
	principal value.Principal,
	permission string,
	target func(scope) entity.AccessScope,
) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target(current)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, errs.ErrNotFound
	}
	return current, tx, nil
}

func encodeMVPCursor(kind string, timestamp time.Time, ref string) string {
	payload := kind + "\n" + timestamp.UTC().Format(time.RFC3339Nano) + "\n" + ref
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeMVPCursor(kind, token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	version, payload, ok := strings.Cut(token, ".")
	if !ok || version != "v1" || len(payload) > 384 {
		return nil, "", errs.ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 3 || parts[0] != kind || parts[2] == "" || len(parts[2]) > 96 {
		return nil, "", errs.ErrInvalid
	}
	parsed, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parsed = parsed.UTC()
	return &parsed, parts[2], nil
}

func validStableKey(value string) bool {
	if len(value) < 2 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character < 'a' || character > 'z' && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
