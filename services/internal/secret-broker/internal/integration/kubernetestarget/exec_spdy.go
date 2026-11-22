package kubernetestarget

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
)

// nativeSPDYOutput transfers one private chunk from a joined fixed reader to the existing owner-credit consumer.
// nativeSPDYOutput передаёт private chunk из joined fixed reader в existing owner-credit consumer.
type nativeSPDYOutput struct {
	channel string
	payload []byte
}

// nativeExecSPDY implements only the pinned v4 fixed channel profile, retaining explicit exit evidence and owned worker joins.
// nativeExecSPDY реализует лишь pinned v4 fixed channel profile с explicit exit evidence и owned worker joins.
type nativeExecSPDY struct {
	*nativeSPDYConnection
	operation                             string
	options                               PodStreamOptions
	stdin, stdout, stderr, resize, status httpstream.Stream
	events                                chan nativeSPDYOutput
	done                                  chan struct{}
	core, aux                             sync.WaitGroup
	readMu, writeMu, stateMu              sync.Mutex
	maximum, spent                        uint64
	inputClosed                           bool
	code                                  *int32
	failure                               bool
}

// openExecSPDY initializes exact logical channels under the original admission, without a replacement attempt.
// openExecSPDY инициализирует exact logical channels под original admission без replacement attempt.
func (credential Credential) openExecSPDY(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, operation string, target PodStreamTarget, options PodStreamOptions, expiresAt, admissionUntil time.Time, maximum uint64) (*nativeExecSPDY, error) {
	if pin.Protocol != PrivilegedSPDYV4 || !validNativeSPDYOperation(operation, pin.Protocol) {
		return nil, errNativeStreamUnknown
	}
	if operation == "kubernetes.cp.upload" || operation == "kubernetes.cp.download" {
		if options.Copy.Validate(operation) != nil {
			return nil, errNativeStreamUnknown
		}
		maximum = min(maximum, options.Copy.MaximumBytes)
	}
	native := &nativeExecSPDY{operation: operation, options: options, maximum: maximum, events: make(chan nativeSPDYOutput), done: make(chan struct{})}
	connection, err := credential.openPodSPDY(ctx, attempt, pin, now, operation, target, options, expiresAt, admissionUntil, maximum, 5, func(connection *nativeSPDYConnection) error {
		var err error
		create := func(kind string) (httpstream.Stream, error) {
			headers := make(http.Header)
			headers.Set(corev1.StreamType, kind)
			return connection.CreateStream(headers)
		}
		if native.status, err = create(corev1.StreamTypeError); err != nil {
			return err
		}
		if options.Stdin {
			if native.stdin, err = create(corev1.StreamTypeStdin); err != nil {
				return err
			}
		}
		if options.Stdout {
			if native.stdout, err = create(corev1.StreamTypeStdout); err != nil {
				return err
			}
		}
		if options.Stderr && !options.TTY {
			if native.stderr, err = create(corev1.StreamTypeStderr); err != nil {
				return err
			}
		}
		if options.TTY {
			if native.resize, err = create(corev1.StreamTypeResize); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	native.nativeSPDYConnection = connection
	native.core.Add(1)
	go native.readStatus()
	for channel, reader := range map[string]httpstream.Stream{"STDOUT": native.stdout, "STDERR": native.stderr} {
		if reader != nil {
			native.core.Add(1)
			go native.readOutput(channel, reader)
		}
	}
	if native.stdin != nil {
		native.aux.Add(1)
		go native.watchStdin()
	}
	go func() {
		native.core.Wait()
		// A normal tar exit can precede the final frame's local stdin close; join that writer before closing transport.
		// Normal tar exit может опередить local stdin close последнего frame; join writer до закрытия transport.
		if native.operation == "kubernetes.cp.upload" {
			native.writeMu.Lock()
			_ = connection.Close()
			native.writeMu.Unlock()
		} else {
			_ = connection.Close()
		}
		native.aux.Wait()
		close(native.done)
	}()
	return native, nil
}

// spend counts input and output together; status has its separate strict control budget.
// spend считает input и output вместе; status имеет отдельный strict control budget.
func (stream *nativeExecSPDY) spend(count int) bool {
	stream.stateMu.Lock()
	defer stream.stateMu.Unlock()
	if count < 0 || uint64(count) > stream.maximum-stream.spent {
		return false
	}
	stream.spent += uint64(count)
	return true
}

// fail records only a safe failure bit and closes I/O through the independent connection watcher.
// fail сохраняет лишь safe failure bit и закрывает I/O через independent connection watcher.
func (stream *nativeExecSPDY) fail() {
	stream.stateMu.Lock()
	stream.failure = true
	stream.stateMu.Unlock()
	stream.cancel()
}

// readStatus preserves bounded explicit remote exit facts even when separate output delivery fails later.
// readStatus сохраняет bounded explicit remote exit facts даже при последующем отказе отдельной output delivery.
func (stream *nativeExecSPDY) readStatus() {
	defer stream.core.Done()
	raw, err := io.ReadAll(io.LimitReader(stream.status, maximumExecStatusBytes+1))
	defer clear(raw)
	code, decodeErr := decodeNativeExecStatus(raw)
	if err != nil || decodeErr != nil {
		stream.fail()
		return
	}
	stream.stateMu.Lock()
	stream.code = &code
	stream.stateMu.Unlock()
}

// readOutput holds at most one bounded chunk and never exposes raw transport errors.
// readOutput хранит не более одного bounded chunk и не выдаёт raw transport errors.
func (stream *nativeExecSPDY) readOutput(channel string, reader io.Reader) {
	defer stream.core.Done()
	var raw [managedkubernetes.MaximumFrameBytes]byte
	defer clear(raw[:])
	for {
		n, err := reader.Read(raw[:])
		if n > 0 {
			if !stream.spend(n) {
				stream.fail()
				return
			}
			output := nativeSPDYOutput{channel: channel, payload: append([]byte(nil), raw[:n]...)}
			select {
			case stream.events <- output:
			case <-stream.ctx.Done():
				clear(output.payload)
				return
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			stream.fail()
			return
		}
		if n == 0 && stream.ctx.Err() != nil {
			return
		}
	}
}

// watchStdin refuses reverse-direction data and remembers a remote input close without inventing remote completion.
// watchStdin отвергает reverse-direction data и запоминает remote input close без выдуманного remote completion.
func (stream *nativeExecSPDY) watchStdin() {
	defer stream.aux.Done()
	var raw [1]byte
	defer clear(raw[:])
	n, err := stream.stdin.Read(raw[:])
	if n > 0 || err != io.EOF && stream.ctx.Err() == nil {
		stream.fail()
	}
	stream.writeMu.Lock()
	stream.inputClosed = true
	stream.writeMu.Unlock()
}

// ReadStream releases complete chunks before an explicit terminal receipt; EOF without status remains unknown.
// ReadStream выдаёт полные chunks до explicit terminal receipt; EOF без status остаётся unknown.
func (stream *nativeExecSPDY) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	select {
	case output := <-stream.events:
		channel := output.channel
		if stream.operation == "kubernetes.cp.download" && channel == "STDOUT" {
			channel = "FILE_CONTENT"
		}
		return value.KubernetesStreamRead{Channel: channel, Payload: output.payload}, nil
	case <-stream.done:
		stream.stateMu.Lock()
		defer stream.stateMu.Unlock()
		result := value.KubernetesStreamRead{}
		if stream.code != nil {
			code := *stream.code
			result.ExitCode = &code
		}
		if stream.failure || stream.code == nil {
			return result, errNativeStreamUnknown
		}
		return result, nil
	}
}

// WriteFrame performs one owner-admitted input write or local stdin half-close, with deadline-triggered transport teardown.
// WriteFrame выполняет один owner-admitted input write либо local stdin half-close с deadline-triggered transport teardown.
func (stream *nativeExecSPDY) WriteFrame(ctx context.Context, frame value.KubernetesStreamFrame) error {
	if stream == nil || ctx == nil || ctx.Err() != nil || stream.ctx.Err() != nil || len(frame.Payload) > managedkubernetes.MaximumFrameBytes || stream.operation == "kubernetes.cp.download" {
		return errNativeStreamUnknown
	}
	channel := frame.Channel
	copyInput := stream.operation == "kubernetes.cp.upload"
	if copyInput {
		if channel != "FILE_CONTENT" {
			return errNativeStreamUnknown
		}
		channel = "STDIN"
	}
	var wire []byte
	switch channel {
	case "STDIN":
		if stream.stdin == nil || len(frame.Payload) == 0 && !frame.EndOfInput || len(frame.Payload) != 0 && frame.EndOfInput && !copyInput {
			return errNativeStreamUnknown
		}
	case "TERMINAL_RESIZE":
		var size struct {
			Columns uint16 `json:"columns"`
			Rows    uint16 `json:"rows"`
		}
		if stream.resize == nil || frame.EndOfInput || internalrpcauth.DecodeExactJSON(frame.Payload, &size) != nil || size.Columns < 1 || size.Columns > 1024 || size.Rows < 1 || size.Rows > 512 {
			return errNativeStreamUnknown
		}
		var err error
		wire, err = json.Marshal(struct{ Width, Height uint16 }{size.Columns, size.Rows})
		if err != nil {
			return errNativeStreamUnknown
		}
		wire = append(wire, '\n')
		defer clear(wire)
	default:
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
	if call.Err() != nil || stream.ctx.Err() != nil || channel == "STDIN" && stream.inputClosed {
		return errNativeStreamUnknown
	}
	var err error
	if channel == "STDIN" {
		if !stream.spend(len(frame.Payload)) {
			stream.fail()
			return errNativeStreamLimit
		}
		if len(frame.Payload) != 0 {
			var n int
			n, err = stream.stdin.Write(frame.Payload)
			if n != len(frame.Payload) {
				err = errNativeStreamUnknown
			}
		}
		if err == nil && frame.EndOfInput {
			stream.inputClosed = true
			err = stream.stdin.Close()
		}
	} else {
		if !stream.spend(len(wire)) {
			stream.fail()
			return errNativeStreamLimit
		}
		var n int
		n, err = stream.resize.Write(wire)
		if n != len(wire) {
			err = errNativeStreamUnknown
		}
	}
	if err != nil || call.Err() != nil {
		stream.fail()
		return errNativeStreamUnknown
	}
	return nil
}

// Close joins connection shutdown, fixed readers, the coordinator and any in-flight writer before returning.
// Close присоединяет connection shutdown, fixed readers, coordinator и in-flight writer до возврата.
func (stream *nativeExecSPDY) Close() error {
	_ = stream.nativeSPDYConnection.Close()
	stream.writeMu.Lock()
	stream.writeMu.Unlock()
	<-stream.done
	return nil
}

// String excludes private buffers and the live connection from ordinary diagnostics.
// String исключает private buffers и live connection из ordinary diagnostics.
func (*nativeExecSPDY) String() string { return nativeExecRedacted }

// GoString preserves redaction during detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (stream *nativeExecSPDY) GoString() string { return stream.String() }
