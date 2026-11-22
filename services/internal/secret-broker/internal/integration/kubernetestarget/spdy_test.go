package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/moby/spdystream"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	apispdy "k8s.io/apimachinery/pkg/util/httpstream/spdy"
)

// spdyPeerFixture exposes actual SDK server streams without replacing any client TLS, negotiation or framing path.
// spdyPeerFixture выдаёт actual SDK server streams без подмены client TLS, negotiation либо framing path.
type spdyPeerFixture struct {
	t        *testing.T
	mu       sync.Mutex
	streams  map[string]httpstream.Stream
	ready    chan struct{}
	done     chan struct{}
	count    int
	expected int
	protocol string
	run      func(map[string]httpstream.Stream)
}

// ServeHTTP negotiates only the requested registered protocol and collects fixed client-created channels.
// ServeHTTP согласует лишь requested registered protocol и собирает fixed client-created channels.
func (peer *spdyPeerFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer close(peer.done)
	if request.Method != http.MethodPost {
		peer.t.Error("SPDY did not use POST")
		writer.WriteHeader(400)
		return
	}
	if _, err := httpstream.Handshake(request, writer, []string{peer.protocol}); err != nil {
		return
	}
	connection := apispdy.NewResponseUpgrader().UpgradeResponse(writer, request, func(stream httpstream.Stream, replySent <-chan struct{}) error {
		key := stream.Headers().Get(corev1.StreamType)
		if port := stream.Headers().Get(corev1.PortHeader); port != "" {
			key = stream.Headers().Get(corev1.PortForwardRequestIDHeader) + "/" + port + "/" + key
		}
		peer.mu.Lock()
		if _, exists := peer.streams[key]; exists {
			peer.mu.Unlock()
			return errNativeStreamUnknown
		}
		peer.streams[key] = stream
		peer.count++
		reached := peer.count == peer.expected
		peer.mu.Unlock()
		if reached {
			go func() { <-replySent; close(peer.ready) }()
		}
		return nil
	})
	if connection == nil {
		return
	}
	defer connection.Close()
	select {
	case <-peer.ready:
	case <-connection.CloseChan():
		return
	case <-time.After(3 * time.Second):
		peer.t.Error("fixed streams were not created")
		return
	}
	peer.mu.Lock()
	owned := make(map[string]httpstream.Stream, len(peer.streams))
	for key, stream := range peer.streams {
		owned[key] = stream
	}
	peer.mu.Unlock()
	if peer.run != nil {
		peer.run(owned)
	}
	<-connection.CloseChan()
}

// newSPDYPeerFixture creates only disposable local TLS and a fixed-channel server; no cluster configuration is read.
// newSPDYPeerFixture создаёт лишь disposable local TLS и fixed-channel server; cluster configuration не читается.
func newSPDYPeerFixture(t *testing.T, certificate bool, protocol string, streams int, run func(map[string]httpstream.Stream)) (Credential, *spdyPeerFixture) {
	t.Helper()
	peer := &spdyPeerFixture{t: t, streams: make(map[string]httpstream.Stream), ready: make(chan struct{}), done: make(chan struct{}), expected: streams, protocol: protocol, run: run}
	credential, _ := transportFixtureWithHandler(t, certificate, peer)
	return credential, peer
}

