package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
)

func TestMCPRequestBodyBoundaryRejectsOversizedBeforeSessionEffects(t *testing.T) {
	const maximumBodyBytes = int64(512)
	for _, test := range []struct {
		name          string
		authorization string
		contentLength int64
	}{
		{name: "content length", authorization: "Bearer session-token", contentLength: maximumBodyBytes + 1},
		{name: "chunked", authorization: "Bearer session-token", contentLength: -1},
		{name: "unauthenticated", contentLength: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			handler := newMCPHandler(service, maximumBodyBytes)
			body := strings.Repeat("x", int(maximumBodyBytes+1))
			request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", strings.NewReader(body))
			request.ContentLength = test.contentLength
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			guards := store.guardSnapshot()
			if store.sessionReads != 0 || guards.calls != 0 || runner.secretReads != 0 || publisher.posts != 0 {
				t.Fatalf("oversized effects: reads=%d guards=%d token_reads=%d posts=%d", store.sessionReads, guards.calls, runner.secretReads, publisher.posts)
			}
		})
	}
}

func TestMCPRequestBodyBoundaryReadsAtMostLimitPlusOne(t *testing.T) {
	const maximumBodyBytes = int64(1024)
	reader := &countingMCPBodyReader{remaining: maximumBodyBytes * 8}
	request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", reader)
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	newMCPHandler(newSessionBarrierServiceOnly(), maximumBodyBytes).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if reader.readBytes > maximumBodyBytes+1 {
		t.Fatalf("transport read %d bytes, want at most %d", reader.readBytes, maximumBodyBytes+1)
	}
}

func TestMCPRequestBodyBoundaryAllowsExactLimit(t *testing.T) {
	const maximumBodyBytes = int64(512)
	payload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"boundary","version":"1"}}}`
	if int64(len(payload)) >= maximumBodyBytes {
		t.Fatal("test payload does not leave room for exact-boundary padding")
	}
	payload += strings.Repeat(" ", int(maximumBodyBytes)-len(payload))
	request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()

	newMCPHandler(newSessionBarrierServiceOnly(), maximumBodyBytes).ServeHTTP(recorder, request)

	if recorder.Code == http.StatusRequestEntityTooLarge || recorder.Code == http.StatusRequestTimeout {
		t.Fatalf("exact-boundary request was rejected by transport: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMCPRequestBodyBoundaryTimesOutSlowChunkedBody(t *testing.T) {
	service, store, runner, publisher := newSessionBarrierService(0)
	server := httptest.NewUnstartedServer(newMCPHandler(service, 1024))
	server.Config.ReadHeaderTimeout = 40 * time.Millisecond
	server.Config.ReadTimeout = 60 * time.Millisecond
	server.Config.IdleTimeout = time.Second
	server.Start()
	defer server.Close()

	reader, writer := io.Pipe()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/mcp/sessions/session-admin", reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer session-token")
	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := writer.Write([]byte(`{"jsonrpc":"2.0"`))
		writeDone <- writeErr
	}()

	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	_ = writer.Close()
	if err != nil {
		t.Fatalf("slow request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestTimeout {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("slow request writer remained blocked")
	}
	guards := store.guardSnapshot()
	if store.sessionReads != 0 || guards.calls != 0 || runner.secretReads != 0 || publisher.posts != 0 {
		t.Fatalf("slow-body effects: reads=%d guards=%d token_reads=%d posts=%d", store.sessionReads, guards.calls, runner.secretReads, publisher.posts)
	}
}

type countingMCPBodyReader struct {
	remaining int64
	readBytes int64
}

func (reader *countingMCPBodyReader) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	count := int64(len(buffer))
	if count > reader.remaining {
		count = reader.remaining
	}
	for index := int64(0); index < count; index++ {
		buffer[index] = 'x'
	}
	reader.remaining -= count
	reader.readBytes += count
	return int(count), nil
}

func newSessionBarrierServiceOnly() *statusservice.AgentSessionService {
	service, _, _, _ := newSessionBarrierService(0)
	return service
}
