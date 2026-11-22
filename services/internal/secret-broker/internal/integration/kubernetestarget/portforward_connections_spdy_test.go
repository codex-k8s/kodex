package kubernetestarget

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	apispdy "k8s.io/apimachinery/pkg/util/httpstream/spdy"
)

// dynamicForwardPeer accepts SDK pairs by owner request generation and positively joins its synthetic handlers without accessing any cluster.
// dynamicForwardPeer принимает SDK pairs по owner request generation и присоединяет synthetic handlers без доступа к cluster.
type dynamicForwardPeer struct {
	t       *testing.T
	mu      sync.Mutex
	streams map[string]map[string]httpstream.Stream
	count   atomic.Int32
	readers sync.WaitGroup
	done    chan struct{}
}

// ServeHTTP keeps an upgraded control connection idle until explicit pairs arrive and sends only finite synthetic TCP echoes.
// ServeHTTP сохраняет upgraded control connection idle до explicit pairs и отправляет лишь finite synthetic TCP echoes.
func (peer *dynamicForwardPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer close(peer.done)
	if _, err := httpstream.Handshake(r, w, []string{PrivilegedPortForwardSPDYV1.WireProtocol()}); err != nil {
		return
	}
	conn := apispdy.NewResponseUpgrader().UpgradeResponse(w, r, func(stream httpstream.Stream, reply <-chan struct{}) error {
		id := stream.Headers().Get(corev1.PortForwardRequestIDHeader)
		kind := stream.Headers().Get(corev1.StreamType)
		if _, err := strconv.ParseUint(id, 10, 63); err != nil || stream.Headers().Get(corev1.PortHeader) != "8080" || kind != corev1.StreamTypeData && kind != corev1.StreamTypeError {
			peer.t.Error("changed pinned pair headers")
			return errNativeStreamUnknown
		}
		peer.mu.Lock()
		if peer.streams[id] == nil {
			peer.streams[id] = make(map[string]httpstream.Stream)
		}
		peer.streams[id][kind] = stream
		diagnostic := peer.streams[id][corev1.StreamTypeError]
		peer.count.Add(1)
		if kind == corev1.StreamTypeData {
			peer.readers.Add(1)
			go func() {
				defer peer.readers.Done()
				<-reply
				defer diagnostic.Close()
				var raw [64]byte
				defer clear(raw[:])
				for {
					n, err := stream.Read(raw[:])
					if n > 0 {
						_, _ = stream.Write(raw[:n])
					}
					if err != nil {
						_ = stream.Close()
						return
					}
				}
			}()
		}
		peer.mu.Unlock()
		return nil
	})
	if conn == nil {
		return
	}
	<-conn.CloseChan()
	_ = conn.Close()
	peer.readers.Wait()
}

// forwardConnectionFrameFixture encodes only synthetic server-assigned metadata and private test bytes under the final shared wire contract.
// forwardConnectionFrameFixture кодирует лишь synthetic server-assigned metadata и private test bytes по final shared wire contract.
func forwardConnectionFrameFixture(t *testing.T, generation uint64, action managedkubernetes.PortForwardConnectionAction, body []byte) value.KubernetesStreamFrame {
	t.Helper()
	ref := "kpf_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(generation)}, 18))
	raw, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{ConnectionRef: ref, Generation: generation, Slot: 0, Action: action, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: raw}
}

