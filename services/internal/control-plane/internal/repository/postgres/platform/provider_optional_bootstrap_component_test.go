package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderOptionalBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute,
		MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap without provider attempt %d: %v", attempt, err)
		}
	}
	var accounts, credentials, sessions int
	if err := pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM control_plane.provider_accounts),
        (SELECT count(*) FROM control_plane.provider_credential_revisions),
        (SELECT count(*) FROM control_plane.sessions)`).Scan(&accounts, &credentials, &sessions); err != nil {
		t.Fatal("read account-free bootstrap")
	}
	if accounts != 0 || credentials != 0 || sessions != 0 {
		t.Fatalf("bootstrap created provider state: accounts=%d credentials=%d sessions=%d", accounts, credentials, sessions)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID:     "20000000-0000-4000-8000-000000000001",
		ExternalTenantID:    "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "First run owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil || assistant.RuntimeState != "UNAVAILABLE" {
		t.Fatalf("unconfigured assistant read: state=%q err=%v", assistant.RuntimeState, err)
	}
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil || len(configuration.Configuration.ProviderPolicy.AccountCandidates) != 0 {
		t.Fatalf("unconfigured assistant policy: configuration=%#v err=%v", configuration, err)
	}
	updated, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateAssistantInstructions, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-assistant-instructions", ExpectedVersion: &assistant.Version},
		Payload:  command.AssistantInstructionsInput{Instructions: "First-run instructions"},
	})
	if err != nil || updated.Assistant == nil || updated.Assistant.WarmSessionRef != "" {
		t.Fatalf("update assistant before account: result=%#v err=%v", updated.Assistant, err)
	}
	result, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-project"},
		Payload:  command.ProjectInput{Name: "First run", Purpose: "Account-free bootstrap", Language: "en"},
	})
	if err != nil || result.Project == nil {
		t.Fatalf("create project before account: %v", err)
	}
	reconcileWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.reconcile",
	}, "runtime-controller")
	if _, desired, required, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, "first-run-runtime"); !errors.Is(err, errs.ErrUnavailable) || desired != nil || required {
		t.Fatalf("execution before account was not rejected: desired=%#v required=%v err=%v", desired, required, err)
	}

	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-provider-create"},
		Payload:  command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: "First-run provider"},
	})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create first provider account: %v", err)
	}
	expires := time.Now().Add(5 * time.Minute)
	authorization := command.ProviderAccountInput{
		AccountRef: created.ProviderAccount.Ref, AuthorizationRef: "pauth_first_run",
		AuthorizationMethod: "DEVICE_CODE", AuthorizationState: "PENDING",
		MaterializerAttemptRef: "pmat_first_run", VerificationURI: "https://provider.invalid/device",
		UserCode: "TEST", AuthorizationExpiresAt: &expires,
	}
	pending, err := service.Execute(ctx, command.Command{
		Kind: command.StartProviderDeviceAuth, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-provider-start", ExpectedVersion: &created.ProviderAccount.Version},
		Payload:  authorization,
	})
	if err != nil || pending.ProviderAccount == nil {
		t.Fatalf("start first provider authorization: %v", err)
	}
	authorization.AuthorizationState = "AUTHORIZED"
	authorization.ExternalAccountMasked = "First-run provider"
	authorization.Credential = &entity.ProviderCredentialDescriptor{
		SecretName: "runtime-provider-first-run", SecretUID: "63000000-0000-4000-8000-000000000010",
		SecretResourceVersion: "1", ContentSHA256: strings.Repeat("6", 64),
	}
	authorized, err := service.Execute(ctx, command.Command{
		Kind: command.RefreshProviderAuthorization, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-provider-authorize", ExpectedVersion: &pending.ProviderAccount.Version},
		Payload:  authorization,
	})
	if err != nil || authorized.ProviderAccount == nil || authorized.ProviderAccount.State != "AUTHORIZED" {
		t.Fatalf("authorize first provider account: account=%#v err=%v", authorized.ProviderAccount, err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
	reconciled, desired, required, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, "first-run-runtime")
	if err != nil || !required || reconciled.WarmSessionRef == "" || desired["providerAccountRef"] != authorized.ProviderAccount.Ref {
		t.Fatalf("create first warm session: assistant=%#v desired=%#v required=%v err=%v", reconciled, desired, required, err)
	}
	firstSessionRef := reconciled.WarmSessionRef
	if _, _, _, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, "first-run-runtime"); err != nil {
		t.Fatalf("repeat first warm reconcile: %v", err)
	}
	var activeSessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.sessions WHERE state = 'ACTIVE'`).Scan(&activeSessions); err != nil {
		t.Fatal("count first warm sessions")
	}
	current, err := service.GetSystemAssistant(ctx, owner)
	if err != nil || current.WarmSessionRef != firstSessionRef || activeSessions != 1 {
		t.Fatalf("first warm session was duplicated: assistant=%#v active=%d err=%v", current, activeSessions, err)
	}
	disabled, err := service.Execute(ctx, command.Command{
		Kind: command.SetProviderAccountEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "first-run-provider-disable", ExpectedVersion: &authorized.ProviderAccount.Version},
		Payload:  command.ProviderAccountInput{AccountRef: authorized.ProviderAccount.Ref, Enabled: false},
	})
	if err != nil || disabled.ProviderAccount == nil {
		t.Fatalf("disable first provider account: %v", err)
	}
	if _, _, _, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, "first-run-runtime"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("disabled first provider remained executable: %v", err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("restart after first provider disable: %v", err)
	}
	current, err = service.GetSystemAssistant(ctx, owner)
	if err != nil || current.RuntimeState != "UNAVAILABLE" {
		t.Fatalf("disabled first provider runtime state: assistant=%#v err=%v", current, err)
	}
}
