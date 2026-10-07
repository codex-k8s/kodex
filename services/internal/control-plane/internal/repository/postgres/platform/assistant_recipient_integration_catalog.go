package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

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
	err := tx.QueryRow(ctx, queryAssistantRecipientIntegrationCatalogContext, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "lease_ref": leaseRef}).Scan(
		&catalog.RecipientKind, &catalog.RecipientRef, &catalog.RecipientName, &catalog.RecipientVersion, &result.ProjectRef, &catalog.ProjectVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	rows, err := tx.Query(ctx, queryAssistantRecipientIntegrationCatalogEntries, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project_id": current.authorityProjectID,
		"project_ref": result.ProjectRef, "recipient_kind": catalog.RecipientKind, "recipient_ref": catalog.RecipientRef, "query": input.Query, "offset": input.Offset})
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
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, key.connection, snapshot.definitionKey, snapshot.definitionVersion, snapshot.definitionDigest)
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
