package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/util/httpstream"
)

// podStreamPreparationFixture joins actual native discovery and private identity loading with synthetic owner metadata.
// It does not substitute an owner ledger or claim a live Kubernetes effect.
// podStreamPreparationFixture соединяет actual native discovery/private identity loading с synthetic owner metadata.
// Fixture не заменяет owner ledger и не заявляет live Kubernetes effect.
func podStreamPreparationFixture(t *testing.T, handler http.HandlerFunc) (*PodStreamPreparer, value.KubernetesOperationSnapshot, targetrepo.OperationChild, *streamMaterialFixture, *resourceWireFixture) {
	t.Helper()
	wire := &resourceWireFixture{podBody: observedPodFixture(), streamHandler: handler}
	credential := resourceTLSFixture(t, wire)
	reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
	t.Cleanup(func() { clear(reader.raw) })
	locator := identityLocatorFixture(t, reader.raw, credential.target)
	input := managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Options: json.RawMessage(`{"command":["true"],"timeoutSeconds":30}`)}
	raw, err := managedkubernetes.EncodeOperationInput("kubernetes.exec", input)
	if err != nil {
		t.Fatal("encode fixture input")
	}
	_, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.exec", raw)
	if err != nil {
		t.Fatal("normalize fixture input")
	}
	snapshot := value.KubernetesOperationSnapshot{Digest: strings.Repeat("a", 64), Operation: "kubernetes.exec", Identity: locator, IdentityRef: locator.IdentityRef, IdentityGeneration: 1,
		SubjectDigest: subjectPin(t), Target: credential.target, Input: input, NormalizedInput: canonical, ExpiresAt: time.Now().Add(time.Minute)}
	factory, err := NewPodStreamPreparer(reader, locator.Namespace)
	if err != nil {
		t.Fatal("create private stream factory")
	}
	observed, err := factory.ObserveKubernetesPod(t.Context(), snapshot)
	if err != nil {
		t.Fatal("observe fixture Pod")
	}
	pin := privilegedPinFixture(t, credential, time.Now())
	pin.NodeName = "node-one"
	node := &value.KubernetesNodeTransport{Ref: pin.Ref, TargetRef: pin.TargetID, NodeName: pin.NodeName, NodeUID: pin.NodeUID,
		Generation: int64(pin.Generation), TargetRevision: int64(pin.TargetRevision), ServerOrigin: pin.ServerURL, TLSServerName: pin.TLSServerName,
		CertificateAuthoritySHA256: pin.CertificateAuthorityHash, ServerPublicKeySHA256: pin.ServerPublicKeyHash, CertificateAuthorityPEM: pin.CertificateAuthorityPEM,
		AddressIPs: pin.AddressPins, NotBefore: pin.NotBefore, ExpiresAt: pin.ExpiresAt, Protocol: pin.Protocol}
	observed.PodWitness.NodeUID = node.NodeUID
	digest := sha256.Sum256(canonical)
	child := targetrepo.OperationChild{Ref: "child_synthetic", Operation: snapshot.Operation, InputDigest: hex.EncodeToString(digest[:]), State: "READY", Resource: observed.Resource,
		Input: input, NormalizedInput: append([]byte(nil), canonical...), NodeTransport: node, Observation: &observed}
	return factory, snapshot, child, reader, wire
}

// podStreamAdmissionFixture creates only structural owner credit; actual Begin/Open owner tests remain separate.
// podStreamAdmissionFixture создаёт только structural owner credit; actual Begin/Open owner tests выполняются отдельно.
func podStreamAdmissionFixture(child targetrepo.OperationChild) (managedkubernetes.Binding, targetrepo.EffectAdmission) {
	binding := managedkubernetes.Binding{WorkloadInstance: "worker_synthetic", InvocationRef: "inv_synthetic", LeaseRef: "lease_synthetic", Fence: "synthetic-private-fence", Generation: 1, InputDigest: child.InputDigest}
	return binding, targetrepo.EffectAdmission{EffectRef: "effect_synthetic", EffectGeneration: 1, ChildInputDigest: child.InputDigest, ExpiresAt: time.Now().Add(3 * time.Second),
		Stream: &value.KubernetesStreamGrant{Ref: "stream_synthetic", Generation: 1, ExpiresAt: time.Now().Add(10 * time.Second), MaximumFrameBytes: 32768, MaximumTotalBytes: 128 << 10,
			InputChannels: []string{}, OutputChannels: []string{"STDOUT", "STDERR", "CONTROL"}}}
}

