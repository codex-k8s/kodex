package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
)

func captureProviderResponsesLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	writer, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() { log.SetOutput(writer); log.SetFlags(flags); log.SetPrefix(prefix) })
	return &output
}

func assertProviderResponsesEvents(t *testing.T, output string, mode string, events ...string) {
	t.Helper()
	for _, event := range events {
		if !strings.Contains(output, "route=RESPONSES mode="+mode+" "+event) {
			t.Fatalf("missing closed event %s in %q", event, output)
		}
	}
	for _, private := range []string{"api.openai.com", "chatgpt.com", "/v1/responses", "/backend-api/codex/responses", "synthetic", "data: first", "data: completed", "Bearer", "dGhlIHNhbXBsZSBub25jZQ=="} {
		if strings.Contains(output, private) {
			t.Fatal("response diagnostic exposed private fixture data")
		}
	}
}

func TestProviderResponsesDiagnosticExactRoutes(t *testing.T) {
	for _, test := range []struct {
		name, host, path, method, rawPath, upgrade, want string
		port                                             int
		provider                                         bool
	}{
		{name: "HTTP", host: "api.openai.com", path: "/v1/responses", method: "POST", port: 443, provider: true, want: "HTTP"},
		{name: "WSS", host: "chatgpt.com", path: "/backend-api/codex/responses", method: "GET", upgrade: "websocket", port: 443, provider: true, want: "WSS"},
		{name: "HTTPChatGPT", host: "chatgpt.com", path: "/backend-api/codex/responses", method: "POST", port: 443, provider: true, want: "HTTP"},
		{name: "WSSOpenAI", host: "api.openai.com", path: "/v1/responses", method: "GET", upgrade: "websocket", port: 443, provider: true, want: "WSS"},
		{name: "foreignHost", host: "private-sentinel.example", path: "/v1/responses", method: "POST", port: 443, provider: true},
		{name: "crossedPath", host: "api.openai.com", path: "/backend-api/codex/responses", method: "POST", port: 443, provider: true},
		{name: "trailingSlash", host: "api.openai.com", path: "/v1/responses/", method: "POST", port: 443, provider: true},
		{name: "encodedPath", host: "api.openai.com", path: "/v1/responses", rawPath: "/v1/%72esponses", method: "POST", port: 443, provider: true},
		{name: "wrongPort", host: "api.openai.com", path: "/v1/responses", method: "POST", port: 8443, provider: true},
		{name: "noProvider", host: "api.openai.com", path: "/v1/responses", method: "POST", port: 443},
		{name: "wrongMethod", host: "api.openai.com", path: "/v1/responses", method: "DELETE", port: 443, provider: true, want: "HTTP"},
		{name: "GETWithoutUpgrade", host: "api.openai.com", path: "/v1/responses", method: "GET", port: 443, provider: true, want: "HTTP"},
		{name: "POSTUpgrade", host: "api.openai.com", path: "/v1/responses", method: "POST", upgrade: "websocket", port: 443, provider: true, want: "WSS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &http.Request{Method: test.method, URL: &url.URL{Path: test.path, RawPath: test.rawPath, RawQuery: "private-query-sentinel"}, Header: http.Header{"Authorization": {"private-header-sentinel"}}}
			if test.upgrade != "" {
				request.Header.Set("Upgrade", test.upgrade)
			}
			diagnostic := newProviderResponsesDiagnostic(request, connect.Target{Hostname: test.host, Port: test.port}, runtimecontract.RuntimeProxyAccess{ProviderAccess: test.provider})
			if diagnostic.mode != test.want {
				t.Fatalf("mode=%s want=%s", diagnostic.mode, test.want)
			}
		})
	}
	for _, request := range []*http.Request{nil, {}} {
		if newProviderResponsesDiagnostic(request, connect.Target{Hostname: "api.openai.com", Port: 443}, runtimecontract.RuntimeProxyAccess{ProviderAccess: true}).enabled() {
			t.Fatal("missing request shape acquired diagnostics")
		}
	}
}

func TestProviderResponsesDiagnosticStatusWhitelist(t *testing.T) {
	allowed := map[int]bool{101: true, 200: true, 201: true, 204: true, 400: true, 401: true, 403: true, 404: true, 408: true, 409: true, 413: true, 415: true, 422: true, 426: true, 429: true, 500: true, 502: true, 503: true, 504: true}
	for status := -1; status <= 1000; status++ {
		want := "UNKNOWN"
		if status == 0 {
			want = "NONE"
		} else if allowed[status] {
			want = strconv.Itoa(status)
		}
		if got := providerResponsesStatus(status); got != want {
			t.Fatalf("status=%d got=%s want=%s", status, got, want)
		}
	}
}