// TestNativeSPDYExecPreservesInputAndExit proves actual token/certificate TLS, fixed channels, input EOF, output and explicit remote exit through the real adapter.
// TestNativeSPDYExecPreservesInputAndExit доказывает actual token/certificate TLS, fixed channels, input EOF, output и explicit remote exit через real adapter.
func TestNativeSPDYExecPreservesInputAndExit(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(fmt.Sprint(certificate), func(t *testing.T) {
			stdin := make(chan []byte, 1)
			credential, peer := newSPDYPeerFixture(t, certificate, PrivilegedSPDYV4.WireProtocol(), 4, func(streams map[string]httpstream.Stream) {
				raw, err := io.ReadAll(io.LimitReader(streams[corev1.StreamTypeStdin], 64))
				if err != nil {
					t.Error("read synthetic input")
				}
				stdin <- raw
				_, _ = streams[corev1.StreamTypeStdout].Write([]byte("actual-output"))
				_ = streams[corev1.StreamTypeStdout].Close()
				_, _ = streams[corev1.StreamTypeStderr].Write([]byte("actual-error-output"))
				_ = streams[corev1.StreamTypeStderr].Close()
				_, _ = streams[corev1.StreamTypeError].Write([]byte(`{"status":"Success"}`))
				_ = streams[corev1.StreamTypeError].Close()
			})
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedSPDYV4
			attempt, cancelAttempt := context.WithCancel(t.Context())
			stream, err := credential.openExecSPDY(t.Context(), attempt, pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"cat"}, Stdin: true, Stdout: true, Stderr: true}, now.Add(3*time.Second), now.Add(time.Second), 4096)
			cancelAttempt()
			if err != nil {
				t.Fatal("open real SPDY exec", err)
			}
			defer stream.Close()
			write, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if stream.WriteFrame(write, value.KubernetesStreamFrame{Channel: "STDIN", Payload: []byte("private-input")}) != nil || stream.WriteFrame(write, value.KubernetesStreamFrame{Channel: "STDIN", EndOfInput: true}) != nil {
				t.Fatal("admitted input write failed")
			}
			output := map[string][]byte{}
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal("SPDY read failed", err)
				}
				if read.ExitCode != nil {
					if *read.ExitCode != 0 {
						t.Fatal("wrong explicit exit")
					}
					break
				}
				output[read.Channel] = append(output[read.Channel], read.Payload...)
				clear(read.Payload)
			}
			if string(output["STDOUT"]) != "actual-output" || string(output["STDERR"]) != "actual-error-output" || string(<-stdin) != "private-input" {
				t.Fatal("SPDY changed private bytes")
			}
			origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: "stream_synthetic", StreamGeneration: 1, Operation: "kubernetes.exec", TargetRef: credential.target.ID, TargetRevision: int64(credential.target.Revision)}
			receipt, err := stream.HandshakeReceipt(origin)
			if err != nil || receipt.Protocol != string(PrivilegedSPDYV4) || receipt.HTTPStatus != 101 || receipt.Validate() != nil {
				t.Fatal("missing observed SPDY receipt")
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-peer.done:
			case <-time.After(time.Second):
				t.Fatal("server did not join")
			}
		})
	}
}

// TestNativeSPDYPortForwardKeepsExactSlots exercises real typed port/requestID headers and isolates repeated remote-port mappings.
// TestNativeSPDYPortForwardKeepsExactSlots проверяет real typed port/requestID headers и изоляцию mappings с одинаковым remote port.
func TestNativeSPDYPortForwardKeepsExactSlots(t *testing.T) {
	credential, peer := newSPDYPeerFixture(t, false, PrivilegedPortForwardSPDYV1.WireProtocol(), 4, func(streams map[string]httpstream.Stream) {
		for slot := 1; slot <= 2; slot++ {
			key := fmt.Sprintf("%d/8080/data", slot)
			var raw [1]byte
			if _, err := io.ReadFull(streams[key], raw[:]); err != nil || raw[0] != byte('a'+slot-1) {
				t.Error("mapping input changed")
			}
			_, _ = streams[key].Write(raw[:])
		}
	})
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardSPDYV1
	stream, err := credential.openPortForwardSPDY(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080, 8080}, now.Add(3*time.Second), now.Add(time.Second), 4096)
	if err != nil {
		t.Fatal("open SPDY tunnel", err)
	}
	defer stream.Close()
	call, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: []byte{2, 'x'}}) == nil {
		t.Fatal("foreign slot accepted")
	}
	for slot := byte(0); slot < 2; slot++ {
		if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: []byte{slot, 'a' + slot}}) != nil {
			t.Fatal("slot write failed")
		}
	}
	seen := map[byte]byte{}
	for range 2 {
		read, err := stream.ReadStream()
		if err != nil || len(read.Payload) != 2 || read.Channel != "PORT_FORWARD" {
			t.Fatal("slot read failed", err)
		}
		seen[read.Payload[0]] = read.Payload[1]
		clear(read.Payload)
	}
	if seen[0] != 'a' || seen[1] != 'b' {
		t.Fatal("slot output crossed")
	}
	if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", EndOfInput: true}) != nil {
		t.Fatal("whole-tunnel EOF failed")
	}
	read, err := stream.ReadStream()
	if err == nil || len(read.Payload) != 0 || read.CleanEOF || read.ExitCode != nil {
		t.Fatal("tunnel close promoted remote success")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peer.done:
	case <-time.After(time.Second):
		t.Fatal("server did not join")
	}
}

