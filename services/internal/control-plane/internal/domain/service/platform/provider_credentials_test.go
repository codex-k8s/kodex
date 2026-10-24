package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// errProviderOwnerCommit marks a synthetic ambiguous owner commit failure used to test
// readback-driven compensation without an external provider.
// errProviderOwnerCommit обозначает синтетическую неопределённую ошибку commit владельца для
// проверки компенсации по readback без внешнего провайдера.
var errProviderOwnerCommit = errors.New("synthetic provider owner commit failure")

// providerFailureRepository records resolution, command and reference-read activity with
// configurable reservation/replay and ambiguous commit outcomes for provider tests.
// providerFailureRepository записывает разрешение principal, команды и чтение ссылок с задаваемыми
// reservation, replay и неопределённым commit для тестов провайдера.
type providerFailureRepository struct {
	platformrepo.Repository
	account            entity.ProviderAccount
	referenced         bool
	referenceErr       error
	referenceChecks    int
	resolveCalls       int
	executed           command.Command
	reservationErr     error
	reservationApplied bool
}

// ReserveProviderAuthorization returns a synthetic stable attempt and advanced reserved version or
// an injected denial, requiring an expected version and digest-shaped request.
// ReserveProviderAuthorization возвращает синтетические устойчивую попытку и увеличенную reserved
// version либо заданный отказ с требованием ожидаемой версии и формы дайджеста запроса.
func (repository *providerFailureRepository) ReserveProviderAuthorization(_ context.Context, _ value.Principal, mutation value.Mutation, accountRef, method, digest string) (entity.ProviderAuthorizationReservation, error) {
	if repository.reservationErr != nil {
		return entity.ProviderAuthorizationReservation{}, repository.reservationErr
	}
	if mutation.ExpectedVersion == nil || len(digest) != 64 {
		return entity.ProviderAuthorizationReservation{}, errs.ErrInvalid
	}
	return entity.ProviderAuthorizationReservation{AttemptRef: providerAuthorizationRef(accountRef, mutation.IdempotencyKey, method), ReservedVersion: *mutation.ExpectedVersion + 1, Applied: repository.reservationApplied}, nil
}