// writeForwardFrameFixture supplies a synthetic owner retention snapshot only for OPEN; it is not proof of control-plane admission.
// Explicit retained tuples model owner-active remote joins whose OUTPUT ACK has not happened yet.
// writeForwardFrameFixture передаёт synthetic owner retention snapshot только для OPEN; это не proof control-plane admission.
// Explicit retained tuples моделируют owner-active remote joins, ещё не получившие OUTPUT ACK.
func writeForwardFrameFixture(t *testing.T, stream *nativeForwardConnections, ctx context.Context, frame value.KubernetesStreamFrame, retained ...managedkubernetes.PortForwardConnectionIdentity) error {
	t.Helper()
	opening, err := managedkubernetes.DecodePortForwardConnectionFrame(frame.Payload)
	if err != nil || opening.Action != managedkubernetes.PortForwardConnectionOpen {
		return stream.WriteFrame(ctx, frame)
	}
	set := managedkubernetes.PortForwardRetentionSet{Connections: append([]managedkubernetes.PortForwardConnectionIdentity(nil), retained...)}
	stream.mu.Lock()
	for _, pair := range stream.pairs {
		set.Connections = append(set.Connections, managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: pair.ref, Generation: pair.generation, Slot: pair.slot})
	}
	stream.mu.Unlock()
	set.Connections = append(set.Connections, managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: opening.ConnectionRef, Generation: opening.Generation, Slot: opening.Slot})
	sort.Slice(set.Connections, func(i, j int) bool { return set.Connections[i].Generation < set.Connections[j].Generation })
	return stream.WritePortForwardOpen(ctx, frame, set)
}

// TestNativePortForwardConnectionsKeepRepeatedConcurrentPairs proves zero eager TCP pairs, owner generations, repeated same-port opens, isolated close and real write-half-close over TLS.
// Synthetic admissions do not qualify the owner ledger, private stage producer or a local listener.
// TestNativePortForwardConnectionsKeepRepeatedConcurrentPairs доказывает zero eager TCP pairs, owner generations, repeated same-port opens, isolated close и real write-half-close через TLS.
// Synthetic admissions не квалифицируют owner ledger, private stage producer либо local listener.
func TestNativePortForwardConnectionsKeepRepeatedConcurrentPairs(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(5*time.Second), now.Add(time.Second), 1<<20)
	if err != nil {
		t.Fatal("open parent control transport", err)
	}
	defer stream.Close()
	if peer.count.Load() != 0 {
		t.Fatal("parent handshake opened an unadmitted TCP pair")
	}
	call, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	write := func(gen uint64, action managedkubernetes.PortForwardConnectionAction, body string) {
		t.Helper()
		frame := forwardConnectionFrameFixture(t, gen, action, []byte(body))
		defer clear(frame.Payload)
		if writeForwardFrameFixture(t, stream, call, frame) != nil {
			t.Fatal("exact admitted connection write failed", gen, action)
		}
	}
	write(1, managedkubernetes.PortForwardConnectionOpen, "")
	write(2, managedkubernetes.PortForwardConnectionOpen, "")
	write(1, managedkubernetes.PortForwardConnectionData, "first")
	write(2, managedkubernetes.PortForwardConnectionData, "second")
	write(1, managedkubernetes.PortForwardConnectionHalfClose, "")
	seen := map[uint64]string{}
	ordinals := map[uint64]uint64{}
	readEnd, closed := false, false
	for len(seen) < 2 || !readEnd || !closed {
		read, err := stream.ReadStream()
		if err != nil || read.ExitCode != nil || read.CleanEOF {
			t.Fatal("per-connection event promoted parent success")
		}
		frame, err := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if frame.ValidateOutput(1) != nil || frame.NativeOrdinal != ordinals[frame.Generation]+1 {
			t.Fatal("native event ordinal changed or was reused")
		}
		ordinals[frame.Generation] = frame.NativeOrdinal
		switch frame.Action {
		case managedkubernetes.PortForwardConnectionData:
			seen[frame.Generation] += string(frame.Body)
		case managedkubernetes.PortForwardConnectionHalfClose:
			readEnd = frame.Generation == 1
		case managedkubernetes.PortForwardConnectionClose:
			closed = frame.Generation == 1
		default:
			t.Fatal("unexpected output action")
		}
		clear(read.Payload)
	}
	if seen[1] != "first" || seen[2] != "second" {
		t.Fatal("concurrent TCP bytes crossed connections")
	}
	write(2, managedkubernetes.PortForwardConnectionData, "still-open")
	read, err := stream.ReadStream()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
	if err != nil || frame.Generation != 2 || string(frame.Body) != "still-open" {
		t.Fatal("closing first pair killed sibling")
	}
	clear(read.Payload)
	write(2, managedkubernetes.PortForwardConnectionClose, "")
	write(3, managedkubernetes.PortForwardConnectionOpen, "")
	write(3, managedkubernetes.PortForwardConnectionData, "reopened")
	read, err = stream.ReadStream()
	if err != nil {
		t.Fatal(err)
	}
	frame, err = managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
	if err != nil || frame.Generation != 3 || string(frame.Body) != "reopened" {
		t.Fatal("sequential connection changed immutable mapping")
	}
	clear(read.Payload)
	write(3, managedkubernetes.PortForwardConnectionClose, "")
	old := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionOpen, nil)
	defer clear(old.Payload)
	if writeForwardFrameFixture(t, stream, call, old) == nil || peer.count.Load() != 6 {
		t.Fatal("old request generation reopened a TCP pair")
	}
	blocked := make(chan error, 1)
	go func() { _, err := stream.ReadStream(); blocked <- err }()
	select {
	case <-blocked:
		t.Fatal("zero live pairs self-completed parent")
	case <-time.After(20 * time.Millisecond):
	}
	if stream.Close() != nil {
		t.Fatal("close parent")
	}
	select {
	case err := <-blocked:
		if err == nil {
			t.Fatal("parent close claimed remote success")
		}
	case <-time.After(time.Second):
		t.Fatal("parent reader did not join")
	}
	select {
	case <-peer.done:
	case <-time.After(time.Second):
		t.Fatal("SPDY peer did not join")
	}
}

