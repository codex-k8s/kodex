package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationCredentialPolicyDependencies reads the current policy and retained active root pins.
// queryIntegrationCredentialPolicyDependencies читает текущую политику и pins сохранённых активных корней.
//
//go:embed sql/integration_credential_policy__dependencies.sql
var queryIntegrationCredentialPolicyDependencies string

// queryIntegrationCredentialPolicySet advances only policy/version and resets common test evidence.
// queryIntegrationCredentialPolicySet меняет только policy/version и сбрасывает общую test evidence.
//
//go:embed sql/integration_credential_policy__set.sql
var queryIntegrationCredentialPolicySet string

// authorizeIntegrationCredentialPolicy requires the dedicated browser operation and current exact
// Connection management authority before receipt replay. Viewing one's key grants no such authority.
// authorizeIntegrationCredentialPolicy требует выделенную browser operation и текущие права управления
// точным Connection до receipt replay. Чтение собственного ключа не выдаёт таких полномочий.
func (repository *Repository) authorizeIntegrationCredentialPolicy(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.IntegrationCredentialPolicyInput)
	if !ok || payload.ConnectionRef == "" || len(payload.ConnectionRef) > 96 || input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion < 1 ||
		(payload.Policy != entity.IntegrationCredentialProjectShared && payload.Policy != entity.IntegrationCredentialInitiatorRequired) {
		return errs.ErrInvalid
	}
	if input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != "platform.integrations.credential-policy.set" || current.authorityProjectID != "" {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: payload.ConnectionRef}); err != nil {
		return errs.ErrNotFound
	}
	return nil
}

// setIntegrationCredentialPolicy serializes with Connection consumers, rejects unsettled effects
// and active immutable root dependencies, then stores policy plus audit/event/receipt in Execute's
// transaction. It neither copies credentials nor rewrites personal bindings or past effect history.
// setIntegrationCredentialPolicy сериализуется с потребителями Connection, отклоняет незавершённые
// эффекты и активные immutable зависимости корня, сохраняет политику с audit/event/receipt в
// транзакции Execute. Не копирует ключи, не переписывает личные привязки и историю эффектов.
func (repository *Repository) setIntegrationCredentialPolicy(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationCredentialPolicyInput)
	if !ok || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	locked, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if locked.version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	effects, grants, err := repository.integrationConnectionDependencies(ctx, tx, locked.id)
	if err != nil {
		return commandOutcome{}, err
	}
	var policy entity.IntegrationCredentialPolicy
	var roots int64
	if err := tx.QueryRow(ctx, queryIntegrationCredentialPolicyDependencies, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_id": locked.id,
	}).Scan(&policy, &roots); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := platformservice.ValidateIntegrationCredentialPolicyChange(policy, payload.Policy, locked.enabled, locked.state, effects, grants, roots); err != nil {
		return commandOutcome{}, err
	}
	if payload.Policy == entity.IntegrationCredentialProjectShared {
		definition, definitionErr := repository.integrationPackage(ctx, tx, current.organizationID, payload.ConnectionRef, locked.definitionKey, locked.definitionVersion, locked.definitionDigest)
		if definitionErr != nil {
			return commandOutcome{}, definitionErr
		}
		if githubScopedDefinition(definition) {
			connection, readErr := readConnection(ctx, tx, current, payload.ConnectionRef)
			if readErr != nil {
				return commandOutcome{}, readErr
			}
			identity, ok := connection.PublicConfiguration["service_identity"].(string)
			if !ok || identity == "" {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
	}
	if policy != payload.Policy {
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, payload.ConnectionRef, locked.definitionKey, locked.definitionVersion, locked.definitionDigest)
		if err != nil {
			return commandOutcome{}, err
		}
		if payload.Policy == entity.IntegrationCredentialInitiatorRequired && (!definition.RequiresConnectionCredential() ||
			definition.Spec.AdapterOwner != string(integrationpackage.OwnerIntegrationGateway) || definition.Spec.ExecutionRoute != string(integrationpackage.RouteManagedMCP)) {
			return commandOutcome{}, errs.ErrInvalid
		}
		var ref string
		if err := tx.QueryRow(ctx, queryIntegrationCredentialPolicySet, pgx.StrictNamedArgs{
			"connection_id": locked.id, "expected_version": locked.version, "policy": string(payload.Policy),
		}).Scan(&ref); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	item, err := readConnection(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref,
		summary: "i18n:INTEGRATION_CREDENTIAL_POLICY_UPDATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED",
		platformAggregateVersion: item.Version, platformState: item.State}, nil
}