// TestPodStreamPreparationTransfersOneNativeTransport proves exact private selection and no reuse after one Open.
// Canceling the unary attempt and closing preparation after handoff cannot kill the worker-owned transport.
// TestPodStreamPreparationTransfersOneNativeTransport доказывает exact private selection и отсутствие reuse после одного Open.
// Отмена unary attempt и закрытие preparation после handoff не завершают worker-owned transport.
func TestPodStreamPreparationTransfersOneNativeTransport(t *testing.T) {
	peerDone := make(chan struct{})
	factory, snapshot, child, reader, wire := podStreamPreparationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(peerDone)
		if r.URL.Path != "/exec/example/selected/11111111-1111-4111-8111-111111111111/first" || r.URL.Query().Get("command") != "true" {
			t.Error("changed native effect route")
			w.WriteHeader(403)
			return
		}
		upgrader := websocket.Upgrader{Subprotocols: []string{PrivilegedWebSocketV5.WireProtocol()}}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
		_ = connection.SetWriteDeadline(time.Now().Add(3 * time.Second))
		_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte("native-output")...))
		_, _, _ = connection.ReadMessage()
	})
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if err != nil {
		t.Fatal("prepare exact native child")
	}
	defer prepared.Close()
	metadata, ready := prepared.Binding()
	if !ready || metadata.Observation.Validate() != nil || wire.streamCalls.Load() != 0 {
		t.Fatal("preparation opened a transport or lost final witness")
	}
	metadata.Observation.PodWitness.NodeUID = "changed"
	again, _ := prepared.Binding()
	if again.Observation.PodWitness.NodeUID != child.NodeTransport.NodeUID {
		t.Fatal("caller mutated prepared observation")
	}
	binding, admission := podStreamAdmissionFixture(child)
	attempt, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, receipt, err := prepared.Open(t.Context(), attempt, binding, admission)
	if err != nil {
		t.Fatal("open exact admitted native stream")
	}
	defer stream.Close()
	cancel()
	prepared.Close()
	read, err := stream.ReadStream()
	if err != nil || string(read.Payload) != "native-output" || receipt.Validate() != nil || receipt.Origin.EffectRef != admission.EffectRef || reader.calls.Load() != 3 || wire.streamCalls.Load() != 1 {
		t.Fatal("native handoff changed identity, receipt or stream lifetime")
	}
	clear(read.Payload)
	if duplicate, _, err := prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || duplicate != nil || wire.streamCalls.Load() != 1 {
		t.Fatal("one preparation reopened a stream")
	}
	_ = stream.Close()
	select {
	case <-peerDone:
	case <-time.After(time.Second):
		t.Fatal("native peer did not join")
	}
}