func TestProviderResponsesPolicyFailureKeepsExistingDenial(t *testing.T) {
	target := connect.Target{Hostname: "api.openai.com", Port: 443}
	access := runtimecontract.RuntimeProxyAccess{ProviderAccess: true}
	for _, test := range []struct{ mutation, want string }{
		{"emptyURI", "REQUEST_ENVELOPE"}, {"absoluteURI", "REQUEST_ENVELOPE"},
		{"foreignHost", "HOST"}, {"foreignPort", "HOST"},
		{"method", "METHOD_PATH"}, {"protocol", "WS_SUBPROTOCOL"},
		{"extensions", "WS_EXTENSIONS"}, {"key", "WS_HANDSHAKE"},
	} {
		t.Run(test.mutation, func(t *testing.T) {
			request := syntheticUpgradeRequest(t, target.Hostname)
			switch test.mutation {
			case "emptyURI":
				request.RequestURI = ""
			case "absoluteURI":
				request.URL.Scheme = "https"
				request.URL.Host = target.Hostname
			case "foreignHost":
				request.Host = "private-host-sentinel.example"
			case "foreignPort":
				request.Host = target.Hostname + ":8443"
			case "method":
				request.Method = "private-method-sentinel"
			case "protocol":
				request.Header.Set("Sec-WebSocket-Protocol", "private-protocol-sentinel")
			case "extensions":
				request.Header.Set("Sec-WebSocket-Extensions", "private-extension-sentinel")
			case "key":
				request.Header.Set("Sec-WebSocket-Key", "private-key-sentinel")
			}
			if proxyRequestAllowed(request, target, access) {
				t.Fatal("diagnostic fixture unexpectedly passed existing policy")
			}
			reason := providerResponsesPolicyFailure(request, target, access)
			if reason != test.want {
				t.Fatalf("reason=%s want=%s", reason, test.want)
			}
			output := captureProviderResponsesLogs(t)
			newProviderResponsesDiagnostic(request, target, access).policy(false, reason)
			assertProviderResponsesEvents(t, output.String(), "WSS", "event=POLICY outcome=DENIED status_class=NONE http_status=NONE failure="+test.want)
			if strings.Contains(output.String(), "private") {
				t.Fatal("policy diagnosis exposed private fixture values")
			}
		})
	}
}

func TestProviderResponsesDiagnosticNeverLogsExternalValues(t *testing.T) {
	output := captureProviderResponsesLogs(t)
	diagnostic := providerResponsesDiagnostic{mode: "WSS"}
	private := errors.New("private-body-header-url-error-sentinel")
	diagnostic.policy(false, private.Error())
	diagnostic.policy(true, private.Error())
	diagnostic.upstream(0, private, false)
	diagnostic.upstream(0, fmt.Errorf("private-wrap: %w", context.Canceled), false)
	diagnostic.upstream(599, nil, false)
	diagnostic.body(429, private)
	diagnostic.body(200, nil)
	diagnostic.upgrade(private.Error(), 599)
	diagnostic.pump(private.Error())
	before := output.String()
	for _, mode := range []string{"", private.Error()} {
		invalid := providerResponsesDiagnostic{mode: mode}
		invalid.policy(true, private.Error())
		invalid.upstream(200, private, false)
		invalid.body(200, private)
		invalid.upgrade("ACCEPTED", 101)
		invalid.pump("IDLE")
	}
	if output.String() != before || strings.Contains(before, "private") || strings.Contains(before, "599") {
		t.Fatal("untrusted values escaped closed diagnostic normalization")
	}
	assertProviderResponsesEvents(t, before, "WSS", "event=POLICY outcome=DENIED", "event=POLICY outcome=ALLOWED", "event=UPSTREAM outcome=FAILED status_class=NONE http_status=NONE failure=UNKNOWN", "event=UPSTREAM outcome=FAILED status_class=NONE http_status=NONE failure=CANCELLED", "event=UPSTREAM outcome=RESPONSE status_class=5XX http_status=UNKNOWN", "event=UPSTREAM_BODY outcome=FAILED status_class=4XX http_status=429 failure=IO", "event=UPGRADE outcome=UNKNOWN", "event=PUMP outcome=CLOSED status_class=1XX http_status=101 failure=UNKNOWN")
	for _, test := range []struct {
		err    error
		client bool
		want   string
	}{{io.EOF, true, "CLIENT_EOF"}, {io.EOF, false, "UPSTREAM_EOF"}, {private, true, "CLIENT_IO"}, {private, false, "UPSTREAM_IO"}} {
		if got := providerWebSocketReadFailure(test.err, test.client); got != test.want {
			t.Fatalf("read failure=%s want=%s", got, test.want)
		}
	}
}
