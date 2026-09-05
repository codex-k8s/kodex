package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/effective_capabilities_agent.sql
var queryEffectiveCapabilitiesAgent string

//go:embed sql/effective_capabilities_workflow.sql
var queryEffectiveCapabilitiesWorkflow string

//go:embed sql/effective_capabilities_grants.sql
var queryEffectiveCapabilitiesGrants string

const (
	capabilityAvailable          = "AVAILABLE"
	capabilityActorDenied        = "ACTOR_PERMISSION_REQUIRED"
	capabilityNotRequested       = "AGENT_CAPABILITY_REQUIRED"
	capabilityRuntimeNotReady    = "RUNTIME_NOT_READY"
	capabilityWorkflowExcluded   = "WORKFLOW_CAPABILITY_NOT_REQUIRED"
	capabilityGrantUnavailable   = "INTEGRATION_GRANT_UNAVAILABLE"
	capabilityPackageUnavailable = "INTEGRATION_REVISION_UNAVAILABLE"
)

func (repository *Repository) GetAgentEffectiveCapabilities(ctx context.Context, principal value.Principal, agentRef, workflowRef, stepKey string, filter query.Filter) (entity.AgentEffectiveCapabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(agentRef) || (workflowRef == "") != (stepKey == "") || workflowRef != "" && (!validOverlayHistoryRef(workflowRef) || len(stepKey) > 96 || !utf8.ValidString(stepKey) || strings.IndexFunc(stepKey, unicode.IsControl) >= 0) || !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, '\x00') || len(filter.Page.Token) > 2048 {
		return entity.AgentEffectiveCapabilities{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AgentEffectiveCapabilities{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.AgentEffectiveCapabilities{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, agentRef); err != nil {
		return entity.AgentEffectiveCapabilities{}, err
	}
	result := entity.AgentEffectiveCapabilities{AgentRef: agentRef, WorkflowRef: workflowRef, StepKey: stepKey, EvaluatedAt: time.Now().UTC(), Items: []entity.EffectiveCapability{}}
	var enabled bool
	var requested []string
	err = tx.QueryRow(ctx, queryEffectiveCapabilitiesAgent, current.organizationID, agentRef).Scan(&result.AgentVersion, &result.ProjectRef, &enabled, &requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	authority, err := repository.capabilityAuthority(ctx, tx, current, result.ProjectRef, agentRef)
	if err != nil {
		return result, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, agentRef)
	if err != nil {
		return result, err
	}
	result.RuntimeConfigurationRef, result.RuntimeConfigurationVersion = view.Configuration.Ref, view.Configuration.Version
	result.EnvironmentVersionRef = view.EnvironmentBinding.VersionRef
	result.RuntimeReady = enabled && repository.runtimeEnvironmentReadiness(view.Environment).Ready
	if result.RuntimeReady {
		result.RuntimeReady, err = capabilityCatalogReady(ctx, tx, current, view.Configuration)
		if err != nil {
			return result, err
		}
	}
	required, err := repository.effectiveWorkflowCapabilities(ctx, tx, current, &result)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	for rows.Next() {
		item := entity.EffectiveCapability{Source: "PLATFORM"}
		var risk string
		if rows.Scan(&item.Key, &item.Name, &item.Description, &risk) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		item.Requested = slices.Contains(requested, item.Key)
		item.Required = slices.Contains(required, item.Key)
		item.Grantable = authority.platformAllowed(item.Key)
		item.Reason = effectiveCapabilityReason(item.Requested, item.Grantable, result.RuntimeReady, workflowRef != "", item.Required)
		item.Effective = item.Reason == capabilityAvailable
		result.Items = append(result.Items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	integrations, err := repository.effectiveIntegrationCapabilities(ctx, tx, current, authority, agentRef, result.RuntimeReady, required, workflowRef != "")
	if err != nil {
		return result, err
	}
	result.Items = append(result.Items, integrations...)
	// Отсутствующее в registry требование этапа тоже объясняется, а не исчезает.
	for _, key := range required {
		if !slices.ContainsFunc(result.Items, func(item entity.EffectiveCapability) bool { return item.Key == key }) {
			result.Items = append(result.Items, entity.EffectiveCapability{Key: key, Name: key, Source: "WORKFLOW", Required: true, Reason: capabilityNotRequested})
		}
	}
	if err := pageEffectiveCapabilities(&result, current, filter); err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

func capabilityCatalogReady(ctx context.Context, tx pgx.Tx, current scope, configuration entity.AgentRuntimeConfiguration) (bool, error) {
	if len(configuration.ProviderPolicy.AccountCandidates) == 0 {
		return false, nil
	}
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, configuration.Provider, candidate.AccountRef)
		if errors.Is(err, errs.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !validRuntimeCatalogPin(candidate) || catalog.Digest != candidate.CatalogDigest || catalog.Revision != candidate.CatalogRevision {
			return false, nil
		}
		if !slices.ContainsFunc(catalog.Models, func(model entity.ModelCapability) bool {
			return model.ID == configuration.Model && model.Available && slices.Contains(model.EligibleProviderAccountRefs, candidate.AccountRef)
		}) {
			return false, nil
		}
	}
	return true, nil
}

func (repository *Repository) effectiveWorkflowCapabilities(ctx context.Context, tx pgx.Tx, current scope, result *entity.AgentEffectiveCapabilities) ([]string, error) {
	if result.WorkflowRef == "" {
		return nil, nil
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "WORKFLOW", ResourceRef: result.WorkflowRef})
	if err != nil {
		return nil, err
	}
	if target.scope.ProjectRef != result.ProjectRef {
		return nil, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "workflow.view", target); err != nil {
		return nil, err
	}
	var projectRef string
	var raw []byte
	err = tx.QueryRow(ctx, queryEffectiveCapabilitiesWorkflow, current.organizationID, result.WorkflowRef).Scan(&projectRef, &result.WorkflowVersionRef, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil || projectRef != result.ProjectRef {
		return nil, errs.ErrUnavailable
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(raw, &version) != nil {
		return nil, errs.ErrUnavailable
	}
	for _, step := range version.Steps {
		if step.Key == result.StepKey {
			if step.AgentRef != result.AgentRef {
				return nil, errs.ErrNotFound
			}
			return append([]string{}, step.RequiredCapabilityKeys...), nil
		}
	}
	return nil, errs.ErrNotFound
}

func effectiveCapabilityReason(requested, allowed, ready, workflow, required bool) string {
	if !allowed {
		return capabilityActorDenied
	}
	if !requested {
		return capabilityNotRequested
	}
	if workflow && !required {
		return capabilityWorkflowExcluded
	}
	if !ready {
		return capabilityRuntimeNotReady
	}
	return capabilityAvailable
}

type effectiveCapabilityCursor struct{ Digest, Scope, After string }

func pageEffectiveCapabilities(result *entity.AgentEffectiveCapabilities, current scope, filter query.Filter) error {
	slices.SortFunc(result.Items, func(a, b entity.EffectiveCapability) int {
		return strings.Compare(effectiveCapabilityRowKey(a), effectiveCapabilityRowKey(b))
	})
	canonical := *result
	canonical.EvaluatedAt = time.Time{}
	canonical.Digest = ""
	canonical.NextPageToken = ""
	canonical.Total = 0
	raw, err := json.Marshal(canonical)
	if err != nil {
		return errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	result.Digest = hex.EncodeToString(sum[:])
	bound := sha256.Sum256([]byte(strings.Join([]string{current.organizationID, current.actorID, current.authorityProjectID, result.AgentRef, result.WorkflowRef, result.StepKey, filter.Query}, "\x00")))
	scopeDigest := hex.EncodeToString(bound[:])
	after := ""
	result.Total, result.NextPageToken = 0, ""
	if filter.Page.Token != "" {
		if len(filter.Page.Token) > 2048 {
			return errs.ErrInvalid
		}
		encoded, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		var cursor effectiveCapabilityCursor
		if err != nil || len(encoded) > 1024 || decodeStrict(encoded, &cursor) != nil || cursor.Scope != scopeDigest || cursor.After == "" {
			return errs.ErrInvalid
		}
		if cursor.Digest != result.Digest {
			return errs.ErrVersionMismatch
		}
		after = cursor.After
	}
	items := []entity.EffectiveCapability{}
	limit := int(boundedPage(filter.Page))
	query := strings.ToLower(filter.Query)
	for _, item := range result.Items {
		if query != "" && !strings.Contains(strings.ToLower(item.Key+" "+item.Name+" "+item.Description), query) {
			continue
		}
		result.Total++
		if effectiveCapabilityRowKey(item) > after {
			items = append(items, item)
		}
	}
	if len(items) > limit {
		items = items[:limit]
		raw, _ := json.Marshal(effectiveCapabilityCursor{Digest: result.Digest, Scope: scopeDigest, After: effectiveCapabilityRowKey(items[len(items)-1])})
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(raw)
	}
	result.Items = items
	return nil
}

func effectiveCapabilityRowKey(item entity.EffectiveCapability) string {
	return item.Key + ":" + item.ConnectionRef + ":" + item.GrantRef
}
