package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

// Сохраняем generated HTTP serialization и реальный socket, меняя только test destination.
func emailFixture(t *testing.T, adapter *Adapter, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	adapter.emailHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != strings.TrimPrefix(emailOrigin, "https://") || r.GetBody != nil {
			t.Error("email origin or replay boundary lost")
		}
		clone := r.Clone(r.Context())
		endpoint := *r.URL
		endpoint.Scheme, endpoint.Host = "http", strings.TrimPrefix(server.URL, "http://")
		clone.URL = &endpoint
		return server.Client().Transport.RoundTrip(clone)
	})}
}

func TestEmailEveryMutationHTTPFailureIsNotRetried(t *testing.T) {
	for operation, raw := range catalogInputs() {
		command, err := api.CommandForIntegration(operation, "mailbox", "sender@example.test", "effect", []byte(raw))
		if err != nil || !api.IsMutation(command.Operation) {
			continue
		}
		for _, failure := range []string{"unknown", "malformed", "oversize", "unavailable", "forbidden", "failed"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				adapter := testAdapter(t)
				var calls atomic.Int32
				emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					switch failure {
					case "malformed":
						_, _ = io.WriteString(w, `{`)
					case "oversize":
						_, _ = io.WriteString(w, strings.Repeat("x", maximumResponseBytes+1))
					case "unavailable":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "forbidden":
						w.WriteHeader(http.StatusForbidden)
					default:
						_ = json.NewEncoder(w).Encode(api.Result{Status: failure, MessageId: "receipt-fixture"})
					}
				})
				var input map[string]any
				_ = json.Unmarshal([]byte(raw), &input)
				request := invocationRequest(t, adapter.definitions["email"], operation, input, testCredential(t, adapter, "unused-email-credential"))
				_, err := adapter.Execute(t.Context(), request)
				unknown := failure != "forbidden" && failure != "failed"
				if err == nil || IsUnknownOutcome(err) != unknown || calls.Load() != 1 {
					t.Fatalf("failure classification: err=%v calls=%d", err, calls.Load())
				}
			})
		}
	}
}

func TestEmailClaimExpiryBoundsHTTP(t *testing.T) {
	adapter := testAdapter(t)
	var calls atomic.Int32
	emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	request := invocationRequest(t, adapter.definitions["email"], "email.message.send", map[string]any{"to": "recipient@example.test", "subject": "Title", "body_text": "Text"}, testCredential(t, adapter, "unused"))
	request.EmailExecution.Lease.ExpiresAt = time.Now().Add(150 * time.Millisecond)
	start := time.Now()
	_, err := adapter.Execute(t.Context(), request)
	if !IsUnknownOutcome(err) || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("lease did not bound effect: %v calls=%d", err, calls.Load())
	}
	_, err = adapter.Execute(t.Context(), request)
	if err == nil || calls.Load() != 1 {
		t.Fatal("expired claim reached HTTP")
	}
}

func TestEmailTestBindingCannotAuthorizeMutation(t *testing.T) {
	adapter := testAdapter(t)
	var calls atomic.Int32
	emailFixture(t, adapter, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	request := invocationRequest(t, adapter.definitions["email"], "email.message.send", map[string]any{"to": "recipient@example.test", "subject": "Title", "body_text": "Text"}, testCredential(t, adapter, "unused"))
	testRef := "test_fixture01"
	request.EmailExecution.InvocationRef, request.EmailExecution.ConnectionTestRef = nil, &testRef
	if _, err := adapter.Execute(t.Context(), request); err == nil || calls.Load() != 0 {
		t.Fatal("test binding acquired mutation authority")
	}
}

func TestEmailReadbackExactIdentity(t *testing.T) {
	command := api.Command{Operation: api.OperationFetch, Uid: "7", UidValidity: 3, Folder: "INBOX"}
	result := api.Result{Uid: "7", UidValidity: 3, Folder: "INBOX"}
	if !validEmailReadback(command, result) {
		t.Fatal("exact IMAP identity rejected")
	}
	for _, bad := range []api.Result{{Uid: "8", UidValidity: 3, Folder: "INBOX"}, {Uid: "7", UidValidity: 4, Folder: "INBOX"}, {Uid: "7", UidValidity: 3, Folder: "Other"}} {
		if validEmailReadback(command, bad) {
			t.Fatal("foreign IMAP identity accepted")
		}
	}
	if validEmailReadback(api.Command{Operation: api.OperationThread, ThreadId: "one"}, api.Result{ThreadId: "two"}) {
		t.Fatal("foreign thread accepted")
	}
}
