package kubernetestarget

import (
	"context"
	"encoding/binary"
	"io"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
)

// nativePortForwardWebSocket carries ordered pinned TCP ports using the upstream kubelet v4 channel protocol.
// It opens no local listener, accepts no host/port override and never interprets socket EOF as remote success.
// nativePortForwardWebSocket передаёт ordered pinned TCP ports через upstream kubelet v4 channel protocol.
// Не открывает local listener, не принимает host/port override и не считает socket EOF remote success.
type nativePortForwardWebSocket struct {
	*nativePodWebSocket
	readMu, writeMu, budgetMu sync.Mutex
	ports                     []uint16
	maximum, spent            uint64
	reader                    io.Reader
	channel                   byte
	prefix                    [managedkubernetes.MaximumForwardPorts * 2][2]byte
	prefixLength              [managedkubernetes.MaximumForwardPorts * 2]int
	buffer                    [managedkubernetes.MaximumFrameBytes]byte
}

// openPortForwardWebSocket admits only the explicitly selected native v4 profile, with no SPDY fallback.
// The upper owner must already have checked the Pod UID, serving-key pin, grants and required Kubernetes RBAC.
// openPortForwardWebSocket допускает лишь явно выбранный native v4 profile без SPDY fallback.
// Верхний owner предварительно проверяет Pod UID, serving-key pin, grants и необходимые Kubernetes RBAC.
func (credential Credential) openPortForwardWebSocket(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time,
	target PodStreamTarget, ports []uint16, expiresAt, admissionUntil time.Time, maximum uint64) (*nativePortForwardWebSocket, error) {
	if pin.Protocol != PrivilegedPortForwardWebSocketV4 || len(ports) == 0 || len(ports) > managedkubernetes.MaximumForwardPorts {
		return nil, errNativeStreamUnknown
	}
	ports = append([]uint16(nil), ports...)
	connection, err := credential.openPodWebSocket(ctx, attempt, pin, now, "kubernetes.port-forward", target, PodStreamOptions{Ports: ports}, expiresAt, admissionUntil, maximum)
	if err != nil {
		return nil, err
	}
	return &nativePortForwardWebSocket{nativePodWebSocket: connection, ports: ports, maximum: maximum}, nil
}

// spend counts application bytes in both directions under one immutable transport budget.
// spend считает application bytes обоих направлений в одном immutable transport budget.
func (stream *nativePortForwardWebSocket) spend(count int) bool {
	stream.budgetMu.Lock()
	defer stream.budgetMu.Unlock()
	if count < 0 || stream.ctx.Err() != nil || uint64(count) > stream.maximum-stream.spent {
		return false
	}
	stream.spent += uint64(count)
	return true
}

// fail hides upstream errors while retaining an existing cancellation cause and joining the local watcher.
// fail скрывает upstream errors, сохраняя существующую cancellation cause и присоединяя local watcher.
func (stream *nativePortForwardWebSocket) fail(cause error) error {
	if err := stream.ctx.Err(); err != nil {
		cause = err
	}
	_ = stream.Close()
	return cause
}

