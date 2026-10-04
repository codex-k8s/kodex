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
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_managed_mcp__health.sql
var queryRuntimeManagedMCPHealth string

//go:embed sql/runtime_managed_mcp__pending.sql
var queryRuntimeManagedMCPPending string

//go:embed sql/runtime_managed_mcp__startup_dependencies.sql
var queryRuntimeManagedMCPStartupDependencies string

var errManagedMCPHealthPending = errors.New("managed MCP health probe is pending")

// Недоступный enabled dependency не исчезает из required startup только потому,
// что общий callable каталог исключил DEGRADED connection. Этот guard не выдаёт
// tools или grants: рабочий список остаётся результатом прежней actor authority.
func requireManagedMCPStartupDependencies(ctx context.Context, tx pgx.Tx, organizationID, agentRef string, grants []runtimecontract.RunnerIntegrationGrant) error {
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

// Ожидание относится только к очереди startup. Рабочий call по-прежнему обязан
// получить свежий receipt, а не пользоваться pending как health authority.
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
	return nil, err
}

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

// Receipt относится к настоящему adapter probe и неизменяемым input pins.
// Изменение grant не изменяет config/credential; current connection version
// привязывается владельцем после проверки тех же semantic pins под SHARE lock.
func runtimeManagedMCPProfiles(ctx context.Context, tx pgx.Tx, organizationID, scopeKind, scopeRef, agentRef, projectRef string, grants []runtimecontract.RunnerIntegrationGrant) ([]runtimecontract.ManagedMCPProfile, error) {
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
	err := tx.QueryRow(ctx, queryRuntimeManagedMCPHealth, pgx.StrictNamedArgs{
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
	if runtimecontract.ValidateManagedMCPReadiness(input, time.Now().UTC()) != nil {
		return nil, errs.ErrConflict
	}
	return input.ManagedMCPProfiles, nil
}