// TestNativeSPDYRejectsMissingAndMalformedExit refuses plain EOF, private diagnostic text and ambiguous status without leaking bytes into an exit receipt.
// TestNativeSPDYRejectsMissingAndMalformedExit отклоняет plain EOF, private diagnostic text и ambiguous status без превращения bytes в exit receipt.
func TestNativeSPDYRejectsMissingAndMalformedExit(t *testing.T) {
	for _, raw := range []string{"", `{"status":"Success","status":"Success"}`, `{"status":"Success","code":null}`, "PRIVATE diagnostic", strings.Repeat("x", maximumExecStatusBytes+1)} {
		t.Run(fmt.Sprint(len(raw)), func(t *testing.T) {
			credential, _ := newSPDYPeerFixture(t, false, PrivilegedSPDYV4.WireProtocol(), 2, func(streams map[string]httpstream.Stream) {
				_, _ = streams[corev1.StreamTypeError].Write([]byte(raw))
				_ = streams[corev1.StreamTypeError].Close()
				_ = streams[corev1.StreamTypeStdout].Close()
			})
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedSPDYV4
			stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(2*time.Second), now.Add(time.Second), 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			read, err := stream.ReadStream()
			if err == nil || read.ExitCode != nil || len(read.Payload) != 0 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("malformed status promoted or exposed")
			}
		})
	}
}

// TestNativeSPDYUpgradeRejectsProtocolRedirectAndOversizedHeaders verifies one actual HTTP attempt and no fallback, status-body echo or unbounded header consumption.
// TestNativeSPDYUpgradeRejectsProtocolRedirectAndOversizedHeaders проверяет одну actual HTTP attempt без fallback, status-body echo либо unbounded header consumption.
func TestNativeSPDYUpgradeRejectsProtocolRedirectAndOversizedHeaders(t *testing.T) {
	for _, mode := range []string{"redirect", "protocol", "headers", "admission", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			done := make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(done)
				switch mode {
				case "redirect":
					writer.Header().Set("Location", "https://foreign.invalid/private")
					writer.WriteHeader(307)
				case "protocol":
					writer.Header().Set(httpstream.HeaderProtocolVersion, "v3.channel.k8s.io")
					writer.Header().Set("Connection", "Upgrade")
					writer.Header().Set("Upgrade", "SPDY/3.1")
					writer.WriteHeader(101)
				case "headers":
					writer.Header().Set("X-Too-Large", strings.Repeat("x", maximumSPDYControlBytes+1))
					writer.WriteHeader(403)
				default:
					<-request.Context().Done()
				}
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedSPDYV4
			attempt, cancel := context.WithCancel(t.Context())
			defer cancel()
			admission := now.Add(100 * time.Millisecond)
			if mode == "cancel" {
				go func() { time.Sleep(30 * time.Millisecond); cancel() }()
			}
			stream, err := credential.openExecSPDY(t.Context(), attempt, pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(time.Second), admission, 4096)
			if err == nil || stream != nil || calls.Load() != 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("invalid upgrade accepted or replayed")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("failed upgrade did not cancel peer")
			}
		})
	}
}

// TestNativeSPDYCloseJoinsBlockedReadersAndPreservesBounds exercises positive local cleanup and redaction independently from remote execution success.
// TestNativeSPDYCloseJoinsBlockedReadersAndPreservesBounds проверяет positive local cleanup и redaction независимо от remote execution success.
func TestNativeSPDYCloseJoinsBlockedReadersAndPreservesBounds(t *testing.T) {
	credential, peer := newSPDYPeerFixture(t, false, PrivilegedSPDYV4.WireProtocol(), 3, nil)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedSPDYV4
	stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.attach", streamTargetFixture(pin), PodStreamOptions{Stdin: true, Stdout: true}, now.Add(3*time.Second), now.Add(time.Second), 64)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := stream.ReadStream(); result <- err }()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("close became remote success")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked reader did not join")
	}
	select {
	case <-peer.done:
	case <-time.After(time.Second):
		t.Fatal("peer did not join")
	}
	if bytes.Contains([]byte(fmt.Sprintf("%#v", stream)), []byte(credential.token)) {
		t.Fatal("private identity escaped diagnostics")
	}
}

