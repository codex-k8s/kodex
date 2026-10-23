package caster

import (
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// errDraftMetadata is the closed response-casting failure, excluding timestamps, scope refs and values.
// errDraftMetadata — закрытая response-casting ошибка без timestamps, scope refs и значений.
var errDraftMetadata = errors.New("secret draft metadata is invalid")

// SecretDraft validates scope, required identifiers/versions, generated enum membership and required
// protobuf timestamps before returning public metadata. It does not authorize the draft, validate
// free-form name/description or impose chronological ordering on the three timestamps.
// SecretDraft проверяет scope, обязательные identifiers/versions, generated enum membership и
// protobuf timestamps перед выдачей публичных metadata. Draft authority, свободные name/description
// и хронологический порядок трёх timestamps здесь не проверяются.
func SecretDraft(draft value.SecretDraft) (*secretbrokerv1.RuntimeSecretDraftMetadata, error) {
	typeNumber, typeOK := secretbrokerv1.RuntimeSecretValueType_value["RUNTIME_SECRET_VALUE_TYPE_"+draft.ValueType]
	stateNumber, stateOK := secretbrokerv1.RuntimeSecretDraftState_value["RUNTIME_SECRET_DRAFT_STATE_"+draft.State]
	if draft.Ref == "" || runtimesecret.ValidateScope(draft.ScopeKind, draft.OrganizationRef, draft.ProjectRef) != nil || draft.SecretRef == "" || draft.Version < 1 || draft.Generation < 1 || draft.SecretVersion < 1 ||
		!typeOK || typeNumber == 0 || !stateOK || stateNumber == 0 || draft.PublishedRevision < 0 {
		return nil, errDraftMetadata
	}
	created, updated, err := metadataTimestamps(draft.CreatedAt, draft.UpdatedAt)
	if err != nil {
		return nil, err
	}
	expires, err := timestamp(draft.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RuntimeSecretDraftMetadata{Ref: draft.Ref, Version: draft.Version, Generation: draft.Generation,
		ScopeKind: secretbrokerv1.RuntimeResourceScopeKind(secretbrokerv1.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+string(draft.ScopeKind)]), OrganizationRef: draft.OrganizationRef,
		ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, Name: draft.Name, Description: draft.Description,
		ValueType: secretbrokerv1.RuntimeSecretValueType(typeNumber), State: secretbrokerv1.RuntimeSecretDraftState(stateNumber),
		PublishedRevision: draft.PublishedRevision, SecretVersion: draft.SecretVersion, CreatedAt: created, UpdatedAt: updated, ExpiresAt: expires}, nil
}

// PublishedSecret rejects nil or malformed scoped/versioned metadata and converts generated enums
// and required timestamps without plaintext or display hints; owner confirmation is a caller precondition.
// PublishedSecret отвергает nil или неверные scoped/versioned metadata и переводит generated enums
// и обязательные timestamps без plaintext/display hints; owner confirmation обязан обеспечить caller.
func PublishedSecret(secret *value.PublishedSecret) (*secretbrokerv1.RuntimeSecretMetadata, error) {
	if secret == nil {
		return nil, errDraftMetadata
	}
	typeNumber, typeOK := secretbrokerv1.RuntimeSecretValueType_value["RUNTIME_SECRET_VALUE_TYPE_"+secret.ValueType]
	stateNumber, stateOK := secretbrokerv1.RuntimeSecretStatus_value["RUNTIME_SECRET_STATUS_"+secret.Status]
	if secret.Ref == "" || runtimesecret.ValidateScope(secret.ScopeKind, secret.OrganizationRef, secret.ProjectRef) != nil || secret.Version < 1 || secret.Revision < 1 || !typeOK || typeNumber == 0 || !stateOK || stateNumber == 0 {
		return nil, errDraftMetadata
	}
	created, updated, err := metadataTimestamps(secret.CreatedAt, secret.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RuntimeSecretMetadata{SecretRef: secret.Ref, ProjectRef: secret.ProjectRef, Name: secret.Name,
		ScopeKind: secretbrokerv1.RuntimeResourceScopeKind(secretbrokerv1.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+string(secret.ScopeKind)]), OrganizationRef: secret.OrganizationRef,
		Description: secret.Description, ValueType: secretbrokerv1.RuntimeSecretValueType(typeNumber), Status: secretbrokerv1.RuntimeSecretStatus(stateNumber),
		Version: secret.Version, Revision: uint64(secret.Revision), CreatedAt: created, UpdatedAt: updated}, nil
}

// metadataTimestamps validates creation before update using the existing required-time boundary;
// an invalid member returns no partial pair and the same closed metadata error.
// metadataTimestamps проверяет creation перед update существующей required-time boundary;
// неверный элемент возвращает пустую пару и ту же закрытую ошибку metadata.
func metadataTimestamps(createdAt, updatedAt time.Time) (*timestamppb.Timestamp, *timestamppb.Timestamp, error) {
	created, err := timestamp(createdAt)
	if err != nil {
		return nil, nil, err
	}
	updated, err := timestamp(updatedAt)
	if err != nil {
		return nil, nil, err
	}
	return created, updated, nil
}

// timestamp rejects Go's zero instant, then validates the protobuf range after conversion;
// protobuf's year-one lower bound does not make a missing Go zero timestamp acceptable here.
// timestamp отвергает нулевой Go instant, затем проверяет protobuf range после преобразования;
// нижняя protobuf граница первого года здесь не разрешает отсутствующий Go zero timestamp.
func timestamp(value time.Time) (*timestamppb.Timestamp, error) {
	if value.IsZero() {
		return nil, errDraftMetadata
	}
	result := timestamppb.New(value)
	if result.CheckValid() != nil {
		return nil, errDraftMetadata
	}
	return result, nil
}
