package kubernetestarget

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
)

// upgradeNativePortForwardFixture verifies the exact UID and port route before a synthetic TLS upgrade.
// upgradeNativePortForwardFixture проверяет exact UID и port route до synthetic TLS upgrade.
func upgradeNativePortForwardFixture(t *testing.T, writer http.ResponseWriter, request *http.Request, protocol string) *websocket.Conn {
	t.Helper()
	if request.Method != http.MethodGet || request.URL.Path != "/portForward/selected/selected-pod/7fb0912f-a690-453f-be9a-3520d2e99a85" || request.URL.RawQuery != "port=8080" || request.Header.Get("Sec-WebSocket-Protocol") != PrivilegedPortForwardWebSocketV4.WireProtocol() {
		t.Error("port-forward route, port or protocol changed")
		writer.WriteHeader(http.StatusForbidden)
		return nil
	}
	upgrader := websocket.Upgrader{Subprotocols: []string{protocol}}
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return nil
	}
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	_ = connection.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return connection
}

// TestNativePortForwardWirePreservesPortAndIdentity exercises real TLS and both channel prefaces with bounded output.
// It proves local transport behavior only, without inventing a remote execution receipt from socket closure.
// TestNativePortForwardWirePreservesPortAndIdentity проверяет настоящий TLS, оба channel prefaces и bounded output.
// Доказывает лишь local transport behavior, не выдумывая remote execution receipt по закрытию socket.
func TestNativePortForwardWirePreservesPortAndIdentity(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			output := strings.Repeat("synthetic-data", 4000)
			serverDone := make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, certificate, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(serverDone)
				conn := upgradeNativePortForwardFixture(t, w, r, PrivilegedPortForwardWebSocketV4.WireProtocol())
				if conn == nil {
					return
				}
				defer conn.Close()
				// The data prefix spans messages; application bytes remain after the completed prefix.
				// Data prefix разделён между messages; application bytes остаются после завершённого prefix.
				for _, message := range [][]byte{{0, 0x90}, {1, 0x90, 0x1f}, {0, 0x1f}} {
					if conn.WriteMessage(websocket.BinaryMessage, message) != nil {
						return
					}
				}
				kind, data, err := conn.ReadMessage()
				if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(data, append([]byte{0}, []byte("input")...)) {
					t.Error("tunnel input changed")
					return
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, append([]byte{0}, []byte(output)...))
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "private diagnostic"))
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedPortForwardWebSocketV4
			stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(3*time.Second), now.Add(2*time.Second), 128<<10)
			if err != nil {
				t.Fatal("native tunnel did not open")
			}
			defer stream.Close()
			frameContext, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: append([]byte{0}, []byte("input")...)}) != nil {
				t.Fatal("native input rejected")
			}
			var got strings.Builder
			for {
				part, err := stream.ReadStream()
				if part.ExitCode != nil || part.CleanEOF || part.RemoteStatusCode != 0 {
					t.Fatal("tunnel fabricated remote success")
				}
				if len(part.Payload) > 0 {
					if part.Channel != "PORT_FORWARD" || len(part.Payload) > managedkubernetes.MaximumFrameBytes {
						t.Fatal("unbounded or foreign output")
					}
					if part.Payload[0] != 0 {
						t.Fatal("foreign mapping slot")
					}
					got.Write(part.Payload[1:])
					clear(part.Payload)
				}
				if err != nil {
					break
				}
			}
			if got.String() != output || calls.Load() != 1 {
				t.Fatal("lost data or repeated connection")
			}
			if stream.Close() != nil {
				t.Fatal("local close failed")
			}
			select {
			case <-serverDone:
			case <-time.After(time.Second):
				t.Fatal("peer did not join")
			}
		})
	}
}

