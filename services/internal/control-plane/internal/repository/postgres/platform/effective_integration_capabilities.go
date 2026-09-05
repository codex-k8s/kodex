package platform

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

type capabilityGrant struct {
	ref, key, connectionRef, definitionKey, definitionVersion, definitionDigest string
	connectionName                                                              string
	version, connectionVersion                                                  int64
	eligible                                                                    bool
}

func (repository *Repository) effectiveIntegrationCapabilities(ctx context.Context, tx pgx.Tx, current scope, authority capabilityAuthority, agentRef string, ready bool, required []string, workflow bool) ([]entity.EffectiveCapability, error) {
	rows, err := tx.Query(ctx, queryEffectiveCapabilitiesGrants, current.organizationID, agentRef)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	grants := []capabilityGrant{}
	for rows.Next() {
		var grant capabilityGrant
		if rows.Scan(&grant.ref, &grant.version, &grant.key, &grant.connectionRef, &grant.connectionVersion, &grant.definitionKey, &grant.definitionVersion, &grant.definitionDigest, &grant.connectionName, &grant.eligible) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		grants = append(grants, grant)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(grants) > 4096 {
		return nil, errs.ErrUnavailable
	}
	items := make([]entity.EffectiveCapability, 0, len(grants))
	for _, grant := range grants {
		item := entity.EffectiveCapability{Key: grant.key, Name: grant.key, Source: "INTEGRATION", Requested: true, Required: slices.Contains(required, grant.key), ConnectionRef: grant.connectionRef, ConnectionVersion: grant.connectionVersion, GrantRef: grant.ref, GrantVersion: grant.version, DefinitionDigest: grant.definitionDigest, Reason: capabilityGrantUnavailable}
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, grant.connectionRef, grant.definitionKey, grant.definitionVersion, grant.definitionDigest)
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			item.Reason = capabilityPackageUnavailable
			items = append(items, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		capability, known := definition.Capability(grant.key)
		if !known || !capability.CallableByAgent() {
			item.Reason = capabilityPackageUnavailable
			items = append(items, item)
			continue
		}
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: grant.connectionRef})
		if errors.Is(err, errs.ErrNotFound) {
			items = append(items, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		item.Grantable = authority.allowed("integration.manage", target.scope)
		allowed := integrationCapabilityAllowed(authority, capability, target.scope)
		if !allowed {
			item.Reason = capabilityActorDenied
		} else if grant.eligible {
			item.Name, item.Description = capability.Name, capability.Description
			item.Reason = effectiveCapabilityReason(true, true, ready, workflow, item.Required)
		}
		item.Effective = item.Reason == capabilityAvailable
		items = append(items, item)
	}
	return items, nil
}

// Preview и runtime читают одинаковые exact grants внутри owner transaction.
func (repository *Repository) agentCapabilityAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string, requested []string) ([]string, []map[string]string, error) {
	rows, err := tx.Query(ctx, queryEffectiveCapabilitiesGrants, current.organizationID, agentRef)
	if err != nil {
		return nil, nil, errs.ErrUnavailable
	}
	grants := []map[string]string{}
	count := 0
	for rows.Next() {
		var grant capabilityGrant
		if rows.Scan(&grant.ref, &grant.version, &grant.key, &grant.connectionRef, &grant.connectionVersion, &grant.definitionKey, &grant.definitionVersion, &grant.definitionDigest, &grant.connectionName, &grant.eligible) != nil {
			rows.Close()
			return nil, nil, errs.ErrUnavailable
		}
		count++
		if grant.eligible {
			grants = append(grants, map[string]string{"ref": grant.ref, "grantVersion": strconv.FormatInt(grant.version, 10), "connectionRef": grant.connectionRef, "connectionName": grant.connectionName, "connectionVersion": strconv.FormatInt(grant.connectionVersion, 10), "definitionKey": grant.definitionKey, "definitionVersion": grant.definitionVersion, "definitionDigest": grant.definitionDigest, "capabilityKey": grant.key})
		}
	}
	rows.Close()
	if rows.Err() != nil || count > 4096 {
		return nil, nil, errs.ErrUnavailable
	}
	return repository.runtimeCapabilityAuthority(ctx, tx, current, projectRef, agentRef, requested, grants)
}

func integrationCapabilityAllowed(authority capabilityAuthority, capability integrationpackage.Capability, target entity.AccessScope) bool {
	permission := "integration.manage"
	if capability.Risk == "READ" {
		permission = "integration.view"
	}
	return authority.allowed(permission, target)
}

// Свежий runtime использует current actor bindings и exact connection package.
// Возвращаемый grant список фильтруется по connection, а не только общему key.
func (repository *Repository) runtimeCapabilityAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string, requested []string, grants []map[string]string) ([]string, []map[string]string, error) {
	authority, err := repository.capabilityAuthority(ctx, tx, current, projectRef, agentRef)
	if err != nil {
		return nil, nil, err
	}
	platform := []string{}
	for _, key := range requested {
		if authority.platformAllowed(key) {
			platform = append(platform, key)
		}
	}
	allowedGrants := []map[string]string{}
	for _, grant := range grants {
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, grant["connectionRef"], grant["definitionKey"], grant["definitionVersion"], grant["definitionDigest"])
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		capability, ok := definition.Capability(grant["capabilityKey"])
		if !ok || !capability.CallableByAgent() {
			continue
		}
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: grant["connectionRef"]})
		if errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if !integrationCapabilityAllowed(authority, capability, target.scope) {
			continue
		}
		schema, err := capability.InputSchema()
		if err != nil {
			return nil, nil, errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return nil, nil, errs.ErrUnavailable
		}
		clone := make(map[string]string, len(grant))
		for key, value := range grant {
			clone[key] = value
		}
		clone["operation"], clone["capabilityName"], clone["capabilityDescription"], clone["risk"] = capability.Operation, capability.Name, capability.Description, capability.Risk
		clone["inputSchema"], clone["inputSchemaSha256"] = string(schema), digest
		allowedGrants = append(allowedGrants, clone)
		platform = append(platform, capability.Key)
	}
	return platform, allowedGrants, nil
}
