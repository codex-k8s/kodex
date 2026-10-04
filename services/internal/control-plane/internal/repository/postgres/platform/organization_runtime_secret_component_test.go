package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/organization_secret_actor_active.sql
var organizationSecretActorActiveSQL string

//go:embed testdata/sql/organization_secret_role_state.sql
var organizationSecretRoleStateSQL string

//go:embed testdata/sql/organization_secret_operation_expire.sql
var organizationSecretOperationExpireSQL string

// Изолированная PostgreSQL suite доказывает owner authority и устойчивые
// receipts. Дескрипторы синтетические: AEAD/Kubernetes проверяются broker suite.
func TestOrganizationRuntimeSecretComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles", DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR, owner.CredentialAMR = "urn:kodex:acr:interactive", []string{"pwd"}
	project, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "organization-secret-project"}, Payload: command.ProjectInput{Name: "Synthetic scope project", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("project fixture: %v", err)
	}
	owner = runtimeSecretOwnerPrincipal(owner, "secret.create")
	worker := func(operation string) value.Principal {
		return runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secret-drafts."+operation)
	}
	prepareInput := func(name string) port.RuntimeSecretDraftPrepareInput {
		return port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Name: name, ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "org-secret-save-" + name}}
	}
	denied := func(err error) bool { return errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) }
	assertScope := func(draft entity.RuntimeSecretDraft) {
		t.Helper()
		if draft.ScopeKind != "ORGANIZATION" || draft.OrganizationRef == "" || draft.ProjectRef != "" {
			t.Fatal("owner lost canonical organization scope")
		}
	}
	claim := func(receipt entity.RuntimeSecretDraftOperationReceipt) entity.RuntimeSecretDraftWork {
		t.Helper()
		work, err := s.ConsumeRuntimeSecretDraft(ctx, worker("operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: receipt.OperationGrant, ClaimantID: "org-secret-broker"})
		if err != nil || work.ClaimGeneration < 1 {
			t.Fatalf("claim: %v", err)
		}
		assertScope(work.Draft)
		return work
	}
	finish := func(work entity.RuntimeSecretDraftWork, action string, encrypted *entity.RuntimeSecretDraftEncryptedDescriptor, materialized *entity.RuntimeSecretMaterialization) (entity.RuntimeSecretDraftResult, error) {
		operation := map[string]string{"COMPLETE": "operations.complete", "RECOVER": "materialization.recover", "CLEANUP": "cleanup.complete"}[action]
		return s.FinishRuntimeSecretDraft(ctx, worker(operation), port.RuntimeSecretDraftWorkInput{Action: action, OperationRef: work.OperationRef, ClaimantID: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: encrypted, Materialization: materialized})
	}
	t.Run("creation boundary", func(t *testing.T) {
		stale := owner
		stale.CredentialAuthenticatedAt = time.Now().Add(-6 * time.Minute)
		if _, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, stale, prepareInput("stale")); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
			t.Fatalf("stale authentication: %v", err)
		}
		projectBound := owner
		projectBound.ProjectRef = project.Project.Ref
		if _, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, projectBound, prepareInput("project-authority")); !denied(err) {
			t.Fatalf("project credential reached org create: %v", err)
		}
		input := prepareInput("payload-project")
		input.ProjectRef = project.Project.Ref
		if _, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("payload selected organization scope: %v", err)
		}
		input = prepareInput("generic-create")
		input.ScopeKind = "ORGANIZATION"
		if _, err := s.PrepareRuntimeSecretDraft(ctx, owner, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("generic create inferred org from empty project: %v", err)
		}
	})
	input := prepareInput("lifecycle")
	first, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	assertScope(first.Draft)
	reissued, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, input)
	if err != nil || first.OperationRef != reissued.OperationRef || first.OperationGrant == reissued.OperationGrant {
		t.Fatalf("grant reissue: %v", err)
	}
	if _, err := s.ConsumeRuntimeSecretDraft(ctx, worker("operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: first.OperationGrant, ClaimantID: "org-secret-broker"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("superseded grant: %v", err)
	}
	work := claim(reissued)
	encrypted := &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: work.StagedNamespace, SecretName: work.StagedSecretName, SecretKey: work.StagedSecretKey, SecretUID: "org-ciphertext-uid", SecretResourceVersion: "10", CiphertextSHA256: runtimeSecretHashB, EncryptionKeyID: "synthetic-draft-key", EncryptionKeyGeneration: 1}
	saved, err := finish(work, "COMPLETE", encrypted, nil)
	if err != nil || saved.Draft.State != "DRAFT" {
		t.Fatalf("encrypted complete: %v", err)
	}
	assertScope(saved.Draft)
	t.Run("disabled owner completed draft replay", func(t *testing.T) {
		tag, err := pool.Exec(ctx, organizationSecretActorActiveSQL, owner.ActorID, false)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("isolated actor fixture: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, organizationSecretActorActiveSQL, owner.ActorID, true); err != nil {
				t.Fatal(err)
			}
		}()
		if _, err := finish(work, "COMPLETE", encrypted, nil); !denied(err) {
			t.Fatalf("disabled owner received completed draft receipt: %v", err)
		}
	})
	next := func(kind, key string, draft entity.RuntimeSecretDraft) entity.RuntimeSecretDraftWork {
		t.Helper()
		input := port.RuntimeSecretDraftPrepareInput{Kind: kind, DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, Mutation: value.Mutation{IdempotencyKey: "org-secret-" + key, ExpectedVersion: &draft.Version}}
		if kind == "PUBLISH" {
			plan, err := s.PrepareRuntimeSecretDraftImpact(ctx, owner, draft.Ref, value.Mutation{IdempotencyKey: "org-secret-impact-" + key, ExpectedVersion: &draft.Version})
			if err != nil || plan.Total != 0 {
				t.Fatalf("impact: %v", err)
			}
			input.ImpactPlanRef = plan.Ref
		}
		receipt, err := s.PrepareRuntimeSecretDraft(ctx, owner, input)
		if err != nil {
			t.Fatalf("prepare %s: %v", kind, err)
		}
		return claim(receipt)
	}
	validated, err := finish(next("VALIDATE", "validate", saved.Draft), "COMPLETE", encrypted, nil)
	if err != nil || validated.Draft.State != "VALID" {
		t.Fatalf("validate: %v", err)
	}
	publish := next("PUBLISH", "publish", validated.Draft)
	name, err := runtimesecret.VersionedKubernetesName(publish.Draft.SecretRef, publish.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	materialized := &entity.RuntimeSecretMaterialization{Namespace: publish.Namespace, SecretName: name, SecretKey: "value", SecretUID: "org-runtime-uid", SecretResourceVersion: "20", ContentSHA256: runtimeSecretHashA}
	published, err := finish(publish, "COMPLETE", encrypted, materialized)
	if err != nil || published.Secret == nil || published.Draft.State != "PUBLISHED" {
		t.Fatalf("publish: %v", err)
	}
	assertScope(published.Draft)
	if published.Secret.ScopeKind != "ORGANIZATION" || published.Secret.OrganizationRef != published.Draft.OrganizationRef || published.Secret.ProjectRef != "" {
		t.Fatal("published scope lost")
	}
	if _, err := finish(publish, "COMPLETE", encrypted, materialized); err != nil {
		t.Fatalf("exact completion replay: %v", err)
	}
	recovered, err := finish(publish, "RECOVER", encrypted, materialized)
	if err != nil || recovered.EncryptedAction != "DELETE" || recovered.MaterializationAction != "KEEP" {
		t.Fatalf("published recovery: %v", err)
	}
	if _, err := finish(publish, "CLEANUP", encrypted, nil); err != nil {
		t.Fatalf("exact ciphertext cleanup ACK: %v", err)
	}
	viewOwner := runtimeSecretOwnerPrincipal(owner, "secret.view")
	listed, _, err := s.ListRuntimeSecrets(ctx, viewOwner, query.Filter{RuntimeResourceScopeKind: "ORGANIZATION", Page: query.Page{Size: 50}})
	if err != nil || len(listed) != 1 || listed[0].Ref != published.Secret.Ref {
		t.Fatalf("organization list: %v", err)
	}
	t.Run("organization role revoked", func(t *testing.T) {
		tag, err := pool.Exec(ctx, organizationSecretRoleStateSQL, owner.ActorID, "REVOKED")
		if err != nil || tag.RowsAffected() < 1 {
			t.Fatalf("isolated role fixture: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, organizationSecretRoleStateSQL, owner.ActorID, "ACTIVE"); err != nil {
				t.Fatal(err)
			}
		}()
		if _, _, err := s.ListRuntimeSecrets(ctx, viewOwner, query.Filter{RuntimeResourceScopeKind: "ORGANIZATION", Page: query.Page{Size: 50}}); !denied(err) {
			t.Fatalf("missing OWNER role read organization catalog: %v", err)
		}
		if _, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, prepareInput("role-revoked")); !denied(err) {
			t.Fatalf("missing OWNER role created organization secret: %v", err)
		}
		retained, err := finish(publish, "RECOVER", nil, materialized)
		if err != nil || retained.MaterializationAction != "KEEP" || retained.Draft.State != "PUBLISHED" {
			t.Fatalf("committed publication was not retained after actor role revocation: %v", err)
		}
	})
	projectView := viewOwner
	projectView.ProjectRef = project.Project.Ref
	if _, err := s.GetRuntimeSecret(ctx, projectView, published.Secret.Ref); !denied(err) {
		t.Fatalf("project credential read organization secret: %v", err)
	}
	if _, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(projectView, "secret.revoke"), port.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: published.Secret.Ref, Mutation: value.Mutation{IdempotencyKey: "org-secret-project-OCC-probe", ExpectedVersion: &published.Secret.Version}}); !denied(err) {
		t.Fatalf("project authority reached organization mutation: %v", err)
	}
	projectList, _, err := s.ListRuntimeSecrets(ctx, viewOwner, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 50}})
	if err != nil || len(projectList) != 0 {
		t.Fatalf("organization secret leaked into project list: %v", err)
	}
	wrong := published.Secret.Version + 1
	if _, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.revoke"), port.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: published.Secret.Ref, Mutation: value.Mutation{IdempotencyKey: "org-secret-stale-revoke", ExpectedVersion: &wrong}}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale OCC: %v", err)
	}
	setActorActive := func(active bool) {
		t.Helper()
		tag, err := pool.Exec(ctx, organizationSecretActorActiveSQL, owner.ActorID, active)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("isolated actor fixture: %v", err)
		}
	}
	t.Run("disabled owner draft claim", func(t *testing.T) {
		pending, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, prepareInput("disabled-claim"))
		if err != nil {
			t.Fatal(err)
		}
		setActorActive(false)
		defer setActorActive(true)
		if _, err := s.ConsumeRuntimeSecretDraft(ctx, worker("operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: pending.OperationGrant, ClaimantID: "org-secret-broker"}); !denied(err) {
			t.Fatalf("disabled owner was claimed: %v", err)
		}
	})
	t.Run("expired orphan ciphertext recovery", func(t *testing.T) {
		receipt, err := s.PrepareOrganizationRuntimeSecretDraft(ctx, owner, prepareInput("orphan-recovery"))
		if err != nil {
			t.Fatal(err)
		}
		orphan := claim(receipt)
		ciphertext := &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: orphan.StagedNamespace, SecretName: orphan.StagedSecretName, SecretKey: orphan.StagedSecretKey, SecretUID: "org-orphan-uid", SecretResourceVersion: "30", CiphertextSHA256: runtimeSecretHashB, EncryptionKeyID: "synthetic-draft-key", EncryptionKeyGeneration: 1}
		if _, err := pool.Exec(ctx, organizationSecretOperationExpireSQL, orphan.OperationRef); err != nil {
			t.Fatal(err)
		}
		if _, err := finish(orphan, "COMPLETE", ciphertext, nil); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("expired operation completed: %v", err)
		}
		setActorActive(false)
		defer setActorActive(true)
		recovered, err := finish(orphan, "RECOVER", ciphertext, nil)
		if err != nil || recovered.State != "FAILED" || recovered.EncryptedAction != "DELETE" || recovered.MaterializationAction != "KEEP" {
			t.Fatalf("expired orphan recovery: %v", err)
		}
		if _, err := finish(orphan, "CLEANUP", ciphertext, nil); err != nil {
			t.Fatalf("orphan durable cleanup ACK: %v", err)
		}
		if _, err := finish(orphan, "CLEANUP", ciphertext, nil); err != nil {
			t.Fatalf("orphan cleanup replay: %v", err)
		}
	})
	t.Run("immediate owner eligibility and reauthentication", func(t *testing.T) {
		input := port.RuntimeSecretPrepareInput{Kind: "REVEAL", SecretRef: published.Secret.Ref, Mutation: value.Mutation{IdempotencyKey: "org-secret-reveal-eligibility", ExpectedVersion: &published.Secret.Version}}
		actor := runtimeSecretOwnerPrincipal(owner, "secret.reveal")
		stale := actor
		stale.CredentialAuthenticatedAt = time.Now().Add(-6 * time.Minute)
		if _, err := s.PrepareRuntimeSecretOperation(ctx, stale, input); !errors.Is(err, errs.ErrUnauthorized) && !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
			t.Fatalf("stale reveal authorized: %v", err)
		}
		receipt, err := s.PrepareRuntimeSecretOperation(ctx, actor, input)
		if err != nil {
			t.Fatal(err)
		}
		consumeActor := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.consume")
		completeActor := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.complete")
		consume := port.RuntimeSecretConsumeInput{OperationGrant: receipt.OperationGrant, ClaimantID: "org-secret-broker"}
		setActorActive(false)
		_, claimErr := s.ConsumeRuntimeSecretOperation(ctx, consumeActor, consume)
		setActorActive(true)
		if !denied(claimErr) {
			t.Fatalf("disabled owner immediate claim: %v", claimErr)
		}
		claim, err := s.ConsumeRuntimeSecretOperation(ctx, consumeActor, consume)
		if err != nil {
			t.Fatal(err)
		}
		complete := port.RuntimeSecretCompleteInput{OperationRef: receipt.OperationRef, ClaimantID: "org-secret-broker", ClaimGeneration: claim.ClaimGeneration}
		setActorActive(false)
		_, completeErr := s.CompleteRuntimeSecretOperation(ctx, completeActor, complete)
		setActorActive(true)
		if !denied(completeErr) {
			t.Fatalf("disabled owner immediate completion: %v", completeErr)
		}
		if _, err := s.CompleteRuntimeSecretOperation(ctx, completeActor, complete); err != nil {
			t.Fatal(err)
		}
		setActorActive(false)
		_, replayErr := s.CompleteRuntimeSecretOperation(ctx, completeActor, complete)
		setActorActive(true)
		if !denied(replayErr) {
			t.Fatalf("disabled owner received completed immediate receipt: %v", replayErr)
		}
	})
	t.Run("reveal revoke canonical scope", func(t *testing.T) {
		for _, kind := range []string{"REVEAL", "REVOKE"} {
			permission := "secret." + strings.ToLower(kind)
			receipt, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, permission), port.RuntimeSecretPrepareInput{Kind: kind, SecretRef: published.Secret.Ref, Mutation: value.Mutation{IdempotencyKey: "org-secret-" + strings.ToLower(kind), ExpectedVersion: &published.Secret.Version}})
			if err != nil {
				t.Fatalf("prepare %s: %v", kind, err)
			}
			claim, err := s.ConsumeRuntimeSecretOperation(ctx, runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.consume"), port.RuntimeSecretConsumeInput{OperationGrant: receipt.OperationGrant, ClaimantID: "org-secret-broker"})
			if err != nil || claim.ScopeKind != "ORGANIZATION" || claim.OrganizationRef != published.Secret.OrganizationRef || claim.ProjectRef != "" || len(claim.RevisionDescriptors) != 1 {
				t.Fatalf("immediate scoped claim: %v", err)
			}
			if claim.RevisionDescriptors[0].SecretUID != materialized.SecretUID || claim.RevisionDescriptors[0].SecretResourceVersion != materialized.SecretResourceVersion {
				t.Fatal("immediate claim lost exact runtime pins")
			}
			completed, err := s.CompleteRuntimeSecretOperation(ctx, runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.complete"), port.RuntimeSecretCompleteInput{OperationRef: receipt.OperationRef, ClaimantID: "org-secret-broker", ClaimGeneration: claim.ClaimGeneration})
			if err != nil || completed.ScopeKind != "ORGANIZATION" {
				t.Fatalf("immediate completion: %v", err)
			}
			if kind == "REVOKE" && completed.State != "REVOKED" {
				t.Fatal("organization revoke did not close secret")
			}
		}
	})
}
