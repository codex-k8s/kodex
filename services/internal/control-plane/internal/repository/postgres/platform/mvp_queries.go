package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
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
		for _, model := range modelcatalog.Models(nil) {
			item.ModelIDs = append(item.ModelIDs, model.ID)
			item.Models = append(item.Models, entity.ModelCapability{ID: model.ID, ProviderDefinitionKey: item.Key,
				DefaultReasoningEffort: model.DefaultEffort, ReasoningEfforts: model.Efforts, Available: item.Available})
		}
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

type modelCapabilityCursor struct {
	Version               int    `json:"v"`
	Filter                string `json:"f"`
	ProviderDefinitionKey string `json:"p"`
	Model                 string `json:"m"`
}

func (repository *Repository) ListModelCapabilities(ctx context.Context, principal value.Principal, definitionKey, accountRef string, filter query.Filter) ([]entity.ModelCapability, int64, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "organization.view", func(current scope) entity.AccessScope {
		return organizationTarget(current.organizationRef)
	})
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if definitionKey != "" && !validStableKey(definitionKey) || accountRef != "" && (!strings.HasPrefix(accountRef, "pacc_") || len(accountRef) > 96) || len([]rune(filter.Query)) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	cursor, err := decodeModelCapabilityCursor(filter.Page.Token, definitionKey, accountRef, filter.Query)
	if err != nil {
		return nil, 0, "", err
	}
	rows, err := tx.Query(ctx, queryMVPListModelCapabilities, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "provider_definition_key": definitionKey,
		"provider_account_ref": accountRef,
	})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.ModelCapability, 0)
	foundAccount := accountRef == ""
	for rows.Next() {
		var key string
		var enabled bool
		var raw []byte
		var eligibleAccounts, matchedAccounts, accountBlockers []string
		if err := rows.Scan(&key, &enabled, &raw, &eligibleAccounts, &matchedAccounts, &accountBlockers); err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		var capabilities map[string]any
		if json.Unmarshal(raw, &capabilities) != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		if len(matchedAccounts) != 0 {
			foundAccount = true
		}
		sort.Strings(accountBlockers)
		reportedModels := []string(nil)
		if accountRef != "" {
			reportedModels = providerReportedModels(capabilities)
		}
		for _, model := range modelcatalog.Models(reportedModels) {
			item := entity.ModelCapability{ID: model.ID, ProviderDefinitionKey: key,
				DefaultReasoningEffort: model.DefaultEffort, ReasoningEfforts: model.Efforts,
				EligibleProviderAccountRefs: eligibleAccounts, Available: enabled && len(eligibleAccounts) != 0}
			if !enabled {
				item.ReadinessBlockers = []string{"PROVIDER_DISABLED"}
			} else if len(eligibleAccounts) == 0 && len(accountBlockers) != 0 {
				item.ReadinessBlockers = append([]string(nil), accountBlockers...)
			} else if len(eligibleAccounts) == 0 {
				item.ReadinessBlockers = []string{"ELIGIBLE_PROVIDER_ACCOUNT_MISSING"}
			}
			result = append(result, item)
		}
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	if !foundAccount {
		return nil, 0, "", errs.ErrNotFound
	}
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	filtered := make([]entity.ModelCapability, 0, len(result))
	for _, item := range result {
		if needle == "" || strings.Contains(strings.ToLower(item.ProviderDefinitionKey+" "+item.ID), needle) {
			filtered = append(filtered, item)
		}
	}
	total := int64(len(filtered))
	start := 0
	if cursor.Model != "" {
		for index, item := range filtered {
			if item.ProviderDefinitionKey == cursor.ProviderDefinitionKey && item.ID == cursor.Model {
				start = index + 1
				break
			}
		}
		if start == 0 {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	limit := int(boundedPage(filter.Page))
	end := min(start+limit, len(filtered))
	items := filtered[start:end]
	next := ""
	if end < len(filtered) {
		last := items[len(items)-1]
		next = encodeModelCapabilityCursor(last.ProviderDefinitionKey, last.ID, definitionKey, accountRef, filter.Query)
		if next == filter.Page.Token {
			return nil, 0, "", errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrConflict
	}
	return items, total, next, nil
}

func modelCapabilityFilterDigest(definitionKey, accountRef, queryValue string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{definitionKey, accountRef, strings.TrimSpace(queryValue)}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

func encodeModelCapabilityCursor(definitionKey, model, filterDefinitionKey, accountRef, queryValue string) string {
	raw, _ := json.Marshal(modelCapabilityCursor{Version: 1,
		Filter: modelCapabilityFilterDigest(filterDefinitionKey, accountRef, queryValue), ProviderDefinitionKey: definitionKey, Model: model})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeModelCapabilityCursor(token, definitionKey, accountRef, queryValue string) (modelCapabilityCursor, error) {
	if strings.TrimSpace(token) == "" {
		return modelCapabilityCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 1024 {
		return modelCapabilityCursor{}, errs.ErrInvalid
	}
	var cursor modelCapabilityCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Filter != modelCapabilityFilterDigest(definitionKey, accountRef, queryValue) ||
		cursor.ProviderDefinitionKey == "" || cursor.Model == "" {
		return modelCapabilityCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

func providerReportedModels(capabilities map[string]any) []string {
	raw, ok := capabilities["modelIds"].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		if model, ok := value.(string); ok {
			result = append(result, model)
		}
	}
	return result
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
		&authorization.UserCode, &expiresAt, &authorization.SafeFailureCode, &authorization.MaterializerAttemptRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ProviderAccount{}, errs.ErrNotFound
		}
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	if authorization.Ref != "" {
		authorization.ExpiresAt = expiresAt
		item.Authorization = &authorization
	}
	if !validProviderAccountLifecycle(item.State, item.Enabled) {
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	item.Ready = item.Enabled && item.State == "AUTHORIZED"
	item.SafeStatusReason = providerAccountStatusReason(item)
	return item, nil
}

func providerAccountStatusReason(item entity.ProviderAccount) string {
	switch item.State {
	case "AUTHORIZED":
		return "AUTHORIZED"
	case "DISABLED":
		return "ACCOUNT_DISABLED"
	case "REVOKED":
		return "ACCOUNT_REVOKED"
	case "REAUTHORIZATION_REQUIRED":
		if item.Authorization != nil && safeProviderAuthorizationFailure(item.Authorization.SafeFailureCode) {
			return item.Authorization.SafeFailureCode
		}
		return "REAUTHORIZATION_REQUIRED"
	case "PENDING_AUTHORIZATION":
		if item.Authorization != nil && item.Authorization.State == "PENDING" {
			return "DEVICE_AUTHORIZATION_PENDING"
		}
		return "CREDENTIAL_CONFIGURATION_REQUIRED"
	default:
		return "ACCOUNT_STATE_UNKNOWN"
	}
}

func safeProviderAuthorizationFailure(value string) bool {
	switch value {
	case "DEVICE_AUTHORIZATION_EXPIRED", "DEVICE_AUTHORIZATION_FAILED", "CREDENTIAL_MATERIALIZATION_FAILED":
		return true
	default:
		return false
	}
}

func validProviderAccountLifecycle(state string, enabled bool) bool {
	switch state {
	case "AUTHORIZED":
		return enabled
	case "PENDING_AUTHORIZATION", "REVOKED", "DISABLED":
		return !enabled
	case "REAUTHORIZATION_REQUIRED":
		return true
	default:
		return false
	}
}

func providerAccountActions(item entity.ProviderAccount, canManage, canAuthorize, canRevoke bool) []string {
	actions := []string{"OPEN"}
	if item.State == "PENDING_AUTHORIZATION" {
		if canAuthorize && item.Authorization == nil {
			actions = append(actions, "CONFIGURE_CREDENTIAL")
		}
		if canAuthorize && item.Authorization != nil && item.Authorization.State == "PENDING" {
			actions = append(actions, "REFRESH_AUTHORIZATION")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		return actions
	}
	switch item.State {
	case "AUTHORIZED":
		if canManage {
			actions = append(actions, "TEST")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		if canManage {
			actions = append(actions, "DISABLE")
		}
	case "DISABLED":
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		if canManage {
			actions = append(actions, "ENABLE")
		}
	case "REAUTHORIZATION_REQUIRED":
		if canAuthorize {
			actions = append(actions, "CONFIGURE_CREDENTIAL")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
	}
	return actions
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
	collectionTarget := providerAccountCollectionTarget()
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

func providerAccountCollectionTarget() entity.AccessScope {
	return entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"}
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
		var input, promptInputs []byte
		if err := rows.Scan(&item.Ref, &item.Revision, &item.Digest, &item.Name, &item.Target.Type,
			&item.Target.Ref, &item.Preset, &item.CronExpression, &item.Timezone, &input,
			&item.SessionPolicy, &item.NotificationPolicy, &item.DSTGapPolicy, &item.DSTFoldPolicy,
			&item.MisfirePolicy, &item.OverlapPolicy, &item.TargetVersion, &item.TargetDigest,
			&item.AutomationText, &promptInputs, &item.CreatedAt); err != nil ||
			json.Unmarshal(input, &item.Input) != nil || json.Unmarshal(promptInputs, &item.PromptInputs) != nil {
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
	item, err := repository.scanRuntimeEnvironment(tx.QueryRow(ctx, queryRuntimeConfigurationGetEnvironment,
		current.organizationID, ref, current.role, current.actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrUnavailable
	}
	result := entity.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.Ref, EnvironmentVersion: item.Version,
		PublishedVersionRef: item.CurrentVersion.Ref, PublishedVersionDigest: item.CurrentVersion.Digest,
		Ready: item.Ready, Blockers: append([]string(nil), item.ReadinessBlockers...), ObservedAt: time.Now().UTC(),
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) runtimeEnvironmentReadiness(item entity.RuntimeEnvironmentSet) entity.RuntimeEnvironmentReadiness {
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
	} else if item.CurrentVersion.Image.RoleRuntimeContractRevision != int64(repository.roleImages.RoleRuntimeContractRevision) ||
		item.CurrentVersion.Image.RoleRuntimeContractSHA256 != repository.roleImages.RoleRuntimeContractSHA256 {
		result.Blockers = append(result.Blockers, "ROLE_RUNTIME_CONTRACT_STALE")
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
		"query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit + 1,
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
			&item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL,
			&item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled,
			&item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model,
			&item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt,
			&item.UpdatedAt, &canManage, &canLaunch)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		setAgentAvatarReadback(&item)
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
		lowercaseLetter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if !lowercaseLetter && !digit && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
