package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestLegacyReworkProblemTrustBoundary accepts only empty-metadata control-plane reasons with FailedPrecondition and never enables automatic retries or exposes upstream details.
// TestLegacyReworkProblemTrustBoundary принимает только control-plane reason без metadata при FailedPrecondition, не включая autoretry и не выдавая upstream details.
func TestLegacyReworkProblemTrustBoundary(t *testing.T) {
	for _, reason := range []string{"LEGACY_REWORK_SCOPE_UNSUPPORTED", "LEGACY_REWORK_WRITE_REQUIRES_RECONCILIATION", "LEGACY_REWORK_PARENT_BUSY", "LEGACY_REWORK_DELIVERY_BLOCKED", "LEGACY_REWORK_READ_LIMIT"} {
		for _, variant := range []string{"trusted", "foreign", "metadata", "unknown", "wrong-status"} {
			t.Run(reason+"/"+variant, func(t *testing.T) {
				info := &errdetails.ErrorInfo{Domain: controlPlaneErrorDomain, Reason: reason}
				code, want := codes.FailedPrecondition, reason
				switch variant {
				case "foreign":
					info.Domain, want = "foreign.invalid", "STATE_CONFLICT"
				case "metadata":
					info.Metadata, want = map[string]string{"private": "private-input-canary"}, "STATE_CONFLICT"
				case "unknown":
					info.Reason, want = "LEGACY_REWORK_UNKNOWN", "STATE_CONFLICT"
				case "wrong-status":
					code, want = codes.Aborted, "VERSION_OR_STATE_CONFLICT"
				}
				upstream, err := status.New(code, "private-input-canary").WithDetails(info)
				if err != nil {
					t.Fatal("encode typed fixture error")
				}
				recorder := httptest.NewRecorder()
				writeRPCProblem(recorder, upstream.Err())
				var body struct {
					Code      string `json:"code"`
					Retryable bool   `json:"retryable"`
				}
				if json.Unmarshal(recorder.Body.Bytes(), &body) != nil || body.Code != want || strings.Contains(recorder.Body.String(), "private-input-canary") {
					t.Fatal("untrusted owner rework detail crossed HTTP boundary")
				}
				if variant != "wrong-status" && (body.Retryable || recorder.Code != 409) {
					t.Fatal("owner rework conflict enabled an automatic retry")
				}
			})
		}
	}
}
