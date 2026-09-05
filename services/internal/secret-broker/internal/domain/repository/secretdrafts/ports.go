package secretdrafts

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

var (
	ErrNotFound    = errors.New("secret draft materialization is not found")
	ErrConflict    = errors.New("secret draft materialization does not match owner intent")
	ErrUnavailable = errors.New("secret draft dependency is unavailable")
	ErrInvalid     = errors.New("secret draft request is invalid")
)

type Owner interface {
	Check(context.Context) error
	Consume(context.Context, string) (value.DraftWork, error)
	Complete(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) (value.DraftResult, error)
	Fail(context.Context, value.DraftWork) error
	ListRecovery(context.Context) ([]value.DraftWork, error)
	Recover(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) (value.DraftRecoveryDecision, error)
	CompleteCleanup(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) error
}

type Cipher interface {
	Encrypt(context.Context, value.SecretDraftBinding, []byte) (value.EncryptedSecretDraft, error)
	Decrypt(context.Context, value.SecretDraftBinding, value.EncryptedSecretDraft) ([]byte, error)
}

type EncryptedStore interface {
	Check(context.Context) error
	Create(context.Context, value.DraftWork, value.EncryptedSecretDraft) (value.DraftEncryptedDescriptor, error)
	Read(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) (value.EncryptedSecretDraft, error)
	Lookup(context.Context, value.DraftWork) (value.DraftEncryptedDescriptor, error)
	Delete(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) error
}

type RuntimeStore interface {
	Publish(context.Context, value.DraftWork, []byte) (value.DraftMaterialization, error)
	Lookup(context.Context, value.DraftWork) (value.DraftMaterialization, error)
	Delete(context.Context, value.DraftWork, value.DraftMaterialization) error
}

type Checker interface{ Check(context.Context) error }
