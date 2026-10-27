package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryProviderAuthorizationExpireFixture embeds fixture-only expiry of a still-RESERVED
// authorization attempt by exact ref. It makes cleanup maintenance due without waiting for real
// wall-clock expiry or changing an external credential.
// queryProviderAuthorizationExpireFixture встраивает fixture-only expiry ещё RESERVED
// authorization attempt по exact ref. Делает cleanup maintenance due без ожидания настоящего
// wall-clock expiry и изменения внешнего credential.
//
//go:embed testdata/sql/provider_authorization_expire_fixture.sql
var queryProviderAuthorizationExpireFixture string

// reservedProviderFixture records device attempts, API materialization calls and a synthetic
// generation counter for durable reservation component tests. All provider/Secret descriptors are
// fabricated; it is serial fixture state without external side effects or concurrency protection.
// reservedProviderFixture фиксирует device attempts, API materialization calls и synthetic
// generation counter для durable reservation component tests. Все provider/Secret descriptors
// вымышлены; это serial fixture state без внешних effects и concurrency protection.
type reservedProviderFixture struct {
	attempts []string
	apiCalls int
	current  int
}

// Check always returns nil to satisfy the fixture readiness seam. It performs no provider, Secret
// store or network readiness check.
// Check всегда возвращает nil для fixture readiness seam. Не проверяет готовность provider, Secret
// store и сети.
func (*reservedProviderFixture) Check(context.Context) error { return nil }

// StartDeviceAuthorization records the attempt, increments fixture generation and derives a
// synthetic materializer ref from attempt/account with a five-minute test deadline. It returns a
// reserved invalid-domain device URL and fixed test code without contacting an identity provider.
// StartDeviceAuthorization фиксирует attempt, увеличивает fixture generation и выводит synthetic
// materializer ref из attempt/account с пяти минутным test deadline. Возвращает device URL
// зарезервированного invalid domain и fixed test code без обращения к identity provider.
func (fixture *reservedProviderFixture) StartDeviceAuthorization(_ context.Context, attempt, account string) (platformservice.ProviderDeviceAuthorizationMaterialization, error) {
	fixture.attempts = append(fixture.attempts, attempt)
	fixture.current++
	digest := sha256.Sum256([]byte(attempt + "\x00" + account))
	return platformservice.ProviderDeviceAuthorizationMaterialization{MaterializerAttemptRef: "pmat_" + hex.EncodeToString(digest[:16]), MaterializerAttemptUID: fmt.Sprintf("64000000-0000-4000-8000-%012d", fixture.current), MaterializerAttemptVersion: "1", VerificationURI: "https://provider.invalid/device", UserCode: "TEST", ExpiresAt: time.Now().Add(5 * time.Minute)}, nil
}

// descriptor constructs deterministic synthetic credential metadata from the current fixture
// generation, using no plaintext credential. It neither writes a Kubernetes Secret nor proves any
// descriptor exists outside the test.
// descriptor строит deterministic synthetic credential metadata из текущего fixture generation без
// plaintext credential. Не записывает Kubernetes Secret и не доказывает существование descriptor
// вне теста.
func (fixture *reservedProviderFixture) descriptor() entity.ProviderCredentialDescriptor {
	return entity.ProviderCredentialDescriptor{SecretName: fmt.Sprintf("runtime-provider-reserved-%d", fixture.current), SecretUID: fmt.Sprintf("65000000-0000-4000-8000-%012d", fixture.current), SecretResourceVersion: "1", ContentSHA256: strings.Repeat("7", 64)}
}

// ObserveDeviceAuthorization returns synthetic AUTHORIZED state and the current fabricated
// descriptor regardless of supplied attempt. This is the positive fake materializer response used
// to test owner activation, not a real provider observation.
// ObserveDeviceAuthorization возвращает synthetic AUTHORIZED и текущий вымышленный descriptor
// независимо от переданной attempt. Это положительный fake materializer response для проверки
// owner activation, а не настоящее provider observation.
func (fixture *reservedProviderFixture) ObserveDeviceAuthorization(context.Context, string) (platformservice.ProviderAuthorizationObservation, error) {
	descriptor := fixture.descriptor()
	return platformservice.ProviderAuthorizationObservation{State: "AUTHORIZED", ExternalAccountMasked: "Synthetic reserved account", Credential: &descriptor}, nil
}

