package providercredential

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

const (
	maximumAPIKeyBytes      = 16 << 10
	minimumAPIKeyBytes      = 8
	defaultDeviceAuthTTL    = 15 * time.Minute
	deviceAuthFinishBudget  = 30 * time.Second
	deviceAuthCleanupBudget = 40 * time.Second
)

var (
	ErrInvalidInput = errors.New("provider credential materializer input is invalid")
	ErrNotFound     = errors.New("provider credential materializer attempt is not found")
	ErrConflict     = errors.New("provider credential materializer state conflicts")
)

type Store interface {
	Check(context.Context) error
	CreateProviderAuthorizationAttempt(context.Context, kubernetesstore.ProviderAuthorizationAttempt) (kubernetesstore.ProviderAuthorizationAttempt, bool, error)
	GetProviderAuthorizationAttempt(context.Context, string) (kubernetesstore.ProviderAuthorizationAttempt, error)
	CompleteProviderAuthorizationAttempt(context.Context, kubernetesstore.ProviderAuthorizationAttempt) (kubernetesstore.ProviderAuthorizationAttempt, error)
	CreateProviderCredential(context.Context, string, string, []byte) (kubernetesstore.ProviderCredentialDescriptor, error)
	ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
	DiscardProviderAuthorizationAttempt(context.Context, string, string, string, string, string) error
	DiscardProviderCredential(context.Context, string, string, kubernetesstore.ProviderCredentialDescriptor) error
	CleanupProviderCredential(context.Context, string, string, int64, kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderCredentialCleanupResult, error)
	ObserveProviderAuthorizationCleanup(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error)
	FenceProviderAuthorization(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) error
	FenceAuthorizationCredentialName(context.Context, kubernetesstore.ProviderAuthorizationCleanupTarget) (*kubernetesstore.ProviderCredentialDescriptor, error)
	ReadProviderCleanupReceipt(context.Context, kubernetesstore.ProviderCleanupReceiptIdentity) (kubernetesstore.ProviderCredentialCleanupResult, bool, error)
	CompleteProviderCleanupReceipt(context.Context, kubernetesstore.ProviderCleanupReceiptIdentity, *kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderCredentialCleanupResult, error)
}

type DeviceAuthorization struct {
	MaterializerAttemptRef, VerificationURI, UserCode  string
	MaterializerAttemptUID, MaterializerAttemptVersion string
	ExpiresAt                                          time.Time
}

type DiscardMaterialization struct {
	AttemptRef, AccountRef                         string
	MaterializerAttemptRef, MaterializerAttemptUID string
	MaterializerAttemptVersion                     string
	Credential                                     *kubernetesstore.ProviderCredentialDescriptor
}

type deviceWorker struct {
	session DeviceAuthorizationSession
	done    chan struct{}
	discard chan struct{}
	once    sync.Once
}

type Service struct {
	lifecycle context.Context
	store     Store
	appServer AppServer
	deviceTTL time.Duration

	mu       sync.Mutex
	sessions map[string]*deviceWorker
	workers  sync.WaitGroup
}

func New(lifecycle context.Context, store Store, appServer AppServer, deviceTTL time.Duration) (*Service, error) {
	if lifecycle == nil || store == nil || appServer == nil || deviceTTL < time.Minute || deviceTTL > time.Hour {
		return nil, errors.New("provider credential materializer configuration is invalid")
	}
	return &Service{lifecycle: lifecycle, store: store, appServer: appServer, deviceTTL: deviceTTL, sessions: map[string]*deviceWorker{}}, nil
}

func (service *Service) Check(ctx context.Context) error {
	return errors.Join(service.store.Check(ctx), service.appServer.Check(ctx))
}

func (service *Service) StartDeviceAuthorization(
	ctx context.Context,
	attemptRef, accountRef string,
) (DeviceAuthorization, error) {
	materializerRef, homeName, err := deviceAuthorizationReferences(attemptRef, accountRef)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	stored, err := service.store.GetProviderAuthorizationAttempt(ctx, materializerRef)
	if err == nil {
		if stored.AttemptRef != attemptRef || stored.AccountRef != accountRef || stored.State != "PENDING" {
			return DeviceAuthorization{}, ErrConflict
		}
		return service.activeDeviceAuthorization(ctx, stored)
	}
	if !errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound) {
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	session, err := service.appServer.StartDeviceAuthorization(ctx, materializerRef, homeName)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	expiresAt := time.Now().UTC().Add(service.deviceTTL)
	wanted := kubernetesstore.ProviderAuthorizationAttempt{
		AttemptRef: attemptRef, AccountRef: accountRef, MaterializerAttemptRef: materializerRef,
		State: "PENDING", VerificationURI: session.VerificationURI(), UserCode: session.UserCode(), ExpiresAt: expiresAt,
	}
	stored, created, err := service.store.CreateProviderAuthorizationAttempt(ctx, wanted)
	if err != nil {
		_ = session.Close()
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	if !created {
		_ = session.Close()
		return service.activeDeviceAuthorization(ctx, stored)
	}
	worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
	service.mu.Lock()
	service.sessions[materializerRef] = worker
	service.mu.Unlock()
	service.workers.Add(1)
	go service.waitForDeviceAuthorization(stored, worker)
	return castDeviceAuthorization(stored), nil
}

func (service *Service) activeDeviceAuthorization(
	ctx context.Context,
	stored kubernetesstore.ProviderAuthorizationAttempt,
) (DeviceAuthorization, error) {
	if time.Now().UTC().Before(stored.ExpiresAt) {
		return castDeviceAuthorization(stored), nil
	}
	stored.State = "EXPIRED"
	stored.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
	stored.VerificationURI = ""
	stored.UserCode = ""
	_, err := service.store.CompleteProviderAuthorizationAttempt(ctx, stored)
	if err != nil && !errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
		return DeviceAuthorization{}, errors.Join(ErrConflict, err)
	}
	return DeviceAuthorization{}, ErrConflict
}

