package kubernetestarget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/gorilla/websocket"
)

const (
	// nativeExecRedacted is the only diagnostic rendering of live transport/input state.
	// nativeExecRedacted — единственное diagnostic rendering live transport/input state.
	nativeExecRedacted = "[managed Kubernetes native exec transport]"
	// maximumNativeHandshakeBytes bounds TLS plus HTTP upgrade bytes before any message decoder.
	// maximumNativeHandshakeBytes ограничивает TLS и HTTP upgrade bytes до message decoder.
	maximumNativeHandshakeBytes = 256 << 10
	// maximumNativeNoProgressSteps bounds parsing work within one read that returns no application bytes/status.
	// Progress resets this local bound; it never limits the number of useful messages over a stream lifetime.
	// maximumNativeNoProgressSteps ограничивает parsing work одного read без application bytes/status.
	// Progress сбрасывает local bound; число полезных messages за stream lifetime не ограничивается.
	maximumNativeNoProgressSteps = 256
	// maximumNativeDuration is a hard transport ceiling, independent of renewable owner leases.
	// maximumNativeDuration — hard transport ceiling независимо от renewable owner leases.
	maximumNativeDuration = 10 * time.Minute
)

var (
	// errNativeStreamUnknown conceals peer errors and never turns transport EOF into remote success.
	// errNativeStreamUnknown скрывает peer errors и не превращает transport EOF в remote success.
	errNativeStreamUnknown = errors.New("managed Kubernetes native stream outcome is unknown")
	// errNativeStreamLimit closes the entire transport when a local immutable budget is exhausted.
	// errNativeStreamLimit закрывает весь transport после исчерпания local immutable budget.
	errNativeStreamLimit = errors.New("managed Kubernetes native stream limit exceeded")
)

// nativeWireConn bounds encrypted network bytes, including pre-upgrade HTTP headers.
// It contains no credential material and never substitutes another node after a failed attempt.
// nativeWireConn ограничивает encrypted network bytes, включая pre-upgrade HTTP headers.
// Не содержит credential material и не подставляет другой node после неуспешной attempt.
type nativeWireConn struct {
	net.Conn
	upgraded atomic.Bool
	read     atomic.Uint64
	written  atomic.Uint64
	maximum  uint64
}

// Read enforces handshake and lifetime wire ceilings before callers can parse additional bytes.
// Read проверяет handshake и lifetime wire ceilings до разбора дополнительных байтов caller.
func (connection *nativeWireConn) Read(buffer []byte) (int, error) {
	limit := connection.maximum
	if !connection.upgraded.Load() {
		limit = maximumNativeHandshakeBytes
	}
	count := connection.read.Load()
	if count >= limit {
		_ = connection.Conn.Close()
		return 0, errNativeStreamLimit
	}
	if uint64(len(buffer)) > limit-count+1 {
		buffer = buffer[:int(limit-count+1)]
	}
	n, err := connection.Conn.Read(buffer)
	if connection.read.Add(uint64(n)) > limit {
		_ = connection.Conn.Close()
		return 0, errNativeStreamLimit
	}
	return n, err
}

// Write bounds outgoing encrypted bytes without claiming that a peer consumed the input.
// Write ограничивает outgoing encrypted bytes без утверждения о потреблении input peer.
func (connection *nativeWireConn) Write(buffer []byte) (int, error) {
	if connection.written.Add(uint64(len(buffer))) > connection.maximum {
		_ = connection.Conn.Close()
		return 0, errNativeStreamLimit
	}
	return connection.Conn.Write(buffer)
}

// nativeExecWebSocket owns exactly one v5 connection and one cancel watcher.
// Input/output bytes still require fresh owner frame credit in the service above this adapter;
// local write success is not remote input execution and local close is not remote process stop.
// nativeExecWebSocket владеет одной v5 connection и одним cancel watcher.
// Input/output bytes требуют fresh owner frame credit в service над этим adapter;
// local write success не означает remote input execution, local close — remote process stop.
type nativeExecWebSocket struct {
	*nativePodWebSocket
	readMu, writeMu, stateMu sync.Mutex
	reader                   io.Reader
	channel                  byte
	options                  PodStreamOptions
	operation                string
	maximum, spent           uint64
	inputClosed, statusEmpty bool
	terminal                 bool
	readBuffer               [managedkubernetes.MaximumFrameBytes]byte
}

