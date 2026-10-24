package roleimage

import (
	"context"
	"strings"
	"testing"

	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// recoveryTerminalRepositoryStub extends resolved failure authority with capture of the
// reconstructed original claim key used for terminal recovery reads.
// recoveryTerminalRepositoryStub дополняет разрешённые полномочия отказа сохранением
// восстановленного исходного claim key чтения terminal recovery.
type recoveryTerminalRepositoryStub struct {
	failureRepositoryStub
	key string
}

// GetAdmissionRecoveryTerminal records the exact recovery claim key and returns synthetic
// CANCELLED proof while incrementing the owner-call counter.
// GetAdmissionRecoveryTerminal записывает точный recovery claim key и возвращает синтетический
// CANCELLED proof с увеличением счётчика вызовов владельца.
func (s *recoveryTerminalRepositoryStub) GetAdmissionRecoveryTerminal(_ context.Context, _ value.Principal, key string) (repo.AdmissionTerminalProof, error) {
	s.calls++
	s.key = key
	return repo.AdmissionTerminalProof{State: "CANCELLED"}, nil
}

// TestAdmissionRecoveryTerminalUsesOwnerResolvedControllerAndOriginalClaim checks the resolved
// controller and exact recovery operation derive the bridge original-claim UUID, rejecting worker,
// permission or run-ID substitutions before owner access.
// TestAdmissionRecoveryTerminalUsesOwnerResolvedControllerAndOriginalClaim проверяет получение
// исходного claim UUID bridge разрешённым controller с точной recovery операцией и отказ подменам
// worker, permission либо run ID до доступа к владельцу.
func TestAdmissionRecoveryTerminalUsesOwnerResolvedControllerAndOriginalClaim(t *testing.T) {
	p := value.Principal{ActorID: "svc_controller", AuthorityTenant: "org_installation", Permission: "platform.role-images.admission.recovery-terminal.get", CallerWorkload: "image-admission-controller", CorrelationRef: "cor_recovery", CredentialRevision: 1}
	runID := "v20261006050000-" + strings.Repeat("a", 40)
	for _, scenario := range []string{"valid", "worker", "permission", "run"} {
		t.Run(scenario, func(t *testing.T) {
			principal, run := p, runID
			switch scenario {
			case "worker":
				principal.CallerWorkload = "image-admission"
			case "permission":
				principal.Permission = "platform.role-images.supply-work.get"
			case "run":
				run = "bad"
			}
			stub := &recoveryTerminalRepositoryStub{failureRepositoryStub: failureRepositoryStub{resolved: principal}}
			catalog, _ := NewCatalog([]Environment{validEnvironment(true, true)})
			service, _ := New(stub, catalog)
			_, err := service.GetAdmissionRecoveryTerminal(t.Context(), p, run)
			if scenario == "valid" {
				want := uuid.NewSHA1(uuid.NameSpaceOID, []byte("image-admission-bridge\x00claim\x00"+runID)).String()
				if err != nil || stub.calls != 1 || stub.key != want {
					t.Fatal("original canonical claim key was not resolved")
				}
			} else if err == nil || stub.calls != 0 {
				t.Fatal("invalid recovery authority reached owner")
			}
		})
	}
}