// TestPodStreamPreparationRejectsChangedAuthority closes changed child and fresh-revocation paths before handshake.
// TestPodStreamPreparationRejectsChangedAuthority закрывает changed child и fresh-revocation paths до handshake.
func TestPodStreamPreparationRejectsChangedAuthority(t *testing.T) {
	for _, variant := range []string{"missing UID", "body digest", "schema digest", "node", "revoke", "credential", "channels", "reconcile"} {
		t.Run(variant, func(t *testing.T) {
			factory, snapshot, child, reader, wire := podStreamPreparationFixture(t, nil)
			switch variant {
			case "missing UID":
				child.Observation.PodWitness.NodeUID = ""
			case "body digest":
				child.Observation.ObservedResourceDigest = strings.Repeat("0", 64)
			case "schema digest":
				child.Observation.RegisteredSchemaDigest = strings.Repeat("0", 64)
			case "node":
				child.NodeTransport.NodeName = "other-node"
			}
			prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
			if variant == "missing UID" || variant == "body digest" || variant == "schema digest" || variant == "node" {
				if err == nil || prepared != nil {
					t.Fatal("changed final authority produced native preparation")
				}
				return
			}
			if err != nil {
				t.Fatal("prepare negative admission fixture")
			}
			defer prepared.Close()
			binding, admission := podStreamAdmissionFixture(child)
			switch variant {
			case "revoke":
				wire.deny.Store(true)
			case "credential":
				reader.corrupt.Store(true)
			case "channels":
				admission.Stream.InputChannels = []string{"STDIN"}
			case "reconcile":
				admission.ReconcileOnly = true
			}
			stream, _, err := prepared.Open(t.Context(), t.Context(), binding, admission)
			if err == nil || stream != nil || wire.streamCalls.Load() != 0 {
				t.Fatal("revoked or changed admission reached stream transport")
			}
		})
	}
}

// TestPodStreamPreparationCloseJoinsOpening cancels a connected pending upgrade and joins before returning.
// The consumed preparation cannot retry the uncertain remote attempt after local closure.
// TestPodStreamPreparationCloseJoinsOpening отменяет connected pending upgrade и присоединяет его до возврата.
// Расходованная preparation не повторяет uncertain remote attempt после local closure.
func TestPodStreamPreparationCloseJoinsOpening(t *testing.T) {
	entered, peerDone := make(chan struct{}), make(chan struct{})
	factory, snapshot, child, _, wire := podStreamPreparationFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		defer close(peerDone)
		close(entered)
		<-r.Context().Done()
	})
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if err != nil {
		t.Fatal("prepare pending upgrade fixture")
	}
	defer prepared.Close()
	binding, admission := podStreamAdmissionFixture(child)
	opened := make(chan bool, 1)
	go func() {
		stream, _, err := prepared.Open(t.Context(), t.Context(), binding, admission)
		refused := err != nil && stream == nil
		if stream != nil {
			_ = stream.Close()
		}
		opened <- refused
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		prepared.Close()
		<-opened
		t.Fatal("upgrade did not reach peer")
	}
	closed := make(chan struct{})
	go func() { prepared.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("preparation close did not join pending upgrade")
	}
	if !<-opened {
		t.Fatal("closed preparation returned a transport")
	}
	select {
	case <-peerDone:
	case <-time.After(time.Second):
		t.Fatal("canceled upgrade peer did not join")
	}
	if stream, _, err := prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || stream != nil || wire.streamCalls.Load() != 1 {
		t.Fatal("uncertain opening was replayed")
	}
}