// TestNativePortForwardConnectionsRejectUnadmittedShapes ensures malformed wire and legacy EOF cannot create streams or dispatch TCP bytes.
// TestNativePortForwardConnectionsRejectUnadmittedShapes гарантирует, что malformed wire и legacy EOF не создают streams и не передают TCP bytes.
func TestNativePortForwardConnectionsRejectUnadmittedShapes(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, true, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(3*time.Second), now.Add(time.Second), 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	call, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, frame := range []value.KubernetesStreamFrame{{Channel: "PORT_FORWARD", EndOfInput: true}, {Channel: "PORT_FORWARD", Payload: []byte{0, 'x'}}, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionData, []byte("no-open"))} {
		if writeForwardFrameFixture(t, stream, call, frame) == nil || peer.count.Load() != 0 {
			t.Fatal("unadmitted envelope opened native streams")
		}
		clear(frame.Payload)
	}
	stream.mu.Lock()
	count := len(stream.pairs)
	stream.mu.Unlock()
	if count != 0 {
		t.Fatal("rejected envelope allocated pair state")
	}
	if stream.Close() != nil {
		t.Fatal("close invalid fixture")
	}
	<-peer.done
}

// The compile assertion keeps the new transport in the original owner stream interface rather than adding an independent execution lane.
// Compile assertion сохраняет новый transport внутри original owner stream interface без отдельной execution lane.
var _ io.Closer = (*nativeForwardConnections)(nil)

// TestNativePortForwardOpenUsesFreshFrameCreditAfterInitialAdmission proves a later protected pair can open while the generic initializer stays permanently closed.
// TestNativePortForwardOpenUsesFreshFrameCreditAfterInitialAdmission доказывает later protected pair под свежим frame credit при постоянно закрытом generic initializer.
func TestNativePortForwardOpenUsesFreshFrameCreditAfterInitialAdmission(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	initial := now.Add(200 * time.Millisecond)
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(3*time.Second), initial, 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	timer := time.NewTimer(time.Until(initial.Add(20 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("test canceled")
	}
	if raw, err := stream.nativeSPDYConnection.CreateStream(http.Header{corev1.StreamType: []string{corev1.StreamTypeData}}); err == nil || raw != nil || peer.count.Load() != 0 {
		t.Fatal("generic initial admission was reopened")
	}
	call, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	open := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionOpen, nil)
	defer clear(open.Payload)
	if writeForwardFrameFixture(t, stream, call, open) != nil || peer.count.Load() != 2 {
		t.Fatal("fresh per-OPEN frame could not outlive initial handshake credit")
	}
	closeFrame := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)
	defer clear(closeFrame.Payload)
	if writeForwardFrameFixture(t, stream, call, closeFrame) != nil {
		t.Fatal("late opened pair did not join")
	}
	if stream.Close() != nil {
		t.Fatal("late open parent did not join")
	}
	<-peer.done
}

