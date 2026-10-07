package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_recipient_integration_catalog__context.sql
var queryAssistantRecipientIntegrationCatalogContext string

//go:embed sql/assistant_recipient_integration_catalog__entries.sql
var queryAssistantRecipientIntegrationCatalogEntries string

func (repository *Repository) assistantRecipientIntegrationCatalogTx(ctx context.Context, tx pgx.Tx, current scope, leaseRef string, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	result := entity.AssistantConfigurationCatalogResponse{Kind: input.Kind, AssistantRef: input.AssistantRef, ScopeKind: "PROJECT", OrganizationRef: current.organizationRef}
	catalog := &entity.AssistantRecipientIntegrationGrantCatalog{Entries: []entity.AssistantRecipientIntegrationGrantCatalogEntry{}}
	err := tx.QueryRow(ctx, queryAssistantRecipientIntegrationCatalogContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "lease_ref": leaseRef, "required_operation": "CHANGE_INTEGRATION_GRANT"}).Scan(
		&catalog.RecipientKind, &catalog.RecipientRef, &catalog.RecipientName, &catalog.RecipientVersion, &result.ProjectRef, &catalog.ProjectVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	catalog.ContextEntityKind, catalog.ContextEntityRef, catalog.ContextEntityVersion = catalog.RecipientKind, catalog.RecipientRef, catalog.RecipientVersion
	if input.EntityKind != "" && (input.EntityKind != catalog.RecipientKind || input.EntityRef != catalog.RecipientRef) {
		if catalog.RecipientKind != "WORKFLOW" || input.EntityKind != "AGENT" {
			return result, errs.ErrNotFound
		}
		before, version, err := repository.readAssistantWorkflowSnapshot(ctx, tx, current, result.ProjectRef, catalog.RecipientRef)
		if err != nil {
			return result, err
		}
		if version != catalog.ContextEntityVersion || !assistantWorkflowContainsAgent(before, input.EntityRef) {
			return result, errs.ErrNotFound
		}
		projection, err := repository.resolveAssistantContext(ctx, tx, current, entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: input.EntityRef}, result.ProjectRef)
		if err != nil || projection.EntityVersion == nil || !contains(projection.AllowedOperations, "CHANGE_INTEGRATION_GRANT") {
			return result, errs.ErrNotFound
		}
		catalog.RecipientKind, catalog.RecipientRef, catalog.RecipientName, catalog.RecipientVersion = "AGENT", projection.EntityRef, projection.EntityName, *projection.EntityVersion
	}
	shippedRevisions, err := repository.assistantRecipientCatalogShippedRevisions()
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, queryAssistantRecipientIntegrationCatalogEntries, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project_id": current.authorityProjectID,
		"project_ref": result.ProjectRef, "recipient_kind": catalog.RecipientKind, "recipient_ref": catalog.RecipientRef, "query": input.Query, "offset": input.Offset, "shipped_revisions": shippedRevisions})
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	type key struct{ connection, capability string }
	keys := []key{}
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.connection, &item.capability); err != nil {
			rows.Close()
			return result, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		keys = append(keys, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, assistantLockedReadError(rows.Err(), errs.ErrUnavailable)
	}
	if len(keys) > 10 {
		keys, result.NextOffset = keys[:10], input.Offset+10
	}
	for _, key := range keys {
		snapshot, err := repository.readAssistantIntegrationGrantSnapshot(ctx, tx, current, result.ProjectRef, key.connection, key.capability, catalog.RecipientKind, catalog.RecipientRef)
		if err != nil {
			return result, err
		}
		definition, executable, err := repository.assistantRecipientCatalogPackage(ctx, tx, current.organizationID, key.connection, snapshot.definitionKey, snapshot.definitionVersion, snapshot.definitionDigest)
		if err != nil {
			return result, err
		}
		capability, found := definition.Capability(key.capability)
		if !found || !capability.CallableByAgent() || snapshot.recipientVersion != catalog.RecipientVersion {
			return result, errs.ErrUnavailable
		}
		schema, err := capability.InputSchema()
		if err != nil {
			return result, errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return result, errs.ErrUnavailable
		}
		candidate := entity.SystemAssistantIntegrationGrantCandidate{Capability: entity.IntegrationCapability{Key: capability.Key, Name: capability.Name, Description: capability.Description, Operation: capability.Operation,
			Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, AllowedApprovalPolicies: append([]string{}, capability.AllowedApprovalPolicies...), ResourceKind: capability.ResourceScope.Kind,
			InputFields: integrationConfigurationFields(capability.InputFields), InputSchema: string(schema), InputSchemaSHA256: digest}, Grantable: snapshot.reason == "READY", Reason: snapshot.reason,
			CurrentGrantRef: snapshot.grantRef, CurrentGrantVersion: snapshot.grantVersion, CurrentGrantEnabled: snapshot.enabled, CurrentApprovalPolicy: snapshot.approvalPolicy, CurrentApprovalScopePaths: append([]string{}, snapshot.approvalScopePaths...)}
		if !executable {
			candidate.Grantable, candidate.Reason = false, "PACKAGE_UNAVAILABLE"
		}
		pins := entity.IntegrationCandidatePins{ConnectionVersion: snapshot.connectionVersion, DefinitionVersion: snapshot.definitionVersion, DefinitionDigest: snapshot.definitionDigest, ProjectVersion: catalog.ProjectVersion, RecipientVersion: catalog.RecipientVersion}
		encoded, err := json.Marshal(struct {
			Organization, Actor, Lease, Project, Kind, Recipient, Connection, Capability string
			Pins                                                                         entity.IntegrationCandidatePins
		}{current.organizationID, current.actorID, leaseRef, result.ProjectRef, catalog.RecipientKind, catalog.RecipientRef, key.connection, key.capability, pins})
		if err != nil {
			return result, errs.ErrUnavailable
		}
		hash := sha256.Sum256(encoded)
		pins.ContextDigest = hex.EncodeToString(hash[:])
		catalog.Entries = append(catalog.Entries, entity.AssistantRecipientIntegrationGrantCatalogEntry{Grant: entity.ProjectAssistantIntegrationGrantCatalogEntry{ConnectionRef: key.connection, ConnectionName: snapshot.connectionName,
			ConnectionVersion: snapshot.connectionVersion, DefinitionVersion: snapshot.definitionVersion, DefinitionDigest: snapshot.definitionDigest, Candidate: candidate}, Pins: pins})
	}
	result.RecipientIntegrationGrants = catalog
	return result, nil
}

