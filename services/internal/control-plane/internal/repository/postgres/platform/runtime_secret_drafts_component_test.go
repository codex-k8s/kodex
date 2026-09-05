package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRuntimeSecretDraftLifecycle(t *testing.T, ctx context.Context, r *Repository) {
	t.Helper()
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, repoport.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Draft owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR = "urn:kodex:acr:interactive"
	owner.CredentialAMR = []string{"pwd"}
	project, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "draft-project-create"}, Payload: command.ProjectInput{Name: "Secret draft project", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create project: %v", err)
	}
	principal := func(operation string) value.Principal {
		return runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secret-drafts."+operation)
	}
	if err := s.CheckRuntimeSecretDraftWork(ctx, principal("readiness.check")); err != nil {
		t.Fatalf("draft readiness: %v", err)
	}
	prepare := func(input repoport.RuntimeSecretDraftPrepareInput) entity.RuntimeSecretDraftOperationReceipt {
		t.Helper()
		result, err := s.PrepareRuntimeSecretDraft(ctx, owner, input)
		if err != nil || result.OperationRef == "" {
			t.Fatalf("prepare %s: %v", input.Kind, err)
		}
		return result
	}
	claim := func(receipt entity.RuntimeSecretDraftOperationReceipt) entity.RuntimeSecretDraftWork {
		t.Helper()
		work, err := s.ConsumeRuntimeSecretDraft(ctx, principal("operations.consume"), repoport.RuntimeSecretDraftWorkInput{OperationGrant: receipt.OperationGrant, ClaimantID: "draft-broker"})
		if err != nil || work.ClaimGeneration != 1 {
			t.Fatalf("claim draft: %v", err)
		}
		return work
	}
	finish := func(work entity.RuntimeSecretDraftWork, action string, encrypted *entity.RuntimeSecretDraftEncryptedDescriptor, materialization *entity.RuntimeSecretMaterialization) (entity.RuntimeSecretDraftResult, error) {
		operation := map[string]string{"COMPLETE": "operations.complete", "RECOVER": "materialization.recover", "CLEANUP": "cleanup.complete"}[action]
		return s.FinishRuntimeSecretDraft(ctx, principal(operation), repoport.RuntimeSecretDraftWorkInput{Action: action, OperationRef: work.OperationRef, ClaimantID: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: encrypted, Materialization: materialization})
	}
	input := repoport.RuntimeSecretDraftPrepareInput{Kind: "SAVE", ProjectRef: project.Project.Ref, Name: "draft-secret", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "draft-save-original"}}
	first := prepare(input)
	if first.Draft.SecretVersion < 1 {
		t.Fatal("new draft lacks authoritative secret version")
	}
	reissued := prepare(input)
	if first.OperationRef != reissued.OperationRef || first.OperationGrant == reissued.OperationGrant {
		t.Fatal("save replay did not rotate grant")
	}
	if _, err := s.ConsumeRuntimeSecretDraft(ctx, principal("operations.consume"), repoport.RuntimeSecretDraftWorkInput{OperationGrant: first.OperationGrant, ClaimantID: "draft-broker"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("superseded grant: %v", err)
	}
	work := claim(reissued)
	if work.StagedNamespace != "kodex-secret-drafts" || work.Namespace != "kodex-runtime" || work.StagedSecretKey != "ciphertext" {
		t.Fatal("incorrect namespace assignment")
	}
	encrypted := &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: work.StagedNamespace, SecretName: work.StagedSecretName, SecretKey: work.StagedSecretKey, SecretUID: "ciphertext-uid", SecretResourceVersion: "10", CiphertextSHA256: runtimeSecretHashB, EncryptionKeyID: "draft-key", EncryptionKeyGeneration: 1}
	saved, err := finish(work, "COMPLETE", encrypted, nil)
	if err != nil || saved.Draft.State != "DRAFT" {
		t.Fatalf("save complete: %v", err)
	}
	if _, err := finish(work, "COMPLETE", nil, nil); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("missing replay descriptor: %v", err)
	}
	if _, err := finish(work, "COMPLETE", encrypted, nil); err != nil {
		t.Fatalf("save complete replay: %v", err)
	}
	prepareNext := func(kind, key string, draft entity.RuntimeSecretDraft) entity.RuntimeSecretDraftWork {
		t.Helper()
		input := repoport.RuntimeSecretDraftPrepareInput{Kind: kind, DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &draft.Version}}
		if kind == "PUBLISH" {
			plan, err := s.PrepareRuntimeSecretDraftImpact(ctx, owner, draft.Ref, value.Mutation{IdempotencyKey: key + "-impact", ExpectedVersion: &draft.Version})
			if err != nil || plan.Total != 0 {
				t.Fatalf("prepare empty impact: %+v %v", plan, err)
			}
			input.ImpactPlanRef = plan.Ref
		}
		return claim(prepare(input))
	}
	validate := prepareNext("VALIDATE", "draft-validate-original", saved.Draft)
	bad := *encrypted
	bad.SecretResourceVersion = "11"
	if _, err := finish(validate, "COMPLETE", &bad, nil); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("mismatched ciphertext: %v", err)
	}
	validated, err := finish(validate, "COMPLETE", encrypted, nil)
	if err != nil || validated.Draft.State != "VALID" {
		t.Fatalf("validate: %v", err)
	}
	publish := prepareNext("PUBLISH", "draft-publish-abandoned", validated.Draft)
	legacy := repoport.RuntimeSecretPrepareInput{Kind: "CREATE", ProjectRef: project.Project.Ref, Name: "draft-secret", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "draft-legacy-competitor"}}
	if _, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.create"), legacy); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("concurrent legacy publish: %v", err)
	}
	name, err := runtimesecret.VersionedKubernetesName(publish.Draft.SecretRef, publish.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	materialization := &entity.RuntimeSecretMaterialization{Namespace: publish.Namespace, SecretName: name, SecretKey: "value", SecretUID: "runtime-uid", SecretResourceVersion: "20", ContentSHA256: runtimeSecretHashA}
	if _, err := r.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_draft_operations SET lease_deadline=clock_timestamp()-interval '1 second' WHERE ref=$1`, publish.OperationRef); err != nil {
		t.Fatal(err)
	}
	if _, err := finish(publish, "COMPLETE", encrypted, materialization); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expired completion: %v", err)
	}
	recovered, err := finish(publish, "RECOVER", encrypted, materialization)
	if err != nil || recovered.MaterializationAction != "DELETE" || recovered.EncryptedAction != "KEEP" || recovered.Draft.State != "VALID" {
		t.Fatalf("recover orphan: %+v %v", recovered, err)
	}
	listed, _, err := s.ListRuntimeSecretDraftRecovery(ctx, principal("operations.recover"), query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed {
		if item.OperationRef == publish.OperationRef {
			found = item.RecoveryMaterialization != nil && item.RecoveryMaterialization.SecretUID == materialization.SecretUID
		}
	}
	if !found {
		t.Fatal("recovery list lost durable materialization fence")
	}
	if result, err := finish(publish, "RECOVER", nil, nil); err != nil || result.MaterializationAction != "DELETE" {
		t.Fatalf("lost cleanup ACK recovery: %v", err)
	}
	if _, err := finish(publish, "CLEANUP", nil, materialization); err != nil {
		t.Fatalf("cleanup ACK: %v", err)
	}
	if _, err := finish(publish, "CLEANUP", nil, materialization); err != nil {
		t.Fatalf("cleanup ACK replay: %v", err)
	}
	retry := prepareNext("PUBLISH", "draft-publish-retry", recovered.Draft)
	if retry.TargetRevision <= publish.TargetRevision {
		t.Fatal("publication reused orphan revision")
	}
	materialization.SecretName, err = runtimesecret.VersionedKubernetesName(retry.Draft.SecretRef, retry.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	materialization.SecretUID = "runtime-retry-uid"
	published, err := finish(retry, "COMPLETE", encrypted, materialization)
	if err != nil || published.Draft.State != "PUBLISHED" || published.Secret == nil || published.Secret.CurrentRevision != retry.TargetRevision {
		t.Fatalf("publish retry: %+v %v", published, err)
	}
	if published.Draft.SecretVersion != published.Secret.Version {
		t.Fatal("publish draft secret version is stale")
	}
	if _, err := finish(retry, "COMPLETE", encrypted, materialization); err != nil {
		t.Fatalf("publish replay: %v", err)
	}
	if _, err := finish(retry, "RECOVER", encrypted, materialization); err != nil {
		t.Fatalf("published cleanup prepare: %v", err)
	}
	if _, err := finish(retry, "CLEANUP", encrypted, nil); err != nil {
		t.Fatalf("published ciphertext cleanup: %v", err)
	}
	stale := owner
	legacyRecovery := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.recover")
	legacyResult, err := s.RecoverRuntimeSecretMaterialization(ctx, legacyRecovery, repoport.RuntimeSecretRecoveryInput{OperationRef: retry.OperationRef, Materialization: *materialization})
	if err != nil || legacyResult.Action != "KEEP" || legacyResult.Secret == nil {
		t.Fatalf("legacy scan lost D6 published revision: %+v %v", legacyResult, err)
	}
	revokeReceipt, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.revoke"), repoport.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: published.Secret.Ref, Mutation: value.Mutation{IdempotencyKey: "draft-published-revoke", ExpectedVersion: &published.Secret.Version}})
	if err != nil {
		t.Fatal(err)
	}
	revokeClaim, err := s.ConsumeRuntimeSecretOperation(ctx, runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.consume"), repoport.RuntimeSecretConsumeInput{OperationGrant: revokeReceipt.OperationGrant, ClaimantID: "draft-revoke"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteRuntimeSecretOperation(ctx, runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.complete"), repoport.RuntimeSecretCompleteInput{OperationRef: revokeReceipt.OperationRef, ClaimantID: "draft-revoke", ClaimGeneration: revokeClaim.ClaimGeneration}); err != nil {
		t.Fatal(err)
	}
	legacyResult, err = s.RecoverRuntimeSecretMaterialization(ctx, legacyRecovery, repoport.RuntimeSecretRecoveryInput{OperationRef: retry.OperationRef, Materialization: *materialization})
	if err != nil || legacyResult.Action != "DELETE" {
		t.Fatalf("legacy scan retained revoked D6 revision: %+v %v", legacyResult, err)
	}
	stale.CredentialAuthenticatedAt = time.Now().Add(-6 * time.Minute)
	if _, err := s.PrepareRuntimeSecretDraft(ctx, stale, input); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale actor accepted: %v", err)
	}
	for _, mode := range []string{"discard", "expiry"} {
		input.Name = "draft-secret-" + mode
		input.Mutation.IdempotencyKey = "draft-save-" + mode
		pending := prepare(input)
		freshWork := claim(pending)
		if freshWork.Draft.Generation <= work.Draft.Generation {
			t.Fatal("draft generation did not advance")
		}
		freshEncrypted := *encrypted
		freshEncrypted.SecretName = freshWork.StagedSecretName
		freshEncrypted.SecretUID = "ciphertext-" + mode
		fresh, err := finish(freshWork, "COMPLETE", &freshEncrypted, nil)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "discard" {
			discard := prepareNext("DISCARD", "draft-discard-one", fresh.Draft)
			read, err := s.GetRuntimeSecretDraft(ctx, owner, fresh.Draft.Ref)
			if err != nil || read.State != "DISCARDED" {
				t.Fatalf("discard did not precede external deletion: %v", err)
			}
			if result, err := finish(discard, "COMPLETE", &freshEncrypted, nil); err != nil || result.Draft.State != "DISCARDED" {
				t.Fatalf("discard complete: %v", err)
			}
			if _, err := finish(discard, "COMPLETE", &freshEncrypted, nil); err != nil {
				t.Fatalf("discard replay: %v", err)
			}
		} else {
			pendingValidate := prepare(repoport.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", DraftRef: fresh.Draft.Ref, Mutation: value.Mutation{IdempotencyKey: "draft-expiry-validate", ExpectedVersion: &fresh.Draft.Version}})
			if _, err := r.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_drafts SET expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, fresh.Draft.Ref); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ConsumeRuntimeSecretDraft(ctx, principal("operations.consume"), repoport.RuntimeSecretDraftWorkInput{OperationGrant: pendingValidate.OperationGrant, ClaimantID: "draft-broker"}); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("expired draft consumed: %v", err)
			}
			expired, err := finish(freshWork, "RECOVER", &freshEncrypted, nil)
			if err != nil || expired.Draft.State != "EXPIRED" || expired.EncryptedAction != "DELETE" {
				t.Fatalf("draft expiry: %+v %v", expired, err)
			}
			var active int
			if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.runtime_secret_draft_operations o JOIN control_plane.runtime_secret_drafts d ON d.id=o.draft_id WHERE d.ref=$1 AND o.state IN ('PREPARED','CLAIMED')`, fresh.Draft.Ref).Scan(&active); err != nil || active != 0 {
				t.Fatalf("expiry retained active operation: %d %v", active, err)
			}
			if _, err := finish(freshWork, "CLEANUP", &freshEncrypted, nil); err != nil {
				t.Fatalf("expired cleanup: %v", err)
			}
		}
	}
}
