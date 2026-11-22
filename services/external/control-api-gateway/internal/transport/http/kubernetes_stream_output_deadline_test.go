package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/observability"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// delayedOutputWriter holds the handler's first body write until a controlled instant, while Unwrap preserves real TLS socket deadlines and flush errors.
// delayedOutputWriter задерживает первую запись тела до заданного момента, а Unwrap сохраняет настоящие TLS socket deadlines и flush errors.
type delayedOutputWriter struct {
	http.ResponseWriter
	until        time.Time
	privateWrite bool
}

// Unwrap exposes the real server writer to the standard deadline and flush controller.
// Unwrap предоставляет настоящий server writer стандартному контроллеру deadline и flush.
func (writer *delayedOutputWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// Write deterministically delays transport admission without changing payload bytes or substituting a fake connection.
// Write детерминированно задерживает передачу transport без изменения payload либо подмены настоящего соединения.
func (writer *delayedOutputWriter) Write(body []byte) (int, error) {
	writer.privateWrite = bytes.Contains(body, []byte("payloadBase64"))
	if delay := time.Until(writer.until); delay > 0 {
		timer := time.NewTimer(delay)
		<-timer.C
	}
	return writer.ResponseWriter.Write(body)
}

// TestKubernetesStreamOutputHTTPRequiresNativeDeadline rejects a writer without deadline support before emitting any private frame bytes.
// TestKubernetesStreamOutputHTTPRequiresNativeDeadline отклоняет writer без deadline до выдачи private frame bytes.
func TestKubernetesStreamOutputHTTPRequiresNativeDeadline(t *testing.T) {
	r := &outputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: outputHTTPFixture(0)}}
	w := httptest.NewRecorder()
	outputHTTPHandler(r).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/output?generation=7&afterSequence=0", nil))
	if w.Code != http.StatusServiceUnavailable || bytes.Contains(w.Body.Bytes(), []byte("payloadBase64")) || !bytes.Equal(r.borrowed, make([]byte, len(r.borrowed))) {
		t.Fatal("unsupported deadline disclosed a private frame")
	}
}

// TestKubernetesStreamOutputHTTPNativeTLSDeadline exercises real HTTP/1 and HTTP/2 TLS writes through telemetry/localization wrappers; expired owner windows, shorter request deadlines and cancellation cannot complete the private response.
// TestKubernetesStreamOutputHTTPNativeTLSDeadline проверяет настоящую TLS запись HTTP/1 и HTTP/2 через telemetry/localization; истёкшее окно владельца, более короткий request deadline и cancel не позволяют завершить private response.
func TestKubernetesStreamOutputHTTPNativeTLSDeadline(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		protocol := "http1"
		if http2 {
			protocol = "http2"
		}
		for _, scenario := range []string{"success", "owner_expiry", "request_expiry", "request_cancel"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				telemetry, err := observability.NewRuntime(context.Background(), observability.RuntimeConfig{Disabled: true, ServiceName: "synthetic", ServiceVersion: "test", Environment: "test"})
				if err != nil {
					t.Fatal("disabled telemetry initialization failed")
				}
				done := make(chan struct{})
				var recorder *outputHTTPRecorder
				var transport *delayedOutputWriter
				var negotiated int
				handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					defer close(done)
					negotiated = request.ProtoMajor
					raw := outputHTTPFixture(0)
					pauseUntil := time.Time{}
					ctx := request.Context()
					if scenario != "success" {
						pauseUntil = time.Now().Add(200 * time.Millisecond)
					}
					switch scenario {
					case "owner_expiry":
						raw.AuthorizedUntil = timestamppb.New(pauseUntil.Add(-50 * time.Millisecond))
					case "request_expiry":
						var cancel context.CancelFunc
						ctx, cancel = context.WithDeadline(ctx, pauseUntil.Add(-50*time.Millisecond))
						defer cancel()
					case "request_cancel":
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(ctx)
						defer cancel()
						timer := time.AfterFunc(50*time.Millisecond, cancel)
						defer timer.Stop()
					}
					recorder = &outputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: raw}}
					route := http.HandlerFunc(func(inner http.ResponseWriter, r *http.Request) {
						outputHTTPHandler(recorder).ServeHTTP(&localizedResponseWriter{ResponseWriter: inner}, r)
					})
					transport = &delayedOutputWriter{ResponseWriter: w, until: pauseUntil}
					telemetry.HTTPMiddleware(func(string) string { return "synthetic" }, nil, route).ServeHTTP(transport, request.WithContext(ctx))
				})
				server := httptest.NewUnstartedServer(handler)
				server.EnableHTTP2 = http2
				server.StartTLS()
				defer server.Close()
				client := server.Client()
				client.Timeout = 3 * time.Second
				response, readErr := client.Get(server.URL + "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/output?generation=7&afterSequence=0")
				var body []byte
				if response != nil {
					body, err = io.ReadAll(io.LimitReader(response.Body, 64<<10))
					_ = response.Body.Close()
					if err != nil {
						readErr = err
					}
				}
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("HTTP writer did not join")
				}
				wantProtocol := 1
				if http2 {
					wantProtocol = 2
				}
				if negotiated != wantProtocol || transport == nil || !transport.privateWrite || recorder == nil || len(recorder.borrowed) == 0 || !bytes.Equal(recorder.borrowed, make([]byte, len(recorder.borrowed))) {
					t.Fatal("native protocol or private buffer cleanup was not observed")
				}
				var view generated.KubernetesStreamOutputReadback
				complete := json.Unmarshal(body, &view) == nil && view.Frame != nil
				if scenario == "success" {
					if readErr != nil || !complete || !bytes.Equal(view.Frame.PayloadBase64, []byte{0, 1, 2, 255}) {
						t.Fatal("authorized native TLS response failed")
					}
				} else if readErr == nil || complete || bytes.Contains(body, []byte("payloadBase64")) {
					t.Fatal("expired native TLS response disclosed a private frame")
				}
			})
		}
	}
}
