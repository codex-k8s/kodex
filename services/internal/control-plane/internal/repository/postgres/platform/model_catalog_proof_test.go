package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

func TestProviderModelCatalogProofRejectsUntrustedPrincipalBeforeLookup(t *testing.T) {
	base := platformrepo.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: platformrepo.ProviderModelCatalogOperation, RequestDigestSHA256: strings.Repeat("a", 64), ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation"}
	for name, mutate := range map[string]func(*platformrepo.ProofPrincipalInput){
		"gateway": func(p *platformrepo.ProofPrincipalInput) { p.CallerWorkload = "control-api-gateway" },
		"other operation": func(p *platformrepo.ProofPrincipalInput) {
			p.Operation = "platform.provider-credentials.model-catalog.observe"
		},
		"project authority": func(p *platformrepo.ProofPrincipalInput) { p.ProjectRef = "project_foreign" },
		"missing digest":    func(p *platformrepo.ProofPrincipalInput) { p.RequestDigestSHA256 = "" },
		"uppercase digest":  func(p *platformrepo.ProofPrincipalInput) { p.RequestDigestSHA256 = strings.Repeat("A", 64) },
		"forged actor":      func(p *platformrepo.ProofPrincipalInput) { p.ExternalActorID = "actor_foreign" },
		"forged tenant":     func(p *platformrepo.ProofPrincipalInput) { p.ExternalTenantID = "tenant_foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			result, err := (&Repository{}).ResolveProviderModelCatalogProof(t.Context(), input)
			if !errors.Is(err, errs.ErrForbidden) || result != (platformrepo.ProofAuthority{}) {
				t.Fatalf("unexpected authority result: %+v, %v", result, err)
			}
			if input.Operation == platformrepo.ProviderModelCatalogOperation {
				result, err = (&Repository{}).ResolveProofAuthority(t.Context(), input)
				if !errors.Is(err, errs.ErrForbidden) || result != (platformrepo.ProofAuthority{}) {
					t.Fatalf("unexpected dispatched authority result: %+v, %v", result, err)
				}
			}
		})
	}
}