// TestNativeSPDYRejectsOversizedControlAndPush exercises actual SDK SYN_REPLY decoding and unsolicited streams under finite local TLS.
// TestNativeSPDYRejectsOversizedControlAndPush проверяет actual SDK SYN_REPLY decoding и unsolicited streams под finite local TLS.
func TestNativeSPDYRejectsOversizedControlAndPush(t *testing.T) {
	for _, mode := range []string{"field", "count", "control", "push"} {
		t.Run(mode, func(t *testing.T) {
			done := make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(done)
				raw, buffered, err := writer.(http.Hijacker).Hijack()
				if err != nil {
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: SPDY/3.1\r\nX-Stream-Protocol-Version: v4.channel.k8s.io\r\n\r\n")
				_ = buffered.Flush()
				connection, err := spdystream.NewConnection(raw, true)
				if err != nil {
					return
				}
				defer func() { _ = raw.Close(); _ = connection.Close(); _ = connection.Wait(0) }()
				connection.Serve(func(stream *spdystream.Stream) {
					headers := make(http.Header)
					switch mode {
					case "field":
						headers.Set("X-Large", strings.Repeat("x", maximumSPDYHeaderField+1))
					case "count":
						for i := 0; i <= maximumSPDYHeaders; i++ {
							headers.Set(fmt.Sprintf("X-%d", i), "x")
						}
					case "control":
						for i := 0; i < maximumSPDYHeaders; i++ {
							random := make([]byte, 3000)
							if _, err := rand.Read(random); err != nil {
								t.Error("generate synthetic compressed metadata")
								return
							}
							headers.Set(fmt.Sprintf("X-%d", i), base64.StdEncoding.EncodeToString(random))
						}
					case "push":
						pushed, err := connection.CreateStream(http.Header{"Streamtype": []string{"stdout"}}, nil, false)
						if err == nil {
							_ = pushed.WaitTimeout(time.Second)
						}
					}
					_ = stream.SendReply(headers, false)
				})
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedSPDYV4
			stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(time.Second), now.Add(time.Second), 4096)
			if stream != nil {
				_ = stream.Close()
			}
			if err == nil || stream != nil || calls.Load() != 1 {
				t.Fatal("invalid SPDY metadata or server push admitted")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("invalid SPDY peer did not join")
			}
		})
	}
}

// TestNativeSPDYPrepareOpenUsesOriginalOwnerGraph proves the actual preparer selects the registered SPDY pin and consumes one admission.
// TestNativeSPDYPrepareOpenUsesOriginalOwnerGraph доказывает выбор registered SPDY pin actual preparer и расход одной admission.
func TestNativeSPDYPrepareOpenUsesOriginalOwnerGraph(t *testing.T) {
	peer := &spdyPeerFixture{t: t, streams: make(map[string]httpstream.Stream), ready: make(chan struct{}), done: make(chan struct{}), expected: 3, protocol: PrivilegedSPDYV4.WireProtocol(), run: func(streams map[string]httpstream.Stream) {
		_, _ = streams[corev1.StreamTypeStdout].Write([]byte("prepared-output"))
		_ = streams[corev1.StreamTypeStdout].Close()
		_ = streams[corev1.StreamTypeStderr].Close()
		_, _ = streams[corev1.StreamTypeError].Write([]byte(`{"status":"Success"}`))
		_ = streams[corev1.StreamTypeError].Close()
	}}
	factory, snapshot, child, reader, wire := podStreamPreparationFixture(t, peer.ServeHTTP)
	child.NodeTransport.Protocol = PrivilegedSPDYV4
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if err != nil {
		t.Fatal("prepare SPDY owner graph", err)
	}
	defer prepared.Close()
	if _, ready := prepared.Binding(); !ready || wire.streamCalls.Load() != 0 {
		t.Fatal("preparation made a native attempt")
	}
	binding, admission := podStreamAdmissionFixture(child)
	stream, receipt, err := prepared.Open(t.Context(), t.Context(), binding, admission)
	if err != nil {
		t.Fatal("open prepared SPDY", err)
	}
	defer stream.Close()
	var output []byte
	for {
		read, err := stream.ReadStream()
		if err != nil {
			t.Fatal("read prepared SPDY", err)
		}
		output = append(output, read.Payload...)
		clear(read.Payload)
		if read.ExitCode != nil {
			break
		}
	}
	if string(output) != "prepared-output" || receipt.Protocol != string(PrivilegedSPDYV4) || receipt.Validate() != nil || reader.calls.Load() != 3 || wire.streamCalls.Load() != 1 {
		t.Fatal("prepared SPDY changed pinned graph or receipt")
	}
	if duplicate, _, err := prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || duplicate != nil || wire.streamCalls.Load() != 1 {
		t.Fatal("consumed SPDY admission replayed")
	}
	_ = stream.Close()
	select {
	case <-peer.done:
	case <-time.After(time.Second):
		t.Fatal("prepared SPDY peer did not join")
	}
}

