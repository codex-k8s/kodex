package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/integration_candidate_scope.sql
var queryIntegrationCandidateScope string

func (repository *Repository) ListIntegrationGrantConnectionCandidates(ctx context.Context, principal value.Principal, input query.IntegrationCandidates) (entity.IntegrationConnectionCandidates, error) {
	result := entity.IntegrationConnectionCandidates{Items: []entity.IntegrationConnectionCandidate{}}
	page, err := repository.integrationCandidates(ctx, principal, "CONNECTION", input, func(ctx context.Context, tx pgx.Tx, current scope, row integrationCandidateRow, pins entity.IntegrationCandidatePins) error {
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, row.ConnectionRef, row.DefinitionKey, row.DefinitionVersion, row.DefinitionDigest)
		if err != nil {
			return err
		}
		item := entity.IntegrationConnectionCandidate{ConnectionRef: row.ConnectionRef, Name: row.Name, DefinitionKey: row.DefinitionKey,
			ProviderName: definition.Spec.Name, ProjectRef: row.ProjectRef, Reason: row.Reason, Pins: pins,
			ResourceScope: map[string]string{}, Grantable: input.Purpose == "GRANT" && row.Reason == "READY", Usable: input.Purpose == "USE" && row.Reason == "READY"}
		if definition.Spec.Credential != nil {
			item.CredentialKind = definition.Spec.Credential.Kind
		}
		configuration, _, _, err := integrationCandidateScope(ctx, tx, current, row)
		if err != nil || definition.ValidateConfiguration(configuration) != nil {
			return errs.ErrUnavailable
		}
		if input.Purpose == "USE" {
			capability, ok := definition.Capability(input.Context.CapabilityKey)
			if !ok || !capability.CallableByAgent() {
				return errs.ErrUnavailable
			}
			item.ResourceScope, err = capability.ResourceScopeValues(configuration)
			if err != nil {
				return errs.ErrUnavailable
			}
		}
		result.Items = append(result.Items, item)
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

func (repository *Repository) ListIntegrationGrantProjectCandidates(ctx context.Context, principal value.Principal, input query.IntegrationCandidates) (entity.IntegrationProjectCandidates, error) {
	result := entity.IntegrationProjectCandidates{Items: []entity.IntegrationProjectCandidate{}}
	page, err := repository.integrationCandidates(ctx, principal, "PROJECT", input, func(_ context.Context, _ pgx.Tx, _ scope, row integrationCandidateRow, pins entity.IntegrationCandidatePins) error {
		result.Items = append(result.Items, entity.IntegrationProjectCandidate{ProjectRef: row.ProjectRef, Name: row.Name, Reason: row.Reason, Grantable: row.Reason == "READY", Pins: pins})
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

func (repository *Repository) ListIntegrationGrantRecipientCandidates(ctx context.Context, principal value.Principal, input query.IntegrationCandidates) (entity.IntegrationRecipientCandidates, error) {
	result := entity.IntegrationRecipientCandidates{Items: []entity.IntegrationRecipientCandidate{}}
	page, err := repository.integrationCandidates(ctx, principal, "RECIPIENT", input, func(_ context.Context, _ pgx.Tx, _ scope, row integrationCandidateRow, pins entity.IntegrationCandidatePins) error {
		result.Items = append(result.Items, entity.IntegrationRecipientCandidate{RecipientRef: row.RecipientRef, RecipientKind: row.RecipientKind,
			ProjectRef: row.ProjectRef, Name: row.Name, Reason: row.Reason, Grantable: row.Reason == "READY", Pins: pins})
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

func (repository *Repository) ListIntegrationGrantCapabilityCandidates(ctx context.Context, principal value.Principal, input query.IntegrationCandidates) (entity.IntegrationCapabilityCandidates, error) {
	result := entity.IntegrationCapabilityCandidates{Items: []entity.IntegrationCapabilityCandidate{}}
	page, err := repository.integrationCandidates(ctx, principal, "CAPABILITY", input, func(ctx context.Context, tx pgx.Tx, current scope, row integrationCandidateRow, pins entity.IntegrationCandidatePins) error {
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, row.ConnectionRef, row.DefinitionKey, row.DefinitionVersion, row.DefinitionDigest)
		if err != nil {
			return err
		}
		capability, ok := definition.Capability(row.CapabilityKey)
		if !ok {
			return errs.ErrUnavailable
		}
		schema, err := capability.InputSchema()
		if err != nil {
			return errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return errs.ErrUnavailable
		}
		configuration, grantRef, grantVersion, err := integrationCandidateScope(ctx, tx, current, row)
		if err != nil || definition.ValidateConfiguration(configuration) != nil {
			return errs.ErrUnavailable
		}
		if _, err := capability.ResourceScopeValues(configuration); err != nil {
			return errs.ErrUnavailable
		}
		result.Items = append(result.Items, entity.IntegrationCapabilityCandidate{Capability: entity.IntegrationCapability{
			Key: capability.Key, Name: capability.Name, Description: capability.Description, Operation: capability.Operation,
			Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, ResourceKind: capability.ResourceScope.Kind,
			InputFields: integrationConfigurationFields(capability.InputFields), InputSchema: string(schema), InputSchemaSHA256: digest,
		}, Grantable: row.Reason == "READY", Reason: row.Reason, CurrentGrantRef: grantRef, CurrentGrantVersion: grantVersion, Pins: pins})
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

func integrationCandidateScope(ctx context.Context, tx pgx.Tx, current scope, row integrationCandidateRow) (map[string]string, string, int64, error) {
	var raw []byte
	var ref string
	var version int64
	err := tx.QueryRow(ctx, queryIntegrationCandidateScope, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"connection_ref": row.ConnectionRef, "recipient_kind": row.RecipientKind, "recipient_ref": row.RecipientRef,
		"capability_key": row.CapabilityKey}).Scan(&raw, &ref, &version)
	configuration := map[string]string{}
	if err != nil || json.Unmarshal(raw, &configuration) != nil {
		return nil, "", 0, errs.ErrUnavailable
	}
	return configuration, ref, version, nil
}
