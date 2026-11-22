package kubernetestarget

import (
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/moby/spdystream"
	corev1 "k8s.io/api/core/v1"
)

// maximumForwardPendingCandidates bounds retained already-read events across active/closing pairs, independently from lifetime connections.
// maximumForwardPendingCandidates ограничивает retained already-read events active/closing pairs независимо от lifetime connections.
const maximumForwardPendingCandidates = managedkubernetes.MaximumPortForwardConnections * 2

// maximumForwardJoinedReceipts bounds private local facts; fresh protected OPEN snapshots retire exact obsolete identities without count-based eviction.
// maximumForwardJoinedReceipts ограничивает private local facts; fresh protected OPEN snapshots удаляют exact obsolete identities без count-based eviction.
const maximumForwardJoinedReceipts = managedkubernetes.MaximumPortForwardConnections * 2

// nativeForwardJoined records only an actual positive local pair join inside its original parent transport, without payload, remote success or reusable admission.
// nativeForwardJoined фиксирует только actual positive local pair join внутри original parent transport, без payload, remote success либо reusable admission.
type nativeForwardJoined struct {
	generation uint64
	slot       byte
}

// nativeForwardPair owns one immutable connection generation and its native DATA/ERROR pair, never an independent owner lease.
// nativeForwardPair владеет immutable connection generation и native DATA/ERROR pair, не отдельной owner lease.
type nativeForwardPair struct {
	eventMu                sync.Mutex
	nativeOrdinal          uint64
	ref                    string
	generation             uint64
	slot                   byte
	data, diagnostic       *spdystream.Stream
	ctx                    context.Context
	cancel                 context.CancelFunc
	readers                sync.WaitGroup
	joined                 chan struct{}
	writeClosed, readEnded bool
	requestedClose         bool
	closeRequested         chan struct{}
}

// nativeForwardConnections carries only owner-admitted connection envelopes over the selected SPDY control connection.
// Zero pairs keep the parent transport alive; per-pair EOF never completes the parent tool or proves remote success.
// nativeForwardConnections передаёт только owner-admitted connection envelopes через selected SPDY control connection.
// Ноль pairs сохраняет parent transport; EOF отдельной пары не завершает parent tool и не доказывает remote success.
type nativeForwardConnections struct {
	*nativeSPDYConnection
	ports                         []uint16
	mu, writeMu, readMu, budgetMu sync.Mutex
	pairs                         map[string]*nativeForwardPair
	joined                        map[string]nativeForwardJoined
	retainable                    map[string]nativeForwardJoined
	retentionGeneration           uint64
	highestGeneration             uint64
	closed                        bool
	maximum, spent                uint64
	events                        chan []byte
	workers                       sync.WaitGroup
	done                          chan struct{}
}

// openPortForwardConnectionsSPDY opens only the pinned control transport, with no eager target TCP connection and no protocol fallback.
// The caller supplies an already-admitted parent effect; every later pair still requires its own staged INPUT OPEN.
// openPortForwardConnectionsSPDY открывает лишь pinned control transport без eager target TCP connection и protocol fallback.
// Caller передаёт already-admitted parent effect; каждая последующая пара требует отдельного staged INPUT OPEN.
func (credential Credential) openPortForwardConnectionsSPDY(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, target PodStreamTarget, ports []uint16, expiresAt, admissionUntil time.Time, maximum uint64) (*nativeForwardConnections, error) {
	if pin.Protocol != PrivilegedPortForwardSPDYV1 || len(ports) < 1 || len(ports) > managedkubernetes.MaximumForwardPorts {
		return nil, errNativeStreamUnknown
	}
	connection, err := credential.openPodSPDY(ctx, attempt, pin, now, "kubernetes.port-forward", target, PodStreamOptions{Ports: ports}, expiresAt, admissionUntil, maximum, managedkubernetes.MaximumPortForwardConnections*2, func(*nativeSPDYConnection) error { return nil })
	if err != nil {
		return nil, err
	}
	stream := &nativeForwardConnections{nativeSPDYConnection: connection, ports: append([]uint16(nil), ports...), pairs: make(map[string]*nativeForwardPair), joined: make(map[string]nativeForwardJoined), retainable: make(map[string]nativeForwardJoined), maximum: maximum, events: make(chan []byte, maximumForwardPendingCandidates), done: make(chan struct{})}
	go stream.superviseConnections()
	return stream, nil
}

