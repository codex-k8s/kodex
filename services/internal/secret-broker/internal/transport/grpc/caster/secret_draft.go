package caster

import (
	"errors"
	"time"

	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errDraftMetadata = errors.New("secret draft metadata is invalid")

func SecretDraft(draft value.SecretDraft) (*secretbrokerv1.RuntimeSecretDraftMetadata, error) {
	typeNumber, typeOK := secretbrokerv1.RuntimeSecretValueType_value["RUNTIME_SECRET_VALUE_TYPE_"+draft.ValueType]
	stateNumber, stateOK := secretbrokerv1.RuntimeSecretDraftState_value["RUNTIME_SECRET_DRAFT_STATE_"+draft.State]
	if draft.Ref == "" || draft.ProjectRef == "" || draft.SecretRef == "" || draft.Version < 1 || draft.Generation < 1 || draft.SecretVersion < 1 ||
		!typeOK || typeNumber == 0 || !stateOK || stateNumber == 0 || draft.PublishedRevision < 0 {
		return nil, errDraftMetadata
	}
	created, err := timestamp(draft.CreatedAt)
	if err != nil {
		return nil, err
	}
	updated, err := timestamp(draft.UpdatedAt)
	if err != nil {
		return nil, err
	}
	expires, err := timestamp(draft.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RuntimeSecretDraftMetadata{Ref: draft.Ref, Version: draft.Version, Generation: draft.Generation,
		ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, Name: draft.Name, Description: draft.Description,
		ValueType: secretbrokerv1.RuntimeSecretValueType(typeNumber), State: secretbrokerv1.RuntimeSecretDraftState(stateNumber),
		PublishedRevision: draft.PublishedRevision, SecretVersion: draft.SecretVersion, CreatedAt: created, UpdatedAt: updated, ExpiresAt: expires}, nil
}

func PublishedSecret(secret *value.PublishedSecret) (*secretbrokerv1.RuntimeSecretMetadata, error) {
	if secret == nil {
		return nil, errDraftMetadata
	}
	typeNumber, typeOK := secretbrokerv1.RuntimeSecretValueType_value["RUNTIME_SECRET_VALUE_TYPE_"+secret.ValueType]
	stateNumber, stateOK := secretbrokerv1.RuntimeSecretStatus_value["RUNTIME_SECRET_STATUS_"+secret.Status]
	if secret.Ref == "" || secret.ProjectRef == "" || secret.Version < 1 || secret.Revision < 1 || !typeOK || typeNumber == 0 || !stateOK || stateNumber == 0 {
		return nil, errDraftMetadata
	}
	created, err := timestamp(secret.CreatedAt)
	if err != nil {
		return nil, err
	}
	updated, err := timestamp(secret.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &secretbrokerv1.RuntimeSecretMetadata{SecretRef: secret.Ref, ProjectRef: secret.ProjectRef, Name: secret.Name,
		Description: secret.Description, ValueType: secretbrokerv1.RuntimeSecretValueType(typeNumber), Status: secretbrokerv1.RuntimeSecretStatus(stateNumber),
		Version: secret.Version, Revision: uint64(secret.Revision), CreatedAt: created, UpdatedAt: updated}, nil
}

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
