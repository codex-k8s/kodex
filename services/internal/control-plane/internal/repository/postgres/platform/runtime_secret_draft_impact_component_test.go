package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testSecretDraftImpactRebind(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, secret entity.RuntimeSecret) {
	t.Helper()
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR = "urn:kodex:acr:interactive"
	owner.CredentialAMR = []string{"pwd"}
	worker := func(operation string) value.Principal {
		return runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secret-drafts."+operation)
	}
	prepare := func(input port.RuntimeSecretDraftPrepareInput) entity.RuntimeSecretDraftWork {
		t.Helper()
		receipt, err := s.PrepareRuntimeSecretDraft(ctx, owner, input)
		if err != nil {
			t.Fatal(err)
		}
		work, err := s.ConsumeRuntimeSecretDraft(ctx, worker("operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: receipt.OperationGrant, ClaimantID: "draft-impact-worker"})
		if err != nil {
			t.Fatal(err)
		}
		return work
	}
	work := prepare(port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", SecretRef: secret.Ref, ValueType: secret.ValueType, ExpectedContentSHA256: runtimeSecretHashC, Mutation: value.Mutation{IdempotencyKey: "draft-impact-save", ExpectedVersion: &secret.Version}})
	encrypted := &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: work.StagedNamespace, SecretName: work.StagedSecretName, SecretKey: work.StagedSecretKey, SecretUID: "draft-impact-ciphertext", SecretResourceVersion: "100", CiphertextSHA256: runtimeSecretHashA, EncryptionKeyID: "draft-key", EncryptionKeyGeneration: 1}
	complete := func(work entity.RuntimeSecretDraftWork, materialization *entity.RuntimeSecretMaterialization) entity.RuntimeSecretDraftResult {
		t.Helper()
		result, err := s.FinishRuntimeSecretDraft(ctx, worker("operations.complete"), port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: work.OperationRef, ClaimantID: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: encrypted, Materialization: materialization})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	draft := complete(work, nil).Draft
	work = prepare(port.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", DraftRef: draft.Ref, Mutation: value.Mutation{IdempotencyKey: "draft-impact-validate", ExpectedVersion: &draft.Version}})
	draft = complete(work, nil).Draft
	mutation := value.Mutation{IdempotencyKey: "draft-impact-plan", ExpectedVersion: &draft.Version}
	plan, err := s.PrepareRuntimeSecretDraftImpact(ctx, owner, draft.Ref, mutation)
	if err != nil || plan.Total != 2 || plan.SecretVersion != secret.Version || plan.SourceRevision != secret.CurrentRevision {
		t.Fatalf("impact plan: %+v %v", plan, err)
	}
	replay, err := s.PrepareRuntimeSecretDraftImpact(ctx, owner, draft.Ref, mutation)
	if err != nil || replay.Ref != plan.Ref || replay.Digest != plan.Digest {
		t.Fatalf("impact replay: %v", err)
	}
	page, err := s.GetRuntimeSecretDraftImpact(ctx, owner, plan.Ref, "", query.Page{Size: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.NextPageToken == "" {
		t.Fatalf("impact page: %+v %v", page, err)
	}
	second, err := s.GetRuntimeSecretDraftImpact(ctx, owner, plan.Ref, "", query.Page{Size: 1, Token: page.NextPageToken})
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("impact next page: %v", err)
	}
	selected := []string{page.Items[0].Ref, second.Items[0].Ref}
	if _, err := s.PrepareRuntimeSecretDraft(ctx, owner, port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, ImpactPlanRef: plan.Ref, SelectedItemRefs: []string{"sdit_foreign00"}, Mutation: value.Mutation{IdempotencyKey: "draft-impact-foreign-selection", ExpectedVersion: &draft.Version}}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("foreign plan item accepted: %v", err)
	}
	unchanged, err := s.GetRuntimeSecretDraft(ctx, owner, draft.Ref)
	if err != nil || unchanged.Version != draft.Version || unchanged.State != "VALID" {
		t.Fatalf("rejected selection changed draft: %+v %v", unchanged, err)
	}
	if _, err := s.GetRuntimeSecretDraftImpact(ctx, owner, plan.Ref, "changed", query.Page{Size: 1, Token: page.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("impact cursor query replay: %v", err)
	}
	// Один consumer изменился после preview; другой обязан получить новый pin.
	if _, err := r.pool.Exec(ctx, `UPDATE control_plane.agents SET version=version+1 WHERE ref=$1`, second.Items[0].Consumer.Consumer.AgentRef); err != nil {
		t.Fatal(err)
	}
	work = prepare(port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, ImpactPlanRef: plan.Ref, SelectedItemRefs: selected, Mutation: value.Mutation{IdempotencyKey: "draft-impact-publish", ExpectedVersion: &draft.Version}})
	name, err := runtimesecret.VersionedKubernetesName(secret.Ref, work.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	result := complete(work, &entity.RuntimeSecretMaterialization{Namespace: work.Namespace, SecretName: name, SecretKey: "value", SecretUID: "draft-impact-runtime", SecretResourceVersion: "200", ContentSHA256: runtimeSecretHashC})
	if result.Secret == nil || result.Secret.CurrentRevision != work.TargetRevision {
		t.Fatal("secret publication missing")
	}
	if _, err := s.GetRuntimeSecretDraftImpact(ctx, owner, plan.Ref, "", query.Page{Size: 1, Token: page.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("impact cursor survived terminal transition: %v", err)
	}
	final, err := s.GetRuntimeSecretDraftImpact(ctx, owner, plan.Ref, "", query.Page{Size: 100})
	if err != nil || final.Plan.State != "APPLIED" || len(final.Items) != 2 {
		t.Fatalf("final impact report: %+v %v", final, err)
	}
	for _, item := range final.Items {
		if item.Ref == selected[0] {
			if item.Outcome != "APPLIED" || item.ResultEnvironmentVersionRef == item.Consumer.EnvironmentVersionRef || item.ResultBindingRef != item.Consumer.Consumer.BindingRef || item.ResultBindingVersion <= item.Consumer.Consumer.BindingVersion {
				t.Fatalf("successful replacement receipt: %+v", item)
			}
		} else if item.Outcome != "CONFLICT" || item.ResultBindingRef != "" {
			t.Fatalf("conflicting replacement receipt: %+v", item)
		}
	}
}
