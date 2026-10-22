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

// Service orchestrates exact owner-authorized draft commands and cleanup recovery through explicit ports;
// its mutex protects recovery readiness, not command serialization.
// Service согласует точные owner-authorized draft commands и cleanup recovery через явные ports; mutex
// защищает recovery readiness, не сериализует команды.
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

// Option configures a new service during trusted composition; it is not a user-facing runtime
// configuration command.
// Option настраивает новый service при доверенной composition; это не пользовательская runtime
// configuration command.
type Option func(*Service)

// WithObserver installs an optional observer, including nil, without starting work or checking dependency
// readiness.
// WithObserver устанавливает необязательный observer, включая nil, без запуска работы и проверки
// dependency readiness.
func WithObserver(observer secretdrafts.Observer) Option {
	return func(service *Service) { service.observer = observer }
}

// New requires nonnil ports, a positive bounded byte limit and distinct nonempty namespaces, then applies
// nonnil options. It does not probe ports, detect typed nils or start recovery.
// New требует nonnil ports, положительный ограниченный byte limit и разные непустые namespaces, затем
// применяет nonnil options. Ports, typed nil и recovery здесь не проверяются и не запускаются.
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

// Check first probes dependencies, then requires the most recent completed reconciliation to have
// succeeded; it does not itself run recovery.
// Check сначала проверяет dependencies, затем требует успеха последнего завершённого reconciliation; сам
// recovery не запускает.
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

// CheckDependencies checks owner, key readiness and staging in that order, mapping key failure to
// Unavailable; it does not probe cipher or runtime storage.
// CheckDependencies проверяет owner, готовность ключей и staging в таком порядке, отображая key failure в
// Unavailable; cipher и runtime storage не проверяет.
func (service *Service) CheckDependencies(ctx context.Context) error {
	if err := service.owner.Check(ctx); err != nil {
		return err
	}
	if err := service.keys.Check(ctx); err != nil {
		return secretdrafts.ErrUnavailable
	}
	return service.staged.Check(ctx)
}

// Execute clears caller plaintext on every return, validates input before consuming a grant and checks
// exact owner work before lease-bounded effects. SAVE seals, VALIDATE/PUBLISH open, DISCARD only cleans;
// uncertain writes/completion are left for recovery, never blindly repeated or failed.
// Execute очищает caller plaintext при любом return, проверяет input до потребления grant и exact owner
// work до lease-bounded effects. SAVE шифрует, VALIDATE/PUBLISH расшифровывают, DISCARD только очищает;
// неопределённые writes/completion оставляет recovery без слепого повтора и Fail.
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
		result.Draft.ScopeKind != work.Draft.ScopeKind || result.Draft.OrganizationRef != work.Draft.OrganizationRef ||
		result.Draft.ProjectRef != work.Draft.ProjectRef || result.Draft.SecretRef != work.Draft.SecretRef {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	finalState := map[value.DraftOperation]string{value.DraftSave: "DRAFT", value.DraftValidate: "VALID", value.DraftPublish: "PUBLISHED", value.DraftDiscard: "DISCARDED"}[kind]
	if result.Draft.State != finalState || kind == value.DraftPublish && (result.Secret == nil ||
		result.Secret.ScopeKind != work.Draft.ScopeKind || result.Secret.OrganizationRef != work.Draft.OrganizationRef ||
		result.Secret.Ref != work.Draft.SecretRef || result.Secret.ProjectRef != work.Draft.ProjectRef || result.Secret.Revision != work.TargetRevision) {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	return result, nil
}

// validateWork matches binding/draft/scope/namespaces and claim shape; active work additionally needs a
// positive claim and future lease/expiry with lease no later than expiry. Recovery skips time freshness,
// not exact binding checks.
// validateWork сверяет binding/draft/scope/namespaces и форму claim; active work дополнительно требует
// положительный claim, будущие lease/expiry и lease не позже expiry. Recovery пропускает актуальность
// времени, не exact binding checks.
func (service *Service) validateWork(work value.DraftWork, active bool) error {
	if work.Binding.Validate() != nil || work.OperationRef == "" || work.ClaimGeneration < 0 ||
		(work.ClaimantID == "") != (work.ClaimGeneration == 0) || active && work.ClaimGeneration == 0 ||
		work.Draft.Ref != work.Binding.DraftRef || work.Draft.Generation != work.Binding.DraftGeneration ||
		work.Draft.ScopeKind != work.Binding.ScopeKind || work.Draft.OrganizationRef != work.Binding.OrganizationRef ||
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

// validateValue enforces nonempty bounded bytes, the binding's exact SHA-256 and BINARY/UTF-8 STRING/UTF-8
// JSON rules; syntactic JSON is not application-schema validation.
// validateValue требует непустые ограниченные байты, точный SHA-256 из binding и правила BINARY/UTF-8
// STRING/UTF-8 JSON; JSON syntax не заменяет application-schema validation.
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

// fail reports the definite failure once using the supplied context; if owner acknowledgement fails it
// joins the original cause with Unavailable, not with raw owner diagnostics.
// fail однократно сообщает определённый failure через переданный context; при отказе owner acknowledgement
// объединяет исходную cause с Unavailable, не с raw owner diagnostics.
func (service *Service) fail(ctx context.Context, work value.DraftWork, cause error) error {
	if err := service.owner.Fail(ctx, work); err != nil {
		return errors.Join(cause, secretdrafts.ErrUnavailable)
	}
	return cause
}
