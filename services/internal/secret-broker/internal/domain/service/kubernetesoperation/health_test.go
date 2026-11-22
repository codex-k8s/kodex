package kubernetesoperation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// healthOwnerFixture models one owner-consumed credit and independently controlled final application.
// It keeps invocation and provider behavior outside the disposable unit's assertions.
// healthOwnerFixture моделирует owner-consumed credit и независимо управляемый final application.
// Invocation и provider behavior не входят в утверждения disposable unit.
type healthOwnerFixture struct {
	admission                 targetrepo.IdentityHealthAdmission
	begins, completes         int
	beginError, completeError error
	applied                   bool
	completion                targetrepo.IdentityHealthCompletion
}

// BeginKubernetesIdentityHealth issues only the fixture's first admission, preserving lost-ACK closure.
// BeginKubernetesIdentityHealth выдаёт лишь первый fixture admission, сохраняя lost-ACK closure.
func (owner *healthOwnerFixture) BeginKubernetesIdentityHealth(context.Context, managedkubernetes.Binding) (targetrepo.IdentityHealthAdmission, error) {
	owner.begins++
	if owner.begins != 1 {
		return targetrepo.IdentityHealthAdmission{}, targetrepo.ErrHealthUnavailable
	}
	return owner.admission, owner.beginError
}

// CompleteKubernetesIdentityHealth verifies independent cleanup context and retains the exact outcome.
// CompleteKubernetesIdentityHealth проверяет independent cleanup context и сохраняет exact outcome.
func (owner *healthOwnerFixture) CompleteKubernetesIdentityHealth(ctx context.Context, input targetrepo.IdentityHealthCompletion) (targetrepo.IdentityHealthReceipt, error) {
	owner.completes++
	owner.completion = input
	if ctx.Err() != nil {
		return targetrepo.IdentityHealthReceipt{}, errors.New("cleanup context was cancelled")
	}
	state := "FAILED"
	if input.Outcome == "AUTHENTICATED" {
		state = "SUCCEEDED"
	}
	return targetrepo.IdentityHealthReceipt{TestRef: input.Binding.ConnectionTestRef, State: state, Applied: owner.applied}, owner.completeError
}

// healthMaterialFixture returns one owned material buffer so tests can inspect zeroization after join.
// healthMaterialFixture возвращает owned material buffer для проверки zeroization после join.
type healthMaterialFixture struct {
	material []byte
	err      error
	reads    int
}

// ReadKubernetesIdentity returns the owned slice, including a deliberate partial-error case.
// ReadKubernetesIdentity возвращает owned slice, включая deliberate partial-error case.
func (reader *healthMaterialFixture) ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error) {
	reader.reads++
	return reader.material, reader.err
}

// healthVerifierFixture exposes only a synthetic digest and can cancel the caller before completion.
// healthVerifierFixture выдаёт лишь synthetic digest и может отменить caller до completion.
type healthVerifierFixture struct {
	digest       string
	err          error
	cancel       context.CancelFunc
	observations int
}

// ObserveKubernetesIdentity counts one selected-identity observation without any alternate identity port.
// ObserveKubernetesIdentity считает один selected-identity observation без alternate identity port.
func (verifier *healthVerifierFixture) ObserveKubernetesIdentity(context.Context, []byte, value.KubernetesTarget) (string, error) {
	verifier.observations++
	if verifier.cancel != nil {
		verifier.cancel()
	}
	return verifier.digest, verifier.err
}

