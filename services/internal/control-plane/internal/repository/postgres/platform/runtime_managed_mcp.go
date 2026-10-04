package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_managed_mcp__health.sql
var queryRuntimeManagedMCPHealth string

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
		return nil, errs.ErrConflict
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
