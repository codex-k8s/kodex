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
	ctx            context.Context
	remaining      int
	interval       time.Duration
	firstDelivered <-chan struct{}
	started        bool
}

func (body *pacedResponseBody) Read(value []byte) (int, error) {
	if body.remaining == 0 {
		return 0, io.EOF
	}
	if body.started && body.firstDelivered != nil {
		select {
		case <-body.ctx.Done():
			return 0, body.ctx.Err()
		case <-body.firstDelivered:
		}
	}
	body.started = true
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

func TestProxyHTTPNonSSEActiveBeyondSingleWriteBudget(t *testing.T) {
	for _, framing := range []string{"chunked", "content_length"} {
		t.Run(framing, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			firstDelivered := make(chan struct{})
			body := &pacedResponseBody{ctx: ctx, remaining: 8, interval: 75 * time.Millisecond, firstDelivered: firstDelivered}
			response := &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: body, ContentLength: int64(8 * len("data: synthetic\n\n")), Close: true}
			if framing == "chunked" {
				response.ContentLength = -1
				response.TransferEncoding = []string{"chunked"}
			}
			done := make(chan error, 1)
			go func() {
				done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 500}, ctx, cancel)
				_ = server.Close()
			}()
			read, err := http.ReadResponse(bufio.NewReader(client), nil)
			if err != nil {
				t.Fatal("download header was not delivered")
			}
			if read.Header.Get("Content-Type") != "application/octet-stream" {
				t.Fatal("download content type changed")
			}
			first := make([]byte, len("data: synthetic\n\n"))
			if _, err := io.ReadFull(read.Body, first); err != nil {
				t.Fatal("first download chunk was not flushed before the next upstream read")
			}
			close(firstDelivered)
			rest, readErr := io.ReadAll(read.Body)
			_ = read.Body.Close()
			if writeErr := <-done; writeErr != nil || readErr != nil || string(first)+string(rest) != strings.Repeat("data: synthetic\n\n", 8) {
				t.Fatal("active download did not deliver its exact complete body and EOF beyond the whole-response write deadline")
			}
		})
	}
}

type cancelledResponseBody struct {
	ctx     context.Context
	entered chan struct{}
	closed  chan struct{}
}

func (body *cancelledResponseBody) Read([]byte) (int, error) {
	close(body.entered)
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (body *cancelledResponseBody) Close() error {
	close(body.closed)
	return nil
}

func TestProxyHTTPResponseIdleAndParentCancellationJoin(t *testing.T) {
	for _, mode := range []string{"non_SSE", "SSE"} {
		for _, cause := range []string{"idle", "parent"} {
			t.Run(mode+"/"+cause, func(t *testing.T) {
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				server, client := net.Pipe()
				defer server.Close()
				defer client.Close()
				_ = client.SetDeadline(time.Now().Add(time.Second))
				body := &cancelledResponseBody{ctx: ctx, entered: make(chan struct{}), closed: make(chan struct{})}
				mediaType := "application/octet-stream"
				if mode == "SSE" {
					mediaType = "text/event-stream"
				}
				response := &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": {mediaType}}, Body: body, ContentLength: -1, TransferEncoding: []string{"chunked"}, Close: true}
				idleMilliseconds := 25
				if cause == "parent" {
					idleMilliseconds = 500
				}
				done := make(chan error, 1)
				go func() {
					done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: idleMilliseconds}, ctx, cancel)
					_ = server.Close()
				}()
				drained := make(chan struct{})
				go func() { _, _ = io.Copy(io.Discard, client); close(drained) }()
				select {
				case <-body.entered:
				case <-time.After(time.Second):
					t.Fatal("response did not enter the upstream body read")
				}
				want := error(context.DeadlineExceeded)
				if cause == "parent" {
					want = context.Canceled
					cancel(context.Canceled)
				}
				select {
				case err := <-done:
					if !errors.Is(err, want) || !errors.Is(context.Cause(ctx), want) {
						t.Fatal("response cancellation lost its closed cause")
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled upstream body read was not joined")
				}
				select {
				case <-body.closed:
				default:
					t.Fatal("cancelled response body was not closed before return")
				}
				<-drained
			})
		}
	}
}

func TestProxyHTTPResponseBlockedDownstreamWriteJoins(t *testing.T) {
	for _, mode := range []string{"non_SSE", "SSE"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			mediaType := "application/octet-stream"
			if mode == "SSE" {
				mediaType = "text/event-stream"
			}
			response := &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": {mediaType}}, Body: io.NopCloser(strings.NewReader("synthetic")), ContentLength: 9, Close: true}
			done := make(chan error, 1)
			go func() {
				done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 25, IdleTimeoutMilliseconds: 500}, ctx, cancel)
			}()
			select {
			case err := <-done:
				if err == nil || !isTimeout(err) {
					t.Fatal("blocked response write escaped its budget")
				}
			case <-time.After(time.Second):
				t.Fatal("blocked response write was not joined")
			}
		})
	}
}

func TestProxyHTTPResponseWithoutBodyPreservesFraming(t *testing.T) {
	for _, body := range []io.ReadCloser{nil, http.NoBody} {
		ctx, cancel := context.WithCancelCause(t.Context())
		server, client := net.Pipe()
		response := &http.Response{StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{}, Body: body, ContentLength: 0, Close: true}
		done := make(chan error, 1)
		go func() {
			done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 500}, ctx, cancel)
			_ = server.Close()
		}()
		_ = client.SetDeadline(time.Now().Add(time.Second))
		read, err := http.ReadResponse(bufio.NewReader(client), nil)
		if err != nil {
			t.Fatal("empty response was not delivered")
		}
		value, readErr := io.ReadAll(read.Body)
		_ = read.Body.Close()
		writeErr := <-done
		_ = client.Close()
		cancel(nil)
		if writeErr != nil || readErr != nil || len(value) != 0 || read.ContentLength != 0 || len(read.TransferEncoding) != 0 {
			t.Fatal("empty response acquired unexpected body framing")
		}
	}
}

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
		done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 500}, ctx, cancel)
		_ = server.Close()
	}()
	read, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal("SSE header was not delivered")
	}
	if read.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("SSE content type changed")
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
		done <- writeProxyHTTPResponse(server, response, policy.Limits{WriteTimeoutMilliseconds: 200, IdleTimeoutMilliseconds: 25}, ctx, cancel)
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