// superviseConnections retains the parent lifetime while idle and joins all owned pairs after closing the original socket.
// superviseConnections сохраняет parent lifetime при idle и присоединяет owned pairs после закрытия original socket.
func (stream *nativeForwardConnections) superviseConnections() {
	defer close(stream.done)
	<-stream.ctx.Done()
	_ = stream.nativeSPDYConnection.Close()
	stream.mu.Lock()
	stream.closed = true
	for _, pair := range stream.pairs {
		pair.cancel()
	}
	stream.mu.Unlock()
	stream.workers.Wait()
	stream.mu.Lock()
	clear(stream.joined)
	clear(stream.retainable)
	stream.mu.Unlock()
	// No producer remains after the positive pair join; destroy all unconsumed private candidates on parent closure.
	// После positive pair join producers нет; parent closure уничтожает все unconsumed private candidates.
	for {
		select {
		case raw := <-stream.events:
			clear(raw)
		default:
			return
		}
	}
}

// rememberJoinedPairLocked retains only exact owner-selected remote closures after positive SDK/readers join; late obsolete joins cannot reinsert retired facts.
// No count-based eviction or lifetime connection cap is used. An impossible capacity mismatch closes the parent instead of discarding a still-needed fact.
// rememberJoinedPairLocked сохраняет лишь exact owner-selected remote closures после positive SDK/readers join; late obsolete joins не возвращают удалённые факты.
// Count-based eviction и lifetime connection cap отсутствуют. Невозможный capacity mismatch закрывает parent вместо удаления нужного факта.
func (stream *nativeForwardConnections) rememberJoinedPairLocked(pair *nativeForwardPair) {
	fact := nativeForwardJoined{generation: pair.generation, slot: pair.slot}
	if pair.generation <= stream.retentionGeneration {
		if selected, known := stream.retainable[pair.ref]; !known || selected != fact {
			return
		}
	}
	if len(stream.joined) >= maximumForwardJoinedReceipts {
		stream.cancel()
		return
	}
	stream.joined[pair.ref] = fact
}

// consumeJoinedPairLocked accepts only CLOSE for the exact positively joined incarnation and removes that fact; it never authorizes DATA or another OPEN.
// consumeJoinedPairLocked принимает только CLOSE для exact positively joined incarnation и удаляет факт; DATA либо новый OPEN не разрешаются.
func (stream *nativeForwardConnections) consumeJoinedPairLocked(frame managedkubernetes.PortForwardConnectionFrame) bool {
	fact, found := stream.joined[frame.ConnectionRef]
	if !found || frame.Action != managedkubernetes.PortForwardConnectionClose || fact.generation != frame.Generation || fact.slot != frame.Slot {
		return false
	}
	delete(stream.joined, frame.ConnectionRef)

	return true
}

// spend accounts complete protected envelopes, including controls and headers, under the unchanged combined-direction byte budget.
// spend учитывает полные protected envelopes, включая controls и headers, в неизменном combined-direction byte budget.
func (stream *nativeForwardConnections) spend(count int) bool {
	stream.budgetMu.Lock()
	defer stream.budgetMu.Unlock()
	if count < 0 || uint64(count) > stream.maximum-stream.spent {
		return false
	}
	stream.spent += uint64(count)
	return true
}