// TestNativePortForwardCloseRetainsAlreadyReadCandidate keeps exact bytes available to the owner CLOSED/loss path while positively joining the selected pair and preserving its sibling.
// TestNativePortForwardCloseRetainsAlreadyReadCandidate сохраняет exact bytes для owner CLOSED/loss path при positive join выбранной пары и сохранении sibling.
func TestNativePortForwardCloseRetainsAlreadyReadCandidate(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(5*time.Second), now.Add(time.Second), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	call, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for _, gen := range []uint64{1, 2} {
		if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, gen, managedkubernetes.PortForwardConnectionOpen, nil)) != nil {
			t.Fatal("open pair failed")
		}
	}
	body := []byte("already-read-before-close")
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionData, body)) != nil {
		t.Fatal("write data failed")
	}
	expected := uint64(4*managedkubernetes.PortForwardConnectionFrameHeaderBytes + 2*len(body))
	until := time.Now().Add(time.Second)
	for {
		stream.budgetMu.Lock()
		spent := stream.spent
		stream.budgetMu.Unlock()
		if spent >= expected {
			break
		}
		if !time.Now().Before(until) {
			t.Fatal("native echo was not read")
		}
		time.Sleep(time.Millisecond)
	}
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)) != nil {
		t.Fatal("selected pair did not join")
	}
	read := make(chan value.KubernetesStreamRead, 1)
	go func() { frame, _ := stream.ReadStream(); read <- frame }()
	select {
	case candidate := <-read:
		frame, err := managedkubernetes.DecodePortForwardConnectionFrame(candidate.Payload)
		defer clear(candidate.Payload)
		if err != nil || frame.Generation != 1 || frame.NativeOrdinal != 1 || frame.Action != managedkubernetes.PortForwardConnectionData || !bytes.Equal(frame.Body, body) {
			t.Fatal("retained candidate changed original native event")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("joined Close discarded already-read native bytes before owner loss accounting")
	}
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 2, managedkubernetes.PortForwardConnectionData, []byte("sibling"))) != nil {
		t.Fatal("closed pair killed sibling")
	}
	candidate, err := stream.ReadStream()
	defer clear(candidate.Payload)
	frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(candidate.Payload)
	if err != nil || decodeErr != nil || frame.Generation != 2 || string(frame.Body) != "sibling" {
		t.Fatal("sibling data did not survive isolated Close")
	}
}

