package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// testClient binds a synthetic local server with disposable test-only authentication.
// testClient связывает локальный synthetic server с одноразовой тестовой авторизацией.
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "synthetic", strings.Repeat("q", 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

// TestClientRejectsExternalAndAmbiguousEndpoints prevents local API auth from reaching another origin.
// TestClientRejectsExternalAndAmbiguousEndpoints не позволяет отправить локальную авторизацию на другой origin.
func TestClientRejectsExternalAndAmbiguousEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"https://127.0.0.1:12345", "http://localhost:12345", "http://example.invalid:12345",
		"http://127.0.0.1:80", "http://127.0.0.1:012345", "http://127.0.0.1:12345/",
		"http://127.0.0.1:12345?", "http://127.0.0.1:12345#fragment", "http://user@127.0.0.1:12345",
	} {
		if _, err := NewClient(endpoint, "synthetic", strings.Repeat("q", 32)); err == nil {
			t.Fatalf("accepted noncanonical local endpoint %q", endpoint)
		}
	}
}

// TestClientDoesNotFollowRedirectWithLocalCredentials checks the actual transport, not only URL validation.
// TestClientDoesNotFollowRedirectWithLocalCredentials проверяет transport, а не только валидацию URL.
func TestClientDoesNotFollowRedirectWithLocalCredentials(t *testing.T) {
	var leaked atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	t.Cleanup(foreign.Close)
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, foreign.URL, http.StatusTemporaryRedirect)
	})
	_, err := client.CreateSession(t.Context(), "Synthetic")
	var rejected *APIError
	if !errors.As(err, &rejected) || rejected.Status != http.StatusTemporaryRedirect || leaked.Load() != 0 {
		t.Fatalf("redirect was not closed: %v, foreign calls %d", err, leaked.Load())
	}
}

// TestClientDoesNotReplayUnknownCreate simulates a server that accepts a write and loses its reply.
// TestClientDoesNotReplayUnknownCreate моделирует принятую запись с потерянным ответом.
func TestClientDoesNotReplayUnknownCreate(t *testing.T) {
	var accepted atomic.Int64
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		accepted.Add(1)
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	})
	if _, err := client.CreateSession(t.Context(), "Synthetic"); !errors.Is(err, ErrUnavailable) || accepted.Load() != 1 {
		t.Fatalf("unknown mutation was replayed: %v, accepts %d", err, accepted.Load())
	}
}

// TestClientClosesDuplicateAndForeignNativeIdentity rejects ambiguous data before a session is used.
// TestClientClosesDuplicateAndForeignNativeIdentity отклоняет неоднозначные данные до использования сессии.
func TestClientClosesDuplicateAndForeignNativeIdentity(t *testing.T) {
	for _, response := range []string{
		`{"id":"ses_one","id":"ses_two"}`, `{"id":"ses_other"}`,
		`{"id":"ses_one"} {"id":"ses_other"}`, `{"id":"ses_one","future":{"x":1,"x":2}}`,
	} {
		t.Run(response, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, request *http.Request) { io.WriteString(writer, response) })
			if _, err := client.Session(t.Context(), "ses_one"); !errors.Is(err, ErrProtocol) {
				t.Fatalf("invalid session identity accepted: %v", err)
			}
		})
	}
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, `{"id":"ses_one","future":{"advisory":true}}`)
	})
	if _, err := client.Session(t.Context(), "ses_one"); err != nil {
		t.Fatal("harmless metadata was rejected", err)
	}
}

// TestClientStreamsBeyondOldLifetimeLimit observes more than4096 events without treating EOF as success.
// TestClientStreamsBeyondOldLifetimeLimit читает более4096 событий, не выдавая EOF за успех.
func TestClientStreamsBeyondOldLifetimeLimit(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for index := range 5001 {
			fmt.Fprintf(writer, "data: {\"type\":\"message.part.delta\",\"properties\":{\"ordinal\":%d}}\n\n", index)
		}
	})
	count := 0
	err := client.Events(t.Context(), func(event Event) error { count++; return nil })
	if count != 5001 || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stream was truncated or promoted to completion: %d %v", count, err)
	}
}

// TestClientPropagatesConsumerStop applies backpressure and does not consume the remaining stream.
// TestClientPropagatesConsumerStop применяет backpressure и не поглощает оставшийся поток.
func TestClientPropagatesConsumerStop(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, "data: {\"type\":\"session.compacted\",\"properties\":{}}\n\n")
	})
	stop := errors.New("synthetic consumer stop")
	if err := client.Events(context.WithoutCancel(t.Context()), func(Event) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
}