// ReadStream checks each channel's little-endian port preface before releasing bounded application data.
// Error-channel data is private diagnostic material and is never emitted as tunnel output or a success receipt.
// ReadStream проверяет little-endian port preface каждого channel до выдачи bounded application data.
// Данные error channel являются private diagnostics и не выдаются как tunnel output либо success receipt.
func (stream *nativePortForwardWebSocket) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	defer clear(stream.buffer[:])
	noProgress := 0
	for {
		noProgress++
		if noProgress > maximumNativeNoProgressSteps {
			return value.KubernetesStreamRead{}, stream.fail(errNativeStreamLimit)
		}
		if stream.ctx.Err() != nil {
			return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
		}
		if stream.reader == nil {
			kind, reader, err := stream.connection.NextReader()
			if err != nil || kind != websocket.BinaryMessage {
				return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
			}
			var channel [1]byte
			if _, err := io.ReadFull(reader, channel[:]); err != nil || int(channel[0]) >= len(stream.ports)*2 {
				return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
			}
			stream.channel, stream.reader = channel[0], reader
		}
		channel := stream.channel
		if stream.prefixLength[channel] < 2 {
			n, err := stream.reader.Read(stream.prefix[channel][stream.prefixLength[channel]:])
			stream.prefixLength[channel] += n
			if err == io.EOF {
				stream.reader = nil
			} else if err != nil {
				return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
			}
			if stream.prefixLength[channel] == 2 && binary.LittleEndian.Uint16(stream.prefix[channel][:]) != stream.ports[channel/2] {
				return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
			}
			continue
		}
		n, err := stream.reader.Read(stream.buffer[1:])
		if err == io.EOF {
			stream.reader = nil
		}
		if channel%2 == 1 && n > 0 {
			return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
		}
		budget := n
		if n > 0 {
			budget++
		}
		if !stream.spend(budget) {
			return value.KubernetesStreamRead{}, stream.fail(errNativeStreamLimit)
		}
		if n == 0 {
			if err != nil && err != io.EOF {
				return value.KubernetesStreamRead{}, stream.fail(errNativeStreamUnknown)
			}
			continue
		}
		stream.buffer[0] = channel / 2
		output := value.KubernetesStreamRead{Channel: "PORT_FORWARD", Payload: append([]byte(nil), stream.buffer[:n+1]...)}
		if err != nil && err != io.EOF {
			return output, stream.fail(errNativeStreamUnknown)
		}
		return output, nil
	}
}

// WriteFrame sends one owner-admitted data message and joins deadline cancellation without replay.
// v4 has no TCP half-close: EndOfInput closes this whole local tunnel, never claiming remote quiescence.
// WriteFrame передаёт одно owner-admitted data message и ожидает deadline cancellation без replay.
// v4 не имеет TCP half-close: EndOfInput закрывает весь local tunnel, не утверждая remote quiescence.
func (stream *nativePortForwardWebSocket) WriteFrame(ctx context.Context, frame value.KubernetesStreamFrame) error {
	if stream == nil || ctx == nil || ctx.Err() != nil || frame.Channel != "PORT_FORWARD" ||
		managedkubernetes.ValidatePortForwardFrame(frame.Payload, frame.EndOfInput, len(stream.ports)) != nil {
		return errNativeStreamUnknown
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.After(time.Now()) {
		return errNativeStreamUnknown
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(joined); _ = stream.Close() })
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if ctx.Err() != nil || stream.ctx.Err() != nil {
		return errNativeStreamUnknown
	}
	if !stream.spend(len(frame.Payload)) {
		return stream.fail(errNativeStreamLimit)
	}
	if frame.EndOfInput {
		return stream.Close()
	}
	if local := time.Now().Add(5 * time.Second); local.Before(deadline) {
		deadline = local
	}
	if lifetime, ok := stream.ctx.Deadline(); ok && lifetime.Before(deadline) {
		deadline = lifetime
	}
	wire := append([]byte{frame.Payload[0] * 2}, frame.Payload[1:]...)
	defer clear(wire)
	if stream.connection.SetWriteDeadline(deadline) != nil || stream.connection.WriteMessage(websocket.BinaryMessage, wire) != nil || ctx.Err() != nil {
		return stream.fail(errNativeStreamUnknown)
	}
	return nil
}

// String prevents formatting private tunnel buffers or its owner-selected endpoint.
// String предотвращает форматирование private tunnel buffers либо owner-selected endpoint.
func (*nativePortForwardWebSocket) String() string {
	return "[managed Kubernetes native port-forward transport]"
}

// GoString preserves redaction for detailed diagnostic formatting.
// GoString сохраняет redaction для подробного diagnostic formatting.
func (stream *nativePortForwardWebSocket) GoString() string { return stream.String() }

// The assertion keeps the transport in the existing credential-free stream lifecycle.
// Проверка удерживает transport внутри existing credential-free stream lifecycle.
var _ targetrepo.LiveStream = (*nativePortForwardWebSocket)(nil)
