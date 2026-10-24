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

// failureRepositoryStub provides a configured resolved principal and counts owner failure/expiry
// calls while leaving unrelated repository methods unsupported.
// failureRepositoryStub предоставляет заданный разрешённый principal и считает вызовы отказа и
// истечения владельца при неподдерживаемых остальных repository методах.
type failureRepositoryStub struct {
	repo.Repository
	resolved value.Principal
	calls    int
}

// ResolvePrincipal returns the configured owner-resolved synthetic principal instead of trusting
// the principal supplied by the test request.
// ResolvePrincipal возвращает заданный разрешённый владельцем синтетический principal вместо
// доверия principal из тестового запроса.
func (s *failureRepositoryStub) ResolvePrincipal(context.Context, value.Principal) (value.Principal, error) {
	return s.resolved, nil
}

// FailAdmission counts one authoritative failure call and returns a synthetic FAILED result
// without external effects or persistent state.
// FailAdmission считает один авторитетный вызов отказа и возвращает синтетический FAILED результат
// без внешних эффектов либо устойчивого состояния.
func (s *failureRepositoryStub) FailAdmission(context.Context, repo.AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error) {
	s.calls++
	return entity.RoleImageAdmissionFailure{State: "FAILED"}, nil
}

// ExpireAdmission counts an owner expiry call and returns synthetic FAILED with the owner-only
// lease-expired code.
// ExpireAdmission считает вызов истечения владельца и возвращает синтетический FAILED с кодом
// истечения lease, принадлежащим владельцу.
func (s *failureRepositoryStub) ExpireAdmission(context.Context, repo.AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error) {
	s.calls++
	return entity.RoleImageAdmissionFailure{State: "FAILED", ErrorCode: "ADMISSION_LEASE_EXPIRED"}, nil
}

// TestAdmissionFailureRequiresClosedExactWorkerAuthority checks closed worker failure codes, exact
// fail/expire permissions and complete pins reject invalid input before owner calls, including
// caller-forged expiry.
// TestAdmissionFailureRequiresClosedExactWorkerAuthority проверяет закрытые коды отказа worker,
// точные fail/expire permissions и полные pins с отказом неверному вводу до вызовов владельца,
// включая поддельное истечение caller.
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
