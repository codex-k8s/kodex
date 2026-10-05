package roleimage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type failureRepositoryStub struct {
	repo.Repository
	resolved value.Principal
	calls    int
}

func (s *failureRepositoryStub) ResolvePrincipal(context.Context, value.Principal) (value.Principal, error) {
	return s.resolved, nil
}
func (s *failureRepositoryStub) FailAdmission(context.Context, repo.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	s.calls++
	return entity.RoleImageAdmissionFailure{State: "FAILED"}, nil
}
func (s *failureRepositoryStub) ExpireAdmission(context.Context, repo.AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error) {
	s.calls++
	return entity.RoleImageAdmissionFailure{State: "FAILED", ErrorCode: "ADMISSION_LEASE_EXPIRED"}, nil
}

func TestAdmissionFailureRequiresClosedExactWorkerAuthority(t *testing.T) {
	principal := value.Principal{ActorID: "svc_admission", AuthorityTenant: "org_installation", Permission: "platform.role-images.admission.fail", CorrelationRef: "cor_failure", CallerWorkload: "image-admission", CredentialRevision: 1}
	input := repo.AdmissionFailureInput{ExpectedAdmissionAttemptRef: "imgadm_12345678", ExpectedAdmissionAttempt: 1, Principal: principal, IdempotencyKey: "admission-failure-unit", ArtifactRef: "imgart_12345678", BuildRef: "imgbld_12345678", ExpectedVersion: 2, ExpectedFence: 1, ExpectedAuthorityGeneration: 1, ExpectedBuildAttempt: 1, RecipeGeneration: 1, ClaimToken: strings.Repeat("t", 43), PolicyRevision: 1, ManifestDigest: "sha256:" + strings.Repeat("a", 64), ImmutableBuildSHA256: strings.Repeat("b", 64), ProvenanceSHA256: strings.Repeat("c", 64), PolicySHA256: strings.Repeat("d", 64), SpecSHA256: strings.Repeat("e", 64), ErrorCode: "ADMISSION_WORKER_FAILED"}
	catalog, err := NewCatalog([]Environment{validEnvironment(true, true)})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "foreign_worker", "foreign_permission", "caller_expiry", "unknown_code", "empty_fence", "empty_generation", "bad_token", "bad_digest"} {
		t.Run(scenario, func(t *testing.T) {
			p := principal
			in := input
			switch scenario {
			case "foreign_worker":
				p.CallerWorkload = "role-image-builder"
			case "foreign_permission":
				p.Permission = "platform.role-images.admission.record"
			case "caller_expiry":
				in.ErrorCode = "ADMISSION_LEASE_EXPIRED"
			case "unknown_code":
				in.ErrorCode = "PRIVATE_ERROR"
			case "empty_fence":
				in.ExpectedFence = 0
			case "empty_generation":
				in.ExpectedAuthorityGeneration = 0
			case "bad_token":
				in.ClaimToken = "short"
			case "bad_digest":
				in.SpecSHA256 = "invalid"
			}
			stub := &failureRepositoryStub{resolved: p}
			service, err := New(stub, catalog)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.FailAdmission(t.Context(), in)
			if scenario == "valid" {
				if err != nil || stub.calls != 1 {
					t.Fatal("valid worker rejected")
				}
			} else {
				want := errs.ErrInvalid
				if scenario == "foreign_worker" || scenario == "foreign_permission" {
					want = errs.ErrForbidden
				}
				if !errors.Is(err, want) || stub.calls != 0 {
					t.Fatal("invalid failure crossed owner boundary")
				}
			}
		})
	}
	principal.Permission = "platform.role-images.admission.expire"
	stub := &failureRepositoryStub{resolved: principal}
	service, _ := New(stub, catalog)
	expiry := repo.AdmissionExpiryInput{ExpectedAdmissionAttemptRef: input.ExpectedAdmissionAttemptRef, ExpectedAdmissionAttempt: input.ExpectedAdmissionAttempt, Principal: principal, IdempotencyKey: input.IdempotencyKey, ArtifactRef: input.ArtifactRef, BuildRef: input.BuildRef, ExpectedVersion: input.ExpectedVersion, ExpectedFence: input.ExpectedFence, ExpectedAuthorityGeneration: input.ExpectedAuthorityGeneration, ExpectedBuildAttempt: input.ExpectedBuildAttempt, RecipeGeneration: input.RecipeGeneration, PolicyRevision: input.PolicyRevision, ManifestDigest: input.ManifestDigest, ImmutableBuildSHA256: input.ImmutableBuildSHA256, ProvenanceSHA256: input.ProvenanceSHA256, PolicySHA256: input.PolicySHA256, SpecSHA256: input.SpecSHA256}
	if _, err := service.ExpireAdmission(t.Context(), expiry); err != nil || stub.calls != 1 {
		t.Fatal("dedicated owner expiry rejected")
	}
	principal.Permission = "platform.role-images.admission.fail"
	stub.resolved = principal
	if _, err := service.ExpireAdmission(t.Context(), expiry); !errors.Is(err, errs.ErrForbidden) || stub.calls != 1 {
		t.Fatal("fail authority performed owner expiry")
	}
}