// emitConnection transfers one owned candidate through bounded backpressure; delivery still requires the existing owner OUTPUT credit and accepted sink.
// emitConnection передаёт один owned candidate через bounded backpressure; delivery требует existing owner OUTPUT credit и accepted sink.
func (stream *nativeForwardConnections) emitConnection(ctx context.Context, pair *nativeForwardPair, action managedkubernetes.PortForwardConnectionAction, body []byte) bool {
	pair.eventMu.Lock()
	defer pair.eventMu.Unlock()
	if pair.nativeOrdinal >= math.MaxInt64 {
		stream.cancel()
		return false
	}
	pair.nativeOrdinal++
	sent := false
	defer func() {
		if !sent {
			pair.nativeOrdinal--
		}
	}()
	raw, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{Action: action, Slot: pair.slot, Generation: pair.generation, ConnectionRef: pair.ref, NativeOrdinal: pair.nativeOrdinal, Body: body})
	if err != nil {
		stream.cancel()
		return false
	}
	if !stream.spend(len(raw)) {
		clear(raw)
		stream.cancel()
		return false
	}
	if action == managedkubernetes.PortForwardConnectionHalfClose || action == managedkubernetes.PortForwardConnectionClose {
		select {
		case stream.events <- raw:
			sent = true
			return true
		case <-pair.closeRequested:
			// INPUT CLOSE carries the actual pair join instead; only an unpublished zero-body control is withdrawn, never already-read DATA.
			// INPUT CLOSE передаёт actual pair join; снимается лишь unpublished zero-body control, не already-read DATA.
			stream.budgetMu.Lock()
			stream.spent -= uint64(len(raw))
			stream.budgetMu.Unlock()
			clear(raw)
			return false
		case <-stream.ctx.Done():
			clear(raw)
			return false
		}
	}
	select {
	case stream.events <- raw:
		sent = true
		return true
	case <-ctx.Done():
		// A pair may close after reading data but before the owner sees its candidate. Retain it for the CLOSED/loss path.
		// If bounded retention is impossible, lose the parent explicitly instead of reporting a successful silent drop.
		// Пара может закрыться после чтения data до owner candidate. Сохраняем его для CLOSED/loss path.
		// При невозможности bounded retention parent явно теряется вместо successful silent drop.
		if stream.ctx.Err() == nil {
			select {
			case stream.events <- raw:
				sent = true
				return true
			default:
				stream.cancel()
			}
		}
		clear(raw)
		return false
	case <-stream.ctx.Done():
		clear(raw)
		return false
	}
}

// readConnectionData preserves binary chunks and records read-end without claiming that SDK EOF distinguishes TCP FIN from reset.
// readConnectionData сохраняет binary chunks и фиксирует read-end без утверждения, что SDK EOF различает TCP FIN и reset.
func (stream *nativeForwardConnections) readConnectionData(pair *nativeForwardPair) {
	defer pair.readers.Done()
	var raw [managedkubernetes.MaximumFrameBytes - managedkubernetes.PortForwardConnectionFrameHeaderBytes]byte
	defer clear(raw[:])
	for {
		n, err := pair.data.Read(raw[:])
		if n > 0 && !stream.emitConnection(pair.ctx, pair, managedkubernetes.PortForwardConnectionData, raw[:n]) {
			return
		}
		// Reset-induced EOF is not an observed peer half-close; already-read data above was retained separately.
		// EOF от Reset не является observed peer half-close; уже прочитанные data выше сохранены отдельно.
		if pair.ctx.Err() != nil {
			return
		}
		if err != nil {
			if err != io.EOF {
				pair.cancel()
				return
			}
			stream.mu.Lock()
			pair.readEnded = true
			both := pair.writeClosed
			stream.mu.Unlock()
			_ = stream.emitConnection(pair.ctx, pair, managedkubernetes.PortForwardConnectionHalfClose, nil)
			if both {
				pair.cancel()
			}
			return
		}
		if pair.ctx.Err() != nil {
			return
		}
	}
}

