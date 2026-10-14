package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderCredentialRenewalComponent(t *testing.T) {
	t.Run("completion", func(t *testing.T) { testProviderCredentialRenewalComponent(t, false) })
	t.Run("unknown-read-recovery", func(t *testing.T) { testProviderCredentialRenewalComponent(t, true) })
}

func testProviderCredentialRenewalComponent(t *testing.T, recoverUnknown bool) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated PostgreSQL")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-6.1-sol", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-component", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute,
		AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging",
		PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckProviderCredentialRenewalReadiness(ctx); err != nil {
		t.Fatal("renewal schema readiness", err)
	}
	seedObservedCatalogFixture(t, ctx, repository, func(observation *port.ProviderModelCatalogObservation) {
		observation.Failure, observation.Source, observation.Models = "AUTHORIZATION_REJECTED", "", nil
	})
	encoder := &providercredentialclient.Client{}
	tasks, err := repository.ClaimProviderCredentialRenewalTasks(ctx, "renewal-component-worker", encoder)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("renewal claim: %d %v", len(tasks), err)
	}
	task := tasks[0]
	if duplicate, err := repository.ClaimProviderCredentialRenewalTasks(ctx, "other-worker", encoder); err != nil || len(duplicate) != 0 {
		t.Fatal("second worker claimed same account")
	}
	service, err := serviceplatform.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	principal := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "secret-broker", Operation: port.ProviderCredentialRenewalAuthorizeOperation, RequestDigestSHA256: task.OwnerRequestDigest}, "secret-broker")
	wrong := task
	wrong.Generation++
	if err := service.AuthorizeProviderCredentialRenewal(ctx, principal, wrong); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("changed binding authorized")
	}
	foreign := principal
	foreign.CallerWorkload = "runtime-controller"
	if err := service.AuthorizeProviderCredentialRenewal(ctx, foreign, task); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign workload authorized")
	}
	if err := service.AuthorizeProviderCredentialRenewal(ctx, principal, task); err != nil {
		t.Fatal("authorize task", err)
	}
	if err := service.AuthorizeProviderCredentialRenewal(ctx, principal, task); err != nil {
		t.Fatal("same authorization replay", err)
	}
	result := port.ProviderCredentialRenewalResult{State: "SUCCEEDED", ReceiptUID: "73000000-0000-4000-8000-000000000001", ReceiptVersion: "1",
		Credential: &entity.ProviderCredentialDescriptor{SecretName: "provider-credential-component-result", SecretUID: "74000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("b", 64)}}
	if recoverUnknown {
		if err := repository.CompleteProviderCredentialRenewalTask(ctx, task, port.ProviderCredentialRenewalResult{State: "UNKNOWN_OUTCOME"}); err != nil {
			t.Fatal("durable unknown outcome", err)
		}
		if duplicate, err := repository.ClaimProviderCredentialRenewalTasks(ctx, "restart-worker", encoder); err != nil || len(duplicate) != 0 {
			t.Fatal("unknown outcome restarted OAuth writer")
		}
		if err := repository.CompleteProviderCredentialRenewalTask(ctx, task, result); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("late writer activated without fresh READ recovery")
		}
		recoveries, err := repository.ListProviderCredentialRenewalRecoveryTasks(ctx)
		if err != nil || len(recoveries) != 1 || recoveries[0].Ref != task.Ref {
			t.Fatal("unknown outcome lost exact recovery task")
		}
		readPrincipal := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
			CallerWorkload: "secret-broker", Operation: port.ProviderCredentialRenewalValidateOperation, RequestDigestSHA256: task.OwnerRequestDigest}, "secret-broker")
		if err := service.ValidateProviderCredentialRenewalObservation(ctx, readPrincipal, task); err != nil {
			t.Fatal("fresh owner READ authorization", err)
		}
		result.Recovered = true
	}
	if err := repository.CompleteProviderCredentialRenewalTask(ctx, task, result); err != nil {
		t.Fatal("atomic completion", err)
	}
	result.ReceiptVersion = "2"
	result.Recovered = true
	if err := repository.CompleteProviderCredentialRenewalTask(ctx, task, result); err != nil {
		t.Fatal("lost-ACK semantic replay", err)
	}
	if err := service.AuthorizeProviderCredentialRenewal(ctx, principal, task); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("terminal task authorized another effect")
	}
	var version, revisions, audits int64
	if err := pool.QueryRow(ctx, `SELECT account.version,
        (SELECT count(*) FROM control_plane.provider_credential_revisions credential WHERE credential.provider_account_id = account.id),
        (SELECT count(*) FROM control_plane.audit_events audit WHERE audit.resource_ref = account.ref AND audit.action = 'PROVIDER_CREDENTIAL_RENEWED')
        FROM control_plane.provider_accounts account WHERE account.ref = $1`, task.AccountRef).Scan(&version, &revisions, &audits); err != nil {
		t.Fatal("completion readback")
	}
	if version != task.AccountVersion+1 || revisions != 2 || audits != 1 {
		t.Fatal("completion duplicated or lost durable effects")
	}
}
