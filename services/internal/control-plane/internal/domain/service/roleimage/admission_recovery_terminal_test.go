package roleimage

import (
	"context"
	"strings"
	"testing"

	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type recoveryTerminalRepositoryStub struct {
	failureRepositoryStub
	key string
}

func (s *recoveryTerminalRepositoryStub) GetAdmissionRecoveryTerminal(_ context.Context, _ value.Principal, key string) (repo.AdmissionTerminalProof, error) {
	s.calls++
	s.key = key
	return repo.AdmissionTerminalProof{State: "CANCELLED"}, nil
}
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