// Только безопасный metadata read: точная published ревизия, несовместимая с
// текущим adapter, видна как PACKAGE_UNAVAILABLE и не получает GRANT authority.
// Исполняемый resolver и mutation admission остаются без compatibility fallback.
func (repository *Repository) assistantRecipientCatalogPackage(ctx context.Context, runner queryRunner, organizationID, connectionRef, key, version, digest string) (integrationpackage.Package, bool, error) {
	shipped, known := repository.integrationDefinitions[key]
	if !known {
		return integrationpackage.Package{}, false, errs.ErrForbidden
	}
	if _, current := integrationpackage.ResolveShippedRevision(shipped, version, digest); current {
		definition, err := repository.integrationPackage(ctx, runner, organizationID, connectionRef, key, version, digest)
		return definition, err == nil, err
	}
	var format, content string
	err := runner.QueryRow(ctx, queryIntegrationPackageBoundRevision, organizationID, connectionRef).Scan(&format, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrationpackage.Package{}, false, errors.Join(errs.ErrForbidden, errIntegrationPackageUnavailable)
	}
	if err != nil {
		return integrationpackage.Package{}, false, errs.ErrUnavailable
	}
	if format != "JSON" && format != "YAML" {
		return integrationpackage.Package{}, false, errs.ErrForbidden
	}
	definition, err := integrationpackage.Parse([]byte(content))
	if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest {
		return integrationpackage.Package{}, false, errs.ErrForbidden
	}
	return definition, integrationpackage.ValidateExecutableRevision(definition, shipped) == nil, nil
}

// Реестр текущего producer, а не устаревшая DB-проекция, определяет exact
// shipped pins. Непригодные unbound ревизии исключаются до пагинации; найденные
// published bindings остаются для полного closed decoder и проверки corruption.
func (repository *Repository) assistantRecipientCatalogShippedRevisions() (string, error) {
	if len(repository.integrationDefinitions) == 0 {
		return "", errs.ErrUnavailable
	}
	type revision struct {
		Version string `json:"version"`
		Digest  string `json:"digest"`
	}
	revisions := make(map[string]revision, len(repository.integrationDefinitions))
	for key, definition := range repository.integrationDefinitions {
		revisions[key] = revision{Version: definition.Metadata.Version, Digest: definition.Digest}
	}
	encoded, err := json.Marshal(revisions)
	if err != nil {
		return "", errs.ErrUnavailable
	}
	return string(encoded), nil
}

// Назначение в exact draft не является grant: отдельный owner admission остаётся обязательным.
func assistantWorkflowContainsAgent(before map[string]any, agentRef string) bool {
	if assistantString(before, "coordinatorAgentRef") == agentRef {
		return true
	}
	steps, ok := before["steps"].([]any)
	if !ok {
		return false
	}
	for _, raw := range steps {
		step, ok := raw.(map[string]any)
		if ok && assistantString(step, "agentRef") == agentRef {
			return true
		}
	}
	return false
}
