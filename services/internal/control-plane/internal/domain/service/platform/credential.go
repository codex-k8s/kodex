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

// maximumIntegrationCredentialBytes limits the trimmed integration credential body to sixteen KiB
// before materialization and owner command execution.
// maximumIntegrationCredentialBytes ограничивает обрезанное по краям тело реквизитов интеграции
// шестнадцатью KiB до материализации и команды владельца.
const maximumIntegrationCredentialBytes = 16 << 10

// ErrCredentialMaterializationConflict identifies materializer reuse of the same identity with
// different content so callers can return the domain idempotency-reuse error.
// ErrCredentialMaterializationConflict обозначает повтор identity materializer с другим содержимым
// для возврата вызывающим доменной ошибки повторного ключа идемпотентности.
var ErrCredentialMaterializationConflict = errors.New("credential materialization conflict")

// MaterializedCredential holds immutable secret reference, UID, resource version and content
// digest for an owner receipt, excluding plaintext credential material.
// MaterializedCredential содержит immutable-ссылку секрета, UID, resource version и дайджест
// содержимого для квитанции владельца без plaintext реквизитов.
type MaterializedCredential struct {
	SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// CredentialMaterializer defines materialization of mutable credential bytes under a stable
// identity, returning exact storage metadata rather than persisting business state.
// CredentialMaterializer задаёт материализацию изменяемых байтов реквизитов по устойчивой identity
// с возвратом точных метаданных хранения без сохранения бизнес-состояния.
type CredentialMaterializer interface {
	// Materialize writes or reconciles the materialization identity using supplied bytes and returns
	// the exact immutable secret descriptor or a conflict/error.
	// Materialize записывает либо сверяет identity материализации по переданным байтам и возвращает
	// точный immutable-дескриптор секрета либо conflict или ошибку.
	Materialize(context.Context, string, []byte) (MaterializedCredential, error)
}

// ConfigureIntegrationCredential trims credential edges, resolves owner scope/version and
// executable definition, materializes a copied buffer and commits only its descriptor through the
// already resolved principal.
// ConfigureIntegrationCredential обрезает края реквизитов, разрешает область и версию владельца и
// исполняемое определение, материализует копию буфера и сохраняет только дескриптор с уже
// разрешённым principal.
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
	// Поставленный OpenAPI-шаблон не исполняется сам по себе. Только owner-read
	// опубликованной привязки возвращает иной digest и capability для команды;
	// repository повторно сверяет exact package в той же write-транзакции.
	managedOpenAPI := registered && connection.DefinitionKey == "openapi-mcp" &&
		connection.DefinitionDigest != definition.Digest && len(connection.Capabilities) > 0
	if !registered || (!managedOpenAPI &&
		!definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) &&
		!definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction)) {
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

// containsString performs an exact case-sensitive membership check without trimming, normalization
// or inferred action availability.
// containsString проверяет точное регистрозависимое вхождение строки без обрезки, нормализации
// либо вывода доступности действия.
func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