// TestProviderMaterializationRequiresDurableReservationAndSkipsAppliedReplay checks a denied owner
// reservation performs no API-key materialization and an applied replay reads the account without
// repeating that effect.
// TestProviderMaterializationRequiresDurableReservationAndSkipsAppliedReplay проверяет отсутствие
// материализации API key при отказе reservation владельца и чтение аккаунта применённым replay без
// повторного эффекта.
func TestProviderMaterializationRequiresDurableReservationAndSkipsAppliedReplay(t *testing.T) {
	t.Parallel()
	version := int64(4)
	repository := &providerFailureRepository{account: entity.ProviderAccount{Ref: "pacc_test123", Version: version}, reservationErr: errs.ErrForbidden}
	materializer := &providerMaterializerRecorder{}
	service, err := New(repository, WithProviderCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	mutation := value.Mutation{IdempotencyKey: "reservation-boundary", ExpectedVersion: &version}
	if _, err := service.AuthorizeProviderAccountAPIKey(context.Background(), providerTestPrincipal(), mutation, repository.account.Ref, []byte("synthetic-api-key")); !errors.Is(err, errs.ErrForbidden) || materializer.apiCalls != 0 {
		t.Fatalf("denied reservation performed materialization: %v calls=%d", err, materializer.apiCalls)
	}
	repository.reservationErr = nil
	repository.reservationApplied = true
	if _, err := service.AuthorizeProviderAccountAPIKey(context.Background(), providerTestPrincipal(), mutation, repository.account.Ref, []byte("synthetic-api-key")); err != nil || materializer.apiCalls != 0 {
		t.Fatalf("applied reservation repeated materialization: %v calls=%d", err, materializer.apiCalls)
	}
}

// ResolvePrincipal counts synthetic principal resolution and returns the supplied principal
// unchanged for owner-compensation assertions.
// ResolvePrincipal подсчитывает синтетическое разрешение principal и возвращает его без изменений
// для проверок компенсации владельца.
func (repository *providerFailureRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	repository.resolveCalls++
	return principal, nil
}

// GetProviderAccount returns the configured synthetic account without querying provider or
// database state.
// GetProviderAccount возвращает настроенный синтетический аккаунт без запроса состояния провайдера
// либо базы данных.
func (repository *providerFailureRepository) GetProviderAccount(_ context.Context, _ value.Principal, _ string) (entity.ProviderAccount, error) {
	return repository.account, nil
}

// Execute records the resolved provider command and always returns the synthetic owner commit
// failure for compensation-path tests.
// Execute записывает разрешённую команду провайдера и всегда возвращает синтетическую ошибку
// commit владельца для тестов пути компенсации.
func (repository *providerFailureRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.executed = input
	return command.Result{}, errProviderOwnerCommit
}

// ProviderMaterializationReferenced counts authoritative-reference fixture reads and returns
// configured referenced/unknown outcomes without accessing real persisted state.
// ProviderMaterializationReferenced подсчитывает тестовые чтения авторитетной ссылки и возвращает
// заданные результаты referenced либо unknown без обращения к настоящему состоянию.
func (repository *providerFailureRepository) ProviderMaterializationReferenced(
	_ context.Context,
	_ value.Principal,
	_, _, _ string,
	_ *entity.ProviderCredentialDescriptor,
) (bool, error) {
	repository.referenceChecks++
	return repository.referenced, repository.referenceErr
}

// providerMaterializerRecorder supplies synthetic device/credential observations and records
// API-key materialization and exact discard targets without calling a provider.
// providerMaterializerRecorder выдаёт синтетические наблюдения устройства и реквизитов и
// записывает материализацию API key и точные цели discard без вызова провайдера.
type providerMaterializerRecorder struct {
	device      ProviderDeviceAuthorizationMaterialization
	observation ProviderAuthorizationObservation
	apiKey      entity.ProviderCredentialDescriptor
	discards    []ProviderMaterializationDiscard
	apiCalls    int
}

// Check reports the local materializer fixture as ready without probing any external dependency.
// Check сообщает готовность локальной оснастки materializer без проверки внешних зависимостей.
func (materializer *providerMaterializerRecorder) Check(context.Context) error { return nil }

// StartDeviceAuthorization returns the configured synthetic device authorization materialization
// without starting an external device flow.
// StartDeviceAuthorization возвращает заданную синтетическую материализацию авторизации устройства
// без запуска внешнего device flow.
func (materializer *providerMaterializerRecorder) StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error) {
	return materializer.device, nil
}

// ObserveDeviceAuthorization returns the configured synthetic observation without polling a real
// authorization provider.
// ObserveDeviceAuthorization возвращает заданное синтетическое наблюдение без опроса настоящего
// провайдера авторизации.
func (materializer *providerMaterializerRecorder) ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error) {
	return materializer.observation, nil
}

// MaterializeAPIKey counts materialization attempts and returns the fixture descriptor and fixed
// masked label without storing supplied key bytes.
// MaterializeAPIKey подсчитывает попытки материализации и возвращает тестовый дескриптор и
// фиксированную скрытую метку без сохранения переданных байтов ключа.
func (materializer *providerMaterializerRecorder) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	materializer.apiCalls++
	return materializer.apiKey, "API key account", nil
}

// Discard records the exact compensation target and succeeds without deleting actual secret or
// attempt resources.
// Discard записывает точную цель компенсации и завершается успешно без удаления настоящих ресурсов
// секрета либо попытки.
func (materializer *providerMaterializerRecorder) Discard(_ context.Context, target ProviderMaterializationDiscard) error {
	materializer.discards = append(materializer.discards, target)
	return nil
}