// MaterializeAPIKey increments API-call and generation counters and returns fabricated
// descriptor/masked account metadata, deliberately ignoring the supplied key bytes. The counter
// detects duplicate materialization; no API key is stored or sent.
// MaterializeAPIKey увеличивает API-call и generation counters и возвращает вымышленные
// descriptor/masked account metadata, намеренно игнорируя key bytes. Counter обнаруживает
// duplicate materialization; API key не сохраняется и не отправляется.
func (fixture *reservedProviderFixture) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	fixture.apiCalls++
	fixture.current++
	return fixture.descriptor(), "Synthetic reserved API account", nil
}

// Discard returns nil without touching any external materialization. It satisfies the fake cleanup
// seam and cannot prove physical Secret deletion or provider revocation.
// Discard возвращает nil без изменения внешней materialization. Реализует fake cleanup seam и не
// доказывает physical Secret deletion и provider revocation.
func (*reservedProviderFixture) Discard(context.Context, platformservice.ProviderMaterializationDiscard) error {
	return nil
}

// testProviderAuthorizationReservation uses real owner reservation/idempotency/OCC state with the
// fake materializer to ensure device/API replay does not repeat external-call counters and changed
// inputs fail. It covers replacement activation, crash-before-effect reservation, deletion/expiry
// preventing revival and durable metadata-cleanup task discovery. It does not execute that cleanup
// or verify real provider/Secret effects.
// testProviderAuthorizationReservation использует real owner reservation/idempotency/OCC state с
// fake materializer, проверяя отсутствие повторных external-call counters для device/API replay и
// отказ changed inputs. Охватывает replacement activation, crash-before-effect reservation, запрет
// revival после deletion/expiry и наличие durable metadata-cleanup task. Не исполняет cleanup и не
// проверяет реальные provider/Secret effects.
func testProviderAuthorizationReservation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.device-reauthorize"}, "control-api-gateway")
	fixture := &reservedProviderFixture{}
	service, err := platformservice.New(repository, platformservice.WithProviderCredentialMaterializer(fixture))
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "reserved-provider-create"}, Payload: command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: "Reserved device account"}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create reserved account: %v", err)
	}
	account := *created.ProviderAccount
	original := value.Mutation{IdempotencyKey: "reserved-device-start", ExpectedVersion: &account.Version}
	pending, err := service.StartProviderAccountDeviceAuthorization(ctx, owner, original, account.Ref)
	if err != nil || pending.Authorization == nil || len(fixture.attempts) != 1 {
		t.Fatalf("reserved start: %v", err)
	}
	firstAttempt := pending.Authorization.Ref
	if _, err := service.StartProviderAccountDeviceAuthorization(ctx, owner, original, account.Ref); err != nil || len(fixture.attempts) != 1 {
		t.Fatalf("start replay repeated external effect: %v", err)
	}
	authorized, err := service.RefreshProviderAccountAuthorization(ctx, owner, value.Mutation{IdempotencyKey: "reserved-device-observe", ExpectedVersion: &pending.Version}, account.Ref)
	if err != nil || authorized.State != "AUTHORIZED" {
		t.Fatalf("activate reserved credential: %v", err)
	}
	reauthMutation := value.Mutation{IdempotencyKey: "reserved-device-reauth", ExpectedVersion: &authorized.Version}
	replacement, err := service.StartProviderAccountDeviceAuthorization(ctx, owner, reauthMutation, account.Ref)
	if err != nil || replacement.Authorization == nil || replacement.Authorization.Ref == firstAttempt || len(fixture.attempts) != 2 {
		t.Fatalf("new reauthorization attempt: %v", err)
	}
	if _, err := service.StartProviderAccountDeviceAuthorization(ctx, owner, reauthMutation, account.Ref); err != nil || len(fixture.attempts) != 2 {
		t.Fatalf("reauthorization replay repeated external effect: %v", err)
	}
	updated, err := service.RefreshProviderAccountAuthorization(ctx, owner, value.Mutation{IdempotencyKey: "reserved-device-new-observe", ExpectedVersion: &replacement.Version}, account.Ref)
	if err != nil || updated.State != "AUTHORIZED" || updated.Ref != account.Ref {
		t.Fatalf("atomic replacement activation: %v", err)
	}
	apiMutation := value.Mutation{IdempotencyKey: "reserved-api-key", ExpectedVersion: &updated.Version}
	apiAccount, err := service.AuthorizeProviderAccountAPIKey(ctx, owner, apiMutation, account.Ref, []byte("synthetic-reservation-key"))
	if err != nil || apiAccount.State != "AUTHORIZED" || fixture.apiCalls != 1 {
		t.Fatalf("API key durable reservation: %v", err)
	}
	if _, err := service.AuthorizeProviderAccountAPIKey(ctx, owner, apiMutation, account.Ref, []byte("synthetic-reservation-key")); err != nil || fixture.apiCalls != 1 {
		t.Fatalf("API key replay repeated materialization: %v", err)
	}
	if _, err := service.AuthorizeProviderAccountAPIKey(ctx, owner, apiMutation, account.Ref, []byte("different-synthetic-key")); !errors.Is(err, errs.ErrIdempotencyReuse) || fixture.apiCalls != 1 {
		t.Fatalf("API key changed input reused reservation: %v", err)
	}
	updated = apiAccount
	// Резервирование без внешнего эффекта тоже даёт достижимую очистку после Delete.
	mutation := value.Mutation{IdempotencyKey: "reserved-crash-before-effect", ExpectedVersion: &updated.Version}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.ReserveProviderAuthorization(ctx, resolved, mutation, account.Ref, "DEVICE_CODE", strings.Repeat("8", 64))
	if err != nil || reservation.Applied {
		t.Fatalf("reserve crash boundary: %v", err)
	}
	repeated, err := repository.ReserveProviderAuthorization(ctx, resolved, mutation, account.Ref, "DEVICE_CODE", strings.Repeat("8", 64))
	if err != nil || repeated.AttemptRef != reservation.AttemptRef {
		t.Fatalf("reservation identity replay: %v", err)
	}
	if _, err := repository.ReserveProviderAuthorization(ctx, resolved, mutation, account.Ref, "DEVICE_CODE", strings.Repeat("9", 64)); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatalf("changed reservation intent: %v", err)
	}
	deleted, err := service.Execute(ctx, command.Command{Kind: command.DeleteProviderAccount, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "reserved-delete", ExpectedVersion: &reservation.ReservedVersion}, Payload: command.ProviderAccountInput{AccountRef: account.Ref}})
	if err != nil || deleted.ProviderAccount == nil || deleted.ProviderAccount.State != "DELETING" {
		t.Fatalf("delete reserved attempt: %v", err)
	}
	if _, err := repository.ReserveProviderAuthorization(ctx, resolved, mutation, account.Ref, "DEVICE_CODE", strings.Repeat("8", 64)); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("deleted reservation was revived: %v", err)
	}
	expiring, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "reserved-expiry-account"}, Payload: command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: "Expired reservation"}})
	if err != nil || expiring.ProviderAccount == nil {
		t.Fatalf("expiry account: %v", err)
	}
	expiryMutation := value.Mutation{IdempotencyKey: "reserved-expiry", ExpectedVersion: &expiring.ProviderAccount.Version}
	expiry, err := repository.ReserveProviderAuthorization(ctx, resolved, expiryMutation, expiring.ProviderAccount.Ref, "API_KEY", strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, queryProviderAuthorizationExpireFixture, expiry.AttemptRef); err != nil {
		t.Fatal(err)
	}
	tasks, err := repository.ClaimProviderCredentialCleanupTasks(ctx, "reserved-cleanup", 16)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range tasks {
		if task.TargetKind == "AUTHORIZATION_METADATA" && task.AccountRef == account.Ref && task.Authorization.AuthorizationAttemptRef == reservation.AttemptRef {
			found = true
		}
	}
	if !found {
		t.Fatal("pre-effect reserved attempt has no durable metadata cleanup task")
	}
	expired, err := service.GetProviderAccount(ctx, owner, expiring.ProviderAccount.Ref)
	if err != nil || expired.State != "REAUTHORIZATION_REQUIRED" {
		t.Fatalf("expired reservation retained pending account: %v", err)
	}
	if _, err := repository.ReserveProviderAuthorization(ctx, resolved, expiryMutation, expired.Ref, "API_KEY", strings.Repeat("c", 64)); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expired reservation replay revived effect: %v", err)
	}
	expiryCleanup := false
	for _, task := range tasks {
		if task.AccountRef == expired.Ref && task.TargetKind == "AUTHORIZATION_METADATA" && task.Authorization.AuthorizationAttemptRef == expiry.AttemptRef {
			expiryCleanup = true
		}
	}
	if !expiryCleanup {
		t.Fatal("expired reservation lost exact cleanup lineage")
	}
}