func (service *Service) Discard(ctx context.Context, target DiscardMaterialization) error {
	if target.MaterializerAttemptRef != "" {
		if target.Credential != nil || target.MaterializerAttemptUID == "" || target.MaterializerAttemptVersion == "" {
			return ErrInvalidInput
		}
		service.mu.Lock()
		worker := service.sessions[target.MaterializerAttemptRef]
		service.mu.Unlock()
		if worker != nil {
			worker.once.Do(func() { close(worker.discard) })
			_ = worker.session.Close()
			select {
			case <-worker.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return service.store.DiscardProviderAuthorizationAttempt(ctx, target.AttemptRef, target.AccountRef,
			target.MaterializerAttemptRef, target.MaterializerAttemptUID, target.MaterializerAttemptVersion)
	}
	if target.Credential == nil {
		return ErrInvalidInput
	}
	return service.store.DiscardProviderCredential(ctx, target.AttemptRef, target.AccountRef, *target.Credential)
}

func (service *Service) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	receipt, err := service.store.CleanupProviderCredential(ctx, taskRef, accountRef, leaseGeneration, descriptor)
	if errors.Is(err, kubernetesstore.ErrProviderCredentialCleanupInvalid) {
		return kubernetesstore.ProviderCredentialCleanupResult{}, ErrInvalidInput
	}
	return receipt, err
}

func (service *Service) ObserveAuthorizationCleanup(ctx context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderAuthorizationCleanupObservation, error) {
	return service.store.ObserveProviderAuthorizationCleanup(ctx, target)
}

func (service *Service) CleanupAuthorization(ctx context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, deviceAuthCleanupBudget)
	defer cancel()
	identity, err := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	if result, found, err := service.store.ReadProviderCleanupReceipt(ctx, identity); err != nil || found {
		return result, err
	}
	// CAS fence закрывает новых producers на всех репликах до локального join.
	if err := service.store.FenceProviderAuthorization(ctx, target); err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	service.mu.Lock()
	worker := service.sessions[target.MaterializerAttemptRef]
	service.mu.Unlock()
	if worker != nil {
		worker.once.Do(func() { close(worker.discard) })
		_ = worker.session.Close()
		select {
		case <-worker.done:
		case <-ctx.Done():
			return kubernetesstore.ProviderCredentialCleanupResult{}, ctx.Err()
		}
	}
	produced, err := service.store.FenceAuthorizationCredentialName(ctx, target)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	return service.store.CompleteProviderCleanupReceipt(ctx, identity, produced)
}

func (service *Service) ObserveDeviceAuthorization(
	ctx context.Context,
	materializerAttemptRef string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	stored, err := service.store.GetProviderAuthorizationAttempt(ctx, materializerAttemptRef)
	if errors.Is(err, kubernetesstore.ErrProviderAttemptNotFound) {
		return kubernetesstore.ProviderAuthorizationAttempt{}, ErrNotFound
	}
	if err != nil {
		return kubernetesstore.ProviderAuthorizationAttempt{}, err
	}
	if stored.State == "PENDING" && !time.Now().UTC().Before(stored.ExpiresAt) {
		stored.State = "EXPIRED"
		stored.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
		stored.VerificationURI = ""
		stored.UserCode = ""
		stored, err = service.store.CompleteProviderAuthorizationAttempt(ctx, stored)
		if err != nil && !errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
			return kubernetesstore.ProviderAuthorizationAttempt{}, err
		}
		if errors.Is(err, kubernetesstore.ErrProviderAttemptConflict) {
			return service.store.GetProviderAuthorizationAttempt(ctx, materializerAttemptRef)
		}
	}
	return stored, nil
}

