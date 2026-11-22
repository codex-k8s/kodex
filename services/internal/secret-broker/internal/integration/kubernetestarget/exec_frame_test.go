package kubernetestarget

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"github.com/gorilla/websocket"
)

// TestNativeFrameWriterRejectsControlSmuggling exercises the actual TLS/WebSocket boundary.
// Only validated stdin, typed resize and a distinct half-close reach the synthetic peer.
// TestNativeFrameWriterRejectsControlSmuggling проверяет настоящий TLS/WebSocket boundary.
// Synthetic peer получает только проверенный stdin, typed resize и отдельный half-close.
func TestNativeFrameWriterRejectsControlSmuggling(t *testing.T) {
	joined := make(chan struct{})
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(joined)
		connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
		if connection == nil {
			return
		}
		defer connection.Close()
		for _, expected := range [][]byte{append([]byte{0}, []byte("synthetic-input")...), append([]byte{4}, []byte(`{"Width":80,"Height":24}`)...), {255, 0}} {
			kind, raw, err := connection.ReadMessage()
			if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(raw, expected) {
				t.Error("unexpected native input reached peer")
				return
			}
		}
		_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"cat"}, Stdin: true, Stdout: true, TTY: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
	if err != nil {
		t.Fatal("native frame fixture failed to open")
	}
	defer stream.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for _, frame := range []value.KubernetesStreamFrame{
		{Channel: "CONTROL", Payload: []byte("synthetic")},
		{Channel: "STDIN", Payload: []byte("synthetic"), EndOfInput: true},
		{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":80,"rows":24,"columns":81}`)},
		{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":80,"rows":24,"extra":true}`)},
		{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":0,"rows":24}`)},
		{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":80,"rows":24}`), EndOfInput: true},
	} {
		if stream.WriteFrame(ctx, frame) == nil {
			t.Fatal("unregistered control accepted")
		}
	}
	valid := value.KubernetesStreamFrame{Channel: "STDIN", Payload: []byte("synthetic-input")}
	if stream.WriteFrame(t.Context(), valid) == nil {
		t.Fatal("unbounded frame context accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if stream.WriteFrame(canceled, valid) == nil {
		t.Fatal("cancelled input accepted")
	}
	if stream.WriteFrame(ctx, valid) != nil || stream.WriteFrame(ctx, value.KubernetesStreamFrame{Channel: "TERMINAL_RESIZE", Payload: []byte(`{"columns":80,"rows":24}`)}) != nil || stream.WriteFrame(ctx, value.KubernetesStreamFrame{Channel: "STDIN", EndOfInput: true}) != nil {
		t.Fatal("validated native frames failed")
	}
	_, _, code, err := stream.ReadOutput()
	if err != nil || code == nil || *code != 0 {
		t.Fatal("explicit synthetic exit receipt missing")
	}
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("synthetic peer did not join")
	}
}

// partialOutputReader emits a valid prefix with a simultaneous transport failure, as io.Reader permits.
// partialOutputReader выдаёт valid prefix одновременно с transport failure согласно io.Reader contract.
type partialOutputReader struct{}

// Read preserves a received prefix independently of the final connection outcome.
// Read сохраняет полученный prefix независимо от final connection outcome.
func (partialOutputReader) Read(buffer []byte) (int, error) {
	return copy(buffer, []byte("synthetic-partial")), errors.New("synthetic interrupted message")
}

// TestNativeOutputRetainsPartialBytesWithFailure keeps already-read output distinct from remote success.
// TestNativeOutputRetainsPartialBytesWithFailure отделяет уже прочитанный output от remote success.
func TestNativeOutputRetainsPartialBytesWithFailure(t *testing.T) {
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
		if connection != nil {
			defer connection.Close()
			_, _, _ = connection.ReadMessage()
		}
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"cat"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
	if err != nil {
		t.Fatal("partial output fixture failed to open")
	}
	defer stream.Close()
	stream.reader = partialOutputReader{}
	stream.channel = 1
	channel, payload, exit, err := stream.ReadOutput()
	if err == nil || exit != nil || channel != "STDOUT" || string(payload) != "synthetic-partial" {
		t.Fatal("partial output discarded or transport failure promoted to success")
	}
	if stream.spent != uint64(len(payload)) {
		t.Fatal("partial received bytes escaped local accounting")
	}
}
