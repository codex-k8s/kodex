package kubernetesoperation

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// streamResolverFixture checks exact lineage and supports concurrent idle revocation.
// streamResolverFixture проверяет exact lineage и поддерживает concurrent idle revocation.
type streamResolverFixture struct {
	snapshot  value.KubernetesOperationSnapshot
	binding   managedkubernetes.Binding
	streamRef string
	calls     atomic.Int32
	revoked   atomic.Bool
}

// ResolveKubernetesStream refuses foreign or revoked streams without a base-read fallback.
// ResolveKubernetesStream отклоняет чужие либо отозванные streams без base-read fallback.
func (owner *streamResolverFixture) ResolveKubernetesStream(ctx context.Context, binding managedkubernetes.Binding, ref string) (value.KubernetesOperationSnapshot, error) {
	owner.calls.Add(1)
	if ctx.Err() != nil || owner.revoked.Load() || binding != owner.binding || ref != owner.streamRef {
		return value.KubernetesOperationSnapshot{}, errors.New("synthetic stream denied")
	}
	return owner.snapshot, nil
}

// streamTargetFixture records target checks independently from owner admission.
// streamTargetFixture фиксирует target checks независимо от owner admission.
type streamTargetFixture struct {
	deadline time.Time
	err      error
	calls    atomic.Int32
	check    func(context.Context)
}

// VerifyKubernetesStreamAccess supplies only a bounded synthetic physical-access deadline.
// VerifyKubernetesStreamAccess предоставляет только bounded synthetic physical-access deadline.
func (target *streamTargetFixture) VerifyKubernetesStreamAccess(ctx context.Context, _ value.KubernetesOperationSnapshot) (time.Time, error) {
	target.calls.Add(1)
	if target.check != nil {
		target.check(ctx)
	}
	return target.deadline, target.err
}