// openExecWebSocket selects the qualified exec/attach parser over the shared pinned Pod transport.
// openExecWebSocket выбирает квалифицированный exec/attach parser поверх общего pinned Pod transport.
func (credential Credential) openExecWebSocket(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, operation string,
	target PodStreamTarget, options PodStreamOptions, expiresAt, admissionUntil time.Time, maximum uint64) (*nativeExecWebSocket, error) {
	if pin.Protocol != PrivilegedWebSocketV5 || operation != "kubernetes.exec" && operation != "kubernetes.attach" && operation != "kubernetes.cp.upload" && operation != "kubernetes.cp.download" {
		return nil, errNativeStreamUnknown
	}
	if operation == "kubernetes.cp.upload" || operation == "kubernetes.cp.download" {
		if options.Copy.Validate(operation) != nil {
			return nil, errNativeStreamUnknown
		}
		maximum = min(maximum, options.Copy.MaximumBytes)
	}
	connection, err := credential.openPodWebSocket(ctx, attempt, pin, now, operation, target, options, expiresAt, admissionUntil, maximum)
	if err != nil {
		return nil, err
	}
	return &nativeExecWebSocket{nativePodWebSocket: connection, options: options, operation: operation, maximum: maximum}, nil
}

// spend reserves immutable local payload credit across both directions, before any delivery.
// spend резервирует immutable local payload credit в обоих направлениях до любой выдачи.
func (stream *nativeExecWebSocket) spend(count int) bool {
	stream.stateMu.Lock()
	defer stream.stateMu.Unlock()
	if count < 0 || stream.spent+uint64(count) > stream.maximum || stream.ctx.Err() != nil || stream.terminal {
		return false
	}
	stream.spent += uint64(count)
	return true
}

// ReadOutput emits one bounded stdout/stderr chunk or an explicit strict exit receipt.
// It accepts the protocol's initial empty notification without treating it as success; peer EOF
// or malformed/duplicate status remains UNKNOWN. Returned chunks still await owner output credit.
// A valid partial chunk can accompany a transport error; consumers retain it before terminal closure.
// ReadOutput выдаёт один bounded stdout/stderr chunk либо explicit strict exit receipt.
// Принимает initial empty notification без превращения в success; peer EOF либо malformed/
// duplicate status остаётся UNKNOWN. Возвращённый chunk ещё требует owner output credit.
// Valid partial chunk может сопровождаться transport error; consumer сохраняет его до terminal closure.
func (stream *nativeExecWebSocket) ReadOutput() (string, []byte, *int32, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	defer clear(stream.readBuffer[:])
	noProgress := 0
	for {
		noProgress++
		if noProgress > maximumNativeNoProgressSteps {
			return "", nil, nil, stream.closeFailure(errNativeStreamLimit)
		}
		if err := stream.ctx.Err(); err != nil {
			return "", nil, nil, err
		}
		if stream.reader == nil {
			messageType, reader, err := stream.connection.NextReader()
			if err != nil || messageType != websocket.BinaryMessage {
				return "", nil, nil, stream.closeFailure(errNativeStreamUnknown)
			}
			var channel [1]byte
			if _, err := io.ReadFull(reader, channel[:]); err != nil {
				return "", nil, nil, stream.closeFailure(errNativeStreamUnknown)
			}
			stream.channel, stream.reader = channel[0], reader
		}
		if stream.channel == 3 {
			raw, err := io.ReadAll(io.LimitReader(stream.reader, maximumExecStatusBytes+1))
			stream.reader = nil
			if err == nil && len(raw) == 0 && !stream.statusEmpty {
				stream.statusEmpty = true
				continue
			}
			code, decodeErr := decodeNativeExecStatus(raw)
			clear(raw)
			if err != nil || decodeErr != nil {
				return "", nil, nil, stream.closeFailure(errExecStatusUnknown)
			}
			// Preserve the one final FILE data+close writer before normal terminal transport teardown.
			// Сохраняем единственный final FILE data+close writer до normal terminal transport teardown.
			if stream.operation == "kubernetes.cp.upload" {
				stream.writeMu.Lock()
			}
			stream.stateMu.Lock()
			stream.terminal = true
			stream.stateMu.Unlock()
			_ = stream.Close()
			if stream.operation == "kubernetes.cp.upload" {
				stream.writeMu.Unlock()
			}
			return "", nil, &code, nil
		}
		channel := ""
		if stream.channel == 1 && stream.options.Stdout {
			channel = "STDOUT"
		} else if stream.channel == 2 && stream.options.Stderr && !stream.options.TTY {
			channel = "STDERR"
		} else {
			return "", nil, nil, stream.closeFailure(errNativeStreamUnknown)
		}
		n, err := stream.reader.Read(stream.readBuffer[:])
		if err == io.EOF {
			stream.reader = nil
		}
		if n == 0 {
			if err != nil && err != io.EOF {
				return "", nil, nil, stream.closeFailure(errNativeStreamUnknown)
			}
			continue
		}
		if !stream.spend(n) {
			return "", nil, nil, stream.closeFailure(errNativeStreamLimit)
		}
		payload := append([]byte(nil), stream.readBuffer[:n]...)
		if err != nil && err != io.EOF {
			return channel, payload, nil, stream.closeFailure(errNativeStreamUnknown)
		}
		return channel, payload, nil, nil
	}
}