// healthFixture supplies exact disposable owner pins without involving a live Kubernetes account.
// healthFixture задаёт exact disposable owner pins без обращения к live Kubernetes account.
func healthFixture(t *testing.T) (managedkubernetes.Binding, *healthOwnerFixture, *healthMaterialFixture, *healthVerifierFixture) {
	t.Helper()
	identity := value.KubernetesIdentityLocator{IdentityRef: "bki_disposable_health", TargetRef: "target_disposable", Revision: 1,
		Namespace: "kodex-kubernetes-identities", UID: "629c395b-32aa-425b-814d-5cd9a9a42da6", ResourceVersion: "7", ContentSHA256: strings.Repeat("a", 64)}
	identity.Name, _ = managedkubernetes.IdentitySecretName(identity.IdentityRef, identity.Revision)
	admission := targetrepo.IdentityHealthAdmission{AttemptRef: "khp_disposable_health", SnapshotDigest: strings.Repeat("b", 64), Identity: identity,
		Target: value.KubernetesTarget{ID: identity.TargetRef, Revision: 1}, ExpiresAt: time.Now().Add(20 * time.Second)}
	binding := managedkubernetes.Binding{ConnectionTestRef: "pct_disposable_health", WorkloadInstance: "worker_disposable", LeaseRef: "lea_disposable", Fence: "fnc_disposable", Generation: 1, InputDigest: strings.Repeat("c", 64)}
	return binding, &healthOwnerFixture{admission: admission, applied: true}, &healthMaterialFixture{material: []byte("disposable-private-material")}, &healthVerifierFixture{digest: strings.Repeat("d", 64)}
}

// TestHealthOneShotCompletionAndMaterialLifetime covers dependency failures and final owner closure.
// These tests prove orchestration; actual TLS authentication is qualified in the target adapter tests.
// TestHealthOneShotCompletionAndMaterialLifetime проверяет dependency failures и final owner closure.
// Проверяет orchestration; actual TLS authentication квалифицируется target adapter tests.
func TestHealthOneShotCompletionAndMaterialLifetime(t *testing.T) {
	for _, scenario := range []string{"authenticated", "lost-begin", "partial-read", "denied", "malformed-subject", "lost-complete", "revoked", "caller-cancel"} {
		t.Run(scenario, func(t *testing.T) {
			binding, owner, reader, verifier := healthFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "lost-begin":
				owner.beginError = targetrepo.ErrHealthUnavailable
			case "partial-read":
				reader.err = targetrepo.ErrHealthUnavailable
			case "denied":
				verifier.err = targetrepo.ErrHealthAuthenticationRejected
			case "malformed-subject":
				verifier.digest = "unproven"
			case "lost-complete":
				owner.completeError = targetrepo.ErrHealthUnavailable
			case "revoked":
				owner.applied = false
			case "caller-cancel":
				verifier.cancel = cancel
			}
			service, err := NewHealth(owner, reader, verifier, owner.admission.Identity.Namespace)
			if err != nil {
				t.Fatal("construct disposable health")
			}
			receipt, err := service.Execute(ctx, binding)
			if scenario == "lost-begin" {
				if err == nil || reader.reads != 0 || owner.completes != 0 {
					t.Fatal("uncertain admission performed private work")
				}
			} else {
				if owner.completes != 1 || reader.reads != 1 || string(reader.material) != strings.Repeat("\x00", len(reader.material)) {
					t.Fatal("material was retained or completion did not join")
				}
				if scenario == "lost-complete" || scenario == "revoked" {
					if err == nil || receipt.TestRef != "" {
						t.Fatal("unapplied or uncertain result escaped owner closure")
					}
				} else if err != nil || receipt.Applied != true {
					t.Fatal("known owner result was lost")
				}
				if scenario == "partial-read" && (verifier.observations != 0 || owner.completion.Outcome != "UNAVAILABLE") {
					t.Fatal("partial material was parsed or reported as authenticated")
				}
				if scenario == "denied" && owner.completion.Outcome != "AUTH_REJECTED" {
					t.Fatal("denied observation changed classification")
				}
			}
			_, err = service.Execute(t.Context(), binding)
			if err == nil || reader.reads > 1 || verifier.observations > 1 || owner.completes > 1 {
				t.Fatal("original health claim replayed external work")
			}
		})
	}
}
