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

func testProviderVerificationFreshObservation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.device-verify",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-create"}, Payload: command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: "Verification fixture"}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create verification account: %v", err)
	}
	account := created.ProviderAccount
	expires := time.Now().Add(5 * time.Minute)
	payload := command.ProviderAccountInput{AccountRef: account.Ref, AuthorizationRef: "pauth_verification_fixture", AuthorizationMethod: "DEVICE_CODE",
		AuthorizationState: "PENDING", MaterializerAttemptRef: "pmat_verification_fixture", VerificationURI: "https://provider.invalid/device", UserCode: "TEST", AuthorizationExpiresAt: &expires}
	pending, err := service.Execute(ctx, command.Command{Kind: command.StartProviderDeviceAuth, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-start", ExpectedVersion: &account.Version}, Payload: payload})
	if err != nil || pending.ProviderAccount == nil {
		t.Fatalf("start verification credential: %v", err)
	}
	payload.AuthorizationState = "AUTHORIZED"
	payload.ExternalAccountMasked = "Verification fixture"
	payload.Credential = &entity.ProviderCredentialDescriptor{SecretName: "runtime-provider-verification", SecretUID: "63000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("6", 64)}
	authorized, err := service.Execute(ctx, command.Command{Kind: command.RefreshProviderAuthorization, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-authorize", ExpectedVersion: &pending.ProviderAccount.Version}, Payload: payload})
	if err != nil || authorized.ProviderAccount == nil {
		t.Fatalf("authorize verification credential: %v", err)
	}
	account = authorized.ProviderAccount
	seedObservedCatalogFixture(t, ctx, repository)
	verify := command.Command{Kind: command.VerifyProviderAuthorization, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "verification-exact", ExpectedVersion: &account.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref}}
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
	observation := platformrepo.ProviderModelCatalogObservation{AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: "REMOTE_CODEX", Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{{ID: "gpt-5", DefaultReasoningEffort: "high", ReasoningEfforts: []string{"low", "medium", "high"}}}}
	if err := repository.CompleteProviderModelCatalogTask(ctx, task, observation); err != nil {
		t.Fatalf("complete fresh verification: %v", err)
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
