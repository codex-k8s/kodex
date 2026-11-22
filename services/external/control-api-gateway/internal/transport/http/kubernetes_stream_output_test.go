package httptransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// outputHTTPRecorder retains the actual decoded response buffer solely to verify its destruction after HTTP projection.
// outputHTTPRecorder удерживает actual decoded response buffer только для проверки очистки после HTTP projection.
type outputHTTPRecorder struct {
	*catalogRPCRecorder
	borrowed []byte
}

// Invoke reuses the existing typed recorder and observes only the returned frame allocation.
// Invoke переиспользует existing typed recorder и наблюдает лишь returned frame allocation.
func (r *outputHTTPRecorder) Invoke(ctx context.Context, method string, in, out any, options ...grpc.CallOption) error {
	err := r.catalogRPCRecorder.Invoke(ctx, method, in, out, options...)
	if v, ok := out.(*cp.ReadManagedKubernetesStreamOutputResponse); ok && v.Frame != nil {
		r.borrowed = v.Frame.Payload
	}
	return err
}

// outputHTTPFixture builds bounded synthetic owner bytes without asserting a live native stream, lease or user authorization.
// outputHTTPFixture создаёт bounded synthetic owner bytes без заявления о live native stream, lease либо user authorization.
func outputHTTPFixture(after uint64) *cp.ReadManagedKubernetesStreamOutputResponse {
	body := []byte{0, 1, 2, 255}
	sum := sha256.Sum256(body)
	return &cp.ReadManagedKubernetesStreamOutputResponse{InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", Generation: 7, AfterSequence: after, OutputWatermark: after + 1, Status: cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_FRAME, AuthorizedUntil: timestamppb.New(time.Now().Add(4 * time.Second)), Frame: &cp.ManagedKubernetesStreamOutputFrame{StageRef: "kof_synthetic", Sequence: after + 1, Channel: "STDOUT", PayloadSha256: hex.EncodeToString(sum[:]), Payload: body}}
}

// outputHTTPHandler uses the real generated route and protected client operation; authentication and PostgreSQL remain separately qualified boundaries.
// outputHTTPHandler использует реальный generated route и protected client operation; authentication и PostgreSQL проверяются отдельно.
func outputHTTPHandler(r *outputHTTPRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesOutput: cp.NewManagedKubernetesStreamOutputServiceClient(r)}})
}

// outputDeadlineRecorder provides explicit deadline capability for synthetic projection tests; native transport enforcement is tested separately over TLS.
// outputDeadlineRecorder даёт явную поддержку deadline синтетическим проверкам projection; native transport проверяется отдельно через TLS.
type outputDeadlineRecorder struct{ *httptest.ResponseRecorder }

// SetWriteDeadline accepts the synthetic recorder deadline without claiming socket enforcement.
// SetWriteDeadline принимает срок синтетического recorder без заявления о контроле socket.
func (*outputDeadlineRecorder) SetWriteDeadline(time.Time) error { return nil }

// TestKubernetesStreamOutputHTTPActualRoute preserves binary bytes and integers above JavaScript's safe range, then clears the borrowed response.
// TestKubernetesStreamOutputHTTPActualRoute сохраняет binary bytes и integers выше безопасного JavaScript range, затем очищает borrowed response.
func TestKubernetesStreamOutputHTTPActualRoute(t *testing.T) {
	after := uint64(1<<53) + 7
	raw := outputHTTPFixture(after)
	r := &outputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: raw}}
	w := &outputDeadlineRecorder{httptest.NewRecorder()}
	outputHTTPHandler(r).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/output?generation=7&afterSequence="+strconv.FormatUint(after, 10), nil))
	var v generated.KubernetesStreamOutputReadback
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Frame == nil || !bytes.Equal(v.Frame.PayloadBase64, raw.Frame.Payload) || v.AfterSequence != strconv.FormatUint(after, 10) || v.Frame.Sequence != strconv.FormatUint(after+1, 10) || !bytes.Equal(r.borrowed, make([]byte, len(raw.Frame.Payload))) {
		t.Fatal("exact output route lost or retained bytes", w.Code)
	}
	request, ok := r.request.(*cp.ReadManagedKubernetesStreamOutputRequest)
	if !ok || r.method != cp.ManagedKubernetesStreamOutputService_ReadManagedKubernetesStreamOutput_FullMethodName || request.Generation != 7 || request.AfterSequence != after || request.InvocationRef != raw.InvocationRef || request.StreamRef != raw.StreamRef {
		t.Fatal("output selected another owner cursor")
	}
}

