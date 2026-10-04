package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

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
			limits := policy.Limits{WriteTimeoutMilliseconds: 50, IdleTimeoutMilliseconds: 500}
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
			limits := policy.Limits{WriteTimeoutMilliseconds: 500, IdleTimeoutMilliseconds: 1000}
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
