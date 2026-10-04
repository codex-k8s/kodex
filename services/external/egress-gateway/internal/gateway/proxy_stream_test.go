package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type pacedResponseBody struct {
	ctx       context.Context
	remaining int
	interval  time.Duration
}

func (body *pacedResponseBody) Read(value []byte) (int, error) {
	if body.remaining == 0 {
		return 0, io.EOF
	}
	timer := time.NewTimer(body.interval)
	defer timer.Stop()
	select {
	case <-body.ctx.Done():
		return 0, body.ctx.Err()
	case <-timer.C:
		body.remaining--
		return copy(value, "data: synthetic\n\n"), nil
	}
}

func (body *pacedResponseBody) Close() error { return nil }

func TestProviderSSEActiveBeyondSingleWriteBudget(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	body := &pacedResponseBody{ctx: ctx, remaining: 8, interval: 75 * time.Millisecond}
	response := &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body, ContentLength: -1, TransferEncoding: []string{"chunked"}, Close: true}
	done := make(chan error, 1)
	go func() {
		done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 500}, true, ctx, cancel)
		_ = server.Close()
	}()
	read, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal("SSE header was not delivered")
	}
	value, readErr := io.ReadAll(read.Body)
	_ = read.Body.Close()
	if writeErr := <-done; writeErr != nil || readErr != nil || strings.Count(string(value), "data: synthetic") != 8 {
		t.Fatal("active SSE was cut off by the whole-response write deadline")
	}
}

func TestProviderSSEIdleReadIsCancelledAndJoined(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	body := &pacedResponseBody{ctx: ctx, remaining: 1, interval: time.Second}
	response := &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body, ContentLength: -1, TransferEncoding: []string{"chunked"}, Close: true}
	done := make(chan error, 1)
	go func() {
		done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 25}, true, ctx, cancel)
		_ = server.Close()
	}()
	_, _ = io.Copy(io.Discard, client)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("SSE idle did not enforce a bounded upstream cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("SSE idle reader was not joined")
	}
}

func TestProviderSSEGuardIsExact(t *testing.T) {
	for _, test := range []struct {
		mode   string
		status int
		types  []string
		want   bool
	}{
		{"HTTP", 200, []string{"text/event-stream"}, true},
		{"HTTP", 200, []string{"text/event-stream; charset=utf-8"}, true},
		{"", 200, []string{"text/event-stream"}, false},
		{"HTTP", 401, []string{"text/event-stream"}, false},
		{"HTTP", 200, []string{"application/json"}, false},
		{"HTTP", 200, []string{"text/event-stream", "text/event-stream"}, false},
		{"HTTP", 200, []string{"text/event-stream; invalid"}, false},
	} {
		response := &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": test.types}}
		if providerResponsesIsSSE(providerResponsesDiagnostic{mode: test.mode}, response) != test.want {
			t.Fatal("non-provider or non-SSE response changed deadline semantics")
		}
	}
}

func TestProviderSSEParentCancellationJoinsRead(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	body := proxyIdleResponseBody{ReadCloser: &pacedResponseBody{ctx: ctx, remaining: 1, interval: time.Second}, ctx: ctx, cancel: cancel, timeout: time.Second}
	done := make(chan error, 1)
	go func() { _, err := body.Read(make([]byte, 32)); done <- err }()
	cancel(context.Canceled)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("parent cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled upstream read was not joined")
	}
}

func TestProviderSSEBlockedWriteBudgetIsPreserved(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		_, err := (proxyDeadlineWriter{connection: server, timeout: 25 * time.Millisecond}).Write([]byte("synthetic"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !isTimeout(err) {
			t.Fatal("blocked downstream write escaped its budget")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked downstream write was not joined")
	}
}
