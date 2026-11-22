package kubernetesoperation

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// activeOwnerFixture records thread-safe metadata and closure without performing a remote operation.
// activeOwnerFixture сохраняет thread-safe metadata и closure без удалённой операции.
type activeOwnerFixture struct {
	expires          time.Time
	frameMutex       sync.Mutex
	outputDeliveries map[uint64]uint32
	closed           chan targetrepo.StreamClosing
	frameBegins      atomic.Int32
	frameCompletes   atomic.Int32
	finalState       string
	closeErr         error
}

// BeginKubernetesFrame supplies bounded credit to the already-opened synthetic stream.
// BeginKubernetesFrame выдаёт bounded credit уже открытому synthetic stream.
func (owner *activeOwnerFixture) BeginKubernetesFrame(_ context.Context, _ managedkubernetes.Binding, frame value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error) {
	owner.frameBegins.Add(1)
	admission := value.KubernetesFrameAdmission{Disposition: "SEND", ExpiresAt: owner.expires}
	if frame.Direction == "OUTPUT" {
		owner.frameMutex.Lock()
		defer owner.frameMutex.Unlock()
		if owner.outputDeliveries == nil {
			owner.outputDeliveries = make(map[uint64]uint32)
		}
		owner.outputDeliveries[frame.Sequence]++
		admission.DeliveryOrdinal = owner.outputDeliveries[frame.Sequence]
		if frame.NativeAuthorizedUntil.Before(admission.ExpiresAt) {
			admission.ExpiresAt = frame.NativeAuthorizedUntil
		}
		if limit := time.Now().Add(5 * time.Second); limit.Before(admission.ExpiresAt) {
			admission.ExpiresAt = limit
		}
	}
	return admission, nil
}

// CompleteKubernetesFrame acknowledges only metadata from a completed local write or client ACK.
// CompleteKubernetesFrame подтверждает только metadata завершённой local write либо client ACK.
func (owner *activeOwnerFixture) CompleteKubernetesFrame(_ context.Context, _ managedkubernetes.Binding, frame value.KubernetesFrameMetadata) (uint64, error) {
	owner.frameCompletes.Add(1)
	return frame.Sequence, nil
}

// OpenKubernetesStream refuses a second open because the fixture starts after owner admission.
// OpenKubernetesStream запрещает second open, поскольку fixture начинается после owner admission.
func (*activeOwnerFixture) OpenKubernetesStream(context.Context, managedkubernetes.Binding, targetrepo.StreamOpening) (value.KubernetesStreamGrant, error) {
	return value.KubernetesStreamGrant{}, errors.New("unexpected stream open")
}

// CloseKubernetesStream validates evidence and preserves an authoritative owner override.
// CloseKubernetesStream проверяет evidence и сохраняет authoritative owner override.
func (owner *activeOwnerFixture) CloseKubernetesStream(ctx context.Context, _ managedkubernetes.Binding, closing targetrepo.StreamClosing) (targetrepo.StreamReceipt, error) {
	digest, err := closing.TerminalReceipt.Digest()
	if err != nil || digest != closing.TerminalReceiptDigest || ctx.Err() != nil {
		return targetrepo.StreamReceipt{}, errors.New("invalid terminal fixture evidence")
	}
	owner.closed <- closing
	if owner.closeErr != nil {
		return targetrepo.StreamReceipt{}, owner.closeErr
	}
	state := closing.TerminalState
	if owner.finalState != "" {
		state = owner.finalState
	}
	return targetrepo.StreamReceipt{State: state, RemoteOutcome: closing.RemoteOutcome}, nil
}