// currentAccessFixture builds valid canonical exec metadata and private identity pins.
// currentAccessFixture создаёт valid canonical exec metadata и private identity pins.
func currentAccessFixture(t *testing.T) (*CurrentStreamAccess, *streamResolverFixture, *streamTargetFixture, managedkubernetes.StreamReceiptOrigin, value.KubernetesStreamGrant) {
	t.Helper()
	_, binding, grant, _, _, _ := inputFrameFixture(t)
	input, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.exec", []byte(`{"group":"","version":"v1","resource":"pods","namespace":"example","name":"web","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17","options":{"command":["echo","example"],"timeoutSeconds":10}}`))
	if err != nil {
		t.Fatal(err)
	}
	name, err := managedkubernetes.IdentitySecretName("identity_synthetic", 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := value.KubernetesOperationSnapshot{Digest: strings.Repeat("a", 64), Operation: "kubernetes.exec", Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT", IdentityRef: "identity_synthetic", SubjectDigest: strings.Repeat("b", 64), IdentityGeneration: 1,
		Identity: value.KubernetesIdentityLocator{IdentityRef: "identity_synthetic", TargetRef: "target_synthetic", Namespace: "kodex-kubernetes-identities", Name: name, UID: "24b97594-dda7-4f98-84c2-ce32bf76d739", ResourceVersion: "12", ContentSHA256: strings.Repeat("e", 64), Revision: 1},
		Target:   value.KubernetesTarget{ID: "target_synthetic", Revision: 2, ClusterUID: "synthetic-cluster", ServerURL: "https://kubernetes.example.invalid", TLSServerName: "kubernetes.example.invalid", CertificateAuthorityHash: strings.Repeat("f", 64), AddressPins: []string{"192.0.2.1"}},
		Input:    input, NormalizedInput: canonical, ExpiresAt: time.Now().Add(time.Minute), PlanRef: "plan_synthetic", PlanDigest: strings.Repeat("c", 64), PlanRevision: 1, ProtectedNamespaces: []string{"kodex-system"}}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: snapshot.Digest, BindingDigest: digest, ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: grant.Ref, StreamGeneration: binding.Generation, Operation: snapshot.Operation, TargetRef: snapshot.Target.ID, TargetRevision: int64(snapshot.Target.Revision)}
	owner := &streamResolverFixture{snapshot: snapshot, binding: binding, streamRef: grant.Ref}
	target := &streamTargetFixture{deadline: time.Now().Add(2 * time.Minute)}
	access, err := NewCurrentStreamAccess(owner, target, binding, origin, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return access, owner, target, origin, grant
}

// TestCurrentStreamAccessRenewal caps a renewed lease by current selected-credential lifetime.
// TestCurrentStreamAccessRenewal ограничивает renewed lease текущим selected-credential lifetime.
func TestCurrentStreamAccessRenewal(t *testing.T) {
	access, owner, target, origin, _ := currentAccessFixture(t)
	first, err := access.AuthorizeStream(t.Context(), owner.binding, origin)
	if err != nil || !first.Equal(owner.snapshot.ExpiresAt) {
		t.Fatal("owner lease not enforced")
	}
	owner.snapshot.ExpiresAt = target.deadline.Add(time.Minute)
	next, err := access.AuthorizeStream(t.Context(), owner.binding, origin)
	if err != nil || !next.Equal(target.deadline) || !next.After(first) {
		t.Fatal("renewal lost target bound")
	}
}

// TestCurrentStreamAccessPins includes fields deliberately hidden by public JSON serialization.
// TestCurrentStreamAccessPins включает поля, намеренно скрытые public JSON serialization.
func TestCurrentStreamAccessPins(t *testing.T) {
	for name, mutate := range map[string]func(*value.KubernetesOperationSnapshot){
		"identity UID":       func(s *value.KubernetesOperationSnapshot) { s.Identity.UID = "34b97594-dda7-4f98-84c2-ce32bf76d739" },
		"identity RV":        func(s *value.KubernetesOperationSnapshot) { s.Identity.ResourceVersion = "13" },
		"identity content":   func(s *value.KubernetesOperationSnapshot) { s.Identity.ContentSHA256 = strings.Repeat("d", 64) },
		"identity namespace": func(s *value.KubernetesOperationSnapshot) { s.Identity.Namespace = "other" },
		"identity revision": func(s *value.KubernetesOperationSnapshot) {
			s.Identity.Revision = 2
			s.Identity.Name, _ = managedkubernetes.IdentitySecretName(s.IdentityRef, 2)
		},
		"identity generation": func(s *value.KubernetesOperationSnapshot) { s.IdentityGeneration++ },
		"subject":             func(s *value.KubernetesOperationSnapshot) { s.SubjectDigest = strings.Repeat("d", 64) },
		"route":               func(s *value.KubernetesOperationSnapshot) { s.Target.ServerURL = "https://other.example.invalid" },
		"address":             func(s *value.KubernetesOperationSnapshot) { s.Target.AddressPins[0] = "192.0.2.2" },
		"protected namespace": func(s *value.KubernetesOperationSnapshot) { s.ProtectedNamespaces[0] = "other" },
		"policy":              func(s *value.KubernetesOperationSnapshot) { s.ApprovalPolicy = "NONE" },
		"plan ref":            func(s *value.KubernetesOperationSnapshot) { s.PlanRef = "plan_other" },
		"plan revision":       func(s *value.KubernetesOperationSnapshot) { s.PlanRevision++ },
		"plan digest":         func(s *value.KubernetesOperationSnapshot) { s.PlanDigest = strings.Repeat("d", 64) },
		"expiry":              func(s *value.KubernetesOperationSnapshot) { s.ExpiresAt = time.Now().Add(-time.Second) },
		"canonical input": func(s *value.KubernetesOperationSnapshot) {
			s.NormalizedInput = append([]byte(" "), s.NormalizedInput...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			access, owner, target, origin, _ := currentAccessFixture(t)
			mutate(&owner.snapshot)
			until, err := access.AuthorizeStream(t.Context(), owner.binding, origin)
			if err == nil || !until.IsZero() || target.calls.Load() != 0 {
				t.Fatal("changed owner pins reached target")
			}
		})
	}
}

// TestCurrentStreamAccessForeignAndRevoked refuses foreign lineage before I/O and revocation before native checks.
// TestCurrentStreamAccessForeignAndRevoked отклоняет foreign lineage до I/O и revocation до native checks.
func TestCurrentStreamAccessForeignAndRevoked(t *testing.T) {
	access, owner, target, origin, _ := currentAccessFixture(t)
	foreign := origin
	foreign.StreamRef = "stream_other"
	if _, err := access.AuthorizeStream(t.Context(), owner.binding, foreign); err == nil || owner.calls.Load() != 0 {
		t.Fatal("foreign stream reached owner")
	}
	binding := owner.binding
	binding.Generation++
	if _, err := access.AuthorizeStream(t.Context(), binding, origin); err == nil || owner.calls.Load() != 0 {
		t.Fatal("foreign generation reached owner")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := access.AuthorizeStream(ctx, owner.binding, origin); err == nil || owner.calls.Load() != 0 {
		t.Fatal("canceled caller reached owner")
	}
	owner.revoked.Store(true)
	if _, err := access.AuthorizeStream(t.Context(), owner.binding, origin); err == nil || target.calls.Load() != 0 {
		t.Fatal("revoked stream reached target")
	}
}

// TestCurrentStreamAccessTargetUncertainty denies credit on expired credentials, target failure or cancellation.
// TestCurrentStreamAccessTargetUncertainty запрещает credit при expiry, target failure либо cancellation.
func TestCurrentStreamAccessTargetUncertainty(t *testing.T) {
	for _, kind := range []string{"expired", "error", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			access, owner, target, origin, _ := currentAccessFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "expired":
				target.deadline = time.Now().Add(-time.Second)
			case "error":
				target.err = errors.New("synthetic target denial")
			case "canceled":
				target.check = func(context.Context) { cancel() }
			}
			until, err := access.AuthorizeStream(ctx, owner.binding, origin)
			if err == nil || !until.IsZero() || target.calls.Load() != 1 {
				t.Fatal("target uncertainty minted credit")
			}
		})
	}
}

// TestCurrentStreamAccessIdleRevocation joins the physical stream without any client poll.
// TestCurrentStreamAccessIdleRevocation ожидает physical stream без client poll.
func TestCurrentStreamAccessIdleRevocation(t *testing.T) {
	access, resolver, target, origin, grant := currentAccessFixture(t)
	target.deadline = time.Now().Add(300 * time.Millisecond)
	owner := &activeOwnerFixture{expires: grant.ExpiresAt, closed: make(chan targetrepo.StreamClosing, 1)}
	transport := &activeTransportFixture{reads: make(chan streamReadEvent, 1), closed: make(chan struct{})}
	handshake := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: "transport_synthetic", TransportGeneration: 3, Protocol: string(managedkubernetes.WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("d", 64)}
	stream, err := StartActiveStream(t.Context(), owner, access, transport, resolver.binding, grant, handshake)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.Close() })
	resolver.revoked.Store(true)
	select {
	case <-stream.done:
	case <-time.After(2 * time.Second):
		t.Fatal("idle revocation did not join stream")
	}
	receipt := <-owner.closed
	if !receipt.TransportJoined || receipt.TerminalState != "LOST" || owner.frameBegins.Load() != 0 || transport.writeCount.Load() != 0 {
		t.Fatal("revocation retained effects or omitted join")
	}
	select {
	case <-stream.readerDone:
	default:
		t.Fatal("reader not joined")
	}
}
