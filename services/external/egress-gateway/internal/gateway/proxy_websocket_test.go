package gateway

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Моделирует конкретный pinned AttackCheck при одном успешном Read на Write.
// Это synthetic conformance, а не доказательство actual TLS packet layout.
type providerWebSocketAttackCheckConn struct {
	net.Conn
	wire                   bytes.Buffer
	packets, receivedBytes int
	attacked               bool
}

func (connection *providerWebSocketAttackCheckConn) Write(data []byte) (int, error) {
	connection.packets++
	connection.receivedBytes += len(data)
	if connection.receivedBytes > 65536 || connection.packets > 512 ||
		connection.packets > 64 && connection.packets*128 > connection.receivedBytes {
		connection.attacked = true
		return 0, errors.New("synthetic handshake attack check rejected fragments")
	}
	return connection.wire.Write(data)
}

func (*providerWebSocketAttackCheckConn) Read([]byte) (int, error) { return 0, io.EOF }
func (*providerWebSocketAttackCheckConn) Close() error             { return nil }
func (*providerWebSocketAttackCheckConn) SetDeadline(time.Time) error {
	return nil
}
func (*providerWebSocketAttackCheckConn) SetWriteDeadline(time.Time) error {
	return nil
}

func TestProviderWebSocketHandshakeAvoidsPinnedTinyReadAttackCheck(t *testing.T) {
	upstream, peer := net.Pipe()
	defer peer.Close()
	response := syntheticUpgradeResponse(upstream)
	for range 20 {
		response.Header.Add("X-Private-Sentinel", "private-value-sentinel")
	}
	var expected bytes.Buffer
	before := *response
	before.Body, before.ContentLength = nil, 0
	if err := before.Write(&expected); err != nil {
		t.Fatal("synthetic reference serialization failed")
	}
	client := &providerWebSocketAttackCheckConn{}
	server := &Server{context: t.Context(), metrics: newTestMetrics(t)}
	server.forwardWebSocket(client, bufio.NewReader(client), response, policy.Limits{MaximumHeaderBytes: 16384, WriteTimeoutMilliseconds: 50, IdleTimeoutMilliseconds: 500}, providerResponsesDiagnostic{})
	if client.attacked {
		t.Fatal("101 serialization triggered pinned tiny-read attack check")
	}
	if client.packets != 1 {
		t.Fatal("101 headers were not coalesced")
	}
	if !bytes.Equal(client.wire.Bytes(), expected.Bytes()) {
		t.Fatal("coalescing changed serialized header bytes")
	}
}

func syntheticUpgradeWithSerializedBytes(t *testing.T, size int) *http.Response {
	t.Helper()
	response := syntheticUpgradeResponse(nil)
	response.Request = &http.Request{Method: http.MethodGet}
	response.Header.Set("X-Private-Sentinel", "")
	var base bytes.Buffer
	if err := response.Write(&base); err != nil || base.Len() > size {
		t.Fatal("synthetic handshake size fixture failed")
	}
	response.Header.Set("X-Private-Sentinel", strings.Repeat("p", size-base.Len()))
	return response
}

func TestProviderWebSocketHandshakeBoundBeforeAnyDownstreamWrite(t *testing.T) {
	for _, test := range []struct {
		name          string
		maximum, size int
		accept        bool
	}{
		{name: "exactPolicy", maximum: 1024, size: 1024, accept: true},
		{name: "overPolicy", maximum: 1024, size: 1025},
		{name: "exactSDK", maximum: 128 << 10, size: 64 << 10, accept: true},
		{name: "overSDK", maximum: 128 << 10, size: (64 << 10) + 1},
		{name: "missingPolicy", maximum: 0, size: 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			var downstream bytes.Buffer
			response := syntheticUpgradeWithSerializedBytes(t, test.size)
			err := writeProviderWebSocketHandshake(response, &downstream, test.maximum)
			if test.accept {
				if err != nil || downstream.Len() != test.size {
					t.Fatal("bounded handshake rejected exact limit")
				}
				return
			}
			if !errors.Is(err, errProviderWebSocketHeaderBound) || downstream.Len() != 0 {
				t.Fatal("header overflow leaked a partial downstream handshake")
			}
		})
	}
	for _, failure := range []error{nil, errors.New("private-write-error-sentinel")} {
		err := writeProviderWebSocketHandshake(syntheticUpgradeResponse(nil), providerHandshakeShortWriter{failure: failure}, 1024)
		if failure != nil && err != failure || failure == nil && err != io.ErrShortWrite {
			t.Fatal("coalesced handshake lost downstream write result")
		}
	}
}

type providerWebSocketWriteNotifyConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (connection *providerWebSocketWriteNotifyConn) Write(data []byte) (int, error) {
	connection.once.Do(func() { close(connection.started) })
	return connection.Conn.Write(data)
}