// TestActiveStreamTerminalEvidencePreservesNativeFacts separates joined evidence from an owner ACK or current authority.
// TestActiveStreamTerminalEvidencePreservesNativeFacts отделяет факты после join от owner ACK и текущих полномочий.
func TestActiveStreamTerminalEvidencePreservesNativeFacts(t *testing.T) {
	for _, variant := range []string{"success", "revoked", "lost-owner-ack"} {
		t.Run(variant, func(t *testing.T) {
			stream, owner, _, transport, _ := activeStreamFixture(t, func(owner *activeOwnerFixture, _ *activeAccessFixture, _ *activeTransportFixture, _ *value.KubernetesStreamGrant) {
				if variant == "revoked" {
					owner.finalState = "REVOKED"
				}
				if variant == "lost-owner-ack" {
					owner.closeErr = errors.New("synthetic lost owner ACK")
				}
			})
			if _, ok := stream.TerminalEvidence(); ok {
				t.Fatal("active stream fabricated terminal evidence")
			}
			exit := int32(0)
			transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
			result, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
			if (err != nil) != (variant == "lost-owner-ack") {
				t.Fatal("owner acknowledgement failure lost")
			}
			if variant == "revoked" && result.State != "REVOKED" {
				t.Fatal("native evidence replaced current owner state")
			}
			closing := <-owner.closed
			evidence, ok := stream.TerminalEvidence()
			digest, digestErr := evidence.Digest()
			if !ok || digestErr != nil || digest != closing.TerminalReceiptDigest || !evidence.TransportJoined || evidence.ExitCode == nil || *evidence.ExitCode != 0 {
				t.Fatal("joined native evidence changed")
			}
			*evidence.ExitCode = 77
			evidence.Origin.EffectRef = "effect_foreign"
			second, ok := stream.TerminalEvidence()
			secondDigest, _ := second.Digest()
			if !ok || secondDigest != digest {
				t.Fatal("returned native evidence aliases retained state")
			}
		})
	}
}

// activeAccessFixture exposes fresh credit and a synchronized revocation switch.
// activeAccessFixture предоставляет fresh credit и synchronized revocation switch.
type activeAccessFixture struct {
	ttl     time.Duration
	calls   atomic.Int32
	revoked atomic.Bool
	check   func(context.Context) error
}

// AuthorizeStream never returns a stale cached allow after the test revokes its authority.
// AuthorizeStream не возвращает stale cached allow после отзыва полномочий в тесте.
func (access *activeAccessFixture) AuthorizeStream(ctx context.Context, _ managedkubernetes.Binding, _ managedkubernetes.StreamReceiptOrigin) (time.Time, error) {
	access.calls.Add(1)
	if access.check != nil {
		if err := access.check(ctx); err != nil {
			return time.Time{}, err
		}
	}
	if access.revoked.Load() {
		return time.Time{}, errors.New("synthetic authority revoked")
	}
	return time.Now().Add(access.ttl), nil
}

// activeTransportFixture blocks reads until native data or close and leaves writer joining to the owner.
// activeTransportFixture блокирует чтение до native data либо close; writer join выполняет владелец.
type activeTransportFixture struct {
	reads      chan streamReadEvent
	closed     chan struct{}
	closeOnce  sync.Once
	readCount  atomic.Int32
	writeCount atomic.Int32
	write      func(context.Context) error
	closeErr   error
}

// ReadStream represents a genuinely blocked native reader that Close must unblock.
// ReadStream представляет действительно blocked native reader, разблокируемый Close.
func (transport *activeTransportFixture) ReadStream() (value.KubernetesStreamRead, error) {
	transport.readCount.Add(1)
	select {
	case event := <-transport.reads:
		return event.read, event.err
	case <-transport.closed:
		return value.KubernetesStreamRead{}, io.EOF
	}
}

// WriteFrame can intentionally outlive cancellation to test the positive writer join boundary.
// WriteFrame может намеренно пережить cancellation для проверки positive writer join boundary.
func (transport *activeTransportFixture) WriteFrame(ctx context.Context, _ value.KubernetesStreamFrame) error {
	transport.writeCount.Add(1)
	if transport.write != nil {
		return transport.write(ctx)
	}
	return nil
}

// Close interrupts the physical reader exactly once and does not forge a remote process exit.
// Close однократно прерывает physical reader и не подделывает remote process exit.
func (transport *activeTransportFixture) Close() error {
	transport.closeOnce.Do(func() { close(transport.closed) })
	return transport.closeErr
}

// activeStreamFixture constructs a valid exec-shaped stream with no network or owner database shortcut.
// activeStreamFixture создаёт valid exec-shaped stream без сети либо owner database shortcut.
func activeStreamFixture(t *testing.T, adjust func(*activeOwnerFixture, *activeAccessFixture, *activeTransportFixture, *value.KubernetesStreamGrant)) (*ActiveStream, *activeOwnerFixture, *activeAccessFixture, *activeTransportFixture, value.KubernetesStreamFrame) {
	t.Helper()
	_, binding, grant, frame, _, _ := inputFrameFixture(t)
	owner := &activeOwnerFixture{expires: grant.ExpiresAt, closed: make(chan targetrepo.StreamClosing, 1)}
	access := &activeAccessFixture{ttl: time.Second}
	transport := &activeTransportFixture{reads: make(chan streamReadEvent, 4), closed: make(chan struct{})}
	if adjust != nil {
		adjust(owner, access, transport, &grant)
	}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	handshake := managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: digest, ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: grant.Ref, StreamGeneration: binding.Generation, Operation: "kubernetes.exec", TargetRef: "target_synthetic", TargetRevision: 2}, TransportRef: "transport_synthetic", TransportGeneration: 3, Protocol: string(managedkubernetes.WebSocketV5), HTTPStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSPKISHA256: strings.Repeat("d", 64)}
	stream, err := StartActiveStream(t.Context(), owner, access, transport, binding, grant, handshake)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.Close() })
	return stream, owner, access, transport, frame
}