// TestNativeSPDYResizeAndNonzeroExit preserves typed terminal dimensions and an explicit failing remote exit without promoting EOF.
// TestNativeSPDYResizeAndNonzeroExit сохраняет typed terminal dimensions и explicit failing remote exit без превращения EOF в успех.
func TestNativeSPDYResizeAndNonzeroExit(t *testing.T) {
	resize := make(chan string, 1)
	credential, _ := newSPDYPeerFixture(t, false, PrivilegedSPDYV4.WireProtocol(), 3, func(streams map[string]httpstream.Stream) {
		var size struct{ Width, Height uint16 }
		if json.NewDecoder(streams[corev1.StreamTypeResize]).Decode(&size) != nil || size.Width != 80 || size.Height != 24 {
			t.Error("resize dimensions changed")
		}
		resize <- "accepted"
		_ = streams[corev1.StreamTypeStdout].Close()
		_, _ = streams[corev1.StreamTypeError].Write([]byte(`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"137"}]}}`))
		_ = streams[corev1.StreamTypeError].Close()
	})
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedSPDYV4
	stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, TTY: true, Stdout: true}, now.Add(3*time.Second), now.Add(time.Second), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	call, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, raw := range [][]byte{[]byte(`{"columns":80,"rows":null}`), []byte(`{"Columns":80,"rows":24}`), []byte(`{"columns":80,"columns":80,"rows":24}`), {0xff}} {
		if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "TERMINAL_RESIZE", Payload: raw}) == nil {
			t.Fatal("malformed resize admitted")
		}
	}
	if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":80,"rows":24}`)}) != nil {
		t.Fatal("valid resize refused")
	}
	read, err := stream.ReadStream()
	if err != nil || read.ExitCode == nil || *read.ExitCode != 137 {
		t.Fatal("explicit nonzero exit lost")
	}
	if <-resize != "accepted" {
		t.Fatal("resize not observed")
	}
}