func TestProviderWebSocketHandshakeWriteBudgetAndCancellation(t *testing.T) {
	for _, boundary := range []string{"budget", "cancel", "clientDisconnect"} {
		t.Run(boundary, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, consumer := net.Pipe()
			defer client.Close()
			defer consumer.Close()
			upstream, peer := net.Pipe()
			defer peer.Close()
			notified := &providerWebSocketWriteNotifyConn{Conn: client, started: make(chan struct{})}
			server := &Server{context: ctx, metrics: newTestMetrics(t)}
			done := make(chan struct{})
			go func() {
				defer close(done)
				server.forwardWebSocket(notified, bufio.NewReader(notified), syntheticUpgradeResponse(upstream), policy.Limits{MaximumHeaderBytes: 16384, WriteTimeoutMilliseconds: 40, IdleTimeoutMilliseconds: 500}, providerResponsesDiagnostic{mode: "WSS"})
			}()
			select {
			case <-notified.started:
			case <-time.After(time.Second):
				t.Fatal("synthetic handshake write did not start")
			}
			switch boundary {
			case "cancel":
				cancel()
			case "clientDisconnect":
				_ = consumer.Close()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("coalesced handshake exceeded write budget or leaked worker")
			}
			_ = peer.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatal("upstream survived failed handshake")
			}
			assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPGRADE outcome=WRITE_FAILED")
			if strings.Contains(logs.String(), "event=HANDSHAKE") || strings.Contains(logs.String(), "event=PUMP") {
				t.Fatal("failed handshake fabricated accepted stream activity")
			}
		})
	}
}

func TestProviderWebSocketHandshakeOverflowClosesUpstream(t *testing.T) {
	logs := captureProviderResponsesLogs(t)
	upstream, peer := net.Pipe()
	defer peer.Close()
	response := syntheticUpgradeWithSerializedBytes(t, 1025)
	response.Body = upstream
	client := &providerWebSocketAttackCheckConn{}
	server := &Server{context: t.Context(), metrics: newTestMetrics(t)}
	server.forwardWebSocket(client, bufio.NewReader(client), response, policy.Limits{MaximumHeaderBytes: 1024, WriteTimeoutMilliseconds: 50, IdleTimeoutMilliseconds: 500}, providerResponsesDiagnostic{mode: "WSS"})
	if client.packets != 0 || client.wire.Len() != 0 {
		t.Fatal("overflow emitted partial handshake")
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal("upstream survived overflow")
	}
	assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPGRADE outcome=WRITE_FAILED")
	if strings.Contains(logs.String(), "private") {
		t.Fatal("overflow exposed headers")
	}
}