// TestActiveStreamTerminalWaitJoinsBeforeReceipt reproduces the former self-cancelled terminal wait.
// TestActiveStreamTerminalWaitJoinsBeforeReceipt воспроизводит прежний self-cancelled terminal wait.
func TestActiveStreamTerminalWaitJoinsBeforeReceipt(t *testing.T) {
	stream, owner, _, transport, _ := activeStreamFixture(t, nil)
	exit := int32(0)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := stream.Exchange(ctx, stream.binding, 0, nil)
	if err != nil || result.State != "COMPLETED" || result.RemoteOutcome != "SUCCEEDED" {
		t.Fatalf("terminal exchange failed: %v %+v", err, result)
	}
	receipt := <-owner.closed
	if !receipt.TransportJoined || receipt.ObservedExitCode == nil || *receipt.ObservedExitCode != 0 {
		t.Fatal("terminal evidence lost explicit zero exit")
	}
	select {
	case <-stream.readerDone:
	default:
		t.Fatal("terminal receipt preceded reader join")
	}
}

// TestActiveStreamPartialOutputWaitsForACK preserves bytes returned alongside a terminal failure.
// TestActiveStreamPartialOutputWaitsForACK сохраняет bytes, возвращённые вместе с terminal failure.
func TestActiveStreamPartialOutputWaitsForACK(t *testing.T) {
	stream, owner, _, transport, _ := activeStreamFixture(t, nil)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{Channel: "STDOUT", Payload: []byte("accepted-prefix")}, err: io.ErrUnexpectedEOF}
	first, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
	if err != nil || len(first.Output) != 1 || string(first.Output[0].Payload) != "accepted-prefix" {
		t.Fatal("partial output lost")
	}
	select {
	case <-owner.closed:
		t.Fatal("closed before output ACK")
	default:
	}
	second, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
	if err != nil || len(second.Output) != 1 || second.Output[0].PayloadDigest != first.Output[0].PayloadDigest || owner.frameBegins.Load() != 2 {
		t.Fatal("lost output reply did not replay retained authorized bytes")
	}
	result, err := stream.Exchange(t.Context(), stream.binding, second.Output[0].Sequence, nil)
	if err != nil || result.State != "LOST" || result.RemoteOutcome != "UNKNOWN" || owner.frameCompletes.Load() != 1 {
		t.Fatal("partial failure fabricated success or lost ACK")
	}
}

// TestActiveStreamCloseWaitsForWriter prevents owner closure while an accepted writer is still alive.
// TestActiveStreamCloseWaitsForWriter запрещает owner closure, пока принятый writer ещё выполняется.
func TestActiveStreamCloseWaitsForWriter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	stream, owner, _, transport, frame := activeStreamFixture(t, func(_ *activeOwnerFixture, _ *activeAccessFixture, transport *activeTransportFixture, _ *value.KubernetesStreamGrant) {
		transport.write = func(ctx context.Context) error { close(started); <-ctx.Done(); <-release; return ctx.Err() }
	})
	exchangeDone := make(chan struct{})
	go func() { defer close(exchangeDone); stream.Exchange(t.Context(), stream.binding, 0, &frame) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	closeDone := make(chan struct{})
	go func() { defer close(closeDone); stream.Close() }()
	select {
	case <-transport.closed:
	case <-time.After(time.Second):
		t.Fatal("transport was not interrupted")
	}
	select {
	case <-owner.closed:
		t.Fatal("owner receipt preceded writer join")
	case <-closeDone:
		t.Fatal("close skipped writer join")
	case <-time.After(20 * time.Millisecond):
	}
	if _, ok := stream.TerminalEvidence(); ok {
		t.Fatal("terminal evidence preceded writer join")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("close did not join released writer")
	}
	<-exchangeDone
	receipt := <-owner.closed
	if !receipt.TransportJoined || receipt.RemoteOutcome != "UNKNOWN" || transport.writeCount.Load() != 1 || owner.frameCompletes.Load() != 0 {
		t.Fatal("uncertain input was acknowledged or replayed")
	}
	evidence, ok := stream.TerminalEvidence()
	if !ok || !evidence.TransportJoined || evidence.RemoteOutcome != "UNKNOWN" {
		t.Fatal("joined uncertain tail became remote success")
	}
}

