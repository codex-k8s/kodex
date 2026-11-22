package kubernetestarget

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
)

// nativePortForwardSPDY binds fixed ordered mapping slots to standard per-stream port/request headers.
// It opens no listeners or replacement TCP streams and never treats transport EOF as remote success.
// nativePortForwardSPDY связывает fixed ordered mapping slots со standard per-stream port/request headers.
// Не открывает listeners либо replacement TCP streams и не считает transport EOF remote success.
type nativePortForwardSPDY struct {
	*nativeSPDYConnection
	ports                     []uint16
	data, errors              []httpstream.Stream
	events                    chan nativeSPDYOutput
	done                      chan struct{}
	workers                   sync.WaitGroup
	readMu, writeMu, budgetMu sync.Mutex
	maximum, spent            uint64
}

// openPortForwardSPDY creates only the exact owner-admitted fixed mapping profile through one pinned upgrade.
// openPortForwardSPDY создаёт лишь exact owner-admitted fixed mapping profile через один pinned upgrade.
func (credential Credential) openPortForwardSPDY(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, target PodStreamTarget, ports []uint16, expiresAt, admissionUntil time.Time, maximum uint64) (*nativePortForwardSPDY, error) {
	if pin.Protocol != PrivilegedPortForwardSPDYV1 || len(ports) < 1 || len(ports) > managedkubernetes.MaximumForwardPorts {
		return nil, errNativeStreamUnknown
	}
	native := &nativePortForwardSPDY{ports: append([]uint16(nil), ports...), data: make([]httpstream.Stream, len(ports)), errors: make([]httpstream.Stream, len(ports)), events: make(chan nativeSPDYOutput), done: make(chan struct{}), maximum: maximum}
	connection, err := credential.openPodSPDY(ctx, attempt, pin, now, "kubernetes.port-forward", target, PodStreamOptions{Ports: native.ports}, expiresAt, admissionUntil, maximum, len(ports)*2, func(connection *nativeSPDYConnection) error {
		for slot, port := range native.ports {
			headers := make(http.Header)
			headers.Set(corev1.StreamType, corev1.StreamTypeError)
			headers.Set(corev1.PortHeader, strconv.Itoa(int(port)))
			headers.Set(corev1.PortForwardRequestIDHeader, strconv.Itoa(slot+1))
			var err error
			native.errors[slot], err = connection.CreateStream(headers)
			if err != nil {
				return err
			}
			if native.errors[slot].Close() != nil {
				return errNativeStreamUnknown
			}
			headers.Set(corev1.StreamType, corev1.StreamTypeData)
			native.data[slot], err = connection.CreateStream(headers)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	native.nativeSPDYConnection = connection
	for slot := range native.ports {
		native.workers.Add(2)
		go native.readData(slot)
		go native.readError(slot)
	}
	go func() { native.workers.Wait(); close(native.done) }()
	return native, nil
}

// spend counts slot headers and application bytes under one immutable combined-direction budget.
// spend считает slot headers и application bytes в одном immutable combined-direction budget.
func (stream *nativePortForwardSPDY) spend(count int) bool {
	stream.budgetMu.Lock()
	defer stream.budgetMu.Unlock()
	if count < 0 || uint64(count) > stream.maximum-stream.spent {
		return false
	}
	stream.spent += uint64(count)
	return true
}

// readData emits bounded slot-prefixed chunks; the fixed-profile EOF closes the tunnel with unknown remote outcome.
// readData выдаёт bounded slot-prefixed chunks; EOF fixed profile закрывает tunnel с unknown remote outcome.
func (stream *nativePortForwardSPDY) readData(slot int) {
	defer stream.workers.Done()
	var raw [managedkubernetes.MaximumFrameBytes]byte
	defer clear(raw[:])
	for {
		n, err := stream.data[slot].Read(raw[1:])
		if n > 0 {
			if !stream.spend(n + 1) {
				stream.cancel()
				return
			}
			raw[0] = byte(slot)
			output := nativeSPDYOutput{channel: "PORT_FORWARD", payload: append([]byte(nil), raw[:n+1]...)}
			select {
			case stream.events <- output:
			case <-stream.ctx.Done():
				clear(output.payload)
				return
			}
		}
		if err != nil {
			stream.cancel()
			return
		}
		if stream.ctx.Err() != nil {
			return
		}
	}
}

// readError consumes only the presence of private diagnostics, never their text; any nonempty error closes the tunnel.
// readError проверяет лишь наличие private diagnostics без их текста; nonempty error закрывает tunnel.
func (stream *nativePortForwardSPDY) readError(slot int) {
	defer stream.workers.Done()
	var raw [1]byte
	defer clear(raw[:])
	n, err := stream.errors[slot].Read(raw[:])
	if n > 0 || err != io.EOF && stream.ctx.Err() == nil {
		stream.cancel()
	}
}

// ReadStream releases only owner-credit candidates; no EOF or connection close is promoted to successful completion.
// ReadStream выдаёт лишь owner-credit candidates; EOF либо connection close не превращаются в successful completion.
func (stream *nativePortForwardSPDY) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	select {
	case output := <-stream.events:
		return value.KubernetesStreamRead{Channel: output.channel, Payload: output.payload}, nil
	case <-stream.done:
		return value.KubernetesStreamRead{}, errNativeStreamUnknown
	}
}

// WriteFrame sends one admitted slot payload exactly once; the explicit empty EOF closes the whole local tunnel.
// WriteFrame передаёт один admitted slot payload ровно один раз; explicit empty EOF закрывает весь local tunnel.
func (stream *nativePortForwardSPDY) WriteFrame(ctx context.Context, frame value.KubernetesStreamFrame) error {
	if stream == nil || ctx == nil || ctx.Err() != nil || stream.ctx.Err() != nil || frame.Channel != "PORT_FORWARD" || managedkubernetes.ValidatePortForwardFrame(frame.Payload, frame.EndOfInput, len(stream.ports)) != nil {
		return errNativeStreamUnknown
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(time.Now()) {
		return errNativeStreamUnknown
	}
	if local := time.Now().Add(5 * time.Second); local.Before(deadline) {
		deadline = local
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
	if !stream.spend(len(frame.Payload)) {
		stream.cancel()
		return errNativeStreamLimit
	}
	if frame.EndOfInput {
		return stream.nativeSPDYConnection.Close()
	}
	payload := frame.Payload[1:]
	n, err := stream.data[frame.Payload[0]].Write(payload)
	if err != nil || n != len(payload) || call.Err() != nil {
		stream.cancel()
		return errNativeStreamUnknown
	}
	return nil
}

// Close positively joins fixed slot readers and any in-flight writer after closing the original connection.
// Close положительно присоединяет fixed slot readers и in-flight writer после закрытия original connection.
func (stream *nativePortForwardSPDY) Close() error {
	_ = stream.nativeSPDYConnection.Close()
	stream.writeMu.Lock()
	stream.writeMu.Unlock()
	<-stream.done
	return nil
}

// String hides private tunnel bytes and operator endpoint coordinates.
// String скрывает private tunnel bytes и operator endpoint coordinates.
func (*nativePortForwardSPDY) String() string {
	return "[managed Kubernetes native SPDY port-forward transport]"
}

// GoString preserves redaction for detailed formatting.
// GoString сохраняет redaction для подробного форматирования.
func (stream *nativePortForwardSPDY) GoString() string { return stream.String() }
