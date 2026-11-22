package kubernetestarget

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
)

// TestNativePortForwardMultipleSlots verifies real TLS multiplexing with equal remote ports, ordered slots and no byte leakage between streams.
// TestNativePortForwardMultipleSlots проверяет actual TLS multiplexing с одинаковыми remote ports, ordered slots и отсутствием смешивания bytes.
func TestNativePortForwardMultipleSlots(t *testing.T) {
	for _, bad := range []string{"", "foreign-slot", "wrong-preface", "error-channel"} {
		t.Run(bad, func(t *testing.T) {
			done := make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				if r.URL.RawQuery != "port=9090&port=8080&port=8080" {
					t.Error("native mapping order changed")
					w.WriteHeader(403)
					return
				}
				upgrader := websocket.Upgrader{Subprotocols: []string{PrivilegedPortForwardWebSocketV4.WireProtocol()}}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if bad != "" {
					message := []byte{4, 0x90, 0x1f, 'x'}
					switch bad {
					case "foreign-slot":
						message[0] = 6
					case "wrong-preface":
						message[1]++
					case "error-channel":
						message[0] = 5
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, message)
					return
				}
				// Each received slot is routed to its exact native DATA channel; error channels are never writable.
				// Каждый slot направляется в exact native DATA channel; error channels недоступны для записи.
				for _, slot := range []byte{2, 0, 1} {
					kind, wire, err := conn.ReadMessage()
					if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(wire, []byte{slot * 2, 'a' + slot}) {
						t.Error("input crossed port mapping")
						return
					}
				}
				for _, slot := range []byte{1, 2, 0} {
					port := uint16(8080)
					if slot == 0 {
						port = 9090
					}
					prefix := []byte{slot * 2, 0, 0}
					binary.LittleEndian.PutUint16(prefix[1:], port)
					_ = conn.WriteMessage(websocket.BinaryMessage, prefix[:2])
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{slot * 2, prefix[2], 'a' + slot})
				}
				_, _, _ = conn.ReadMessage()
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			pin.Protocol = PrivilegedPortForwardWebSocketV4
			ports := []uint16{9090, 8080, 8080}
			stream, err := credential.openPortForwardWebSocket(t.Context(), t.Context(), pin, now, streamTargetFixture(pin), ports, now.Add(3*time.Second), now.Add(time.Second), 1024)
			if err != nil {
				t.Fatal("native multiplex open failed")
			}
			defer stream.Close()
			ports[0] = 1 // The caller cannot mutate retained mapping authority after admission.
			// Caller не меняет сохранённый mapping после admission.
			call, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if bad == "" {
				if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: []byte{3, 'x'}}) == nil {
					t.Fatal("foreign slot accepted")
				}
				for _, slot := range []byte{2, 0, 1} {
					if stream.WriteFrame(call, value.KubernetesStreamFrame{Channel: "PORT_FORWARD", Payload: []byte{slot, 'a' + slot}}) != nil {
						t.Fatal("valid slot rejected")
					}
				}
				for _, slot := range []byte{1, 2, 0} {
					part, err := stream.ReadStream()
					if err != nil || part.Channel != "PORT_FORWARD" || !bytes.Equal(part.Payload, []byte{slot, 'a' + slot}) {
						t.Fatal("output crossed mapping or lost prefix")
					}
					clear(part.Payload)
				}
			} else {
				part, err := stream.ReadStream()
				if err == nil || len(part.Payload) > 0 || part.CleanEOF || part.ExitCode != nil {
					t.Fatal("malformed multiplex leaked output or success")
				}
			}
			_ = stream.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("peer not joined")
			}
			if calls.Load() != 1 {
				t.Fatal("connection replayed")
			}
		})
	}
}
