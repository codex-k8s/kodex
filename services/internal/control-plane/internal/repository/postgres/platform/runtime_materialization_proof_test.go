package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

func TestRuntimeMaterializationProofRejectsUntrustedLocatorBeforeDatabase(t *testing.T) {
	mutations := map[string]func(*platformrepo.ProofPrincipalInput){
		"wrong-worker":           func(input *platformrepo.ProofPrincipalInput) { input.CallerWorkload = "control-api-gateway" },
		"wrong-actor":            func(input *platformrepo.ProofPrincipalInput) { input.ExternalActorID = "fixture-other-actor" },
		"wrong-tenant":           func(input *platformrepo.ProofPrincipalInput) { input.ExternalTenantID = "fixture-other-tenant" },
		"missing-project":        func(input *platformrepo.ProofPrincipalInput) { input.ProjectRef = "" },
		"assistant-with-project": func(input *platformrepo.ProofPrincipalInput) { input.Operation = assistantMaterializationOperation },
		"missing-digest":         func(input *platformrepo.ProofPrincipalInput) { input.RequestDigestSHA256 = "" },
		"invalid-digest":         func(input *platformrepo.ProofPrincipalInput) { input.RequestDigestSHA256 = strings.Repeat("z", 64) },
		"noncanonical-digest":    func(input *platformrepo.ProofPrincipalInput) { input.RequestDigestSHA256 = strings.Repeat("A", 64) },
		"short-digest":           func(input *platformrepo.ProofPrincipalInput) { input.RequestDigestSHA256 = strings.Repeat("a", 62) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			input := platformrepo.ProofPrincipalInput{
				CallerWorkload: "runtime-controller", Operation: runtimeMaterializationOperation,
				ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
				ProjectRef: "prj_fixture", RequestDigestSHA256: strings.Repeat("a", 64),
			}
			mutate(&input)
			result, err := (&Repository{}).ResolveProofAuthority(context.Background(), input)
			if !errors.Is(err, errs.ErrForbidden) || result != (platformrepo.ProofAuthority{}) {
				t.Fatalf("untrusted runtime locator accepted: %v", err)
			}
		})
	}
}
