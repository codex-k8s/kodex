package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/provider_usage__accounts.sql
var queryProviderUsageAccounts string

//go:embed sql/provider_usage__agents.sql
var queryProviderUsageAgents string

//go:embed sql/provider_usage__profile.sql
var queryProviderUsageProfile string

type providerUsageProfile struct {
	Ref, Provider, Model, RuntimeRevision string
	Enabled                               bool
}

type providerUsageAccount struct {
	Ref, Provider, State, CredentialID, ObservationID, ContentDigest, CatalogSource string
	Version, Maximum, Active                                                        int64
	Enabled, ProviderEnabled                                                        bool
	CredentialObservedAt                                                            *time.Time
	Catalog                                                                         entity.ModelCatalogStatus
	Models                                                                          []platformrepo.ProviderModelCatalogRecord
	ObservedAt                                                                      time.Time
}

type providerUsageAgent struct {
	Ref, State, ProjectRef, ConfigRef, ConfigDigest, Provider, Model, Overlay, SelectedAccount string
	Version                                                                                    int64
	Enabled                                                                                    bool
	Candidates                                                                                 []entity.ProviderAccountCandidate
}

type providerUsageContext struct {
	Input           *entity.ProviderAccountUsageContext
	Agent           providerUsageAgent
	ActorAllowed    bool
	AuthorityDigest string
	Profile         providerUsageProfile
}

