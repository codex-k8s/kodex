package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeManagedMCPHealth embeds the latest successful integration-gateway Context7 probe
// within five minutes, matching current connection/package/configuration/credential pins under
// SHARE locks. It returns receipt metadata; it neither runs a probe nor grants a tool capability.
// queryRuntimeManagedMCPHealth встраивает latest successful integration-gateway Context7 probe за
// пять минут с current connection/package/configuration/credential pins под SHARE locks.
// Возвращает receipt metadata; probe не запускает и tool capability не выдаёт.
//
//go:embed sql/runtime_managed_mcp__health.sql
var queryRuntimeManagedMCPHealth string

// queryRuntimeManagedMCPPending embeds exact enabled agent grant-pair/current connection pins and
// a live refresh attempt chain whose origin is at most thirty seconds old. DUE or fenced live
// CLAIMED work may be pending; pending is queue status, not successful health or authority to call
// tools.
// queryRuntimeManagedMCPPending встраивает exact enabled agent grant-pair/current connection pins
// и live refresh attempt chain с origin не старше тридцати секунд. DUE либо fenced live CLAIMED
// work может быть pending; pending описывает queue, а не successful health или право вызова tools.
//
//go:embed sql/runtime_managed_mcp__pending.sql
var queryRuntimeManagedMCPPending string

// queryRuntimeManagedMCPStartupDependencies embeds total enabled Context7 agent dependencies and
// the subset matching the exact active connection, ready adapter/package and READ/NONE
// resolve/query grants. Comparing both counts prevents degraded or mismatched required
// dependencies from silently disappearing.
// queryRuntimeManagedMCPStartupDependencies встраивает total enabled Context7 agent dependencies и
// подмножество с exact active connection, ready adapter/package и READ/NONE resolve/query grants.
// Сравнение обоих counts не позволяет degraded либо mismatched required dependencies незаметно
// исчезнуть.
//
//go:embed sql/runtime_managed_mcp__startup_dependencies.sql
var queryRuntimeManagedMCPStartupDependencies string

// queryRuntimeManagedMCPStale embeds a stable-input latest terminal success at least five minutes
// old with refresh eligibility and no active refresh task, under connection/credential SHARE
// locks. It identifies a possible recovery origin, not a fresh usable health receipt.
// queryRuntimeManagedMCPStale встраивает stable-input latest terminal success не моложе пяти минут
// с refresh eligibility и без active refresh task под connection/credential SHARE locks.
// Определяет возможный recovery origin, а не fresh usable health receipt.
//
//go:embed sql/runtime_managed_mcp__stale.sql
var queryRuntimeManagedMCPStale string

// errManagedMCPHealthPending marks bounded startup waiting for an exact pending refresh rather
// than an ordinary ready profile or permanent failure. This internal sentinel grants no health
// authority and does not itself reschedule work.
// errManagedMCPHealthPending помечает ограниченное startup waiting точного pending refresh вместо
// ready profile либо permanent failure. Internal sentinel не выдаёт health authority и сам не
// назначает work.
var errManagedMCPHealthPending = errors.New("managed MCP health probe is pending")

// requireManagedMCPStartupDependencies derives whether Context7 is required from effective
// capability-scope intersection, rejects stray grants outside that scope, then requires exactly
// two matching dependencies/grants unless none exist. Query errors retain the existing
// serializable marker mapping; it creates no grants or transaction commit.
// requireManagedMCPStartupDependencies выводит необходимость Context7 из effective
// capability-scope intersection, отклоняет лишние grants вне scope, затем требует ровно две
// matching dependencies/grants либо их полного отсутствия. Query errors сохраняют прежнее
// serializable marker mapping; grants и commit не создаются.
func requireManagedMCPStartupDependencies(ctx context.Context, tx pgx.Tx, organizationID, agentRef string, grants []runtimecontract.RunnerIntegrationGrant, capabilityScopes ...[]string) error {
	if !managedMCPStartupScopeRequired(capabilityScopes...) {
		for _, grant := range grants {
			if grant.DefinitionKey == "context7" {
				return errs.ErrConflict
			}
		}
		return nil
	}
	resolve, query, valid := managedMCPPendingGrantPair(grants)
	var required, matched int64
	err := tx.QueryRow(ctx, queryRuntimeManagedMCPStartupDependencies, pgx.StrictNamedArgs{
		"organization_id": organizationID, "agent_ref": agentRef,
		"connection_ref": resolve.ConnectionRef, "connection_version": resolve.ConnectionVersion,
		"definition_version": resolve.DefinitionVersion, "definition_digest": resolve.DefinitionDigest,
		"resolve_ref": resolve.Ref, "resolve_version": resolve.GrantVersion,
		"query_ref": query.Ref, "query_version": query.GrantVersion,
	}).Scan(&required, &matched)
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if required == 0 && !valid && resolve.Ref == "" && query.Ref == "" {
		return nil
	}
	if !valid || required != 2 || matched != 2 {
		return errs.ErrConflict
	}
	return nil
}