// readConnectionDiagnostic checks only presence of private upstream errors; no diagnostic byte is exposed to the agent or local bridge.
// readConnectionDiagnostic проверяет лишь наличие private upstream errors; diagnostic bytes не выдаются agent либо local bridge.
func (stream *nativeForwardConnections) readConnectionDiagnostic(pair *nativeForwardPair) {
	defer pair.readers.Done()
	var raw [1]byte
	defer clear(raw[:])
	n, err := pair.diagnostic.Read(raw[:])
	if n > 0 || err != io.EOF && pair.ctx.Err() == nil {
		pair.cancel()
	}
}

// supervisePair resets and joins one pair before publishing local closure, retaining its concurrency slot until the closure candidate is retained.
// Cleanup may close the parent socket on expiry; it never retries opening or writes new application bytes.
// supervisePair сбрасывает и присоединяет одну пару до local closure publication, удерживая concurrency slot до сохранения closure candidate.
// Истёкший cleanup может закрыть parent socket; он не повторяет opening и не пишет application bytes.
func (stream *nativeForwardConnections) supervisePair(pair *nativeForwardPair) {
	defer stream.workers.Done()
	<-pair.ctx.Done()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(stream.ctx), 5*time.Second)
	joined := make(chan struct{})
	stop := context.AfterFunc(cleanup, func() { defer close(joined); _ = stream.nativeSPDYConnection.Close() })
	_ = pair.data.Reset()
	_ = pair.diagnostic.Reset()
	pair.readers.Wait()
	stream.forgetForwardStreams(pair.data, pair.diagnostic)
	if !stop() {
		<-joined
	}
	cancel()
	stream.mu.Lock()
	requested := pair.requestedClose
	stream.mu.Unlock()
	if !requested && stream.ctx.Err() == nil {
		// Read-end and local join remain UNKNOWN remote facts, even when the opposite direction already half-closed.
		// Read-end и local join остаются UNKNOWN remote facts, даже если противоположное направление уже half-closed.
		_ = stream.emitConnection(stream.ctx, pair, managedkubernetes.PortForwardConnectionClose, nil)
	}
	stream.mu.Lock()
	if !pair.requestedClose && stream.ctx.Err() == nil {
		stream.rememberJoinedPairLocked(pair)
	}
	delete(stream.pairs, pair.ref)
	stream.mu.Unlock()
	close(pair.joined)
}

// openConnection consumes one new owner generation before creating its exact requestID pair; an uncertain opening closes the parent instead of retrying.
// openConnection расходует новую owner generation до создания exact requestID pair; uncertain opening закрывает parent вместо retry.
func (stream *nativeForwardConnections) openConnection(ctx context.Context, frame managedkubernetes.PortForwardConnectionFrame, retention managedkubernetes.PortForwardRetentionSet) error {
	stream.mu.Lock()
	_, alreadyJoined := stream.joined[frame.ConnectionRef]
	if stream.closed || stream.ctx.Err() != nil || len(stream.pairs) >= managedkubernetes.MaximumPortForwardConnections || frame.Generation <= stream.highestGeneration || stream.pairs[frame.ConnectionRef] != nil || alreadyJoined {
		stream.mu.Unlock()
		return errNativeStreamUnknown
	}
	stream.retainable = make(map[string]nativeForwardJoined, len(retention.Connections))
	for _, item := range retention.Connections {
		stream.retainable[item.ConnectionRef] = nativeForwardJoined{generation: item.Generation, slot: item.Slot}
	}
	stream.retentionGeneration = frame.Generation
	for ref, fact := range stream.joined {
		if selected, found := stream.retainable[ref]; !found || selected != fact {
			delete(stream.joined, ref)
		}
	}
	stream.highestGeneration = frame.Generation
	stream.mu.Unlock()
	until, ok := ctx.Deadline()
	if !ok {
		return errNativeStreamUnknown
	}
	headers := make(http.Header)
	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	headers.Set(corev1.PortHeader, strconv.Itoa(int(stream.ports[frame.Slot])))
	headers.Set(corev1.PortForwardRequestIDHeader, strconv.FormatUint(frame.Generation, 10))
	diagnostic, err := stream.createStream(headers, until, true)
	if err != nil {
		stream.cancel()
		return errNativeStreamUnknown
	}
	if diagnostic.Close() != nil {
		stream.cancel()
		return errNativeStreamUnknown
	}
	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	data, err := stream.createStream(headers, until, true)
	if err != nil || ctx.Err() != nil {
		stream.cancel()
		return errNativeStreamUnknown
	}
	pairCtx, cancel := context.WithCancel(stream.ctx)
	pair := &nativeForwardPair{ref: frame.ConnectionRef, generation: frame.Generation, slot: frame.Slot, data: data, diagnostic: diagnostic, ctx: pairCtx, cancel: cancel, joined: make(chan struct{}), closeRequested: make(chan struct{})}
	stream.mu.Lock()
	if stream.closed || stream.ctx.Err() != nil {
		stream.mu.Unlock()
		cancel()
		return errNativeStreamUnknown
	}
	stream.pairs[pair.ref] = pair
	pair.readers.Add(2)
	stream.workers.Add(1)
	go stream.readConnectionData(pair)
	go stream.readConnectionDiagnostic(pair)
	go stream.supervisePair(pair)
	stream.mu.Unlock()
	return nil
}