// TestNativePortForwardRejectsForeignFrames verifies port/channel/protocol boundaries and hides error-channel bytes.
// TestNativePortForwardRejectsForeignFrames проверяет port/channel/protocol boundaries и скрывает error-channel bytes.
func TestNativePortForwardRejectsForeignFrames(t *testing.T) {
	for _, scenario := range []string{"foreign-port", "error-channel", "unknown-channel", "text", "missing-prefix", "protocol", "limit"} {
		t.Run(scenario, func(t *testing.T) {
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				protocol := PrivilegedPortForwardWebSocketV4.WireProtocol()
				if scenario == "protocol" {
					protocol = PrivilegedWebSocketV5.WireProtocol()
				}
				conn := upgradeNativePortForwardFixture(t, w, r, protocol)
				if conn == nil {
					return
				}
				defer conn.Close()
				kind := websocket.BinaryMessage
				data := []byte{0, 0x90, 0x1f}
				switch scenario {
				case "foreign-port":
					data = []byte{0, 0x91, 0x1f}
				case "error-channel":
					data = append([]byte{1, 0x90, 0x1f}, []byte("synthetic-private-error")...)
				case "unknown-channel":
					data = []byte{2, 0x90, 0x1f}
				case "text":
					kind = websocket.TextMessage
				case "missing-prefix":
					data = []byte{0, 0x90}
				case "limit":
					data = append(data, bytes.Repeat([]byte{'x'}, 2048)...)
				}
				_ = conn.WriteMessage(kind, data)
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedPortForwardWebSocketV4
			stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(3*time.Second), now.Add(time.Second), 1024)
			if scenario == "protocol" {
				if err == nil {
					stream.Close()
					t.Fatal("foreign protocol accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("fixture did not open")
			}
			defer stream.Close()
			part, err := stream.ReadStream()
			if err == nil || len(part.Payload) != 0 || part.ExitCode != nil || part.CleanEOF || calls.Load() != 1 {
				t.Fatal("malformed tunnel accepted or private output escaped")
			}
			if strings.Contains(err.Error(), "synthetic-private-error") {
				t.Fatal("private diagnostic escaped")
			}
		})
	}
}

// TestNativePortForwardCancellationJoinsRead checks full local close, invalid input and retained non-success on EOF.
// TestNativePortForwardCancellationJoinsRead проверяет полное local close, invalid input и отсутствие success по EOF.
func TestNativePortForwardCancellationJoinsRead(t *testing.T) {
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := upgradeNativePortForwardFixture(t, w, r, PrivilegedPortForwardWebSocketV4.WireProtocol())
		if conn == nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.Protocol = PrivilegedPortForwardWebSocketV4
	stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(3*time.Second), now.Add(time.Second), 1024)
	if err != nil {
		t.Fatal("fixture open failed")
	}
	defer stream.Close()
	frameContext, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, frame := range []value.KubernetesStreamFrame{{Channel: "STDIN", Payload: append([]byte{0}, []byte("input")...)}, {Channel: "PORT_FORWARD"}, {Channel: "PORT_FORWARD", Payload: append([]byte{0}, []byte("input")...), EndOfInput: true}} {
		if stream.WriteFrame(frameContext, frame) == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if stream.WriteFrame(t.Context(), value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: append([]byte{0}, []byte("input")...)}) == nil {
		t.Fatal("unbounded input accepted")
	}
	done := make(chan error, 1)
	go func() {
		part, err := stream.ReadStream()
		if part.ExitCode != nil || part.CleanEOF {
			err = nil
		}
		done <- err
	}()
	if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", EndOfInput: true}) != nil {
		t.Fatal("whole tunnel close failed")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("EOF promoted to success")
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not join")
	}
	if stream.WriteFrame(frameContext, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: append([]byte{0}, []byte("again")...)}) == nil {
		t.Fatal("closed tunnel revived")
	}
}

// TestNativePortForwardRejectsBeforeDial proves protocol and empty-port rejection precedes credential-bearing network I/O.
// TestNativePortForwardRejectsBeforeDial доказывает отказ protocol и пустого port до network I/O с credentials.
func TestNativePortForwardRejectsBeforeDial(t *testing.T) {
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid selection reached peer")
		w.WriteHeader(403)
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	for _, protocol := range []PrivilegedProtocol{PrivilegedWebSocketV5, PrivilegedSPDYV4, PrivilegedPortForwardSPDYV1, "UNKNOWN"} {
		pin.Protocol = protocol
		if stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), []uint16{8080}, now.Add(time.Second), now.Add(time.Second), 1024); err == nil {
			stream.Close()
			t.Fatal("unsupported profile opened")
		}
	}
	pin.Protocol = PrivilegedPortForwardWebSocketV4
	for _, ports := range [][]uint16{nil, {0}, {8080, 0}, make([]uint16, managedkubernetes.MaximumForwardPorts+1)} {
		if stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), ports, now.Add(time.Second), now.Add(time.Second), 1024); err == nil {
			stream.Close()
			t.Fatal("invalid ports opened")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid selection performed I/O")
	}
}
