package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func TestTrustedSTTProjectionBindsOwnerSession(t *testing.T) {
	repository := &Repository{trustedCluster: true}
	p := value.Principal{ActorID: "11111111-1111-4111-8111-111111111111", AuthorityTenant: "22222222-2222-4222-8222-222222222222",
		CallerWorkload: "secret-broker", Permission: platformrepo.TrustedSTTCredentialOperation, CredentialRevision: 9}
	authority := platformrepo.CredentialProjectionAuthority{RPCProfile: transportprofile.TrustedCluster,
		ActorID: p.ActorID, TenantID: p.AuthorityTenant, SourceRevision: 9, CallerCredentialRevision: 9,
		SourceDigestSHA256: strings.Repeat("a", 64), CallerWorkloadID: "stt-tts-service", CallerFullMethod: sttProjectionMethod,
		ExpiresAt: time.Now().Add(20 * time.Second)}
	if !repository.validSTTProjectionAuthority(p, authority) {
		t.Fatal("valid owner-bound projection rejected")
	}
	if (&Repository{}).validSTTProjectionAuthority(p, authority) {
		t.Fatal("protected profile accepted trusted payload")
	}
	for _, mutate := range []func(*value.Principal, *platformrepo.CredentialProjectionAuthority){
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.RPCProfile = ""
			a.ProofJTI = "33333333-3333-4333-8333-333333333333"
		},
		func(p *value.Principal, _ *platformrepo.CredentialProjectionAuthority) {
			p.ActorID = "33333333-3333-4333-8333-333333333333"
		},
		func(p *value.Principal, _ *platformrepo.CredentialProjectionAuthority) {
			p.AuthorityTenant = "33333333-3333-4333-8333-333333333333"
		},
		func(p *value.Principal, _ *platformrepo.CredentialProjectionAuthority) { p.CredentialRevision++ },
		func(p *value.Principal, _ *platformrepo.CredentialProjectionAuthority) {
			p.CallerWorkload = "stt-tts-service"
		},
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) { a.ProjectID = "prj_untrusted" },
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.ProofJTI = "33333333-3333-4333-8333-333333333333"
		},
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.CallerFullMethod = runtimeProjectionMethod
		},
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.SourceDigestSHA256 = "invalid"
		},
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.ExpiresAt = time.Now().Add(-time.Second)
		},
		func(_ *value.Principal, a *platformrepo.CredentialProjectionAuthority) {
			a.ExpiresAt = time.Now().Add(time.Minute)
		},
	} {
		principal, candidate := p, authority
		mutate(&principal, &candidate)
		if repository.validSTTProjectionAuthority(principal, candidate) {
			t.Fatal("projection escaped verified session boundary")
		}
	}
}
