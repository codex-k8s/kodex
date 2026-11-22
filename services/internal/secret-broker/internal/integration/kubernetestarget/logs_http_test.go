package kubernetestarget

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// logsInputFixture pins synthetic request coordinates without claiming selected-identity authorization.
// logsInputFixture закрепляет synthetic request coordinates без утверждения selected-identity authorization.
func logsInputFixture() (managedkubernetes.OperationInput, ResourceAddress) {
	input := managedkubernetes.OperationInput{Version: "v1", Resource: "pods", Namespace: "example", Name: "web", Options: json.RawMessage(`{"container":"main","follow":true,"previous":false,"timestamps":true,"tailLines":100,"sinceSeconds":1,"limitBytes":131072,"timeoutSeconds":10}`)}
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespace: "example", Name: "web", Namespaced: true, UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "17"}
	return input, address
}

// TestNativeLogsHTTPUsesPrivateIdentityAndWorkerLifetime checks actual TLS, exact query and bounded chunks after unary handoff.
// TestNativeLogsHTTPUsesPrivateIdentityAndWorkerLifetime проверяет actual TLS, exact query и bounded chunks после unary handoff.
func TestNativeLogsHTTPUsesPrivateIdentityAndWorkerLifetime(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			payload := bytes.Repeat([]byte("synthetic log line\n"), 4000)
			credential, calls := transportFixtureWithHandler(t, certificate, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/example/pods/web/log" || query.Get("container") != "main" || query.Get("follow") != "true" || query.Get("previous") != "false" || query.Get("timestamps") != "true" || query.Get("tailLines") != "100" || query.Get("sinceSeconds") != "1" || query.Get("limitBytes") != "131072" || len(query) != 7 {
					t.Error("changed Pod log route or closed query")
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(payload)
			}))
			input, address := logsInputFixture()
			attempt, cancel := context.WithCancel(t.Context())
			now := time.Now()
			stream, err := credential.openLogsHTTP(t.Context(), attempt, input, address, "main", now.Add(3*time.Second), now.Add(time.Second), 128<<10)
			cancel()
			if err != nil {
				t.Fatal("native logs open failed")
			}
			defer stream.Close()
			origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: "stream_synthetic", StreamGeneration: 1, Operation: "kubernetes.logs", TargetRef: credential.target.ID, TargetRevision: int64(credential.target.Revision)}
			receipt, err := stream.HandshakeReceipt(origin)
			if err != nil || receipt.Validate() != nil || receipt.Protocol != managedkubernetes.HTTPLogsV1 {
				t.Fatal("logs lost observed handshake")
			}
			var received bytes.Buffer
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal("logs lost worker lifetime")
				}
				if read.CleanEOF {
					break
				}
				if read.Channel != "STDOUT" || len(read.Payload) > managedkubernetes.MaximumFrameBytes || read.ExitCode != nil {
					t.Fatal("logs escaped output contract")
				}
				received.Write(read.Payload)
				clear(read.Payload)
			}
			if !bytes.Equal(payload, received.Bytes()) || calls.Load() != 1 {
				t.Fatal("logs changed bytes or reconnected")
			}
			if stream.WriteFrame(t.Context(), value.KubernetesStreamFrame{Channel: "STDIN", Payload: []byte("denied")}) == nil {
				t.Fatal("logs accepted input")
			}
		})
	}
}

// TestNativeLogsHTTPRejectsUnsafeRequestsAndReplies prevents unknown query, UID drift, redirects and upstream diagnostics from escaping.
// TestNativeLogsHTTPRejectsUnsafeRequestsAndReplies запрещает unknown query, UID drift, redirects и выдачу upstream diagnostics.
func TestNativeLogsHTTPRejectsUnsafeRequestsAndReplies(t *testing.T) {
	credential, calls := transportFixture(t, false)
	input, address := logsInputFixture()
	for _, variant := range []string{"unsafe TLS", "foreign UID", "container", "non-Pod", "limit"} {
		changed, location := input, address
		container := "main"
		switch variant {
		case "unsafe TLS":
			changed.Options = json.RawMessage(`{"timeoutSeconds":10,"insecureSkipTLSVerifyBackend":true}`)
		case "foreign UID":
			changed.UID = "22222222-2222-4222-8222-222222222222"
		case "container":
			container = "other"
		case "non-Pod":
			location.Resource = "secrets"
		case "limit":
			changed.Options = json.RawMessage(`{"timeoutSeconds":10,"limitBytes":0}`)
		}
		now := time.Now()
		if stream, err := credential.openLogsHTTP(t.Context(), t.Context(), changed, location, container, now.Add(time.Second), now.Add(time.Second), 4096); err == nil || stream != nil {
			t.Fatal("invalid logs request opened")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe request reached network")
	}
	for _, status := range []int{301, 401, 403, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://example.invalid/private")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private-upstream-diagnostic"))
			}))
			now := time.Now()
			stream, err := credential.openLogsHTTP(t.Context(), t.Context(), input, address, "main", now.Add(time.Second), now.Add(time.Second), 4096)
			if err == nil || stream != nil || strings.Contains(err.Error(), "private-upstream") || calls.Load() != 1 {
				t.Fatal("unsafe reply escaped or was retried")
			}
		})
	}
}

// TestNativeLogsHTTPCancelAndByteBudget proves bounded receive and positive reader return after cancellation.
// TestNativeLogsHTTPCancelAndByteBudget доказывает bounded receive и positive reader return после cancellation.
func TestNativeLogsHTTPCancelAndByteBudget(t *testing.T) {
	for _, variant := range []string{"limit", "opening cancel", "reader cancel"} {
		t.Run(variant, func(t *testing.T) {
			entered := make(chan struct{})
			peerDone := make(chan struct{})
			credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(peerDone)
				close(entered)
				if variant == "limit" {
					_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
					return
				}
				if variant == "reader cancel" {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			input, address := logsInputFixture()
			now := time.Now()
			attempt, cancel := context.WithCancel(t.Context())
			defer cancel()
			if variant == "opening cancel" {
				result := make(chan error, 1)
				go func() {
					stream, err := credential.openLogsHTTP(t.Context(), attempt, input, address, "main", now.Add(3*time.Second), now.Add(time.Second), 1024)
					if stream != nil {
						_ = stream.Close()
					}
					result <- err
				}()
				<-entered
				cancel()
				select {
				case err := <-result:
					if err == nil {
						t.Fatal("canceled opening succeeded")
					}
				case <-time.After(time.Second):
					t.Fatal("canceled opening failed to join")
				}
			} else {
				stream, err := credential.openLogsHTTP(t.Context(), attempt, input, address, "main", now.Add(3*time.Second), now.Add(time.Second), 1024)
				if err != nil {
					t.Fatal("logs open failed")
				}
				defer stream.Close()
				if variant == "limit" {
					read, err := stream.ReadStream()
					if err == nil || len(read.Payload) != 0 || read.CleanEOF {
						t.Fatal("logs exceeded local ceiling")
					}
				} else {
					result := make(chan error, 1)
					go func() { _, err := stream.ReadStream(); result <- err }()
					_ = stream.Close()
					select {
					case err := <-result:
						if err == nil {
							t.Fatal("cancellation became EOF")
						}
					case <-time.After(time.Second):
						t.Fatal("reader failed to join")
					}
				}
			}
			select {
			case <-peerDone:
			case <-time.After(time.Second):
				t.Fatal("HTTP peer did not close")
			}
		})
	}
}
