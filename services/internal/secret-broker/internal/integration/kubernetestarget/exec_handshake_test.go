package kubernetestarget

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestNativeHandshakeReceiptUsesObservedTLS verifies actual token/certificate transport facts.
// Owner graph references are synthetic; their acceptance still belongs to the control-plane ledger.
// TestNativeHandshakeReceiptUsesObservedTLS проверяет actual token/certificate transport facts.
// Owner graph references synthetic; их допуск по-прежнему принадлежит control-plane ledger.
func TestNativeHandshakeReceiptUsesObservedTLS(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			credential, calls := transportFixtureWithHandler(t, certificate, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
				if connection != nil {
					defer connection.Close()
					_, _, _ = connection.ReadMessage()
				}
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"cat"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
			if err != nil {
				t.Fatal("observed handshake fixture failed")
			}
			defer stream.Close()
			origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: "stream_synthetic", StreamGeneration: 7, Operation: "kubernetes.exec", TargetRef: credential.target.ID, TargetRevision: int64(credential.target.Revision)}
			receipt, err := stream.HandshakeReceipt(origin)
			if err != nil || receipt.HTTPStatus != 101 || receipt.NegotiatedProtocol != PrivilegedWebSocketV5.WireProtocol() || receipt.PeerSPKISHA256 != pin.ServerPublicKeyHash || receipt.TransportRef != pin.Ref || receipt.TransportGeneration != int64(pin.Generation) {
				t.Fatal("receipt lost observed transport pins")
			}
			if _, err := receipt.Digest(); err != nil {
				t.Fatal("observed receipt cannot be verified by owner")
			}
			for _, change := range []func(*managedkubernetes.StreamReceiptOrigin){func(o *managedkubernetes.StreamReceiptOrigin) { o.TargetRef = "foreign_target" }, func(o *managedkubernetes.StreamReceiptOrigin) { o.TargetRevision++ }, func(o *managedkubernetes.StreamReceiptOrigin) { o.Operation = "kubernetes.attach" }, func(o *managedkubernetes.StreamReceiptOrigin) { o.ChildInputDigest = "" }} {
				candidate := origin
				change(&candidate)
				if _, err := stream.HandshakeReceipt(candidate); err == nil {
					t.Fatal("observed connection rebound to foreign origin")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("receipt generation opened another connection")
			}
		})
	}
}
