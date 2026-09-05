package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/integration_package__bound_revision.sql
var queryIntegrationPackageBoundRevision string

//go:embed sql/integration_package__bind_connection.sql
var queryIntegrationPackageBindConnection string

func (repository *Repository) bindIntegrationPackage(ctx context.Context, tx pgx.Tx, current scope, connectionRef, format, content string) error {
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, connectionRef)
	if err != nil {
		return err
	}
	definition, err := repository.executableIntegrationPackage(format, content)
	if err != nil || definition.Metadata.Key != connection.definitionKey {
		return errs.ErrInvalid
	}
	stored, err := readConnection(ctx, tx, current, connectionRef)
	if err != nil {
		return err
	}
	configuration, valid := integrationStringConfiguration(stored.PublicConfiguration)
	if !valid || definition.ValidateConfiguration(configuration) != nil {
		return errs.ErrInvalid
	}
	effects, _, err := repository.integrationConnectionDependencies(ctx, tx, connection.id)
	if err != nil {
		return err
	}
	if effects != 0 {
		return errs.ErrConflict
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": connectionRef}
	for _, query := range []string{queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest} {
		if _, err := tx.Exec(ctx, query, args); err != nil {
			return errs.ErrUnavailable
		}
	}
	tag, err := tx.Exec(ctx, queryIntegrationPackageBindConnection, current.organizationID, connectionRef, definition.Metadata.Version, definition.Digest)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", connectionRef, "i18n:INTEGRATION_CONNECTION_UPDATED", connection.version+1, "")
}

func projectConnectionPackage(ctx context.Context, querier connectionQuerier, current scope, item *entity.IntegrationConnection) error {
	var format, content string
	err := querier.QueryRow(ctx, queryIntegrationPackageBoundRevision, current.organizationID, item.Ref).Scan(&format, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || (format != "JSON" && format != "YAML") {
		return errs.ErrUnavailable
	}
	definition, err := integrationpackage.Parse([]byte(content))
	if err != nil || definition.Metadata.Key != item.DefinitionKey || definition.Metadata.Version != item.DefinitionVersion || definition.Digest != item.DefinitionDigest {
		return errs.ErrUnavailable
	}
	item.DefinitionName = definition.Spec.Name
	item.Capabilities = make([]entity.IntegrationCapability, 0, len(definition.Spec.Capabilities))
	for _, capability := range definition.Spec.Capabilities {
		schema, err := capability.InputSchema()
		if err != nil {
			return errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return errs.ErrUnavailable
		}
		item.Capabilities = append(item.Capabilities, entity.IntegrationCapability{
			Key: capability.Key, Name: capability.Name, Description: capability.Description,
			Operation: capability.Operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy,
			ResourceKind: capability.ResourceScope.Kind, InputFields: integrationConfigurationFields(capability.InputFields),
			InputSchema: string(schema), InputSchemaSHA256: digest,
		})
		if capability.Operation == definition.Spec.HealthCheck.Operation {
			item.TestRequiresApproval = capability.ApprovalPolicy != "NONE"
		}
	}
	return nil
}

// Один owner read path для connection, grant, invocation и private worker claim.
func (repository *Repository) integrationPackage(ctx context.Context, tx pgx.Tx, organizationID, connectionRef, key, version, digest string) (integrationpackage.Package, error) {
	shipped, ok := repository.integrationDefinitions[key]
	if !ok {
		return integrationpackage.Package{}, errs.ErrForbidden
	}
	if shipped.Metadata.Version == version && shipped.Digest == digest {
		return shipped, nil
	}
	var format, content string
	err := tx.QueryRow(ctx, queryIntegrationPackageBoundRevision, organizationID, connectionRef).Scan(&format, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrationpackage.Package{}, errs.ErrForbidden
	}
	if err != nil {
		return integrationpackage.Package{}, errs.ErrUnavailable
	}
	definition, err := repository.executableIntegrationPackage(format, content)
	if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest {
		return integrationpackage.Package{}, errs.ErrForbidden
	}
	return definition, nil
}

func (repository *Repository) normalizeIntegrationDraft(format, content, managedBy string) (string, string) {
	if format != "JSON" && format != "YAML" {
		return format, content
	}
	_, canonical, err := integrationpackage.NormalizeManagedRevision([]byte(content), managedBy, repository.integrationDefinitions)
	if err != nil {
		// Невалидный draft сохраняется для диагностики; публикация запрещена.
		return format, content
	}
	return "JSON", string(canonical)
}

func (repository *Repository) executableIntegrationPackage(format, content string) (integrationpackage.Package, error) {
	if format != "JSON" && format != "YAML" {
		return integrationpackage.Package{}, errs.ErrInvalid
	}
	definition, err := integrationpackage.Parse([]byte(content))
	baseline, ok := repository.integrationDefinitions[definition.Metadata.Key]
	if err != nil || !ok || integrationpackage.ValidateExecutableRevision(definition, baseline) != nil {
		return integrationpackage.Package{}, errs.ErrInvalid
	}
	return definition, nil
}