// TestProviderAPIKeyKeepsExactMaterializationAfterAmbiguousOwnerCommitFailure checks two exact
// synthetic retries preserve owner-referenced materialization after commit errors, performing
// fresh reference and principal checks without discard.
// TestProviderAPIKeyKeepsExactMaterializationAfterAmbiguousOwnerCommitFailure проверяет сохранение
// материала со ссылкой владельца после ошибок commit при двух точных синтетических повторах с
// новым чтением ссылки и principal без discard.
func TestProviderAPIKeyKeepsExactMaterializationAfterAmbiguousOwnerCommitFailure(t *testing.T) {
	t.Parallel()
	version := int64(4)
	repository := &providerFailureRepository{account: entity.ProviderAccount{
		Ref: "pacc_test123", Version: version, NextActions: []string{"CONFIGURE_CREDENTIAL"},
	}, referenced: true}
	descriptor := entity.ProviderCredentialDescriptor{
		SecretName: "provider-credential-test", SecretUID: "uid-provider-credential-test",
		SecretResourceVersion: "101", ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	materializer := &providerMaterializerRecorder{apiKey: descriptor}
	service, err := New(repository, WithProviderCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	mutation := value.Mutation{IdempotencyKey: "provider-api-key-retry", ExpectedVersion: &version}
	for attempt := 0; attempt < 2; attempt++ {
		_, err = service.AuthorizeProviderAccountAPIKey(context.Background(), providerTestPrincipal(), mutation,
			repository.account.Ref, []byte("sk-synthetic-provider-key"))
		if !errors.Is(err, errProviderOwnerCommit) {
			t.Fatalf("attempt %d error = %v", attempt+1, err)
		}
	}
	if materializer.apiCalls != 2 || len(materializer.discards) != 0 || repository.referenceChecks != 2 || repository.resolveCalls != 2 {
		t.Fatalf("materialize/discard/reference/resolve calls = %d/%d/%d/%d",
			materializer.apiCalls, len(materializer.discards), repository.referenceChecks, repository.resolveCalls)
	}
}

// TestProviderAPIKeyCompensatesOnlyAfterAuthoritativeNonReference checks confirmed nonreference
// discards the exact descriptor once and raw synthetic key material stays out of persisted command
// fields and error text.
// TestProviderAPIKeyCompensatesOnlyAfterAuthoritativeNonReference проверяет один discard точного
// дескриптора после подтверждённого отсутствия ссылки и отсутствие исходного синтетического ключа
// в сохраняемых полях команды и ошибке.
func TestProviderAPIKeyCompensatesOnlyAfterAuthoritativeNonReference(t *testing.T) {
	t.Parallel()
	version := int64(4)
	repository := &providerFailureRepository{account: entity.ProviderAccount{
		Ref: "pacc_test123", Version: version, NextActions: []string{"CONFIGURE_CREDENTIAL"},
	}}
	descriptor := entity.ProviderCredentialDescriptor{
		SecretName: "provider-credential-test", SecretUID: "uid-provider-credential-test",
		SecretResourceVersion: "101", ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	materializer := &providerMaterializerRecorder{apiKey: descriptor}
	service, err := New(repository, WithProviderCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	mutation := value.Mutation{IdempotencyKey: "provider-api-key-retry", ExpectedVersion: &version}
	const rawAPIKey = "sk-synthetic-provider-key"
	_, err = service.AuthorizeProviderAccountAPIKey(context.Background(), providerTestPrincipal(), mutation,
		repository.account.Ref, []byte(rawAPIKey))
	if !errors.Is(err, errProviderOwnerCommit) || len(materializer.discards) != 1 || repository.referenceChecks != 1 || repository.resolveCalls != 1 {
		t.Fatalf("error/discards/reference/resolve checks = %v/%#v/%d/%d",
			err, materializer.discards, repository.referenceChecks, repository.resolveCalls)
	}
	payload, ok := repository.executed.Payload.(command.ProviderAccountInput)
	if !ok || payload.Credential == nil || strings.Contains(err.Error(), rawAPIKey) ||
		strings.Contains(payload.ExternalAccountMasked, rawAPIKey) || strings.Contains(payload.SafeFailureCode, rawAPIKey) {
		t.Fatalf("raw API key crossed the control-plane persistence boundary")
	}
	discard := materializer.discards[0]
	if discard.Credential == nil || *discard.Credential != descriptor ||
		discard.AccountRef != repository.account.Ref || discard.AttemptRef == "" || discard.MaterializerAttemptRef != "" {
		t.Fatalf("unexpected exact compensation: %#v", discard)
	}
}

// TestProviderDeviceAuthorizationCompensatesExactAttemptAfterAuthoritativeNonReference checks
// device-flow commit failure compensates only the exact attempt ref, UID and resource version
// after owner nonreference.
// TestProviderDeviceAuthorizationCompensatesExactAttemptAfterAuthoritativeNonReference проверяет
// компенсацию только точных ref, UID и resource version попытки device flow после ошибки commit и
// отсутствия ссылки владельца.
func TestProviderDeviceAuthorizationCompensatesExactAttemptAfterAuthoritativeNonReference(t *testing.T) {
	t.Parallel()
	version := int64(2)
	repository := &providerFailureRepository{account: entity.ProviderAccount{
		Ref: "pacc_device123", Version: version, NextActions: []string{"CONFIGURE_CREDENTIAL"},
	}}
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	materializer := &providerMaterializerRecorder{device: ProviderDeviceAuthorizationMaterialization{
		MaterializerAttemptRef: "pmat_device123", MaterializerAttemptUID: "uid-provider-attempt",
		MaterializerAttemptVersion: "201", VerificationURI: "https://example.invalid/device",
		UserCode: "ABCD-EFGH", ExpiresAt: expiresAt,
	}}
	service, _ := New(repository, WithProviderCredentialMaterializer(materializer))
	mutation := value.Mutation{IdempotencyKey: "provider-device-retry", ExpectedVersion: &version}
	_, err := service.StartProviderAccountDeviceAuthorization(context.Background(), providerTestPrincipal(), mutation, repository.account.Ref)
	if !errors.Is(err, errProviderOwnerCommit) || len(materializer.discards) != 1 || repository.resolveCalls != 1 {
		t.Fatalf("error/discards/resolve calls = %v/%#v/%d", err, materializer.discards, repository.resolveCalls)
	}
	discard := materializer.discards[0]
	if discard.AttemptRef == "" || discard.AccountRef != repository.account.Ref ||
		discard.MaterializerAttemptRef != materializer.device.MaterializerAttemptRef ||
		discard.MaterializerAttemptUID != materializer.device.MaterializerAttemptUID ||
		discard.MaterializerAttemptVersion != materializer.device.MaterializerAttemptVersion || discard.Credential != nil {
		t.Fatalf("unexpected exact attempt compensation: %#v", discard)
	}
}

// TestProviderCompensationKeepsMaterializationWhenReferenceReadIsUnavailable checks failed
// reference readback preserves material, joins owner/unavailable errors and does not repeat
// principal resolution.
// TestProviderCompensationKeepsMaterializationWhenReferenceReadIsUnavailable проверяет сохранение
// материала при ошибке чтения ссылки, объединение ошибок владельца и unavailable и отсутствие
// повторного разрешения principal.
func TestProviderCompensationKeepsMaterializationWhenReferenceReadIsUnavailable(t *testing.T) {
	t.Parallel()
	version := int64(2)
	repository := &providerFailureRepository{
		account: entity.ProviderAccount{
			Ref: "pacc_device123", Version: version, NextActions: []string{"CONFIGURE_CREDENTIAL"},
		},
		referenceErr: errors.New("synthetic reference read failure"),
	}
	materializer := &providerMaterializerRecorder{device: ProviderDeviceAuthorizationMaterialization{
		MaterializerAttemptRef: "pmat_device123", MaterializerAttemptUID: "uid-provider-attempt",
		MaterializerAttemptVersion: "201", VerificationURI: "https://example.invalid/device",
		UserCode: "ABCD-EFGH", ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}}
	service, _ := New(repository, WithProviderCredentialMaterializer(materializer))
	mutation := value.Mutation{IdempotencyKey: "provider-device-retry", ExpectedVersion: &version}
	_, err := service.StartProviderAccountDeviceAuthorization(context.Background(), providerTestPrincipal(), mutation, repository.account.Ref)
	if !errors.Is(err, errProviderOwnerCommit) || !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("unexpected ambiguous commit/readback error: %v", err)
	}
	if len(materializer.discards) != 0 || repository.referenceChecks != 1 || repository.resolveCalls != 1 {
		t.Fatalf("ambiguous readback discarded materialization or repeated principal resolution: %#v/%d",
			materializer.discards, repository.resolveCalls)
	}
}

// TestProviderRefreshCompensatesObservedCredentialAfterAuthoritativeNonReference checks a refresh
// observation survives in the owner command and exact credential compensation occurs once only
// after authoritative nonreference.
// TestProviderRefreshCompensatesObservedCredentialAfterAuthoritativeNonReference проверяет
// сохранение наблюдения refresh в команде владельца и однократную компенсацию точных реквизитов
// только после авторитетного отсутствия ссылки.
func TestProviderRefreshCompensatesObservedCredentialAfterAuthoritativeNonReference(t *testing.T) {
	t.Parallel()
	version := int64(3)
	descriptor := entity.ProviderCredentialDescriptor{
		SecretName: "provider-credential-device", SecretUID: "uid-provider-credential-device",
		SecretResourceVersion: "301", ContentSHA256: strings.Repeat("b", 64),
	}
	repository := &providerFailureRepository{account: entity.ProviderAccount{
		Ref: "pacc_device123", Version: version, State: "PENDING_AUTHORIZATION",
		Authorization: &entity.ProviderAuthorization{
			Ref: "pauth_device123", Method: "DEVICE_CODE", State: "PENDING",
			MaterializerAttemptRef: "pmat_device123",
		},
	}}
	materializer := &providerMaterializerRecorder{observation: ProviderAuthorizationObservation{
		State: "AUTHORIZED", ExternalAccountMasked: "Device account", Credential: &descriptor,
	}}
	service, _ := New(repository, WithProviderCredentialMaterializer(materializer))
	mutation := value.Mutation{IdempotencyKey: "provider-device-refresh", ExpectedVersion: &version}
	_, err := service.RefreshProviderAccountAuthorization(
		context.Background(), providerTestPrincipal(), mutation, repository.account.Ref,
	)
	if !errors.Is(err, errProviderOwnerCommit) {
		t.Fatalf("refresh error = %v", err)
	}
	if len(materializer.discards) != 1 || repository.referenceChecks != 1 || repository.resolveCalls != 1 {
		t.Fatalf("refresh compensation/reference/resolve = %#v/%d/%d",
			materializer.discards, repository.referenceChecks, repository.resolveCalls)
	}
	payload, ok := repository.executed.Payload.(command.ProviderAccountInput)
	if !ok || payload.Credential == nil || *payload.Credential != descriptor {
		t.Fatalf("refresh command lost exact credential descriptor: %#v", repository.executed.Payload)
	}
	discard := materializer.discards[0]
	if discard.AttemptRef != repository.account.Authorization.Ref || discard.AccountRef != repository.account.Ref ||
		discard.MaterializerAttemptRef != "" || discard.Credential == nil || *discard.Credential != descriptor {
		t.Fatalf("refresh compensation lost exact credential descriptor: %#v", discard)
	}
}

// TestProviderRefreshKeepsObservedCredentialAfterCommittedOwnerReference checks ambiguous refresh
// commit failure does not discard an observed credential already referenced by authoritative owner
// state.
// TestProviderRefreshKeepsObservedCredentialAfterCommittedOwnerReference проверяет отсутствие
// discard наблюдаемых реквизитов со ссылкой авторитетного состояния владельца при неопределённой
// ошибке commit refresh.
func TestProviderRefreshKeepsObservedCredentialAfterCommittedOwnerReference(t *testing.T) {
	t.Parallel()
	version := int64(3)
	descriptor := entity.ProviderCredentialDescriptor{
		SecretName: "provider-credential-device", SecretUID: "uid-provider-credential-device",
		SecretResourceVersion: "302", ContentSHA256: strings.Repeat("c", 64),
	}
	repository := &providerFailureRepository{referenced: true, account: entity.ProviderAccount{
		Ref: "pacc_device456", Version: version, State: "PENDING_AUTHORIZATION",
		Authorization: &entity.ProviderAuthorization{Ref: "pauth_device456", Method: "DEVICE_CODE", State: "PENDING", MaterializerAttemptRef: "pmat_device456"},
	}}
	materializer := &providerMaterializerRecorder{observation: ProviderAuthorizationObservation{State: "AUTHORIZED", Credential: &descriptor}}
	service, _ := New(repository, WithProviderCredentialMaterializer(materializer))
	_, err := service.RefreshProviderAccountAuthorization(context.Background(), providerTestPrincipal(),
		value.Mutation{IdempotencyKey: "provider-device-refresh-committed", ExpectedVersion: &version}, repository.account.Ref)
	if !errors.Is(err, errProviderOwnerCommit) || repository.referenceChecks != 1 || len(materializer.discards) != 0 {
		t.Fatalf("committed reference was discarded: err=%v checks=%d discards=%#v", err, repository.referenceChecks, materializer.discards)
	}
}

// providerTestPrincipal builds a synthetic provider-authorization principal with explicit actor,
// tenant, workload, correlation and credential revision.
// providerTestPrincipal создаёт синтетический principal авторизации провайдера с явными actor,
// tenant, workload, correlation и ревизией реквизитов.
func providerTestPrincipal() value.Principal {
	return value.Principal{
		ActorID: "owner-test", AuthorityTenant: "organization-test", Permission: "platform.provider-accounts.authorize",
		CorrelationRef: "correlation-test", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
}