// TestKubernetesStreamOutputHTTPRejectsAmbiguousCursor rejects duplicate, noncanonical and overflow coordinates before owner I/O.
// TestKubernetesStreamOutputHTTPRejectsAmbiguousCursor отклоняет duplicate, noncanonical и overflow coordinates до owner I/O.
func TestKubernetesStreamOutputHTTPRejectsAmbiguousCursor(t *testing.T) {
	for _, query := range []string{"generation=7", "generation=07&afterSequence=0", "generation=7&afterSequence=00", "generation=7&afterSequence=-1", "generation=7&afterSequence=9223372036854775808", "generation=9223372036854775808&afterSequence=0", "generation=7&afterSequence=0&afterSequence=1", "generation=7&afterSequence=0&salt=caller", "generation=7&afterSequence=0&%xx=1"} {
		t.Run(query, func(t *testing.T) {
			r := &outputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: outputHTTPFixture(0)}}
			w := httptest.NewRecorder()
			outputHTTPHandler(r).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/output?"+query, nil))
			if w.Code != 400 || r.method != "" {
				t.Fatal("invalid cursor reached owner", w.Code)
			}
		})
	}
}

// TestKubernetesStreamOutputHTTPRejectsUnsafeResponse keeps malformed/foreign/expired owner data out of the public body and clears private buffers even on rejection.
// TestKubernetesStreamOutputHTTPRejectsUnsafeResponse не выдаёт malformed/foreign/expired owner data и очищает private buffers даже при отказе.
func TestKubernetesStreamOutputHTTPRejectsUnsafeResponse(t *testing.T) {
	for _, name := range []string{"origin", "generation", "cursor", "digest", "channel", "stage", "expiry", "future", "unknown_status", "unknown_field", "unjoined_end", "owner_denied"} {
		t.Run(name, func(t *testing.T) {
			raw := outputHTTPFixture(0)
			r := &outputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: raw}}
			want := 502
			switch name {
			case "origin":
				raw.InvocationRef = "inv_foreign_invocation"
			case "generation":
				raw.Generation++
			case "cursor":
				raw.Frame.Sequence++
			case "digest":
				raw.Frame.PayloadSha256 = hex.EncodeToString(make([]byte, 32))
			case "channel":
				raw.Frame.Channel = "STDIN"
			case "stage":
				raw.Frame.StageRef = "kif_synthetic"
			case "expiry":
				raw.AuthorizedUntil = timestamppb.New(time.Now().Add(-time.Second))
			case "future":
				raw.AuthorizedUntil = timestamppb.New(time.Now().Add(6 * time.Second))
			case "unknown_status":
				raw.Status = 99
			case "unknown_field":
				raw.Frame.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
			case "unjoined_end":
				raw.Status = cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_ENDED
			case "owner_denied":
				r.failure = status.Error(codes.PermissionDenied, "denied")
				want = 403
			}
			w := httptest.NewRecorder()
			outputHTTPHandler(r).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/output?generation=7&afterSequence=0", nil))
			if w.Code != want || bytes.Contains(w.Body.Bytes(), []byte("payloadBase64")) || !bytes.Equal(r.borrowed, make([]byte, len(r.borrowed))) {
				t.Fatal("unsafe output response escaped", w.Code)
			}
		})
	}
}
