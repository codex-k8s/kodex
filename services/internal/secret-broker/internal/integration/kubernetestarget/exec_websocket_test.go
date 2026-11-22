package kubernetestarget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/gorilla/websocket"
)

// upgradeNativeExecFixture checks the exact PodUID/container route before a local TLS upgrade.
// It neither runs a remote command nor claims owner/RBAC admission from these synthetic fields.
// upgradeNativeExecFixture проверяет exact PodUID/container route до local TLS upgrade.
// Не запускает remote command и не выводит owner/RBAC admission из synthetic fields.
func upgradeNativeExecFixture(t *testing.T, writer http.ResponseWriter, request *http.Request, protocol string) *websocket.Conn {
	t.Helper()
	if request.Method != http.MethodGet || request.URL.Path != "/exec/selected/selected-pod/7fb0912f-a690-453f-be9a-3520d2e99a85/selected-container" ||
		request.Header.Get("Sec-WebSocket-Protocol") != PrivilegedWebSocketV5.WireProtocol() {
		t.Error("native fixture received a foreign route, method or protocol offer")
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

// TestNativeExecWebSocketWirePreservesIdentityAndBoundedChunks exercises real TLS/WebSocket v5,
// stdin close, chunking and explicit exit status using token and certificate identities.
// There is no live cluster, owner grant or actual remote process in this fixture.
// TestNativeExecWebSocketWirePreservesIdentityAndBoundedChunks проверяет настоящий TLS/WebSocket v5,
// stdin close, chunking и explicit exit status для token и certificate identities.
// Fixture не использует live cluster, owner grant либо actual remote process.
func TestNativeExecWebSocketWirePreservesIdentityAndBoundedChunks(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			output := strings.Repeat("synthetic-output-", 3000)
			serverDone := make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, certificate, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(serverDone)
				connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
				if connection == nil {
					return
				}
				defer connection.Close()
				if request.URL.Query().Get("input") != "true" || request.URL.Query().Get("output") != "true" || request.URL.Query().Get("command") != "cat" {
					t.Error("native exec channels or argv changed")
				}
				if err := connection.WriteMessage(websocket.BinaryMessage, []byte{1}); err != nil {
					return
				}
				kind, data, err := connection.ReadMessage()
				if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(data, append([]byte{0}, []byte("synthetic-stdin")...)) {
					t.Error("native stdin was widened or changed")
					return
				}
				_, data, err = connection.ReadMessage()
				if err != nil || !bytes.Equal(data, []byte{255, 0}) {
					t.Error("native v5 stdin half-close changed")
					return
				}
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte(output)...))
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin),
				PodStreamOptions{Command: []string{"cat"}, Stdin: true, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
			if err != nil {
				t.Fatal("exact native v5 handshake failed")
			}
			defer stream.Close()
			if stream.SendStdin([]byte("synthetic-stdin"), false) != nil || stream.SendStdin(nil, true) != nil || stream.SendStdin(nil, true) == nil {
				t.Fatal("native stdin or half-close did not preserve one-way closure")
			}
			var received strings.Builder
			for {
				channel, payload, code, err := stream.ReadOutput()
				if err != nil {
					t.Fatal("native output lost its explicit exit receipt")
				}
				if code != nil {
					if *code != 0 {
						t.Fatal("native success exit changed")
					}
					break
				}
				if channel != "STDOUT" || len(payload) > managedkubernetes.MaximumFrameBytes {
					t.Fatal("native output escaped its registered channel or frame budget")
				}
				received.Write(payload)
				clear(payload)
			}
			if received.String() != output || calls.Load() != 1 {
				t.Fatal("native transport lost output or attempted another identity/connection")
			}
			select {
			case <-serverDone:
			case <-time.After(2 * time.Second):
				t.Fatal("native server fixture did not join")
			}
		})
	}
}