// TestNativeSPDYCopyUsesFrozenCommandAndPrivateChannels proves native copy dispatch without claiming archive or artifact authority.
// TestNativeSPDYCopyUsesFrozenCommandAndPrivateChannels доказывает native copy dispatch без заявления archive либо artifact authority.
func TestNativeSPDYCopyUsesFrozenCommandAndPrivateChannels(t *testing.T) {
	for _, operation := range []string{"kubernetes.cp.upload", "kubernetes.cp.download"} {
		t.Run(operation, func(t *testing.T) {
			profile := managedkubernetes.CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/file name", MaximumBytes: 65536, TimeoutSeconds: 30}
			if operation == "kubernetes.cp.upload" {
				profile.ArtifactRef = "artifact_synthetic"
				profile.ArtifactSHA256 = strings.Repeat("a", 64)
			}
			raw, _ := json.Marshal(profile)
			options, _, err := podStreamInputOptions(operation, managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Options: raw})
			if err != nil {
				t.Fatal(err)
			}
			count := 3
			if operation == "kubernetes.cp.upload" {
				count++
			}
			peer := &spdyPeerFixture{t: t, streams: make(map[string]httpstream.Stream), ready: make(chan struct{}), done: make(chan struct{}), expected: count, protocol: PrivilegedSPDYV4.WireProtocol(), run: func(streams map[string]httpstream.Stream) {
				if operation == "kubernetes.cp.upload" {
					raw, err := io.ReadAll(io.LimitReader(streams[corev1.StreamTypeStdin], 64))
					if err != nil || string(raw) != "owner-private-source" {
						t.Error("copy private source changed")
					}
				}
				_, _ = streams[corev1.StreamTypeStdout].Write([]byte("synthetic-copy-output"))
				_ = streams[corev1.StreamTypeStdout].Close()
				_ = streams[corev1.StreamTypeStderr].Close()
				_, _ = streams[corev1.StreamTypeError].Write([]byte(`{"status":"Success"}`))
				_ = streams[corev1.StreamTypeError].Close()
			}}
			credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Join(r.URL.Query()["command"], "\x00") != strings.Join(options.Command, "\x00") {
					t.Error("copy argv changed")
				}
				peer.ServeHTTP(w, r)
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedSPDYV4
			stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, operation, streamTargetFixture(pin), options, now.Add(3*time.Second), now.Add(time.Second), 65536)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			call, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "STDIN", Payload: []byte("not-authorized")}) == nil {
				t.Fatal("copy accepted ordinary stdin")
			}
			if operation == "kubernetes.cp.upload" {
				if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: []byte("owner-private-source")}) != nil || stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", EndOfInput: true}) != nil {
					t.Fatal("copy private input failed")
				}
			} else if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "FILE_CONTENT", Payload: []byte("wrong-direction")}) == nil {
				t.Fatal("download accepted input")
			}
			var output []byte
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal(err)
				}
				if read.ExitCode != nil {
					if *read.ExitCode != 0 {
						t.Fatal("copy exit changed")
					}
					break
				}
				expected := "STDOUT"
				if operation == "kubernetes.cp.download" {
					expected = "FILE_CONTENT"
				}
				if read.Channel != expected {
					t.Fatal("copy output channel changed")
				}
				output = append(output, read.Payload...)
				clear(read.Payload)
			}
			if string(output) != "synthetic-copy-output" {
				t.Fatal("copy output changed")
			}
		})
	}
}

// TestNativeSPDYRetainsExactTLSPins rejects foreign identity/target pins before any HTTP request and ignores ambient proxies.
// TestNativeSPDYRetainsExactTLSPins отклоняет foreign identity/target pins до HTTP request и игнорирует ambient proxies.
func TestNativeSPDYRetainsExactTLSPins(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.synthetic.invalid:1")
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid pin reached HTTP"); w.WriteHeader(403) }))
	now := time.Now()
	original := privilegedPinFixture(t, credential, now)
	original.Protocol = PrivilegedSPDYV4
	for _, variant := range []string{"SPKI", "CA", "SNI", "target", "address", "protocol"} {
		t.Run(variant, func(t *testing.T) {
			pin := original
			switch variant {
			case "SPKI":
				pin.ServerPublicKeyHash = strings.Repeat("a", 64)
			case "CA":
				pin.CertificateAuthorityHash = strings.Repeat("a", 64)
			case "SNI":
				pin.TLSServerName = "foreign.invalid"
			case "target":
				pin.TargetID = "foreign"
			case "address":
				pin.AddressPins = []string{"192.0.2.1"}
			case "protocol":
				pin.Protocol = PrivilegedWebSocketV5
			}
			stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(original), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(time.Second), now.Add(100*time.Millisecond), 4096)
			if stream != nil {
				_ = stream.Close()
			}
			if err == nil || stream != nil || calls.Load() != 0 {
				t.Fatal("invalid pin admitted or transport retried")
			}
		})
	}
}

