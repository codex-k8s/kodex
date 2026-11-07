package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// personalCredentialConfigureIntentPayload excludes mutable Kubernetes resource versions from
// semantic retry identity while retaining exact content, owner-qualified materialization and config OCC.
// personalCredentialConfigureIntentPayload исключает изменяемую Kubernetes resourceVersion из
// semantic retry identity, сохраняя точный content, owner-qualified materialization и config OCC.
type personalCredentialConfigureIntentPayload struct {
	ConnectionRef, MaterializationRef, ContentSHA256 string
	ExpectedConnectionVersion                        int64
}

// personalCredentialConfigureIntent extracts only stable metadata; plaintext never enters a command.
// personalCredentialConfigureIntent извлекает только устойчивые metadata; plaintext не входит в команду.
func personalCredentialConfigureIntent(payload command.PersonalIntegrationCredentialInput) personalCredentialConfigureIntentPayload {
	return personalCredentialConfigureIntentPayload{payload.ConnectionRef, payload.MaterializationRef, payload.Credential.ContentSHA256, payload.ExpectedConnectionVersion}
}

// PersonalIntegrationCredentialConfigureIntentDigest shares the exact command frame with its
// read-only receipt reconciliation, avoiding a second materialization after a lost response.
// PersonalIntegrationCredentialConfigureIntentDigest использует точный frame команды при read-only
// сверке receipt, исключая вторую материализацию после потерянного ответа.
func PersonalIntegrationCredentialConfigureIntentDigest(payload command.PersonalIntegrationCredentialInput) (string, error) {
	if payload.ConnectionRef == "" || payload.MaterializationRef == "" || payload.ExpectedConnectionVersion < 1 ||
		payload.Credential == nil || !validIntegrationCredentialDigest(payload.Credential.ContentSHA256) {
		return "", errs.ErrInvalid
	}
	return digest(struct {
		Kind    command.Kind
		Payload any
	}{command.ConfigurePersonalIntegrationCredential, personalCredentialConfigureIntent(payload)}), nil
}

// GetPersonalIntegrationCredential resolves the transport principal once, then asks the owner for
// that same actor's metadata. A browser cannot pass a target user or arbitrary Secret reference.
// GetPersonalIntegrationCredential единожды разрешает transport principal и запрашивает metadata
// того же actor у владельца. Browser не передаёт целевого пользователя либо произвольный Secret ref.
func (service *Service) GetPersonalIntegrationCredential(ctx context.Context, principal value.Principal, connectionRef string) (entity.PersonalIntegrationCredential, error) {
	reader, ok := service.repository.(platformrepo.PersonalIntegrationCredentialReader)
	if !ok {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	return reader.GetPersonalIntegrationCredential(ctx, resolved, connectionRef)
}

// ConfigurePersonalIntegrationCredential reconciles an earlier receipt before materialization,
// checks current self/configuration OCC, writes one owner-qualified backend key and commits only
// its immutable descriptor. A failed personal key never routes to the shared credential command.
// ConfigurePersonalIntegrationCredential сверяет прежний receipt до материализации, проверяет
// текущие self/configuration OCC, записывает один owner-qualified backend key и сохраняет только
// immutable descriptor. Отказ личного ключа не направляется к команде общей credential.
func (service *Service) ConfigurePersonalIntegrationCredential(ctx context.Context, principal value.Principal, mutation value.Mutation, connectionRef string, expectedConnectionVersion int64, credentialValue []byte) (entity.PersonalIntegrationCredential, error) {
	reader, ok := service.repository.(platformrepo.PersonalIntegrationCredentialReader)
	credentialValue = bytes.TrimSpace(credentialValue)
	if !ok || service.credentialMaterializer == nil || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 ||
		expectedConnectionVersion < 1 || len(credentialValue) == 0 || len(credentialValue) > maximumIntegrationCredentialBytes ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 {
		return entity.PersonalIntegrationCredential{}, errs.ErrInvalid
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	binding, err := reader.GetPersonalIntegrationCredential(ctx, resolved, connectionRef)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if binding.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired {
		return entity.PersonalIntegrationCredential{}, errs.ErrConflict
	}
	identity := entity.IntegrationCredentialInitiator{OrganizationID: binding.OrganizationID, SubjectID: binding.SubjectID, SubjectKind: "USER"}
	materializationRef, err := PersonalIntegrationCredentialMaterializationRef(identity, connectionRef, mutation.IdempotencyKey)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	hash := sha256.Sum256(credentialValue)
	payload := command.PersonalIntegrationCredentialInput{ConnectionRef: connectionRef, MaterializationRef: materializationRef,
		ExpectedConnectionVersion: expectedConnectionVersion, Credential: &entity.IntegrationCredentialRevision{ContentSHA256: hex.EncodeToString(hash[:])}}
	replayed, found, err := reader.ReadPersonalIntegrationCredentialReceipt(ctx, resolved, mutation, payload)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if found {
		if !samePersonalIntegrationCredentialOwner(binding, replayed) {
			return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
		}
		return replayed, nil
	}
	if binding.Version != *mutation.ExpectedVersion {
		return entity.PersonalIntegrationCredential{}, errs.ErrVersionMismatch
	}
	connection, err := service.repository.GetIntegrationConnection(ctx, resolved, connectionRef)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if connection.Version != expectedConnectionVersion {
		return entity.PersonalIntegrationCredential{}, errs.ErrVersionMismatch
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	definition, exists := definitions[connection.DefinitionKey]
	managedOpenAPI := exists && connection.DefinitionKey == "openapi-mcp" && connection.DefinitionDigest != definition.Digest && len(connection.Capabilities) > 0
	if !exists || connection.CredentialSecretKey == "" || !managedOpenAPI &&
		!definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
		return entity.PersonalIntegrationCredential{}, errs.ErrForbidden
	}
	copy := append([]byte(nil), credentialValue...)
	defer clear(copy)
	materialized, err := service.credentialMaterializer.Materialize(ctx, materializationRef, copy)
	if err != nil {
		if errors.Is(err, ErrCredentialMaterializationConflict) {
			return entity.PersonalIntegrationCredential{}, errs.ErrIdempotencyReuse
		}
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	payload.Credential = &entity.IntegrationCredentialRevision{SecretRef: materialized.SecretRef, SecretUID: materialized.SecretUID,
		SecretResourceVersion: materialized.SecretResourceVersion, ContentSHA256: materialized.ContentSHA256}
	if payload.Credential.ContentSHA256 != hex.EncodeToString(hash[:]) || !payload.Credential.ValidDescriptor() ||
		payload.Credential.SecretRef != "kodex-system/kodex-integration-credentials#"+materializationRef {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	result, err := service.executeResolved(ctx, command.Command{Kind: command.ConfigurePersonalIntegrationCredential,
		Principal: resolved, Mutation: mutation, Payload: payload})
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if result.PersonalIntegrationCredential == nil || !samePersonalIntegrationCredentialOwner(binding, *result.PersonalIntegrationCredential) {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	return *result.PersonalIntegrationCredential, nil
}

// samePersonalIntegrationCredentialOwner compares immutable owner coordinates before exposing a result.
// samePersonalIntegrationCredentialOwner сравнивает immutable owner coordinates до выдачи результата.
func samePersonalIntegrationCredentialOwner(first, second entity.PersonalIntegrationCredential) bool {
	return first.Ref == second.Ref && first.OrganizationID == second.OrganizationID && first.SubjectID == second.SubjectID && first.ConnectionRef == second.ConnectionRef
}
