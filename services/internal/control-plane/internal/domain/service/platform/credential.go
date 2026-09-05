package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const maximumIntegrationCredentialBytes = 16 << 10

var ErrCredentialMaterializationConflict = errors.New("credential materialization conflict")

type MaterializedCredential struct {
	SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
}

type CredentialMaterializer interface {
	Materialize(context.Context, string, []byte) (MaterializedCredential, error)
}

func (service *Service) ConfigureIntegrationCredential(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	connectionRef string,
	credentialValue []byte,
) (entity.IntegrationConnection, error) {
	credentialValue = bytes.TrimSpace(credentialValue)
	if service.credentialMaterializer == nil || mutation.ExpectedVersion == nil ||
		strings.TrimSpace(connectionRef) == "" || len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 ||
		len(credentialValue) == 0 || len(credentialValue) > maximumIntegrationCredentialBytes {
		return entity.IntegrationConnection{}, errs.ErrInvalid
	}
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	connection, err := service.repository.GetIntegrationConnection(ctx, principal, connectionRef)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if connection.Version != *mutation.ExpectedVersion {
		return entity.IntegrationConnection{}, errs.ErrVersionMismatch
	}
	if connection.CredentialSecretKey == "" || !containsString(connection.NextActions, "CONFIGURE_CREDENTIAL") {
		return entity.IntegrationConnection{}, errs.ErrForbidden
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	definition, registered := definitions[connection.DefinitionKey]
	if !registered || !(definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) ||
		definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction)) {
		return entity.IntegrationConnection{}, errs.ErrForbidden
	}
	digest := sha256.Sum256([]byte(connectionRef + "\x00" + mutation.IdempotencyKey))
	materializationRef := "integration-" + hex.EncodeToString(digest[:16])
	credentialCopy := append([]byte(nil), credentialValue...)
	defer func() {
		for index := range credentialCopy {
			credentialCopy[index] = 0
		}
	}()
	materialized, err := service.credentialMaterializer.Materialize(ctx, materializationRef, credentialCopy)
	if err != nil {
		if errors.Is(err, ErrCredentialMaterializationConflict) {
			return entity.IntegrationConnection{}, errs.ErrIdempotencyReuse
		}
		return entity.IntegrationConnection{}, errors.Join(errs.ErrUnavailable, err)
	}
	result, err := service.executeResolved(ctx, command.Command{
		Kind: command.ConfigureConnectionCredential, Principal: principal, Mutation: mutation,
		Payload: command.ConnectionInput{
			Ref: connectionRef, MaterializationRef: materializationRef,
			CredentialRevision: &entity.IntegrationCredentialRevision{
				SecretRef: materialized.SecretRef, SecretUID: materialized.SecretUID,
				SecretResourceVersion: materialized.SecretResourceVersion,
				ContentSHA256:         materialized.ContentSHA256,
			},
		},
	})
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if result.Connection == nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	return *result.Connection, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