func readProviderUsageAccounts(ctx context.Context, tx pgx.Tx, organization string, refs []string, models bool) ([]providerUsageAccount, error) {
	if refs == nil {
		refs = []string{}
	}
	rows, err := tx.Query(ctx, queryProviderUsageAccounts, pgx.StrictNamedArgs{"organization_id": organization, "account_refs": refs, "include_models": models})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []providerUsageAccount{}
	for rows.Next() {
		var item providerUsageAccount
		var raw []byte
		if rows.Scan(&item.Ref, &item.Version, &item.Provider, &item.State, &item.Enabled, &item.ProviderEnabled, &item.CredentialID,
			&item.CredentialObservedAt, &item.Maximum, &item.Active, &item.ObservationID, &item.Catalog.ObservedAt, &item.Catalog.ExpiresAt,
			&item.Catalog.State, &item.Catalog.Source, &item.Catalog.Failure, &item.ContentDigest, &raw, &item.ObservedAt, &item.CatalogSource) != nil {
			return nil, errs.ErrUnavailable
		}
		if len(result) >= 4096 || len(raw) > 131072 || item.Maximum < 1 || item.Active < 0 || !validProviderAccountLifecycle(item.State, item.Enabled) || decodeStrict(raw, &item.Models) != nil {
			return nil, errs.ErrUnavailable
		}
		if !validProviderUsageCatalog(item) {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func readProviderUsageAgents(ctx context.Context, tx pgx.Tx, organization string, refs []string) (map[string]providerUsageAgent, error) {
	if len(refs) == 0 || len(refs) > 4096 {
		return nil, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryProviderUsageAgents, pgx.StrictNamedArgs{"organization_id": organization, "agent_refs": refs})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := map[string]providerUsageAgent{}
	for rows.Next() {
		var item providerUsageAgent
		var candidates []byte
		if rows.Scan(&item.Ref, &item.Version, &item.Enabled, &item.State, &item.ProjectRef, &item.ConfigRef, &item.ConfigDigest, &item.Provider, &item.Model, &candidates, &item.Overlay, &item.SelectedAccount) != nil || len(candidates) > 131072 || decodeStrict(candidates, &item.Candidates) != nil {
			return nil, errs.ErrUnavailable
		}
		result[item.Ref] = item
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) resolveProviderUsageContext(ctx context.Context, tx pgx.Tx, current scope, input *entity.ProviderAccountUsageContext) (providerUsageContext, error) {
	result := providerUsageContext{Input: input}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return result, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return result, err
	}
	result.AuthorityDigest = digestBytes(asJSON(subject), asJSON(bindings))
	if input == nil {
		return result, nil
	}
	if !validOverlayHistoryRef(input.AgentRef) || !slices.Contains([]string{"CONFIGURE", "LAUNCH"}, input.Purpose) {
		return result, errs.ErrInvalid
	}
	if input.Purpose == "LAUNCH" && (input.Model != "" || input.ProviderDefinitionKey != "" || input.ReasoningEffort != "" || input.RuntimeProfileRef != "") {
		return result, errs.ErrInvalid
	}
	if input.Purpose == "CONFIGURE" && (!validStableKey(input.RuntimeProfileRef) || !validStableKey(input.ProviderDefinitionKey) || input.Model != "" && !validModel(input.Model) || input.Model == "" && input.ReasoningEffort != "" || len(input.ReasoningEffort) > 80) {
		return result, errs.ErrInvalid
	}
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", input.AgentRef)
	if err != nil {
		return result, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return result, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return result, errs.ErrNotFound
	}
	usePermission := "agent.manage"
	if input.Purpose == "LAUNCH" {
		usePermission = "agent.launch"
	}
	if permission == "organization.manage" {
		usePermission = permission
	}
	if err := repository.requireAccess(ctx, tx, current, usePermission, target); err == nil {
		result.ActorAllowed = true
	} else if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		return result, err
	}
	agents, err := readProviderUsageAgents(ctx, tx, current.organizationID, []string{input.AgentRef})
	if err != nil {
		return result, err
	}
	var ok bool
	result.Agent, ok = agents[input.AgentRef]
	if !ok {
		return result, errs.ErrNotFound
	}
	if input.Purpose == "CONFIGURE" {
		err = tx.QueryRow(ctx, queryProviderUsageProfile, pgx.StrictNamedArgs{"profile_ref": input.RuntimeProfileRef}).Scan(&result.Profile.Ref, &result.Profile.Provider, &result.Profile.Model, &result.Profile.RuntimeRevision, &result.Profile.Enabled)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrUnavailable
		}
	}
	return result, nil
}

func providerDimension(state, reason, remediation string) entity.ProviderUsageDimension {
	return entity.ProviderUsageDimension{State: state, Reason: reason, Remediation: remediation}
}

func providerAccountUsage(item providerUsageAccount, context providerUsageContext) (*entity.ProviderAccountUsage, error) {
	ready := providerDimension("READY", "AVAILABLE", "NONE")
	notEvaluated := providerDimension("NOT_EVALUATED", "CONTEXT_REQUIRED", "SELECT_CONTEXT")
	u := &entity.ProviderAccountUsage{Context: context.Input, AccountVersion: item.Version, AgentVersion: context.Agent.Version,
		RuntimeConfigurationRef: context.Agent.ConfigRef, RuntimeConfigurationDigest: context.Agent.ConfigDigest,
		Lifecycle: ready, Credential: providerDimension("READY", "CREDENTIAL_READY", "NONE"),
		ProviderHealth: providerDimension("UNKNOWN", "PROVIDER_HEALTH_UNOBSERVED", "NONE"), ModelCompatibility: notEvaluated, ActorEligibility: notEvaluated,
		Capacity: providerDimension("READY", "CAPACITY_AVAILABLE", "NONE"), MaximumConcurrentExecutions: item.Maximum, ActiveExecutions: item.Active,
		CatalogStatus: &item.Catalog, ObservedAt: item.ObservedAt, ExpiresAt: item.ObservedAt.Add(10 * time.Second), OperationalState: "NOT_EVALUATED"}
	u.ProviderHealthScope = "CREDENTIALED_CATALOG_REACHABILITY"
	// READY относится только к подтверждённому пути текущей credential, не к SLA провайдера.
	if item.CredentialID != "" && item.Catalog.State == "READY" && item.Catalog.Failure == "NONE" &&
		(item.Catalog.Source == "REMOTE_API" || item.Catalog.Source == "REMOTE_CODEX") &&
		item.Catalog.ObservedAt != nil && !item.Catalog.ObservedAt.After(item.ObservedAt) && item.Catalog.ExpiresAt != nil && item.Catalog.ExpiresAt.After(item.ObservedAt) {
		u.ProviderHealth = providerDimension("READY", "CREDENTIALED_CATALOG_REACHABLE", "NONE")
		u.ProviderHealthObservedAt, u.ProviderHealthExpiresAt = item.Catalog.ObservedAt, item.Catalog.ExpiresAt
	}
	if !item.ProviderEnabled {
		u.Lifecycle = providerDimension("BLOCKED", "PROVIDER_DISABLED", "CONTACT_ADMINISTRATOR")
	} else if item.State == "DISABLED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_DISABLED", "ENABLE_ACCOUNT")
	} else if item.State == "REVOKED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_REVOKED", "AUTHORIZE_ACCOUNT")
	} else if item.State == "DELETING" || item.State == "DELETED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_REVOKED", "NONE")
	} else if item.State != "AUTHORIZED" {
		u.Lifecycle = providerDimension("BLOCKED", "AUTHORIZATION_REQUIRED", "AUTHORIZE_ACCOUNT")
	}
	if item.CredentialID == "" {
		u.Credential = providerDimension("BLOCKED", "CREDENTIAL_MISSING", "AUTHORIZE_ACCOUNT")
	}
	if item.Active >= item.Maximum {
		u.Capacity = providerDimension("BLOCKED", "CAPACITY_EXHAUSTED", "WAIT_FOR_CAPACITY")
	}
	if item.Catalog.State == "READY" && item.Catalog.ExpiresAt != nil && item.Catalog.ExpiresAt.Before(u.ExpiresAt) {
		u.ExpiresAt = *item.Catalog.ExpiresAt
	}
	var err error
	u.CatalogDigest, err = modelcatalog.Digest(nil, item.CatalogSource)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	u.CatalogRevision = "mcat_" + u.CatalogDigest
	if context.Input != nil {
		u.ActorEligibility = ready
		if !context.ActorAllowed {
			u.ActorEligibility = providerDimension("BLOCKED", "PERMISSION_REQUIRED", "CONTACT_ADMINISTRATOR")
		}
		u.ModelCompatibility = providerModelCompatibility(item, context, u.CatalogRevision, u.CatalogDigest)
		u.EligibleForSelection = u.Lifecycle.State == "READY" && u.Credential.State == "READY" && u.ActorEligibility.State == "READY"
		if context.Input.Purpose == "CONFIGURE" && context.Input.ProviderDefinitionKey != item.Provider {
			u.EligibleForSelection = false
		}
		u.AllowedToSubmit = u.EligibleForSelection && u.ModelCompatibility.State == "READY"
		u.OperationalState = "UNKNOWN"
		if u.ProviderHealth.State == "READY" {
			u.OperationalState = "READY"
		}
		if !u.AllowedToSubmit || u.Capacity.State == "BLOCKED" {
			u.OperationalState = "BLOCKED"
		}
	}
	u.ContextDigest = digestBytes(asJSON(context), []byte(providerUsageAccountDigest(item)))
	return u, nil
}