// TestNativePortForwardCloseRefusesUnretainableCandidate preserves UNKNOWN rather than successful Close when bounded pending output cannot be retained.
// TestNativePortForwardCloseRefusesUnretainableCandidate сохраняет UNKNOWN вместо successful Close, если bounded pending output нельзя удержать.
func TestNativePortForwardCloseRefusesUnretainableCandidate(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(5*time.Second), now.Add(time.Second), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	call, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionOpen, nil)) != nil {
		t.Fatal("open pair failed")
	}
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionData, bytes.Repeat([]byte{17}, 4096))) != nil {
		t.Fatal("write data failed")
	}
	until := time.Now().Add(time.Second)
	for len(stream.events) != maximumForwardPendingCandidates {
		if !time.Now().Before(until) {
			t.Fatal("bounded native queue did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	// One additional already-read candidate is now blocked behind the full queue.
	// Ещё один already-read candidate блокируется за заполненной queue.
	// The peer emits fixed 64-byte DATA frames, so this strict charge proves the extra native read, not merely a full queue.
	// Peer выдаёт fixed 64-byte DATA frames; этот strict charge доказывает extra native read, не только full queue.
	queuedCharge := uint64(2*managedkubernetes.PortForwardConnectionFrameHeaderBytes + 4096 + maximumForwardPendingCandidates*(managedkubernetes.PortForwardConnectionFrameHeaderBytes+64))
	for {
		stream.budgetMu.Lock()
		spent := stream.spent
		stream.budgetMu.Unlock()
		if spent > queuedCharge {
			break
		}
		if !time.Now().Before(until) {
			t.Fatal("extra native candidate was not read behind full queue")
		}
		time.Sleep(time.Millisecond)
	}
	if writeForwardFrameFixture(t, stream, call, forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)) == nil || stream.ctx.Err() == nil {
		t.Fatal("unretained output was reported as successful isolated Close")
	}
	if stream.Close() != nil {
		t.Fatal("failed parent did not positively join")
	}
	if len(stream.events) != 0 {
		t.Fatal("parent close retained private candidates")
	}
}

// TestNativePortForwardCloseRejoinsAlreadyJoinedPair covers the gap between actual remote half-close/local join and owner ACK of its queued OUTPUT CLOSE.
// An independently admitted INPUT CLOSE must reuse only exact local join evidence, without reopening or affecting the sibling.
// TestNativePortForwardCloseRejoinsAlreadyJoinedPair покрывает окно между actual remote half-close/local join и owner ACK queued OUTPUT CLOSE.
// Отдельно допущенный INPUT CLOSE использует лишь exact local join evidence, не открывает пару повторно и не затрагивает sibling.
func TestNativePortForwardCloseRejoinsAlreadyJoinedPair(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080, 8080}, now.Add(10*time.Second), now.Add(time.Second), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	call, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	for _, generation := range []uint64{1, 2} {
		open := forwardConnectionFrameFixture(t, generation, managedkubernetes.PortForwardConnectionOpen, nil)
		if writeForwardFrameFixture(t, stream, call, open) != nil {
			t.Fatal("open native pair")
		}
		clear(open.Payload)
	}
	closing := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)
	defer clear(closing.Payload)
	coordinates, err := managedkubernetes.DecodePortForwardConnectionFrame(closing.Payload)
	if err != nil {
		t.Fatal(err)
	}
	stream.mu.Lock()
	pair := stream.pairs[coordinates.ConnectionRef]
	stream.mu.Unlock()
	if pair == nil {
		t.Fatal("native pair was not created")
	}
	half := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionHalfClose, nil)
	if writeForwardFrameFixture(t, stream, call, half) != nil {
		t.Fatal("write native half-close")
	}
	clear(half.Payload)
	select {
	case <-pair.joined:
	case <-call.Done():
		t.Fatal("remote half-close did not positively join the native pair")
	}
	// Sequential explicitly acknowledged closes cannot evict the remote join whose OUTPUT ACK is still pending.
	// Последовательные explicitly acknowledged closes не вытесняют remote join с ещё ожидаемым OUTPUT ACK.
	for generation := uint64(3); generation <= 42; generation++ {
		for _, action := range []managedkubernetes.PortForwardConnectionAction{managedkubernetes.PortForwardConnectionOpen, managedkubernetes.PortForwardConnectionClose} {
			control := forwardConnectionFrameFixture(t, generation, action, nil)
			err := writeForwardFrameFixture(t, stream, call, control, managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: coordinates.ConnectionRef, Generation: coordinates.Generation, Slot: coordinates.Slot})
			clear(control.Payload)
			if err != nil {
				t.Fatal("explicit close churn failed", generation, action, err)
			}
		}
	}
	stream.mu.Lock()
	retained := len(stream.joined)
	stream.mu.Unlock()
	if retained != 1 {
		t.Fatal("explicit CLOSE churn polluted bounded remote join facts")
	}
	for _, mutate := range []func(*managedkubernetes.PortForwardConnectionFrame){func(frame *managedkubernetes.PortForwardConnectionFrame) { frame.Generation++ }, func(frame *managedkubernetes.PortForwardConnectionFrame) { frame.Slot = 1 }, func(frame *managedkubernetes.PortForwardConnectionFrame) {
		frame.Action = managedkubernetes.PortForwardConnectionData
		frame.Body = []byte("no-replay")
	}} {
		wrong := coordinates
		mutate(&wrong)
		raw, err := managedkubernetes.EncodePortForwardConnectionFrame(wrong)
		if err != nil {
			t.Fatal(err)
		}
		err = writeForwardFrameFixture(t, stream, call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: raw})
		clear(raw)
		if err == nil {
			t.Fatal("join metadata authorized different generation or DATA replay")
		}
	}
	expired, stopExpired := context.WithCancel(call)
	stopExpired()
	if stream.WriteFrame(expired, closing) == nil {
		t.Fatal("expired frame context reused local join evidence")
	}
	// Neither OUTPUT candidate has reached the owner; it can still legitimately have an OPEN connection row.
	// Ни один OUTPUT candidate ещё не дошёл до owner; его connection row законно может оставаться OPEN.
	if err := writeForwardFrameFixture(t, stream, call, closing); err != nil {
		t.Fatal("admitted CLOSE lost exact already-joined local evidence", err)
	}
	if peer.count.Load() != 84 || stream.ctx.Err() != nil {
		t.Fatal("known local join reopened or killed the parent transport")
	}
	for _, expected := range []managedkubernetes.PortForwardConnectionAction{managedkubernetes.PortForwardConnectionHalfClose, managedkubernetes.PortForwardConnectionClose} {
		read, err := stream.ReadStream()
		frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
		clear(read.Payload)
		if err != nil || decodeErr != nil || frame.Generation != 1 || frame.Action != expected || read.CleanEOF || read.ExitCode != nil {
			t.Fatal("join recheck altered queued remote facts or fabricated success")
		}
	}

	data := forwardConnectionFrameFixture(t, 2, managedkubernetes.PortForwardConnectionData, []byte("sibling"))
	if writeForwardFrameFixture(t, stream, call, data) != nil {
		t.Fatal("exact joined CLOSE changed sibling availability")
	}
	clear(data.Payload)
	read, err := stream.ReadStream()
	frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
	defer clear(read.Payload)
	if err != nil || decodeErr != nil || frame.Generation != 2 || string(frame.Body) != "sibling" {
		t.Fatal("sibling did not retain its own native bytes")
	}
	stream.mu.Lock()
	var sibling *nativeForwardPair
	for _, current := range stream.pairs {
		if current.generation == 2 {
			sibling = current
		}
	}
	stream.mu.Unlock()
	if sibling == nil {
		t.Fatal("sibling pair disappeared before half-close")
	}
	halfSibling := forwardConnectionFrameFixture(t, 2, managedkubernetes.PortForwardConnectionHalfClose, nil)
	if writeForwardFrameFixture(t, stream, call, halfSibling) != nil {
		t.Fatal("half-close sibling")
	}
	clear(halfSibling.Payload)
	select {
	case <-sibling.joined:
	case <-call.Done():
		t.Fatal("sibling join did not complete")
	}
	stream.mu.Lock()
	retained = len(stream.joined)
	stream.mu.Unlock()
	if retained != 1 {
		t.Fatal("remote sibling join fact missing before parent cleanup")
	}
	if stream.Close() != nil {
		t.Fatal("parent cleanup did not positively join")
	}
	stream.mu.Lock()
	retained = len(stream.joined) + len(stream.retainable)
	stream.mu.Unlock()
	if retained != 0 || len(stream.events) != 0 {
		t.Fatal("parent cleanup retained private join or output state")
	}
	select {
	case <-peer.done:
	case <-call.Done():
		t.Fatal("native peer did not join")
	}

}

