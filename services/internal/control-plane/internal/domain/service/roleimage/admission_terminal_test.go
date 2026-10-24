package roleimage

import (
	"context"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"strings"
	"testing"
)

// terminalRepositoryStub reuses configured owner-resolved failure identity and call accounting for
// the distinct terminal-proof repository operation.
// terminalRepositoryStub использует заданную разрешённую владельцем failure identity и учёт
// вызовов для отдельной repository операции terminal proof.
type terminalRepositoryStub struct{ failureRepositoryStub }

// GetAdmissionTerminal counts the exact terminal-proof owner read and returns synthetic CANCELLED
// state without deciding a lifecycle transition.
// GetAdmissionTerminal считает точное чтение terminal proof владельца и возвращает синтетическое
// CANCELLED состояние без решения о lifecycle переходе.
func (s *terminalRepositoryStub) GetAdmissionTerminal(context.Context, repo.AdmissionTerminalInput) (repo.AdmissionTerminalProof, error) {
	s.calls++
	return repo.AdmissionTerminalProof{State: "CANCELLED"}, nil
}

// TestAdmissionTerminalReadRequiresFreshExactWorkerAuthority checks current resolved worker
// permission, original claim key, attempt and evidence pins, denying foreign authority or
// malformed terminal proof coordinates before owner reads.
// TestAdmissionTerminalReadRequiresFreshExactWorkerAuthority проверяет текущие разрешённые worker
// permission, исходный claim key, attempt и evidence pins с отказом чужим полномочиям либо
// неверным координатам terminal proof до чтений владельца.
func TestAdmissionTerminalReadRequiresFreshExactWorkerAuthority(t *testing.T) {
	p := value.Principal{ActorID: "svc_admission", AuthorityTenant: "org_installation", Permission: "platform.role-images.admission.terminal.get", CorrelationRef: "cor_terminal", CallerWorkload: "image-admission", CredentialRevision: 2}
	input := repo.AdmissionTerminalInput{ClaimIdempotencyKey: "original-claim-key", AdmissionExpiryInput: repo.AdmissionExpiryInput{Principal: p, ExpectedAdmissionAttemptRef: "imgadm_12345678", ExpectedAdmissionAttempt: 1, ArtifactRef: "imgart_12345678", BuildRef: "imgbld_12345678", ExpectedVersion: 2, ExpectedFence: 1, ExpectedAuthorityGeneration: 1, ExpectedBuildAttempt: 1, RecipeGeneration: 1, PolicyRevision: 1, ManifestDigest: "sha256:" + strings.Repeat("a", 64), ImmutableBuildSHA256: strings.Repeat("b", 64), ProvenanceSHA256: strings.Repeat("c", 64), PolicySHA256: strings.Repeat("d", 64), SpecSHA256: strings.Repeat("e", 64)}}
	catalog, _ := NewCatalog([]Environment{validEnvironment(true, true)})
	for _, scenario := range []string{"valid", "foreign_worker", "foreign_permission", "bad_key", "empty_attempt", "bad_digest", "bad_risk"} {
		t.Run(scenario, func(t *testing.T) {
			in, principal := input, p
			switch scenario {
			case "foreign_worker":
				principal.CallerWorkload = "image-admission-controller"
			case "foreign_permission":
				principal.Permission = "platform.role-images.admission.claim"
			case "bad_key":
				in.ClaimIdempotencyKey = "short"
			case "empty_attempt":
				in.ExpectedAdmissionAttempt = 0
			case "bad_digest":
				in.SpecSHA256 = "bad"
			case "bad_risk":
				in.RiskAcceptanceSHA256 = "bad"
			}
			stub := &terminalRepositoryStub{failureRepositoryStub: failureRepositoryStub{resolved: principal}}
			service, _ := New(stub, catalog)
			_, err := service.GetAdmissionTerminal(t.Context(), in)
			if scenario == "valid" {
				if err != nil || stub.calls != 1 {
					t.Fatal("valid exact worker read denied")
				}
			} else if err == nil || stub.calls != 0 {
				t.Fatal("invalid caller crossed terminal read boundary")
			}
		})
	}
}