func (service *Service) MaterializeAPIKey(
	ctx context.Context,
	attemptRef, accountRef string,
	apiKey []byte,
) (kubernetesstore.ProviderCredentialDescriptor, string, error) {
	if _, _, err := deviceAuthorizationReferences(attemptRef, accountRef); err != nil ||
		len(apiKey) < minimumAPIKeyBytes || len(apiKey) > maximumAPIKeyBytes ||
		len(strings.TrimSpace(string(apiKey))) != len(apiKey) || strings.ContainsAny(string(apiKey), "\r\n\x00") {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrInvalidInput
	}
	encoded, err := json.Marshal(struct {
		APIKey   string `json:"OPENAI_API_KEY"`
		AuthMode string `json:"auth_mode"`
	}{APIKey: string(apiKey), AuthMode: "apikey"})
	if err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", errors.New("encode provider API key authorization")
	}
	defer clear(encoded)
	descriptor, err := service.store.CreateProviderCredential(ctx, attemptRef, accountRef, encoded)
	if errors.Is(err, kubernetesstore.ErrProviderCredentialConflict) {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrConflict
	}
	if err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", err
	}
	return descriptor, "API key account", nil
}

func (service *Service) Close() error {
	service.mu.Lock()
	sessions := make([]*deviceWorker, 0, len(service.sessions))
	for _, worker := range service.sessions {
		sessions = append(sessions, worker)
	}
	service.sessions = map[string]*deviceWorker{}
	service.mu.Unlock()
	var result error
	for _, worker := range sessions {
		result = errors.Join(result, worker.session.Close())
	}
	service.workers.Wait()
	return result
}

func (service *Service) waitForDeviceAuthorization(
	attempt kubernetesstore.ProviderAuthorizationAttempt,
	worker *deviceWorker,
) {
	defer service.workers.Done()
	defer close(worker.done)
	defer func() {
		service.mu.Lock()
		delete(service.sessions, attempt.MaterializerAttemptRef)
		service.mu.Unlock()
		_ = worker.session.Close()
	}()
	session := worker.session
	wait, cancel := context.WithDeadline(service.lifecycle, attempt.ExpiresAt)
	// Cleanup мог установить fence между Create pending и регистрацией worker.
	// Свежий owner read закрывает запуск такого локального producer.
	current, readErr := service.store.GetProviderAuthorizationAttempt(wait, attempt.MaterializerAttemptRef)
	if readErr != nil || current.State != "PENDING" || current.AttemptRef != attempt.AttemptRef || current.AccountRef != attempt.AccountRef ||
		current.SecretUID != attempt.SecretUID || current.MaterializerAttemptRef != attempt.MaterializerAttemptRef {
		cancel()
		return
	}
	authJSON, masked, err := session.Wait(wait)
	cancel()
	select {
	case <-worker.discard:
		clear(authJSON)
		return
	default:
	}
	terminal := attempt
	terminal.VerificationURI = ""
	terminal.UserCode = ""
	if err == nil {
		finish, finishCancel := context.WithTimeout(context.WithoutCancel(service.lifecycle), deviceAuthFinishBudget)
		descriptor, materializeErr := service.store.CreateProviderCredential(finish, attempt.AttemptRef, attempt.AccountRef, authJSON)
		clear(authJSON)
		if materializeErr == nil {
			terminal.State = "AUTHORIZED"
			terminal.ExternalAccountMasked = masked
			terminal.Credential = &descriptor
		} else {
			terminal.State = "FAILED"
			terminal.SafeFailureCode = "CREDENTIAL_MATERIALIZATION_FAILED"
		}
		_, _ = service.store.CompleteProviderAuthorizationAttempt(finish, terminal)
		finishCancel()
		return
	}
	clear(authJSON)
	if errors.Is(err, context.DeadlineExceeded) {
		terminal.State = "EXPIRED"
		terminal.SafeFailureCode = "DEVICE_AUTHORIZATION_EXPIRED"
	} else {
		terminal.State = "FAILED"
		terminal.SafeFailureCode = "DEVICE_AUTHORIZATION_FAILED"
	}
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(service.lifecycle), deviceAuthFinishBudget)
	_, _ = service.store.CompleteProviderAuthorizationAttempt(finish, terminal)
	finishCancel()
}

func deviceAuthorizationReferences(attemptRef, accountRef string) (string, string, error) {
	if !validReference(attemptRef) || !validReference(accountRef) {
		return "", "", ErrInvalidInput
	}
	digest := sha256.Sum256([]byte(attemptRef + "\x00" + accountRef))
	suffix := hex.EncodeToString(digest[:16])
	return "pmat_" + suffix, "auth-" + suffix, nil
}

func validReference(value string) bool {
	if len(value) < 8 || len(value) > 96 {
		return false
	}
	for index, character := range value {
		if index == 0 && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
		if index > 0 && (character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-') {
			continue
		}
		if index > 0 {
			return false
		}
	}
	return true
}

func castDeviceAuthorization(value kubernetesstore.ProviderAuthorizationAttempt) DeviceAuthorization {
	return DeviceAuthorization{
		MaterializerAttemptRef: value.MaterializerAttemptRef, VerificationURI: value.VerificationURI,
		UserCode: value.UserCode, ExpiresAt: value.ExpiresAt,
		MaterializerAttemptUID: value.SecretUID, MaterializerAttemptVersion: value.ResourceVersion,
	}
}

func DefaultDeviceAuthorizationTTL() time.Duration { return defaultDeviceAuthTTL }