// TestNativePortForwardCloseWithdrawsOnlyUnpublishedControls proves a full native queue cannot deadlock explicit Close behind its own zero-body HALF_CLOSE/CLOSE publication.
// All actual sibling DATA remains queued and is verified byte-for-byte; no TCP byte is discarded or acknowledged by this transport fixture.
// TestNativePortForwardCloseWithdrawsOnlyUnpublishedControls доказывает, что полная native queue не блокирует explicit Close за публикацией собственного zero-body HALF_CLOSE/CLOSE.
// Все actual sibling DATA сохраняются и проверяются побайтно; transport fixture не отбрасывает и не подтверждает TCP byte.
func TestNativePortForwardCloseWithdrawsOnlyUnpublishedControls(t *testing.T) {
	for _, buffered := range []int{maximumForwardPendingCandidates - 1, maximumForwardPendingCandidates} {
		t.Run(strconv.Itoa(buffered), func(t *testing.T) {
			peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
			credential, _ := transportFixtureWithHandler(t, false, peer)
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedPortForwardSPDYV1
			stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(10*time.Second), now.Add(time.Second), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stream.Close() })
			call, stop := context.WithTimeout(t.Context(), 8*time.Second)
			defer stop()
			for _, gen := range []uint64{1, 2} {
				frame := forwardConnectionFrameFixture(t, gen, managedkubernetes.PortForwardConnectionOpen, nil)
				err := writeForwardFrameFixture(t, stream, call, frame)
				clear(frame.Payload)
				if err != nil {
					t.Fatal("open native pair", err)
				}
			}
			body := bytes.Repeat([]byte{42}, buffered*64)
			data := forwardConnectionFrameFixture(t, 2, managedkubernetes.PortForwardConnectionData, body)
			err = stream.WriteFrame(call, data)
			clear(data.Payload)
			if err != nil {
				t.Fatal("write sibling queue data", err)
			}
			until := time.Now().Add(time.Second)
			for len(stream.events) != buffered {
				if !time.Now().Before(until) {
					t.Fatal("sibling queue did not fill")
				}
				time.Sleep(time.Millisecond)
			}
			half := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionHalfClose, nil)
			err = stream.WriteFrame(call, half)
			clear(half.Payload)
			if err != nil {
				t.Fatal("half-close selected pair", err)
			}
			header := managedkubernetes.PortForwardConnectionFrameHeaderBytes
			expected := uint64(2*header + (header + len(body)) + buffered*(header+64) + header)
			if buffered == maximumForwardPendingCandidates-1 {
				expected += uint64(2 * header)
			} else {
				expected += uint64(header)
			}
			until = time.Now().Add(time.Second)
			for {
				stream.budgetMu.Lock()
				spent := stream.spent
				stream.budgetMu.Unlock()
				if spent >= expected && len(stream.events) == maximumForwardPendingCandidates {
					break
				}
				if !time.Now().Before(until) {
					t.Fatal("unpublished control did not reach full queue")
				}
				time.Sleep(time.Millisecond)
			}
			closing := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)
			closeCtx, cancelClose := context.WithTimeout(call, time.Second)
			err = stream.WriteFrame(closeCtx, closing)
			cancelClose()
			clear(closing.Payload)
			if err != nil || stream.ctx.Err() != nil {
				t.Fatal("explicit Close deadlocked behind unpublished control", err)
			}
			var got []byte
			halves := 0
			for i := 0; i < maximumForwardPendingCandidates; i++ {
				read, err := stream.ReadStream()
				frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
				if err != nil || decodeErr != nil || read.ExitCode != nil || read.CleanEOF {
					clear(read.Payload)
					t.Fatal("queue drain changed parent outcome")
				}
				if frame.Generation == 2 && frame.Action == managedkubernetes.PortForwardConnectionData {
					got = append(got, frame.Body...)
				} else if frame.Generation == 1 && frame.Action == managedkubernetes.PortForwardConnectionHalfClose {
					halves++
				} else {
					clear(read.Payload)
					t.Fatal("unexpected queued connection control")
				}
				clear(read.Payload)
			}
			defer clear(got)
			if !bytes.Equal(got, body) || halves != maximumForwardPendingCandidates-buffered {
				t.Fatal("Close discarded sibling bytes or published a withdrawn control")
			}
			stream.budgetMu.Lock()
			spent := stream.spent
			stream.budgetMu.Unlock()
			if spent != expected {
				t.Fatal("unpublished control changed combined byte accounting")
			}
			alive := forwardConnectionFrameFixture(t, 2, managedkubernetes.PortForwardConnectionData, []byte("alive"))
			err = stream.WriteFrame(call, alive)
			clear(alive.Payload)
			if err != nil {
				t.Fatal("control cancellation killed sibling", err)
			}
			read, err := stream.ReadStream()
			frame, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
			defer clear(read.Payload)
			if err != nil || decodeErr != nil || frame.Generation != 2 || string(frame.Body) != "alive" {
				t.Fatal("sibling did not progress after full-queue Close")
			}
			if stream.Close() != nil {
				t.Fatal("native parent did not join")
			}
			select {
			case <-peer.done:
			case <-call.Done():
				t.Fatal("native peer did not join")
			}
		})
	}
}