// TestNativeSPDYCloseDrainsResetAfterAdmissionWaiterExpired reproduces the pinned SDK's unbuffered reset waiter and requires positive Serve/shutdown join.
// It observes SDK stream removal before cleanup, so the remote reset has already selected the expired local waiter.
// TestNativeSPDYCloseDrainsResetAfterAdmissionWaiterExpired воспроизводит unbuffered reset waiter pinned SDK и требует positive Serve/shutdown join.
// Тест наблюдает SDK stream removal до cleanup: remote reset уже выбрал истёкший local waiter.
func TestNativeSPDYCloseDrainsResetAfterAdmissionWaiterExpired(t *testing.T) {
	left, right := net.Pipe()
	client, err := spdystream.NewConnection(left, false)
	if err != nil {
		t.Fatal(err)
	}
	server, err := spdystream.NewConnection(right, true)
	if err != nil {
		t.Fatal(err)
	}
	peerStream := make(chan *spdystream.Stream, 1)
	peerDone := make(chan struct{})
	go func() { defer close(peerDone); server.Serve(func(stream *spdystream.Stream) { peerStream <- stream }) }()
	ctx, cancel := context.WithCancel(t.Context())
	native := &nativeSPDYConnection{connection: client, raw: left, ctx: ctx, cancel: cancel, serveDone: make(chan struct{}), watchDone: make(chan struct{})}
	go func() {
		defer close(native.serveDone)
		client.Serve(func(stream *spdystream.Stream) { _ = stream.Reset() })
	}()
	go func() { defer close(native.watchDone); <-ctx.Done(); native.shutdown() }()
	stream, err := client.CreateStream(http.Header{"Streamtype": []string{"error"}}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	native.streams = []*spdystream.Stream{stream}
	if stream.WaitTimeout(time.Millisecond) != spdystream.ErrTimeout {
		t.Fatal("synthetic admission waiter did not expire")
	}
	peer := <-peerStream
	if peer.Reset() != nil {
		t.Fatal("send synthetic remote reset")
	}
	limit := time.Now().Add(time.Second)
	for client.FindStream(stream.Identifier()) != nil {
		if time.Now().After(limit) {
			t.Fatal("remote reset was not selected")
		}
		runtime.Gosched()
	}
	closed := make(chan struct{})
	go func() { _ = native.Close(); close(closed) }()
	joined := true
	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond):
		joined = false
		_ = stream.WaitTimeout(20 * time.Millisecond)
		<-closed
	}
	_ = right.Close()
	_ = server.Close()
	_ = server.Wait(0)
	<-peerDone
	if !joined {
		t.Fatal("expired admission reset stranded SDK frame worker")
	}
}

// TestNativeSPDYWriteCancellationJoinsBackpressuredIO fills the fixed SDK queue against a non-reading peer, then requires deadline-driven local join.
// TestNativeSPDYWriteCancellationJoinsBackpressuredIO заполняет fixed SDK queue при non-reading peer и требует deadline-driven local join.
func TestNativeSPDYWriteCancellationJoinsBackpressuredIO(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	credential, peer := newSPDYPeerFixture(t, false, PrivilegedSPDYV4.WireProtocol(), 2, func(streams map[string]httpstream.Stream) {
		<-release
		for _, stream := range streams {
			_ = stream.Reset()
		}
	})
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedSPDYV4
	stream, err := credential.openExecSPDY(t.Context(), t.Context(), pin, now, "kubernetes.attach", streamTargetFixture(pin), PodStreamOptions{Stdin: true}, now.Add(3*time.Second), now.Add(time.Second), 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	call, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	payload := bytes.Repeat([]byte("x"), managedkubernetes.MaximumFrameBytes)
	defer clear(payload)
	failed := false
	for range 2048 {
		if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "STDIN", Payload: payload}) != nil {
			failed = true
			break
		}
	}
	deadline, _ := call.Deadline()
	if !failed || time.Now().Before(deadline) || stream.spent >= 64<<20 {
		t.Fatal("backpressured write did not close at its original deadline")
	}
	releaseOnce.Do(func() { close(release) })
	if stream.Close() != nil {
		t.Fatal("backpressured close failed")
	}
	select {
	case <-peer.done:
	case <-time.After(time.Second):
		t.Fatal("backpressured peer did not join")
	}
}
