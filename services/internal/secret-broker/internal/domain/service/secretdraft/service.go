package secretdraft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

type Service struct {
	owner                             secretdrafts.Owner
	cipher                            secretdrafts.Cipher
	keys                              secretdrafts.Checker
	staged                            secretdrafts.EncryptedStore
	runtime                           secretdrafts.RuntimeStore
	maximumBytes                      int
	stagedNamespace, runtimeNamespace string
	now                               func() time.Time
	observer                          secretdrafts.Observer
	recoveryMu                        sync.RWMutex
	recoveryRan, recoveryReady        bool
}

type Option func(*Service)

func WithObserver(observer secretdrafts.Observer) Option {
	return func(service *Service) { service.observer = observer }
}

func New(owner secretdrafts.Owner, cipher secretdrafts.Cipher, keys secretdrafts.Checker, staged secretdrafts.EncryptedStore,
	runtime secretdrafts.RuntimeStore, maximumBytes int, stagedNamespace, runtimeNamespace string, options ...Option) (*Service, error) {
	if owner == nil || cipher == nil || keys == nil || staged == nil || runtime == nil || maximumBytes < 1 ||
		maximumBytes > value.MaximumDraftValueBytes || stagedNamespace == "" || runtimeNamespace == "" || stagedNamespace == runtimeNamespace {
		return nil, secretdrafts.ErrInvalid
	}
	service := &Service{owner: owner, cipher: cipher, keys: keys, staged: staged, runtime: runtime, maximumBytes: maximumBytes,
		stagedNamespace: stagedNamespace, runtimeNamespace: runtimeNamespace, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

func (service *Service) Check(ctx context.Context) error {
	if err := service.CheckDependencies(ctx); err != nil {
		return err
	}
	service.recoveryMu.RLock()
	ready := service.recoveryRan && service.recoveryReady
	service.recoveryMu.RUnlock()
	if !ready {
		return secretdrafts.ErrUnavailable
	}
	return nil
}

func (service *Service) CheckDependencies(ctx context.Context) error {
	if err := service.owner.Check(ctx); err != nil {
		return err
	}
	if err := service.keys.Check(ctx); err != nil {
		return secretdrafts.ErrUnavailable
	}
	return service.staged.Check(ctx)
}

func (service *Service) Execute(ctx context.Context, kind value.DraftOperation, grant string, plaintext []byte) (value.DraftResult, error) {
	// Владение request bytes заканчивается вместе с командой, включая ошибки consume.
	defer clear(plaintext)
	if grant == "" || len(grant) > 16384 {
		return value.DraftResult{}, secretdrafts.ErrInvalid
	}
	switch kind {
	case value.DraftSave:
		if len(plaintext) == 0 || len(plaintext) > service.maximumBytes {
			return value.DraftResult{}, secretdrafts.ErrInvalid
		}
	case value.DraftValidate, value.DraftPublish, value.DraftDiscard:
		if len(plaintext) != 0 {
			return value.DraftResult{}, secretdrafts.ErrInvalid
		}
	default:
		return value.DraftResult{}, secretdrafts.ErrInvalid
	}
	work, err := service.owner.Consume(ctx, grant)
	if err != nil {
		return value.DraftResult{}, err
	}
	if err := service.validateWork(work, true); err != nil || work.Kind != kind {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	wantedState := map[value.DraftOperation]string{value.DraftSave: "PREPARING", value.DraftPublish: "PUBLISHING", value.DraftDiscard: "DISCARDED"}
	if state, ok := wantedState[kind]; ok && work.Draft.State != state {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	if kind == value.DraftValidate && work.Draft.State != "DRAFT" && work.Draft.State != "VALID" {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	// После этой границы внешние действия ограничены точной lease владельца.
	operation, cancel := context.WithDeadline(ctx, work.LeaseDeadline)
	defer cancel()
	var encrypted *value.DraftEncryptedDescriptor
	var materialization *value.DraftMaterialization
	switch kind {
	case value.DraftSave:
		if err := service.validateValue(work.Binding, plaintext); err != nil {
			return value.DraftResult{}, service.fail(operation, work, err)
		}
		sealed, err := service.cipher.Encrypt(operation, work.Binding, plaintext)
		if err != nil {
			return value.DraftResult{}, service.fail(operation, work, secretdrafts.ErrUnavailable)
		}
		defer clear(sealed.Ciphertext)
		descriptor, err := service.staged.Create(operation, work, sealed)
		// Неизвестный исход Create сохраняет intent для recovery, не повторяет SAVE.
		if err != nil {
			return value.DraftResult{}, err
		}
		encrypted = &descriptor
	case value.DraftValidate, value.DraftPublish:
		if work.Encrypted == nil {
			return value.DraftResult{}, service.fail(operation, work, secretdrafts.ErrConflict)
		}
		sealed, err := service.staged.Read(operation, work, *work.Encrypted)
		if err != nil {
			return value.DraftResult{}, err
		}
		defer clear(sealed.Ciphertext)
		opened, err := service.cipher.Decrypt(operation, work.Binding, sealed)
		defer clear(opened)
		if err != nil {
			return value.DraftResult{}, service.fail(operation, work, secretdrafts.ErrConflict)
		}
		if err := service.validateValue(work.Binding, opened); err != nil {
			return value.DraftResult{}, service.fail(operation, work, err)
		}
		encrypted = work.Encrypted
		if kind == value.DraftPublish {
			actual, err := service.runtime.Publish(operation, work, opened)
			if err != nil {
				return value.DraftResult{}, err
			}
			materialization = &actual
		}
	case value.DraftDiscard:
		if work.Draft.State != "DISCARDED" {
			return value.DraftResult{}, secretdrafts.ErrConflict
		}
		encrypted = work.Encrypted
		if encrypted != nil {
			if err := service.staged.Delete(operation, work, *encrypted); err != nil {
				return value.DraftResult{}, err
			}
		}
	}
	// Completion идемпотентна у владельца. Потерянный ответ не означает FAIL
	// и не разрешает повторный внешний эффект; восстановление идёт по owner work.
	result, err := service.owner.Complete(operation, work, encrypted, materialization)
	if err != nil {
		return value.DraftResult{}, err
	}
	if result.Draft.Ref != work.Draft.Ref || result.Draft.Generation != work.Draft.Generation ||
		result.Draft.ProjectRef != work.Draft.ProjectRef || result.Draft.SecretRef != work.Draft.SecretRef {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	finalState := map[value.DraftOperation]string{value.DraftSave: "DRAFT", value.DraftValidate: "VALID", value.DraftPublish: "PUBLISHED", value.DraftDiscard: "DISCARDED"}[kind]
	if result.Draft.State != finalState || kind == value.DraftPublish && (result.Secret == nil ||
		result.Secret.Ref != work.Draft.SecretRef || result.Secret.ProjectRef != work.Draft.ProjectRef || result.Secret.Revision != work.TargetRevision) {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	return result, nil
}

func (service *Service) validateWork(work value.DraftWork, active bool) error {
	if work.Binding.Validate() != nil || work.OperationRef == "" || work.ClaimGeneration < 0 ||
		(work.ClaimantID == "") != (work.ClaimGeneration == 0) || active && work.ClaimGeneration == 0 ||
		work.Draft.Ref != work.Binding.DraftRef || work.Draft.Generation != work.Binding.DraftGeneration ||
		work.Draft.SecretRef != work.Binding.SecretRef || work.Draft.ProjectRef != work.Binding.ProjectRef ||
		work.Draft.ValueType != work.Binding.ValueType || work.StagedNamespace != service.stagedNamespace ||
		work.RuntimeNamespace != service.runtimeNamespace || work.StagedName == "" || work.StagedKey == "" {
		return secretdrafts.ErrConflict
	}
	if active && (!work.LeaseDeadline.After(service.now()) || !work.ExpiresAt.After(service.now()) || work.LeaseDeadline.After(work.ExpiresAt)) {
		return secretdrafts.ErrConflict
	}
	if work.Kind == value.DraftPublish && work.TargetRevision < 1 {
		return secretdrafts.ErrConflict
	}
	return nil
}

func (service *Service) validateValue(binding value.SecretDraftBinding, plaintext []byte) error {
	if len(plaintext) == 0 || len(plaintext) > service.maximumBytes {
		return secretdrafts.ErrInvalid
	}
	digest := sha256.Sum256(plaintext)
	if hex.EncodeToString(digest[:]) != binding.ContentSHA256 {
		return secretdrafts.ErrConflict
	}
	switch binding.ValueType {
	case "BINARY":
		return nil
	case "STRING":
		if utf8.Valid(plaintext) {
			return nil
		}
	case "JSON":
		if utf8.Valid(plaintext) && json.Valid(plaintext) {
			return nil
		}
	}
	return secretdrafts.ErrInvalid
}

func (service *Service) fail(ctx context.Context, work value.DraftWork, cause error) error {
	if err := service.owner.Fail(ctx, work); err != nil {
		return errors.Join(cause, secretdrafts.ErrUnavailable)
	}
	return cause
}
