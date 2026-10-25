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

// queryIntegrationCandidateScope embeds the exact organization, connection, recipient and capability scope lookup for catalog decoration.
// queryIntegrationCandidateScope встраивает точный organization, connection, recipient и capability scope lookup для дополнения каталога.
//
//go:embed sql/integration_candidate_scope.sql
var queryIntegrationCandidateScope string

// ListIntegrationGrantConnectionCandidates decorates already scoped connection candidates with
// exact package metadata, validated configuration and optional use-scope values without creating a
// grant.
// ListIntegrationGrantConnectionCandidates дополняет уже ограниченные scope connection candidates
// точными package metadata, проверенной configuration и необязательными use-scope values без
// создания grant.
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

// ListIntegrationGrantProjectCandidates projects owner-filtered project rows with current
// eligibility reason and pins while the common candidate path retains authorization and
// pagination.
// ListIntegrationGrantProjectCandidates проецирует отфильтрованные владельцем project rows с
// текущими eligibility reason и pins при сохранении authorization и pagination в общем candidate
// path.
func (repository *Repository) ListIntegrationGrantProjectCandidates(ctx context.Context, principal value.Principal, input query.IntegrationCandidates) (entity.IntegrationProjectCandidates, error) {
	result := entity.IntegrationProjectCandidates{Items: []entity.IntegrationProjectCandidate{}}
	page, err := repository.integrationCandidates(ctx, principal, "PROJECT", input, func(_ context.Context, _ pgx.Tx, _ scope, row integrationCandidateRow, pins entity.IntegrationCandidatePins) error {
		result.Items = append(result.Items, entity.IntegrationProjectCandidate{ProjectRef: row.ProjectRef, Name: row.Name, Reason: row.Reason, Grantable: row.Reason == "READY", Pins: pins})
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

// ListIntegrationGrantRecipientCandidates projects scoped recipient rows with kind/project pins
// and current readiness without treating a displayed candidate as execution authority.
// ListIntegrationGrantRecipientCandidates проецирует ограниченные scope recipient rows с kind,
// project pins и текущей readiness без признания показанного кандидата полномочием исполнения.
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

// ListIntegrationGrantCapabilityCandidates resolves the exact package capability, materializes its
// schema and validates connection/resource scope before exposing candidate metadata and existing
// grant pins.
// ListIntegrationGrantCapabilityCandidates разрешает точную package capability, материализует её
// схему и проверяет connection и resource scope до выдачи candidate metadata и существующих grant
// pins.
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
		projection, err := integrationCapabilityProjection(capability)
		if err != nil {
			return err
		}
		configuration, grantRef, grantVersion, err := integrationCandidateScope(ctx, tx, current, row)
		if err != nil || definition.ValidateConfiguration(configuration) != nil {
			return errs.ErrUnavailable
		}
		if _, err := capability.ResourceScopeValues(configuration); err != nil {
			return errs.ErrUnavailable
		}
		if capability.Operation == repositoryBundleOperation {
			eligible, err := repositoryBundleRecipientEligible(ctx, tx, current, row.RecipientKind, row.RecipientRef)
			if err != nil {
				return err
			}
			if !eligible {
				row.Reason = "RECIPIENT_UNAVAILABLE"
			}
		}
		result.Items = append(result.Items, entity.IntegrationCapabilityCandidate{Capability: projection, Grantable: row.Reason == "READY", Reason: row.Reason, CurrentGrantRef: grantRef, CurrentGrantVersion: grantVersion, Pins: pins})
		return nil
	})
	result.IntegrationCandidatePage = page
	return result, err
}

// integrationCandidateScope reads configuration and existing grant locators through the
// organization/recipient/capability-scoped owner query, returning a fixed unavailable error on
// read or JSON decode failure.
// integrationCandidateScope читает configuration и существующие grant locators через owner query с
// organization, recipient и capability scope, возвращая фиксированный unavailable при ошибке
// чтения либо JSON decode.
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