// TestPodStreamPreparationOpensPinnedPortForward joins actual owner preparation to the selected SPDY control transport without eager TCP pairs.
// Synthetic frame admissions remain separate from native owner-ledger qualification; no local listener is created.
// TestPodStreamPreparationOpensPinnedPortForward соединяет actual owner preparation с selected SPDY control transport без eager TCP pairs.
// Synthetic frame admissions остаются отдельными от native owner-ledger qualification; local listener не создаётся.
func TestPodStreamPreparationOpensPinnedPortForward(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	factory, snapshot, child, _, wire := podStreamPreparationFixture(t, peer.ServeHTTP)
	snapshot.Operation = "kubernetes.port-forward"
	snapshot.Input.Options = json.RawMessage(`{"ports":[{"port":18080,"targetPort":8080,"protocol":"TCP"},{"port":19090,"targetPort":9090}],"timeoutSeconds":30}`)
	raw, err := managedkubernetes.EncodeOperationInput(snapshot.Operation, snapshot.Input)
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot.NormalizedInput, err = managedkubernetes.DecodeOperationInput(snapshot.Operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(snapshot.NormalizedInput)
	child.Operation = snapshot.Operation
	child.Input = snapshot.Input
	child.NormalizedInput = append([]byte(nil), snapshot.NormalizedInput...)
	child.InputDigest = hex.EncodeToString(sum[:])
	child.NodeTransport.Protocol = PrivilegedPortForwardSPDYV1
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	binding, admission := podStreamAdmissionFixture(child)
	admission.Stream.InputChannels = []string{"PORT_FORWARD"}
	admission.Stream.OutputChannels = []string{"PORT_FORWARD", "CONTROL"}
	stream, receipt, err := prepared.Open(t.Context(), t.Context(), binding, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if receipt.Protocol != string(PrivilegedPortForwardSPDYV1) || receipt.Validate() != nil || peer.count.Load() != 0 {
		t.Fatal("parent preparation opened TCP without an OPEN input")
	}
	call, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	opening := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionOpen, nil)
	if stream.WriteFrame(call, opening) == nil || peer.count.Load() != 0 {
		t.Fatal("ordinary frame port bypassed owner retention admission")
	}
	writer, ok := stream.(targetrepo.PortForwardOpenWriter)
	if !ok {
		t.Fatal("prepared port-forward lost protected owner retention port")
	}
	frame, err := managedkubernetes.DecodePortForwardConnectionFrame(opening.Payload)
	if err != nil {
		t.Fatal("decode synthetic admitted OPEN")
	}
	retention := managedkubernetes.PortForwardRetentionSet{Connections: []managedkubernetes.PortForwardConnectionIdentity{{ConnectionRef: frame.ConnectionRef, Generation: frame.Generation, Slot: frame.Slot}}}
	if writer.WritePortForwardOpen(call, opening, retention) != nil || stream.WriteFrame(call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionData, []byte("prepared-pair"))) != nil {
		t.Fatal("prepared connection input failed")
	}
	part, err := stream.ReadStream()
	defer clear(part.Payload)
	frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(part.Payload)
	if err != nil || decodeErr != nil || frame.Generation != 1 || string(frame.Body) != "prepared-pair" || frame.NativeOrdinal != 1 {
		t.Fatal("production preparation lost canonical connection output")
	}
	if stream.WriteFrame(call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)) != nil || stream.Close() != nil {
		t.Fatal("prepared connection did not positively join")
	}
	if _, _, err = prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || wire.streamCalls.Load() != 1 {
		t.Fatal("preparation repeated physical control transport")
	}
}

// TestPodStreamPreparationRejectsLegacyPortForwardBeforeOpening rejects the exact pinned WSv4 profile with a closed reason before Begin/native upgrade or TCP pairing.
// TestPodStreamPreparationRejectsLegacyPortForwardBeforeOpening отклоняет exact pinned WSv4 profile с closed reason до Begin/native upgrade либо TCP pairing.
func TestPodStreamPreparationRejectsLegacyPortForwardBeforeOpening(t *testing.T) {
	factory, snapshot, child, _, wire := podStreamPreparationFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unsupported profile reached native upgrade") })
	snapshot.Operation = "kubernetes.port-forward"
	snapshot.Input.Options = json.RawMessage(`{"ports":[{"port":18080,"targetPort":8080}],"timeoutSeconds":30}`)
	raw, err := managedkubernetes.EncodeOperationInput(snapshot.Operation, snapshot.Input)
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot.NormalizedInput, err = managedkubernetes.DecodeOperationInput(snapshot.Operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(snapshot.NormalizedInput)
	child.Operation = snapshot.Operation
	child.Input = snapshot.Input
	child.NormalizedInput = append([]byte(nil), snapshot.NormalizedInput...)
	child.InputDigest = hex.EncodeToString(sum[:])
	child.NodeTransport.Protocol = PrivilegedPortForwardWebSocketV4
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if prepared != nil || !errors.Is(err, targetrepo.ErrPortForwardProfileUnsupported) || wire.streamCalls.Load() != 0 {
		t.Fatal("unsupported transport acquired a native opening")
	}
}