func providerModelCompatibility(item providerUsageAccount, context providerUsageContext, revision, digest string) entity.ProviderUsageDimension {
	blocked := func(reason, remediation string) entity.ProviderUsageDimension {
		return providerDimension("BLOCKED", reason, remediation)
	}
	provider, model, effort := context.Input.ProviderDefinitionKey, context.Input.Model, context.Input.ReasoningEffort
	if context.Input.Purpose == "CONFIGURE" && (!context.Profile.Enabled || context.Profile.Ref != context.Input.RuntimeProfileRef || context.Profile.Provider != provider || context.Profile.Model == "" || context.Profile.RuntimeRevision == "") {
		return blocked("RUNTIME_PROFILE_UNAVAILABLE", "REPUBLISH_CONFIGURATION")
	}
	var selected *entity.ProviderAccountCandidate
	if context.Input.Purpose == "LAUNCH" {
		if !context.Agent.Enabled || context.Agent.State != "READY" {
			return blocked("AGENT_UNAVAILABLE", "CONTACT_ADMINISTRATOR")
		}
		if context.Agent.ConfigRef == "" {
			return blocked("RUNTIME_CONFIGURATION_MISSING", "REPUBLISH_CONFIGURATION")
		}
		provider, model = context.Agent.Provider, context.Agent.Model
		for _, candidate := range context.Agent.Candidates {
			if candidate.AccountRef == item.Ref {
				if selected != nil {
					return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
				}
				copy := candidate
				selected = &copy
			}
		}
		if selected == nil {
			return blocked("ACCOUNT_NOT_SELECTED", "REPUBLISH_CONFIGURATION")
		}
		overlay, err := runtimecontract.ParseConfigOverlay(context.Agent.Overlay)
		if err != nil {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		effort = overlay.ModelReasoningEffort
	}
	if provider != item.Provider {
		return blocked("PROVIDER_MISMATCH", "SELECT_MODEL")
	}
	if model == "" {
		return providerDimension("NOT_EVALUATED", "MODEL_REQUIRED", "SELECT_MODEL")
	}
	if item.Catalog.State != "READY" {
		reason := "CATALOG_" + item.Catalog.State
		if item.Catalog.State == "FAILED" {
			reason = "CATALOG_" + item.Catalog.Failure
		}
		remediation := "WAIT_FOR_OBSERVATION"
		if reason == "CATALOG_AUTHORIZATION_REJECTED" {
			remediation = "AUTHORIZE_ACCOUNT"
		}
		if reason == "CATALOG_UNVERIFIED_SOURCE" {
			remediation = "CONTACT_ADMINISTRATOR"
		}
		return blocked(reason, remediation)
	}
	if selected != nil && (!validRuntimeCatalogPin(*selected) || selected.CatalogRevision != revision || selected.CatalogDigest != digest || selected.ProviderDefinitionKey != provider) {
		return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
	}
	for _, capability := range item.Models {
		if capability.ID != model {
			continue
		}
		if selected != nil && selected.DefaultReasoningEffort != capability.DefaultReasoningEffort {
			return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
		}
		if effort != "" && !slices.Contains(capability.ReasoningEfforts, effort) {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		if len(runtimecontract.DiagnoseConfigOverlay(context.Agent.Overlay, capability.ReasoningEfforts)) != 0 {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		return providerDimension("READY", "AVAILABLE", "NONE")
	}
	return blocked("MODEL_UNSUPPORTED", "SELECT_MODEL")
}

func providerUsageAccountDigest(item providerUsageAccount) string {
	item.Models = nil
	item.ObservedAt = time.Time{}
	return digestBytes(asJSON(item))
}

func validProviderUsageCatalog(item providerUsageAccount) bool {
	if !slices.Contains([]string{"READY", "PENDING", "FAILED", "EXPIRED"}, item.Catalog.State) || len(item.Models) > 128 {
		return false
	}
	if !slices.Contains([]string{"", "NONE", "UNAVAILABLE", "UNVERIFIED_SOURCE", "AUTHORIZATION_REJECTED"}, item.Catalog.Failure) || !slices.Contains([]string{"", "REMOTE_API", "REMOTE_CODEX"}, item.Catalog.Source) {
		return false
	}
	if item.Catalog.State == "READY" && (item.Catalog.Failure != "NONE" || item.Catalog.Source == "" || item.Catalog.ObservedAt == nil || item.Catalog.ExpiresAt == nil || !item.Catalog.ExpiresAt.After(item.ObservedAt)) {
		return false
	}
	seen := map[string]bool{}
	for _, model := range item.Models {
		if !validModel(model.ID) || seen[model.ID] || len(model.ReasoningEfforts) > 16 {
			return false
		}
		seen[model.ID] = true
		efforts := map[string]bool{}
		for _, effort := range model.ReasoningEfforts {
			if efforts[effort] || runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil {
				return false
			}
			efforts[effort] = true
		}
		if len(efforts) == 0 && model.DefaultReasoningEffort != "" || len(efforts) > 0 && !efforts[model.DefaultReasoningEffort] {
			return false
		}
	}
	return true
}

func (repository *Repository) providerUsageRead(ctx context.Context, p value.Principal, target func(scope) entity.AccessScope) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "provider.account.view", target(current)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, errs.ErrNotFound
	}
	return current, tx, nil
}

// providerAdmissionForAgents возвращает выбранный фактической policy account;
// чтение не резервирует capacity и не заменяет launch authority владельца.
type providerAdmissionSnapshot struct {
	AllowedToSubmit                 bool
	OperationalState, ContextDigest string
	Usage                           *entity.ProviderAccountUsage
}

func (repository *Repository) providerAdmissionForAgents(ctx context.Context, tx pgx.Tx, current scope, refs []string) (map[string]providerAdmissionSnapshot, error) {
	agents, err := readProviderUsageAgents(ctx, tx, current.organizationID, refs)
	if err != nil {
		return nil, err
	}
	accountRefs := []string{}
	for _, agent := range agents {
		if agent.SelectedAccount != "" && !slices.Contains(accountRefs, agent.SelectedAccount) {
			accountRefs = append(accountRefs, agent.SelectedAccount)
		}
	}
	accounts := []providerUsageAccount{}
	if len(accountRefs) > 0 {
		accounts, err = readProviderUsageAccounts(ctx, tx, current.organizationID, accountRefs, true)
		if err != nil {
			return nil, err
		}
	}
	byRef := map[string]providerUsageAccount{}
	for _, account := range accounts {
		byRef[account.Ref] = account
	}
	result := map[string]providerAdmissionSnapshot{}
	for _, ref := range refs {
		agent := agents[ref]
		account, ok := byRef[agent.SelectedAccount]
		if !ok {
			result[ref] = providerAdmissionSnapshot{OperationalState: "BLOCKED", ContextDigest: digestBytes(asJSON(agent))}
			continue
		}
		// Actor admission выполняет общий Workflow/Agent caller; это внутренний dependency snapshot.
		usage, err := providerAccountUsage(account, providerUsageContext{Input: &entity.ProviderAccountUsageContext{Purpose: "LAUNCH", AgentRef: ref}, Agent: agent, ActorAllowed: true})
		if err != nil {
			return nil, err
		}
		result[ref] = providerAdmissionSnapshot{AllowedToSubmit: usage.AllowedToSubmit, OperationalState: usage.OperationalState, ContextDigest: usage.ContextDigest, Usage: usage}
	}
	return result, nil
}