// managedMCPStartupScopeRequired intersects the two Context7 capability keys with every supplied
// scope and reports whether any survive. Omitted scopes preserve the startup guard; an explicit
// empty layer removes it. One surviving key still requires a complete grant pair downstream.
// managedMCPStartupScopeRequired пересекает две Context7 capability keys со всеми supplied scopes
// и сообщает, сохранилась ли хотя бы одна. Отсутствие scopes сохраняет startup guard; explicit
// empty layer снимает его. Одна surviving key всё ещё требует complete grant pair далее.
func managedMCPStartupScopeRequired(capabilityScopes ...[]string) bool {
	layers := append([][]string{{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability}}, capabilityScopes...)
	return len(promptservice.Intersection(layers...)) != 0
}

// runtimeManagedMCPProfilesForStartup first requests fresh profiles; only Conflict enters exact
// pair/pending lookup and then stale stable-origin recovery classification. A pending origin
// returns its sentinel, a single valid stale profile returns a recovery error carrying original
// scope/grants, and other failures retain their mappings. It never returns stale profiles as ready
// or resets the pending budget itself.
// runtimeManagedMCPProfilesForStartup сначала запрашивает fresh profiles; только Conflict
// запускает exact pair/pending lookup, затем классификацию stale stable-origin recovery. Pending
// origin возвращает sentinel, единственный valid stale profile — recovery error с исходными
// scope/grants, прочие failures сохраняют mappings. Никогда не возвращает stale profiles как ready
// и сам не сбрасывает pending budget.
func runtimeManagedMCPProfilesForStartup(ctx context.Context, tx pgx.Tx, organizationID, scopeKind, scopeRef, agentRef, projectRef string, grants []runtimecontract.RunnerIntegrationGrant) ([]runtimecontract.ManagedMCPProfile, error) {
	profiles, err := runtimeManagedMCPProfiles(ctx, tx, organizationID, scopeKind, scopeRef, agentRef, projectRef, grants)
	if !errors.Is(err, errs.ErrConflict) {
		return profiles, err
	}
	resolve, query, valid := managedMCPPendingGrantPair(grants)
	if !valid {
		return nil, err
	}
	var pending bool
	readErr := tx.QueryRow(ctx, queryRuntimeManagedMCPPending, pgx.StrictNamedArgs{
		"organization_id": organizationID, "agent_ref": agentRef,
		"connection_ref": resolve.ConnectionRef, "connection_version": resolve.ConnectionVersion,
		"definition_version": resolve.DefinitionVersion, "definition_digest": resolve.DefinitionDigest,
		"resolve_ref": resolve.Ref, "resolve_version": resolve.GrantVersion,
		"query_ref": query.Ref, "query_version": query.GrantVersion,
	}).Scan(&pending)
	if readErr != nil {
		return nil, serializableTransactionError(readErr, errs.ErrUnavailable)
	}
	if pending {
		return nil, errManagedMCPHealthPending
	}
	profiles, staleErr := runtimeManagedMCPProfilesWithHealthQuery(ctx, tx, organizationID, scopeKind, scopeRef, agentRef, projectRef, grants, queryRuntimeManagedMCPStale, false)
	if staleErr != nil {
		if !errors.Is(staleErr, errs.ErrConflict) {
			return nil, staleErr
		}
		return nil, err
	}
	if len(profiles) != 1 {
		return nil, err
	}
	return nil, &managedMCPStartupRecovery{scopeKind: scopeKind, scopeRef: scopeRef, agentRef: agentRef, projectRef: projectRef, grants: grants}
}

// managedMCPPendingGrantPair selects only Context7 grants and requires exactly two distinct
// READ/NONE versioned resolve/query entries with matching connection/package pins. Other
// integrations are ignored; returned partial entries on false must not be treated as admitted
// authority, and full profile validation remains downstream.
// managedMCPPendingGrantPair выбирает только Context7 grants и требует ровно две разные READ/NONE
// versioned resolve/query entries с matching connection/package pins. Другие integrations
// игнорируются; partial entries при false не являются admitted authority, полная profile
// validation остаётся далее.
func managedMCPPendingGrantPair(grants []runtimecontract.RunnerIntegrationGrant) (runtimecontract.RunnerIntegrationGrant, runtimecontract.RunnerIntegrationGrant, bool) {
	var resolve, query runtimecontract.RunnerIntegrationGrant
	count := 0
	for _, grant := range grants {
		if grant.DefinitionKey != "context7" {
			continue
		}
		count++
		if grant.Risk != "READ" || grant.ApprovalPolicy != "NONE" || grant.Ref == "" || grant.GrantVersion < 1 || grant.ConnectionVersion < 1 {
			return resolve, query, false
		}
		switch {
		case grant.CapabilityKey == runtimecontract.Context7ResolveCapability && grant.Operation == runtimecontract.Context7ResolveCapability:
			resolve = grant
		case grant.CapabilityKey == runtimecontract.Context7QueryCapability && grant.Operation == runtimecontract.Context7QueryCapability:
			query = grant
		default:
			return resolve, query, false
		}
	}
	valid := count == 2 && resolve.Ref != "" && query.Ref != "" && resolve.Ref != query.Ref &&
		resolve.ConnectionRef == query.ConnectionRef && resolve.ConnectionVersion == query.ConnectionVersion &&
		resolve.DefinitionVersion == query.DefinitionVersion && resolve.DefinitionDigest == query.DefinitionDigest
	return resolve, query, valid
}

