package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
)

// testProviderVerificationFreshObservation checks explicit fresh provider verification despite
// cached catalog state, exact replay and stale completion after disabling the account.
// testProviderVerificationFreshObservation проверяет явную свежую верификацию провайдера вопреки
// кэшу каталога, точный повтор и устаревшее завершение после отключения записи.
func testProviderVerificationFreshObservation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.authorization.verify",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-create"}, Payload: command.ProviderAccountInput{DefinitionKey: repository.modelProvider.DefinitionKey(), Name: "Verification fixture"}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create verification account: %v", err)
	}
	account := created.ProviderAccount
	payload := command.ProviderAccountInput{AccountRef: account.Ref, AuthorizationRef: "pauth_verification_fixture", AuthorizationMethod: "API_KEY",
		AuthorizationState: "AUTHORIZED", ExternalAccountMasked: "Verification fixture",
		Credential: &entity.ProviderCredentialDescriptor{SecretName: "runtime-provider-verification", SecretUID: "63000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("6", 64)}}
	authorized, err := service.Execute(ctx, command.Command{Kind: command.AuthorizeProviderAPIKey, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-authorize", ExpectedVersion: &account.Version}, Payload: payload})
	if err != nil || authorized.ProviderAccount == nil {
		t.Fatalf("authorize verification credential: %v", err)
	}
	account = authorized.ProviderAccount
	seedObservedCatalogFixture(t, ctx, repository)
	verify := command.Command{Kind: command.VerifyProviderAuthorization, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-exact", ExpectedVersion: &account.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref}}
	configured := repository.modelProvider
	repository.modelProvider = nil
	_, unavailableErr := service.Execute(ctx, verify)
	repository.modelProvider = configured
	if !errors.Is(unavailableErr, errs.ErrConflict) {
		t.Fatal("unconfigured route accepted verification", unavailableErr)
	}
	requested, err := service.Execute(ctx, verify)
	if err != nil || requested.ProviderAccount == nil || requested.ProviderAccount.Verification == nil || requested.ProviderAccount.Verification.State != "PENDING" || requested.ProviderAccount.Version != account.Version {
		t.Fatalf("request fresh verification: result=%#v err=%v", requested.ProviderAccount, err)
	}
	verificationRef := requested.ProviderAccount.Verification.Ref
	replayed, err := service.Execute(ctx, verify)
	if err != nil || replayed.ProviderAccount == nil || replayed.ProviderAccount.Verification.Ref != verificationRef {
		t.Fatalf("verification replay: %v", err)
	}
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "verification-fixture", 16, &providercredentialclient.Client{})
	if err != nil {
		t.Fatalf("claim fresh verification: %v", err)
	}
	var task platformrepo.ProviderModelCatalogTask
	for _, candidate := range tasks {
		if candidate.AccountRef == account.Ref {
			task = candidate
		}
	}
	if task.Ref == "" {
		t.Fatal("cached catalog suppressed explicit verification")
	}
	observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: "REMOTE_API", Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{{ID: "gpt-5", DefaultReasoningEffort: "high", ReasoningEfforts: []string{"low", "medium", "high"}}}}
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
		t.Fatalf("complete fresh verification: %v", err)
	}
	var realtimeEvents int
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM control_plane.outbox_events event
WHERE convert_from(event.payload, 'UTF8')::jsonb->>'eventName' = 'PROVIDER_ACCOUNT_CHANGED'
  AND convert_from(event.payload, 'UTF8')::jsonb->>'aggregateRef' = $1
  AND convert_from(event.payload, 'UTF8')::jsonb->>'correlationRef' = $2`, account.Ref, task.Ref).Scan(&realtimeEvents); err != nil || realtimeEvents != 1 {
		t.Fatalf("provider verification realtime event: count=%d err=%v", realtimeEvents, err)
	}
	replayed, err = service.Execute(ctx, verify)
	if err != nil || replayed.ProviderAccount == nil || replayed.ProviderAccount.Verification.State != "VERIFIED" || replayed.ProviderAccount.Verification.CompletedAt == nil || replayed.ProviderAccount.Verification.Ref != verificationRef {
		t.Fatalf("fresh verification receipt: account=%#v err=%v", replayed.ProviderAccount, err)
	}
	second := verify
	second.Mutation.IdempotencyKey = "verification-second"
	if _, err := service.Execute(ctx, second); err != nil {
		t.Fatalf("request next verification: %v", err)
	}
	tasks, err = repository.ClaimProviderModelCatalogTasks(ctx, "verification-stale", 16, &providercredentialclient.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range tasks {
		if candidate.AccountRef == account.Ref {
			task = candidate
		}
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.SetProviderAccountEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "verification-disable", ExpectedVersion: &account.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref, Enabled: false}}); err != nil {
		t.Fatalf("disable verification account: %v", err)
	}
	observation.ObservedAt = time.Now()
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale verification completion: %v", err)
	}
	stale, err := service.GetProviderAccount(ctx, owner, account.Ref)
	if err != nil || stale.Verification == nil || stale.Verification.State != "STALE" || stale.Verification.CompletedAt == nil {
		t.Fatalf("stale verification readback: %#v %v", stale.Verification, err)
	}
}
