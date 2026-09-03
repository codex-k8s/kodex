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

var errProviderOwnerCommit = errors.New("synthetic provider owner commit failure")

type providerFailureRepository struct {
	platformrepo.Repository
	account         entity.ProviderAccount
	referenced      bool
	referenceErr    error
	referenceChecks int
	resolveCalls    int
	executed        command.Command
}

func (repository *providerFailureRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	repository.resolveCalls++
	return principal, nil
}

func (repository *providerFailureRepository) GetProviderAccount(_ context.Context, _ value.Principal, _ string) (entity.ProviderAccount, error) {
	return repository.account, nil
}

func (repository *providerFailureRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.executed = input
	return command.Result{}, errProviderOwnerCommit
}

func (repository *providerFailureRepository) ProviderMaterializationReferenced(
	_ context.Context,
	_ value.Principal,
	_, _, _ string,
	_ *entity.ProviderCredentialDescriptor,
) (bool, error) {
	repository.referenceChecks++
	return repository.referenced, repository.referenceErr
}

type providerMaterializerRecorder struct {
	device      ProviderDeviceAuthorizationMaterialization
	observation ProviderAuthorizationObservation
	apiKey      entity.ProviderCredentialDescriptor
	discards    []ProviderMaterializationDiscard
	apiCalls    int
}

func (materializer *providerMaterializerRecorder) Check(context.Context) error { return nil }

func (materializer *providerMaterializerRecorder) StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error) {
	return materializer.device, nil
}

func (materializer *providerMaterializerRecorder) ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error) {
	return materializer.observation, nil
}

func (materializer *providerMaterializerRecorder) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	materializer.apiCalls++
	return materializer.apiKey, "API key account", nil
}

func (materializer *providerMaterializerRecorder) Discard(_ context.Context, target ProviderMaterializationDiscard) error {
	materializer.discards = append(materializer.discards, target)
	return nil
}

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

func TestProviderRefreshKeepsObservedCredentialAfterAmbiguousOwnerCommitFailure(t *testing.T) {
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
	if len(materializer.discards) != 0 || repository.referenceChecks != 0 || repository.resolveCalls != 1 {
		t.Fatalf("ambiguous refresh discarded observed materialization or repeated principal resolution: %#v/%d",
			materializer.discards, repository.resolveCalls)
	}
	payload, ok := repository.executed.Payload.(command.ProviderAccountInput)
	if !ok || payload.Credential == nil || *payload.Credential != descriptor {
		t.Fatalf("refresh command lost exact credential descriptor: %#v", repository.executed.Payload)
	}
}

func providerTestPrincipal() value.Principal {
	return value.Principal{
		ActorID: "owner-test", AuthorityTenant: "organization-test", Permission: "platform.provider-accounts.authorize",
		CorrelationRef: "correlation-test", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
}
