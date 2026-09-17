package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

func TestTrustedProjectionRejectsCallerAssignedScopeAndMixedProfiles(t *testing.T) {
	trusted := &Repository{trustedCluster: true}
	protected := &Repository{}
	selector := platformrepo.CredentialProjectionAuthority{RPCProfile: transportprofile.TrustedCluster,
		CallerWorkloadID: "runtime-controller", CallerFullMethod: runtimeProjectionMethod}
	if !trusted.validRuntimeProjectionAuthority(selector, false) || protected.validRuntimeProjectionAuthority(selector, false) ||
		trusted.validRuntimeProjectionAuthority(selector, true) {
		t.Fatal("profile or selector boundary changed")
	}
	for name, mutate := range map[string]func(*platformrepo.CredentialProjectionAuthority){
		"actor":      func(a *platformrepo.CredentialProjectionAuthority) { a.ActorID = uuid.NewString() },
		"tenant":     func(a *platformrepo.CredentialProjectionAuthority) { a.TenantID = uuid.NewString() },
		"project":    func(a *platformrepo.CredentialProjectionAuthority) { a.ProjectID = uuid.NewString() },
		"expiry":     func(a *platformrepo.CredentialProjectionAuthority) { a.ExpiresAt = time.Now().Add(time.Minute) },
		"proof":      func(a *platformrepo.CredentialProjectionAuthority) { a.ProofJTI = uuid.NewString() },
		"generation": func(a *platformrepo.CredentialProjectionAuthority) { a.CallerCredentialRevision = 1 },
		"source":     func(a *platformrepo.CredentialProjectionAuthority) { a.SourceRevision = 1 },
		"digest":     func(a *platformrepo.CredentialProjectionAuthority) { a.SourceDigestSHA256 = strings.Repeat("a", 64) },
		"caller":     func(a *platformrepo.CredentialProjectionAuthority) { a.CallerWorkloadID = "secret-broker" },
		"method":     func(a *platformrepo.CredentialProjectionAuthority) { a.CallerFullMethod = sttProjectionMethod },
		"profile":    func(a *platformrepo.CredentialProjectionAuthority) { a.RPCProfile = "insecure" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := selector
			mutate(&candidate)
			if trusted.validRuntimeProjectionAuthority(candidate, false) {
				t.Fatal("caller assigned authority was accepted")
			}
		})
	}
	snapshot := selector
	snapshot.ActorID, snapshot.TenantID, snapshot.ProjectID = uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot.SourceRevision, snapshot.CallerCredentialRevision = 4, 2
	snapshot.SourceDigestSHA256, snapshot.ExpiresAt = strings.Repeat("b", 64), time.Now().Add(time.Minute)
	if !trusted.validRuntimeProjectionAuthority(snapshot, true) || trusted.validRuntimeProjectionAuthority(snapshot, false) ||
		protected.validRuntimeProjectionAuthority(snapshot, true) {
		t.Fatal("snapshot is not restricted to trusted recovery")
	}
	snapshot.RPCProfile, snapshot.ProofJTI = "", uuid.NewString()
	if trusted.validRuntimeProjectionAuthority(snapshot, true) || !protected.validRuntimeProjectionAuthority(snapshot, true) {
		t.Fatal("legacy proof crossed the trusted profile boundary")
	}
}