// SendStdin writes one owner-admitted bounded data frame or a v5 stdin half-close.
// ACK means local transport accepted the write, not remote execution. Any failed write stays
// non-replayable; the owner must retain UNKNOWN instead of resending the same stdin sequence.
// SendStdin пишет один owner-admitted bounded data frame либо v5 stdin half-close.
// ACK означает local transport acceptance, не remote execution. Ошибка write не допускает
// replay; owner сохраняет UNKNOWN вместо повтора той же stdin sequence.
func (stream *nativeExecWebSocket) SendStdin(payload []byte, end bool) error {
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if !stream.options.Stdin || stream.inputClosed || len(payload) > managedkubernetes.MaximumFrameBytes ||
		len(payload) == 0 && !end || len(payload) != 0 && end || !stream.spend(len(payload)) {
		return errNativeStreamUnknown
	}
	wire := append([]byte{0}, payload...)
	if end {
		wire = []byte{255, 0}
		stream.inputClosed = true
	}
	return stream.writeFrame(wire)
}

// Resize writes only a bounded typed terminal size on the registered resize channel.
// It requires its own current owner frame credit and never accepts a raw control message.
// Resize пишет только bounded typed terminal size в registered resize channel.
// Требует current owner frame credit и не принимает raw control message.
func (stream *nativeExecWebSocket) Resize(columns, rows uint16) error {
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if !stream.options.TTY || columns == 0 || rows == 0 || columns > 1024 || rows > 512 {
		return errNativeStreamUnknown
	}
	payload, err := json.Marshal(struct{ Width, Height uint16 }{Width: columns, Height: rows})
	if err != nil || !stream.spend(len(payload)) {
		return errNativeStreamUnknown
	}
	return stream.writeFrame(append([]byte{4}, payload...))
}

// writeFrame sends one private protocol message while the caller holds writeMu.
// Its local acknowledgement does not prove peer consumption or remote process execution.
// writeFrame отправляет private protocol message под writeMu caller.
// Local acknowledgement не доказывает peer consumption либо remote process execution.
func (stream *nativeExecWebSocket) writeFrame(wire []byte) error {
	defer clear(wire)
	if stream.ctx.Err() != nil {
		return stream.closeFailure(errNativeStreamUnknown)
	}
	deadline := time.Now().Add(5 * time.Second)
	if ownerDeadline, ok := stream.ctx.Deadline(); ok && ownerDeadline.Before(deadline) {
		deadline = ownerDeadline
	}
	if stream.connection.SetWriteDeadline(deadline) != nil || stream.connection.WriteMessage(websocket.BinaryMessage, wire) != nil {
		return stream.closeFailure(errNativeStreamUnknown)
	}
	return nil
}

// closeFailure preserves the pre-existing cancellation cause while joining local transport.
// Cleanup's own cancellation cannot overwrite an already observed protocol or limit failure.
// closeFailure сохраняет прежнюю cancellation cause при join local transport.
// Собственная cancellation cleanup не заменяет наблюдённую protocol либо limit failure.
func (stream *nativeExecWebSocket) closeFailure(failure error) error {
	if err := stream.ctx.Err(); err != nil {
		failure = err
	}
	_ = stream.Close()
	return failure
}

// String prevents accidental diagnostic rendering of queued data and command arguments.
// String предотвращает случайный diagnostic rendering queued data и command arguments.
func (*nativeExecWebSocket) String() string { return nativeExecRedacted }

// GoString keeps detailed Go formatting inside the same private transport boundary.
// GoString удерживает detailed Go formatting внутри той же private transport boundary.
func (*nativeExecWebSocket) GoString() string { return nativeExecRedacted }