func syntheticUpgradeRequest(t *testing.T, host string) *http.Request {
	t.Helper()
	request, err := http.ReadRequest(bufio.NewReader(strings.NewReader("GET /v1/responses HTTP/1.1\r\nHost: " + host + "\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func syntheticUpgradeResponse(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: 101, Status: "101 Switching Protocols", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Body: body,
		Header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}, "Sec-Websocket-Accept": {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="}}}
}

func TestProviderWebSocketRejectsUserWebUpgradeAndInvalidResponses(t *testing.T) {
	for _, mode := range []string{runtimecontract.RuntimeWebAccessFullPublic, runtimecontract.RuntimeWebAccessAllowlistFull} {
		request := syntheticUpgradeRequest(t, "example.org")
		access := runtimecontract.RuntimeProxyAccess{WebAccess: runtimecontract.RuntimeWebAccess{Mode: mode, Rules: []runtimecontract.RuntimeWebAccessRule{{DomainPattern: "example.org", Protocol: "HTTPS", Port: 443, HTTPMethods: []string{"GET"}}}}}
		if proxyRequestAllowed(request, connect.Target{Hostname: "example.org", Port: 443}, access) {
			t.Fatal("user web policy granted an upgrade")
		}
	}
	for _, mutation := range []string{"accept", "protocol", "extensions", "connection", "upgrade", "readonly"} {
		t.Run(mutation, func(t *testing.T) {
			upstream, peer := net.Pipe()
			defer upstream.Close()
			defer peer.Close()
			response := syntheticUpgradeResponse(upstream)
			if !validWebSocketResponse(response, syntheticUpgradeRequest(t, "api.openai.com")) {
				t.Fatal("valid fixture rejected")
			}
			switch mutation {
			case "accept":
				response.Header.Set("Sec-WebSocket-Accept", "foreign")
			case "protocol":
				response.Header.Set("Sec-WebSocket-Protocol", "foreign")
			case "extensions":
				response.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate")
			case "connection":
				response.Header.Set("Connection", "keep-alive, Upgrade")
			case "upgrade":
				response.Header.Set("Upgrade", "h2c")
			case "readonly":
				response.Body = io.NopCloser(strings.NewReader(""))
			}
			if validWebSocketResponse(response, syntheticUpgradeRequest(t, "api.openai.com")) {
				t.Fatal("invalid upstream upgrade accepted")
			}
		})
	}
}

func TestProviderWebSocketPumpClosesAndJoinsEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"idle", "shutdown", "clientEOF", "upstreamEOF", "blockedWrite"} {
		t.Run(boundary, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, consumer := net.Pipe()
			upstream, producer := net.Pipe()
			defer consumer.Close()
			defer producer.Close()
			server := &Server{context: ctx, metrics: newTestMetrics(t)}
			limits := policy.Limits{MaximumHeaderBytes: 16384, WriteTimeoutMilliseconds: 50, IdleTimeoutMilliseconds: 500}
			if boundary == "idle" {
				limits.IdleTimeoutMilliseconds = 30
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				server.forwardWebSocket(client, bufio.NewReader(client), syntheticUpgradeResponse(upstream), limits, providerResponsesDiagnostic{mode: "WSS"})
			}()
			_ = consumer.SetReadDeadline(time.Now().Add(time.Second))
			response, err := http.ReadResponse(bufio.NewReader(consumer), nil)
			if err != nil || response.StatusCode != 101 {
				t.Fatal("synthetic upgrade write failed")
			}
			var writeResult <-chan error
			switch boundary {
			case "shutdown":
				cancel()
			case "clientEOF":
				_ = consumer.Close()
			case "upstreamEOF":
				_ = producer.Close()
			case "blockedWrite":
				result := make(chan error, 1)
				writeResult = result
				go func() { _, err := producer.Write(make([]byte, 64<<10)); result <- err }()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("websocket pumps were not joined")
			}
			if writeResult != nil {
				select {
				case <-writeResult:
				case <-time.After(time.Second):
					t.Fatal("blocked upstream writer survived shutdown")
				}
			}
			_ = producer.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := producer.Read(make([]byte, 1)); err == nil {
				t.Fatal("upstream survived terminal stream boundary")
			}
			want := map[string]string{"idle": "IDLE", "shutdown": "SHUTDOWN", "clientEOF": "CLIENT_EOF", "upstreamEOF": "UPSTREAM_EOF", "blockedWrite": "WRITE_FAILED"}[boundary]
			assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPGRADE outcome=ACCEPTED", "event=PUMP outcome=CLOSED status_class=1XX http_status=101 failure="+want)
			presence := "client_data=ABSENT upstream_data=ABSENT"
			if boundary == "blockedWrite" {
				presence = "client_data=ABSENT upstream_data=PRESENT"
			}
			assertProviderResponsesEvents(t, logs.String(), "WSS", "event=HANDSHAKE outcome=OBSERVED status_class=1XX http_status=101 failure=NONE http_version=HTTP11 connection_close=FALSE header_lines=WITHIN_124", "event=PUMP outcome=CLOSED status_class=1XX http_status=101 failure="+want+" "+presence)
		})
	}
}

func TestProviderWebSocketPumpDataPresenceAfterJoin(t *testing.T) {
	for _, direction := range []string{"client", "upstream", "both"} {
		t.Run(direction, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, consumer := net.Pipe()
			upstream, producer := net.Pipe()
			defer consumer.Close()
			defer producer.Close()
			server := &Server{context: ctx, metrics: newTestMetrics(t)}
			limits := policy.Limits{MaximumHeaderBytes: 16384, WriteTimeoutMilliseconds: 500, IdleTimeoutMilliseconds: 1000}
			done := make(chan struct{})
			go func() {
				defer close(done)
				server.forwardWebSocket(client, bufio.NewReader(client), syntheticUpgradeResponse(upstream), limits, providerResponsesDiagnostic{mode: "WSS"})
			}()
			reader := bufio.NewReader(consumer)
			_ = consumer.SetReadDeadline(time.Now().Add(time.Second))
			if response, err := http.ReadResponse(reader, nil); err != nil || response.StatusCode != 101 {
				t.Fatal("synthetic handshake failed")
			}
			forward := func(source net.Conn, destination io.Reader) {
				t.Helper()
				payload := "private-frame-sentinel"
				written := make(chan error, 1)
				go func() { _, err := io.WriteString(source, payload); written <- err }()
				buffer := make([]byte, len(payload))
				if _, err := io.ReadFull(destination, buffer); err != nil || string(buffer) != payload {
					t.Fatal("opaque stream changed synthetic bytes")
				}
				if err := <-written; err != nil {
					t.Fatal("synthetic frame write failed")
				}
			}
			clientPresence, upstreamPresence := "ABSENT", "ABSENT"
			if direction == "client" || direction == "both" {
				_ = producer.SetReadDeadline(time.Now().Add(time.Second))
				forward(consumer, producer)
				clientPresence = "PRESENT"
			}
			if direction == "upstream" || direction == "both" {
				forward(producer, reader)
				upstreamPresence = "PRESENT"
			}
			_ = consumer.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("websocket pumps were not joined")
			}
			assertProviderResponsesEvents(t, logs.String(), "WSS", "event=PUMP outcome=CLOSED status_class=1XX http_status=101 failure=CLIENT_EOF client_data="+clientPresence+" upstream_data="+upstreamPresence)
			if strings.Contains(logs.String(), "private") {
				t.Fatal("pump diagnostic exposed payload")
			}
		})
	}
}