// WriteFrame dispatches one original owner-staged envelope; controls never become a generic EOF, target selector or transport fallback.
// WriteFrame исполняет original owner-staged envelope; controls не превращаются в generic EOF, target selector либо transport fallback.
func (stream *nativeForwardConnections) WriteFrame(ctx context.Context, input value.KubernetesStreamFrame) error {
	return stream.writeFrame(ctx, input, nil)
}

// WritePortForwardOpen accepts only the protected owner admission snapshot; public KPF bytes cannot supply this separate purpose port.
// WritePortForwardOpen принимает лишь protected owner admission snapshot; public KPF bytes не передают данные этому отдельному purpose port.
func (stream *nativeForwardConnections) WritePortForwardOpen(ctx context.Context, input value.KubernetesStreamFrame, retention managedkubernetes.PortForwardRetentionSet) error {
	return stream.writeFrame(ctx, input, &retention)
}

// writeFrame preserves common deadlines and byte bounds while requiring a fresh exact retention snapshot before every native OPEN.
// writeFrame сохраняет common deadlines и byte bounds, требуя fresh exact retention snapshot перед каждым native OPEN.
func (stream *nativeForwardConnections) writeFrame(ctx context.Context, input value.KubernetesStreamFrame, retention *managedkubernetes.PortForwardRetentionSet) error {
	if stream == nil || ctx == nil || ctx.Err() != nil || stream.ctx.Err() != nil || input.Channel != "PORT_FORWARD" || input.EndOfInput {
		return errNativeStreamUnknown
	}
	frame, err := managedkubernetes.DecodePortForwardConnectionFrame(input.Payload)
	if err != nil || frame.ValidateInput(len(stream.ports)) != nil {
		return errNativeStreamUnknown
	}
	if frame.Action == managedkubernetes.PortForwardConnectionOpen {
		opening := managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: frame.ConnectionRef, Generation: frame.Generation, Slot: frame.Slot}
		if retention == nil || retention.Validate(len(stream.ports), &opening) != nil {
			return errNativeStreamUnknown
		}
	} else if retention != nil {
		return errNativeStreamUnknown
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(time.Now()) {
		return errNativeStreamUnknown
	}
	if limit := time.Now().Add(5 * time.Second); limit.Before(deadline) {
		deadline = limit
	}
	hard, bounded := stream.ctx.Deadline()
	if !bounded {
		return errNativeStreamUnknown
	}
	if hard.Before(deadline) {
		deadline = hard
	}
	call, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	joined := make(chan struct{})
	stop := context.AfterFunc(call, func() { defer close(joined); _ = stream.nativeSPDYConnection.Close() })
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if call.Err() != nil || stream.ctx.Err() != nil {
		return errNativeStreamUnknown
	}
	if !stream.spend(len(input.Payload)) {
		stream.cancel()
		return errNativeStreamLimit
	}
	if frame.Action == managedkubernetes.PortForwardConnectionOpen {
		return stream.openConnection(call, frame, retention.Clone())
	}
	stream.mu.Lock()
	pair := stream.pairs[frame.ConnectionRef]
	if pair == nil {
		if call.Err() != nil || stream.ctx.Err() != nil {
			stream.mu.Unlock()
			return errNativeStreamUnknown
		}
		known := stream.consumeJoinedPairLocked(frame)
		stream.mu.Unlock()
		if known && call.Err() == nil && stream.ctx.Err() == nil {
			return nil
		}
		return errNativeStreamUnknown
	}
	if pair.generation != frame.Generation || pair.slot != frame.Slot || pair.ctx.Err() != nil && frame.Action != managedkubernetes.PortForwardConnectionClose {
		stream.mu.Unlock()
		return errNativeStreamUnknown
	}
	writeClosed := pair.writeClosed
	if frame.Action == managedkubernetes.PortForwardConnectionClose {
		if !pair.requestedClose {
			pair.requestedClose = true
			close(pair.closeRequested)
		}
	}
	stream.mu.Unlock()
	switch frame.Action {
	case managedkubernetes.PortForwardConnectionData:
		if writeClosed {
			return errNativeStreamUnknown
		}
		n, writeErr := pair.data.Write(frame.Body)
		if writeErr != nil || n != len(frame.Body) || call.Err() != nil {
			pair.cancel()
			return errNativeStreamUnknown
		}
	case managedkubernetes.PortForwardConnectionHalfClose:
		if writeClosed || pair.data.Close() != nil {
			pair.cancel()
			return errNativeStreamUnknown
		}
		stream.mu.Lock()
		pair.writeClosed = true
		both := pair.readEnded
		stream.mu.Unlock()
		if both {
			pair.cancel()
		}
	case managedkubernetes.PortForwardConnectionClose:
		pair.cancel()
		select {
		case <-pair.joined:
		case <-call.Done():
			return errNativeStreamUnknown
		}
	default:
		return errNativeStreamUnknown
	}
	if call.Err() != nil || stream.ctx.Err() != nil {
		return errNativeStreamUnknown
	}
	return nil
}

// ReadStream releases only protected per-connection output candidates; a closed parent remains an unknown transport outcome.
// ReadStream выдаёт лишь protected per-connection output candidates; закрытый parent остаётся unknown transport outcome.
func (stream *nativeForwardConnections) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	select {
	case raw := <-stream.events:
		return value.KubernetesStreamRead{Channel: "PORT_FORWARD", Payload: raw}, nil
	case <-stream.done:
		return value.KubernetesStreamRead{}, errNativeStreamUnknown
	}
}

// Close joins parent and all pairs without asserting remote TCP delivery or process termination.
// Close присоединяет parent и все pairs без утверждения remote TCP delivery либо process termination.
func (stream *nativeForwardConnections) Close() error {
	_ = stream.nativeSPDYConnection.Close()
	stream.writeMu.Lock()
	stream.writeMu.Unlock()
	<-stream.done
	return nil
}

// String hides all live private buffers and connection coordinates.
// String скрывает live private buffers и connection coordinates.
func (*nativeForwardConnections) String() string {
	return "[managed Kubernetes port-forward connections]"
}

// GoString preserves detailed diagnostic redaction.
// GoString сохраняет redaction при подробной диагностике.
func (stream *nativeForwardConnections) GoString() string { return stream.String() }
