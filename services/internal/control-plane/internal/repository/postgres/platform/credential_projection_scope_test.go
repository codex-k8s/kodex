package platform

import (
	"strings"
	"testing"
	"time"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

func TestRuntimeCredentialProjectionScopeMatrix(t *testing.T) {
	base := platformrepo.CredentialProjectionAuthority{ActorID: uuid.NewString(), TenantID: uuid.NewString(),
		ProjectID: uuid.NewString(), ProofJTI: uuid.NewString(), SourceRevision: 1, CallerCredentialRevision: 1,
		SourceDigestSHA256: strings.Repeat("a", 64), CallerWorkloadID: "runtime-controller", CallerFullMethod: runtimeProjectionMethod,
		ExpiresAt: time.Now().Add(time.Minute)}
	for _, test := range []struct {
		name, method, project string
		allowed               bool
	}{
		{"project runtime", runtimeProjectionMethod, base.ProjectID, true},
		{"runtime cannot omit project", runtimeProjectionMethod, "", false},
		{"organization assistant", assistantProjectionMethod, "", true},
		{"assistant cannot use project", assistantProjectionMethod, base.ProjectID, false},
		{"unknown method", "/unknown", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			candidate.CallerFullMethod, candidate.ProjectID = test.method, test.project
			if validRuntimeProjectionAuthority(candidate) != test.allowed {
				t.Fatal("credential scope matrix mismatch")
			}
		})
	}
}