// TestActiveStreamIdleRevocationClosesWithoutPolling proves authority is checked without a client exchange.
// TestActiveStreamIdleRevocationClosesWithoutPolling доказывает проверку authority без client exchange.
func TestActiveStreamIdleRevocationClosesWithoutPolling(t *testing.T) {
	stream, owner, access, _, _ := activeStreamFixture(t, func(owner *activeOwnerFixture, access *activeAccessFixture, _ *activeTransportFixture, _ *value.KubernetesStreamGrant) {
		access.ttl = 100 * time.Millisecond
		owner.finalState = "REVOKED"
	})
	access.revoked.Store(true)
	select {
	case <-stream.done:
	case <-time.After(time.Second):
		t.Fatal("idle revoked stream stayed open")
	}
	result, err := stream.snapshot()
	if err != nil || result.State != "REVOKED" || result.RemoteOutcome != "UNKNOWN" || access.calls.Load() < 2 {
		t.Fatal("fresh revoke or owner terminal readback lost")
	}
	if receipt := <-owner.closed; !receipt.TransportJoined {
		t.Fatal("revocation skipped local join")
	}
}

// TestActiveStreamUnaryCancellationDoesNotCancelIdleTransport isolates one lost poll from stream lifetime.
// TestActiveStreamUnaryCancellationDoesNotCancelIdleTransport отделяет lost poll от stream lifetime.
func TestActiveStreamUnaryCancellationDoesNotCancelIdleTransport(t *testing.T) {
	stream, _, _, transport, _ := activeStreamFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := stream.Exchange(ctx, stream.binding, 0, nil); err == nil {
		t.Fatal("cancelled poll succeeded")
	}
	select {
	case <-transport.closed:
		t.Fatal("cancelled poll killed idle transport")
	default:
	}
	exit := int32(0)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
	result, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
	if err != nil || result.State != "COMPLETED" {
		t.Fatal("stream could not continue after cancelled poll")
	}
}

// TestActiveStreamBindingAndHardDeadlineRejectReuse prevents foreign input and infinite idle renewal.
// TestActiveStreamBindingAndHardDeadlineRejectReuse запрещает foreign input и бесконечный idle renewal.
func TestActiveStreamBindingAndHardDeadlineRejectReuse(t *testing.T) {
	stream, owner, access, transport, frame := activeStreamFixture(t, func(owner *activeOwnerFixture, access *activeAccessFixture, _ *activeTransportFixture, grant *value.KubernetesStreamGrant) {
		grant.ExpiresAt = time.Now().Add(120 * time.Millisecond)
		owner.expires = grant.ExpiresAt
		access.ttl = time.Hour
	})
	foreign := stream.binding
	foreign.Generation++
	if _, err := stream.Exchange(t.Context(), foreign, 0, &frame); err == nil || access.calls.Load() != 1 || transport.writeCount.Load() != 0 {
		t.Fatal("foreign binding reached authority or writer")
	}
	select {
	case <-stream.done:
	case <-time.After(time.Second):
		t.Fatal("renewal extended hard deadline")
	}
	receipt := <-owner.closed
	if receipt.TerminalState != "EXPIRED" || receipt.RemoteOutcome != "UNKNOWN" {
		t.Fatal("hard deadline fabricated remote exit")
	}
}

// TestActiveStreamCloseBeforeACKKeepsObservedExit separates delivery loss from an observed remote exit.
// TestActiveStreamCloseBeforeACKKeepsObservedExit отделяет delivery loss от observed remote exit.
func TestActiveStreamCloseBeforeACKKeepsObservedExit(t *testing.T) {
	stream, owner, _, transport, _ := activeStreamFixture(t, nil)
	exit := int32(0)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{Channel: "STDOUT", Payload: []byte("final-output"), ExitCode: &exit}}
	first, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
	if err != nil || len(first.Output) != 1 {
		t.Fatal("final output was not delivered")
	}
	retained := stream.output.frame.Payload
	result, err := stream.Close()
	if err != nil || result.State != "LOST" || result.RemoteOutcome != "SUCCEEDED" {
		t.Fatal("local output loss changed independently observed remote exit")
	}
	for _, value := range retained {
		if value != 0 {
			t.Fatal("close retained private output bytes")
		}
	}
	receipt := <-owner.closed
	if receipt.TerminalReceipt.ExitCode == nil || *receipt.TerminalReceipt.ExitCode != 0 || owner.frameCompletes.Load() != 0 {
		t.Fatal("close invented output ACK or lost explicit remote exit")
	}
}