// TestNativeExecWebSocketDoesNotPromoteEOFOrBadStatus proves completed transport and partial
// stdout cannot replace a bounded strict remote exit receipt, including duplicate-field attacks.
// TestNativeExecWebSocketDoesNotPromoteEOFOrBadStatus доказывает, что completed transport и partial
// stdout не заменяют bounded strict remote exit receipt, включая duplicate-field attacks.
func TestNativeExecWebSocketDoesNotPromoteEOFOrBadStatus(t *testing.T) {
	for _, scenario := range []string{"eof", "duplicate", "oversized", "text", "unknown-channel", "no-protocol"} {
		t.Run(scenario, func(t *testing.T) {
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				protocol := PrivilegedWebSocketV5.WireProtocol()
				if scenario == "no-protocol" {
					protocol = PrivilegedSPDYV4.WireProtocol()
				}
				connection := upgradeNativeExecFixture(t, writer, request, protocol)
				if connection == nil {
					return
				}
				defer connection.Close()
				if scenario == "no-protocol" {
					return
				}
				_ = connection.WriteMessage(websocket.BinaryMessage, []byte{3})
				switch scenario {
				case "eof":
					_ = connection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "synthetic-private-reason"))
				case "duplicate":
					_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Failure","status":"Success"}`)...))
				case "oversized":
					_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(strings.Repeat(" ", maximumExecStatusBytes+1))...))
				case "text":
					_ = connection.WriteMessage(websocket.TextMessage, []byte("synthetic-private-reason"))
				case "unknown-channel":
					_ = connection.WriteMessage(websocket.BinaryMessage, []byte{99, 'x'})
				}
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin),
				PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
			if scenario == "no-protocol" {
				if err == nil || stream != nil || calls.Load() != 1 {
					t.Fatal("missing protocol caused fallback or a usable native transport")
				}
				return
			}
			if err != nil {
				t.Fatal("synthetic v5 negative fixture did not open")
			}
			defer stream.Close()
			_, payload, code, err := stream.ReadOutput()
			if err == nil || code != nil || payload != nil || strings.Contains(err.Error(), "synthetic-private") || calls.Load() != 1 {
				t.Fatal("missing/invalid exit became success, leaked text or retried an effect")
			}
		})
	}
}