// TestNativePortForwardOwnerRetentionPreservesOldJoinAcrossSequentialRemoteClosures proves GC follows current owner tuples rather than a lifetime FIFO count.
// The fixture supplies synthetic protected snapshots; it does not replace the actual owner SQL qualification.
// TestNativePortForwardOwnerRetentionPreservesOldJoinAcrossSequentialRemoteClosures доказывает GC по current owner tuples, а не lifetime FIFO count.
// Fixture передаёт synthetic protected snapshots и не заменяет actual owner SQL qualification.
func TestNativePortForwardOwnerRetentionPreservesOldJoinAcrossSequentialRemoteClosures(t *testing.T) {
	peer := &dynamicForwardPeer{t: t, streams: make(map[string]map[string]httpstream.Stream), done: make(chan struct{})}
	credential, _ := transportFixtureWithHandler(t, false, peer)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardConnectionsSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(15*time.Second), now.Add(time.Second), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	call, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	first := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionOpen, nil)
	parsed, _ := managedkubernetes.DecodePortForwardConnectionFrame(first.Payload)
	retained := managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: parsed.ConnectionRef, Generation: 1, Slot: 0}
	// Generic writes and stale/missing snapshots cannot create even the first pair.
	// Generic writes и stale/missing snapshots не создают даже первую pair.
	if stream.WriteFrame(call, first) == nil || peer.count.Load() != 0 {
		t.Fatal("OPEN without owner snapshot reached native transport")
	}
	for gen := uint64(1); gen <= 42; gen++ {
		opening := forwardConnectionFrameFixture(t, gen, managedkubernetes.PortForwardConnectionOpen, nil)
		var keep []managedkubernetes.PortForwardConnectionIdentity
		if gen > 1 {
			keep = append(keep, retained)
		}
		if err := writeForwardFrameFixture(t, stream, call, opening, keep...); err != nil {
			clear(opening.Payload)
			t.Fatal("sequential owner-approved OPEN", gen, err)
		}
		clear(opening.Payload)
		half := forwardConnectionFrameFixture(t, gen, managedkubernetes.PortForwardConnectionHalfClose, nil)
		if stream.WriteFrame(call, half) != nil {
			clear(half.Payload)
			t.Fatal("half close", gen)
		}
		clear(half.Payload)
		for _, action := range []managedkubernetes.PortForwardConnectionAction{managedkubernetes.PortForwardConnectionHalfClose, managedkubernetes.PortForwardConnectionClose} {
			read, readErr := stream.ReadStream()
			output, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(read.Payload)
			clear(read.Payload)
			if readErr != nil || decodeErr != nil || output.Generation != gen || output.Action != action {
				t.Fatal("remote join controls changed", gen, action)
			}
		}
		// The next snapshot excludes this closed/ACKed synthetic owner row, but retains incarnation one.
		// Следующий snapshot исключает closed/ACKed synthetic owner row, сохраняя incarnation one.
		until := time.Now().Add(time.Second)
		for {
			stream.mu.Lock()
			done := len(stream.pairs) == 0
			count := len(stream.joined)
			stream.mu.Unlock()
			if done {
				if count > 2 {
					t.Fatal("owner-retired history accumulated")
				}
				break
			}
			if !time.Now().Before(until) {
				t.Fatal("native pair did not join")
			}
			time.Sleep(time.Millisecond)
		}
	}
	closing := forwardConnectionFrameFixture(t, 1, managedkubernetes.PortForwardConnectionClose, nil)
	if stream.WriteFrame(call, closing) != nil || stream.ctx.Err() != nil {
		clear(closing.Payload)
		t.Fatal("old owner-selected join was evicted")
	}
	clear(closing.Payload)
	clear(first.Payload)
	if stream.Close() != nil {
		t.Fatal("parent did not join")
	}
	select {
	case <-peer.done:
	case <-call.Done():
		t.Fatal("peer did not join")
	}
}