// TestActiveStreamCancelledAuthorityPollDoesNotRevoke isolates caller cancellation during a fresh check.
// TestActiveStreamCancelledAuthorityPollDoesNotRevoke отделяет caller cancellation во время fresh check.
func TestActiveStreamCancelledAuthorityPollDoesNotRevoke(t *testing.T) {
	var checks atomic.Int32
	stream, _, _, transport, _ := activeStreamFixture(t, func(_ *activeOwnerFixture, access *activeAccessFixture, _ *activeTransportFixture, _ *value.KubernetesStreamGrant) {
		access.check = func(ctx context.Context) error {
			if checks.Add(1) == 2 {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := stream.Exchange(ctx, stream.binding, 0, nil); err == nil {
		t.Fatal("cancelled authority check succeeded")
	}
	select {
	case <-transport.closed:
		t.Fatal("caller cancellation was confused with owner revocation")
	default:
	}
	exit := int32(1)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
	result, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
	if err != nil || result.State != "COMPLETED" || result.RemoteOutcome != "FAILED" {
		t.Fatal("stream lost valid subsequent terminal evidence")
	}
}

// TestActiveStreamClonesGrantChannels prevents caller mutation from silently widening input authority.
// TestActiveStreamClonesGrantChannels запрещает расширение input authority через caller mutation.
func TestActiveStreamClonesGrantChannels(t *testing.T) {
	var channels []string
	stream, _, _, transport, frame := activeStreamFixture(t, func(_ *activeOwnerFixture, _ *activeAccessFixture, _ *activeTransportFixture, grant *value.KubernetesStreamGrant) {
		channels = grant.InputChannels
	})
	channels[0] = "TERMINAL_RESIZE"
	if stream.grant.InputChannels[0] != "STDIN" {
		t.Fatal("caller mutated retained grant")
	}
	exit := int32(0)
	transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
	result, err := stream.Exchange(t.Context(), stream.binding, 0, &frame)
	if err != nil || result.State != "COMPLETED" || result.AcknowledgedInput != frame.Sequence || transport.writeCount.Load() != 1 {
		t.Fatal("original admitted input was replaced by caller mutation")
	}
}

// TestActiveStreamTerminalSnapshotKeepsAcknowledgedInput retains a confirmed input ACK through both explicit close and native completion; repeated terminal reads neither regress the sequence nor repeat the write.
// TestActiveStreamTerminalSnapshotKeepsAcknowledgedInput сохраняет подтверждённый input ACK при explicit close и native completion; повторные terminal reads не уменьшают sequence и не повторяют запись.
func TestActiveStreamTerminalSnapshotKeepsAcknowledgedInput(t *testing.T) {
	for _, finish := range []string{"close", "native-exit"} {
		t.Run(finish, func(t *testing.T) {
			stream, owner, _, transport, frame := activeStreamFixture(t, nil)
			first, err := stream.Exchange(t.Context(), stream.binding, 0, &frame)
			if err != nil || first.State != "ACTIVE" || first.AcknowledgedInput != frame.Sequence || transport.writeCount.Load() != 1 {
				t.Fatal("initial input did not receive one confirmed active ACK")
			}
			if finish == "close" {
				closed := make(chan struct{})
				go func() { _, _ = stream.Close(); close(closed) }()
				select {
				case <-closed:
				case <-time.After(2 * time.Second):
					t.Fatal("asynchronous close did not join")
				}
			} else {
				exit := int32(0)
				transport.reads <- streamReadEvent{read: value.KubernetesStreamRead{ExitCode: &exit}}
				if final, err := stream.Exchange(t.Context(), stream.binding, 0, nil); err != nil || final.State != "COMPLETED" || final.AcknowledgedInput != frame.Sequence {
					t.Fatal("native completion lost original input ACK")
				}
			}
			<-owner.closed
			for range 32 {
				result, err := stream.Exchange(t.Context(), stream.binding, 0, nil)
				if err != nil || result.AcknowledgedInput != frame.Sequence || result.State == "ACTIVE" || len(result.Output) != 0 || transport.writeCount.Load() != 1 || owner.frameCompletes.Load() != 1 {
					t.Fatal("terminal snapshot regressed confirmed ACK or repeated an effect")
				}
			}
		})
	}
}