// runtimeManagedMCPProfiles delegates profile derivation to the current health query with
// freshness enforcement enabled. It uses the caller transaction and existing grants; no probe,
// grant issuance or commit is performed.
// runtimeManagedMCPProfiles делегирует profile derivation current health query с enabled freshness
// enforcement. Использует caller transaction и существующие grants; probe, grant issuance и commit
// отсутствуют.
func runtimeManagedMCPProfiles(ctx context.Context, tx pgx.Tx, organizationID, scopeKind, scopeRef, agentRef, projectRef string, grants []runtimecontract.RunnerIntegrationGrant) ([]runtimecontract.ManagedMCPProfile, error) {
	return runtimeManagedMCPProfilesWithHealthQuery(ctx, tx, organizationID, scopeKind, scopeRef, agentRef, projectRef, grants, queryRuntimeManagedMCPHealth, true)
}

// runtimeManagedMCPProfilesWithHealthQuery requires all Context7 entries to reference one
// connection, reads receipt/configuration metadata through the selected query, canonicalizes
// configuration JSON and derives the exact managed profile. Shared profile validation always runs;
// readiness time validation runs only when requested. No Context7 grants yields nil success,
// missing/invalid health Conflict, backend errors the existing retry-aware mapping; scope inputs
// must come from the trusted caller.
// runtimeManagedMCPProfilesWithHealthQuery требует одну connection для всех Context7 entries,
// читает receipt/configuration metadata выбранным query, канонизирует configuration JSON и выводит
// exact managed profile. Shared profile validation выполняется всегда; readiness time validation —
// по флагу. Отсутствие Context7 grants даёт nil success, missing/invalid health — Conflict,
// backend errors — прежнее retry-aware mapping; scope inputs поступают от trusted caller.
func runtimeManagedMCPProfilesWithHealthQuery(ctx context.Context, tx pgx.Tx, organizationID, scopeKind, scopeRef, agentRef, projectRef string, grants []runtimecontract.RunnerIntegrationGrant, healthQuery string, requireFresh bool) ([]runtimecontract.ManagedMCPProfile, error) {
	var selected *runtimecontract.RunnerIntegrationGrant
	for i := range grants {
		grant := &grants[i]
		if grant.DefinitionKey != "context7" {
			continue
		}
		if selected != nil && selected.ConnectionRef != grant.ConnectionRef {
			return nil, errs.ErrConflict
		}
		selected = grant
	}
	if selected == nil {
		return nil, nil
	}
	var health runtimecontract.ManagedMCPHealthProof
	var configuration []byte
	err := tx.QueryRow(ctx, healthQuery, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_ref": selected.ConnectionRef,
		"connection_version": selected.ConnectionVersion, "definition_version": selected.DefinitionVersion,
		"definition_digest": selected.DefinitionDigest,
	}).Scan(&health.TestRef, &health.Generation, &health.ConnectionRef, &health.ConnectionVersion,
		&configuration, &health.CredentialRevisionRef, &health.CredentialRevision,
		&health.CredentialSHA256, &health.DefinitionKey, &health.DefinitionVersion,
		&health.DefinitionDigest, &health.CheckedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrConflict
		}
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	var configurationValue map[string]any
	if json.Unmarshal(configuration, &configurationValue) != nil {
		return nil, errs.ErrConflict
	}
	canonicalConfiguration, err := json.Marshal(configurationValue)
	if err != nil {
		return nil, errs.ErrConflict
	}
	digest := sha256.Sum256(canonicalConfiguration)
	health.ConfigurationSHA256 = hex.EncodeToString(digest[:])
	health.Probe, health.CheckedAt = runtimecontract.ManagedMCPHealthProbe, health.CheckedAt.UTC()
	profile, err := runtimecontract.DeriveContext7ManagedMCPProfile(scopeKind, scopeRef, grants, health)
	if err != nil {
		return nil, errs.ErrConflict
	}
	input := runtimecontract.RunnerInput{AgentRef: agentRef, ProjectRef: projectRef, AssistantScope: runtimecontract.AssistantScopeNone,
		IntegrationGrants: grants, ManagedMCPProfiles: []runtimecontract.ManagedMCPProfile{profile}}
	if scopeKind == "SYSTEM" {
		input.AssistantScope = runtimecontract.AssistantScopeSystem
	} else if scopeKind == "PROJECT" {
		input.AssistantScope, input.AssistantProfileRef = runtimecontract.AssistantScopeProject, scopeRef
	}
	if runtimecontract.ValidateManagedMCPProfiles(input) != nil || (requireFresh && runtimecontract.ValidateManagedMCPReadiness(input, time.Now().UTC()) != nil) {
		return nil, errs.ErrConflict
	}
	return input.ManagedMCPProfiles, nil
}
