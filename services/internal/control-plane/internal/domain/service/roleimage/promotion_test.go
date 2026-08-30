package roleimage

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type promotionRepositoryStub struct {
	repository.Repository
	resolved value.Principal
	receipt  entity.RoleImagePromotionReceipt
	input    repository.PromotionRequestInput
	calls    int
}

func (stub *promotionRepositoryStub) ResolvePrincipal(context.Context, value.Principal) (value.Principal, error) {
	return stub.resolved, nil
}

func (stub *promotionRepositoryStub) RequestPromotion(_ context.Context, input repository.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	stub.input = input
	stub.calls++
	return stub.receipt, nil
}

func TestPromoteRoleImageBuildsSpecializedMutation(t *testing.T) {
	catalog, err := NewCatalog([]Environment{validEnvironment(true, true)})
	if err != nil {
		t.Fatalf("construct role image catalog: %v", err)
	}
	principal := value.Principal{
		ActorID: "usr_owner", AuthorityTenant: "org_installation",
		Permission: permissionRequestPromotion, CorrelationRef: "cor_promotion",
		CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
	receipt := entity.RoleImagePromotionReceipt{Ref: "imgprom_12345678", State: "QUEUED"}
	stub := &promotionRepositoryStub{resolved: principal, receipt: receipt}
	service, err := New(stub, catalog)
	if err != nil {
		t.Fatalf("construct role image service: %v", err)
	}
	expectedVersion := int64(7)
	result, err := service.Promote(context.Background(), repository.PromotionRequestInput{
		Principal: principal,
		Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-unit", ExpectedVersion: &expectedVersion},
		RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678",
		ExpectedProvenanceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil || result != receipt {
		t.Fatalf("promote role image: receipt=%#v err=%v", result, err)
	}
	if stub.calls != 1 || stub.input.Mutation.Operation != "controlplane.promote_role_image" ||
		len(stub.input.Mutation.IntentDigest) != 64 || stub.input.Mutation.ExpectedVersion == nil ||
		*stub.input.Mutation.ExpectedVersion != expectedVersion {
		t.Fatalf("specialized promotion mutation mismatch: calls=%d input=%#v", stub.calls, stub.input)
	}
	firstIntent := stub.input.Mutation.IntentDigest
	otherVersion := expectedVersion + 1
	_, err = service.Promote(context.Background(), repository.PromotionRequestInput{
		Principal: principal,
		Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-unit", ExpectedVersion: &otherVersion},
		RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678",
		ExpectedProvenanceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil || stub.input.Mutation.IntentDigest == firstIntent {
		t.Fatalf("promotion OCC version is not bound to intent: first=%s second=%s err=%v",
			firstIntent, stub.input.Mutation.IntentDigest, err)
	}
}

func TestPromoteRoleImageRejectsInvalidBoundary(t *testing.T) {
	catalog, err := NewCatalog([]Environment{validEnvironment(true, true)})
	if err != nil {
		t.Fatalf("construct role image catalog: %v", err)
	}
	validPrincipal := value.Principal{
		ActorID: "usr_owner", AuthorityTenant: "org_installation",
		Permission: permissionRequestPromotion, CorrelationRef: "cor_promotion",
		CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
	tests := []struct {
		name      string
		principal value.Principal
		version   *int64
		sha256    string
		want      error
	}{
		{name: "wrong transport permission", principal: func() value.Principal {
			value := validPrincipal
			value.Permission = permissionManageRecipe
			return value
		}(), version: pointer(int64(1)), sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: errs.ErrForbidden},
		{name: "missing OCC", principal: validPrincipal, sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: errs.ErrInvalid},
		{name: "invalid provenance", principal: validPrincipal, version: pointer(int64(1)), sha256: "not-a-digest", want: errs.ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &promotionRepositoryStub{resolved: test.principal}
			service, serviceErr := New(stub, catalog)
			if serviceErr != nil {
				t.Fatalf("construct role image service: %v", serviceErr)
			}
			_, promoteErr := service.Promote(context.Background(), repository.PromotionRequestInput{
				Principal: test.principal,
				Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-unit", ExpectedVersion: test.version},
				RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678",
				ExpectedProvenanceSHA256: test.sha256,
			})
			if !errors.Is(promoteErr, test.want) || stub.calls != 0 {
				t.Fatalf("unexpected rejection: err=%v calls=%d", promoteErr, stub.calls)
			}
		})
	}
}

func pointer[T any](value T) *T { return &value }