// TestNativeExecWebSocketCancelJoinsWithoutRemoteStop cancels a blocked real transport and checks
// that only local join is established; no process exit is inferred from the canceled socket.
// TestNativeExecWebSocketCancelJoinsWithoutRemoteStop отменяет blocked real transport и проверяет
// только local join; process exit не выводится из отменённого socket.
func TestNativeExecWebSocketCancelJoinsWithoutRemoteStop(t *testing.T) {
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
		if connection != nil {
			defer connection.Close()
			_, _, _ = connection.ReadMessage()
		}
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := credential.openExecWebSocket(ctx, ctx, pin, now, "kubernetes.exec", streamTargetFixture(pin),
		PodStreamOptions{Command: []string{"sleep"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
	if err != nil {
		t.Fatal("cancel fixture did not open native transport")
	}
	defer stream.Close()
	done := make(chan error, 1)
	go func() {
		_, _, code, err := stream.ReadOutput()
		if code != nil {
			done <- errors.New("cancellation invented a remote exit")
			return
		}
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("canceled native read lost its cancellation or claimed remote completion")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native transport did not join on cancellation")
	}
	if stream.Close() != nil {
		t.Fatal("native local join was not idempotent")
	}
}

// TestNativeExecWebSocketResizeUsesTypedChannel verifies exact native resize wire semantics and
// a nonzero explicit exit without permitting arbitrary control channels or oversized dimensions.
// TestNativeExecWebSocketResizeUsesTypedChannel проверяет exact native resize wire semantics и
// nonzero explicit exit без произвольных control channels либо oversized dimensions.
func TestNativeExecWebSocketResizeUsesTypedChannel(t *testing.T) {
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
		if connection == nil {
			return
		}
		defer connection.Close()
		_, data, err := connection.ReadMessage()
		var size struct{ Width, Height uint16 }
		if err != nil || len(data) < 2 || data[0] != 4 || json.Unmarshal(data[1:], &size) != nil || size.Width != 80 || size.Height != 24 {
			t.Error("native terminal resize changed channel or dimensions")
			return
		}
		_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"137"}]}}`)...))
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin),
		PodStreamOptions{Command: []string{"sh"}, Stdin: true, Stdout: true, TTY: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
	if err != nil {
		t.Fatal("resize fixture did not open native transport")
	}
	defer stream.Close()
	if stream.Resize(0, 24) == nil || stream.Resize(2000, 24) == nil || stream.Resize(80, 24) != nil {
		t.Fatal("native resize did not enforce its typed limits")
	}
	_, _, code, err := stream.ReadOutput()
	if err != nil || code == nil || *code != 137 {
		t.Fatal("native nonzero exit receipt was lost or converted to success")
	}
}

// TestNativeExecWebSocketBoundsOutputAndHandshake closes both decoder and pre-decoder budgets;
// an oversized stream cannot consume unlimited memory or promote a later success status.
// TestNativeExecWebSocketBoundsOutputAndHandshake закрывает decoder и pre-decoder budgets;
// oversized stream не потребляет unlimited memory и не продвигает поздний success status.
func TestNativeExecWebSocketBoundsOutputAndHandshake(t *testing.T) {
	for _, scenario := range []string{"output", "headers"} {
		t.Run(scenario, func(t *testing.T) {
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if scenario == "headers" {
					upgrader := websocket.Upgrader{Subprotocols: []string{PrivilegedWebSocketV5.WireProtocol()}}
					connection, _ := upgrader.Upgrade(writer, request, http.Header{"X-Synthetic-Oversized": {strings.Repeat("x", maximumNativeHandshakeBytes+1)}})
					if connection != nil {
						_ = connection.Close()
					}
					return
				}
				connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
				if connection == nil {
					return
				}
				defer connection.Close()
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte(strings.Repeat("x", 8192))...))
				_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin),
				PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 1024)
			if scenario == "headers" {
				if err == nil || stream != nil || calls.Load() != 1 {
					t.Fatal("oversized native upgrade headers produced a usable or retried connection")
				}
				return
			}
			if err != nil {
				t.Fatal("output budget fixture did not open")
			}
			defer stream.Close()
			_, data, code, err := stream.ReadOutput()
			if !errors.Is(err, errNativeStreamLimit) || data != nil || code != nil || calls.Load() != 1 {
				t.Fatal("native output exceeded immutable credit or consumed later success")
			}
		})
	}
}

// TestNativeExecWebSocketRetainsTLSPinsAndIgnoresAmbientProxy proves the actual WebSocket dialer
// preserves exact serving-key verification and never consults host proxy or kubeconfig state.
// TestNativeExecWebSocketRetainsTLSPinsAndIgnoresAmbientProxy доказывает сохранение exact serving-key
// verification actual WebSocket dialer без обращения к host proxy либо kubeconfig state.
func TestNativeExecWebSocketRetainsTLSPinsAndIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.synthetic.invalid:1")
	t.Setenv("HTTP_PROXY", "http://proxy.synthetic.invalid:1")
	t.Setenv("ALL_PROXY", "http://proxy.synthetic.invalid:1")
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection := upgradeNativeExecFixture(t, writer, request, PrivilegedWebSocketV5.WireProtocol())
		if connection != nil {
			defer connection.Close()
			_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
		}
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	wrong := pin
	wrong.ServerPublicKeyHash = strings.Repeat("a", 64)
	if stream, err := credential.openExecWebSocket(t.Context(), t.Context(), wrong, now, "kubernetes.exec", streamTargetFixture(pin),
		PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10); err == nil || stream != nil || calls.Load() != 0 {
		t.Fatal("native WebSocket ignored the serving-key pin")
	}
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin),
		PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
	if err != nil {
		t.Fatal("native WebSocket consulted an ambient proxy instead of pinned addresses")
	}
	defer stream.Close()
	_, _, code, err := stream.ReadOutput()
	if err != nil || code == nil || *code != 0 || calls.Load() != 1 {
		t.Fatal("native WebSocket changed its exact TLS target or explicit exit receipt")
	}
}

// TestNativeExecAdmissionDoesNotOwnStreamLifetime preserves the socket after its first opening lease expires.
// TestNativeExecAdmissionDoesNotOwnStreamLifetime сохраняет socket после истечения первой opening lease.
func TestNativeExecAdmissionDoesNotOwnStreamLifetime(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		connection := upgradeNativeExecFixture(t, w, r, PrivilegedWebSocketV5.WireProtocol())
		if connection == nil {
			return
		}
		defer connection.Close()
		<-release
		if connection.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte("after-opening-lease")...)) != nil {
			return
		}
		_, _, _ = connection.ReadMessage()
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	admission := now.Add(300 * time.Millisecond)
	attempt, cancelAttempt := context.WithCancel(t.Context())
	defer cancelAttempt()
	stream, err := credential.openExecWebSocket(t.Context(), attempt, pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), admission, 128<<10)
	if err != nil {
		close(release)
		t.Fatal("first admitted handshake failed")
	}
	t.Cleanup(func() { stream.Close() })
	cancelAttempt()
	timer := time.NewTimer(time.Until(admission.Add(30 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		close(release)
		t.Fatal("test deadline")
	}
	close(release)
	channel, payload, _, err := stream.ReadOutput()
	if err != nil || channel != "STDOUT" || string(payload) != "after-opening-lease" || calls.Load() != 1 {
		t.Fatal("short admission cut the lifetime or reopened transport")
	}
	if stream.Close() != nil {
		t.Fatal("stream did not close")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("peer did not join")
	}
}

// TestNativeExecAttemptCancellationStopsAdmission refuses canceled RPCs and interrupts a pending handshake.
// Cancellation never grants retry credit or implies that the remote command did not start.
// TestNativeExecAttemptCancellationStopsAdmission отвергает отменённый RPC и прерывает pending handshake.
// Отмена не даёт retry credit и не доказывает отсутствие запуска remote command.
func TestNativeExecAttemptCancellationStopsAdmission(t *testing.T) {
	for _, beforeDial := range []bool{true, false} {
		t.Run(map[bool]string{true: "before dial", false: "pending handshake"}[beforeDial], func(t *testing.T) {
			entered, joined := make(chan struct{}), make(chan struct{})
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				defer close(joined)
				close(entered)
				<-r.Context().Done()
			}))
			attempt, cancel := context.WithCancel(t.Context())
			defer cancel()
			if beforeDial {
				cancel()
			}
			done := make(chan bool, 1)
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			go func() {
				stream, err := credential.openExecWebSocket(t.Context(), attempt, pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 128<<10)
				refused := err != nil && stream == nil
				if stream != nil {
					_ = stream.Close()
				}
				done <- refused
			}()
			if !beforeDial {
				select {
				case <-entered:
				case <-time.After(time.Second):
					cancel()
					<-done
					t.Fatal("handshake did not reach the selected peer")
				}
				cancel()
			}
			select {
			case refused := <-done:
				if !refused {
					t.Fatal("canceled admission returned a transport")
				}
			case <-time.After(time.Second):
				t.Fatal("canceled admission did not join")
			}
			if beforeDial {
				if calls.Load() != 0 {
					t.Fatal("canceled attempt dialed a peer")
				}
			} else {
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Fatal("pending handshake peer did not join")
				}
				if calls.Load() != 1 {
					t.Fatal("canceled attempt reopened a connection")
				}
			}
		})
	}
}

// TestNativeExecExpiredAdmissionNeverDials rejects stale opening authority even when the hard lifetime remains valid.
// TestNativeExecExpiredAdmissionNeverDials отклоняет stale opening authority при действующем hard lifetime.
func TestNativeExecExpiredAdmissionNeverDials(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	for _, deadline := range []time.Time{now.Add(-time.Second), now.Add(4 * time.Second)} {
		stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), deadline, 128<<10)
		if err == nil || stream != nil || calls.Load() != 0 {
			t.Fatal("invalid admission opened transport")
		}
	}
}

// TestNativeExecSlowHandshakeCannotOutliveAdmission bounds a waiting peer by the short opening lease.
// TestNativeExecSlowHandshakeCannotOutliveAdmission ограничивает ожидающий peer короткой opening lease.
func TestNativeExecSlowHandshakeCannotOutliveAdmission(t *testing.T) {
	done := make(chan struct{})
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { defer close(done); <-r.Context().Done() }))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(150*time.Millisecond), 128<<10)
	if err == nil || stream != nil || calls.Load() != 1 || time.Since(now) > time.Second {
		t.Fatal("handshake escaped short admission")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed handshake did not join peer")
	}
}

// TestNativeExecReadBoundsOnlyUnproductiveWork rejects an empty-message flood within one read while
// allowing many useful messages across independent reads under the existing byte/deadline budget.
// TestNativeExecReadBoundsOnlyUnproductiveWork отвергает empty-message flood внутри одного read,
// сохраняя useful messages отдельных reads под существующим byte/deadline budget.
func TestNativeExecReadBoundsOnlyUnproductiveWork(t *testing.T) {
	for _, productive := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "progress"}[productive], func(t *testing.T) {
			done := make(chan struct{})
			credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				connection := upgradeNativeExecFixture(t, w, r, PrivilegedWebSocketV5.WireProtocol())
				if connection == nil {
					return
				}
				defer connection.Close()
				for index := 0; index < maximumNativeNoProgressSteps+3; index++ {
					message := []byte{1}
					if productive {
						message = append(message, 'x')
					}
					if connection.WriteMessage(websocket.BinaryMessage, message) != nil {
						return
					}
				}
				if productive {
					_ = connection.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
				}
			}))
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			stream, err := credential.openExecWebSocket(t.Context(), t.Context(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}, now.Add(3*time.Second), now.Add(3*time.Second), 2048)
			if err != nil {
				t.Fatal("open progress fixture", err)
			}
			defer stream.Close()
			if productive {
				for index := 0; index < maximumNativeNoProgressSteps+3; index++ {
					channel, payload, code, err := stream.ReadOutput()
					if err != nil || channel != "STDOUT" || len(payload) != 1 || code != nil {
						t.Fatal("useful read spent another read's work budget", err)
					}
					clear(payload)
				}
				_, _, code, err := stream.ReadOutput()
				if err != nil || code == nil || *code != 0 {
					t.Fatal("progress stream lost explicit exit", err)
				}
			} else if _, payload, code, err := stream.ReadOutput(); !errors.Is(err, errNativeStreamLimit) || len(payload) != 0 || code != nil {
				clear(payload)
				t.Fatal("empty read work was not bounded", err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("progress fixture did not join")
			}
		})
	}
}
