package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestKubernetesStagingQuotaProblemRequiresExactTrustedReason distinguishes storage capacity from
// rate limiting; it never converts upstream text or foreign metadata into a no-write guarantee.
// TestKubernetesStagingQuotaProblemRequiresExactTrustedReason отличает storage capacity от rate
// limiting; upstream text либо foreign metadata не создают no-write guarantee.
func TestKubernetesStagingQuotaProblemRequiresExactTrustedReason(t *testing.T) {
	for _, test := range []struct {
		name, domain string
		code         codes.Code
		metadata     map[string]string
		quota        bool
	}{
		{name: "owner-capacity", domain: controlPlaneErrorDomain, code: codes.ResourceExhausted, quota: true},
		{name: "foreign-domain", domain: "untrusted.invalid", code: codes.ResourceExhausted},
		{name: "wrong-status", domain: controlPlaneErrorDomain, code: codes.Unavailable},
		{name: "unknown-metadata", domain: controlPlaneErrorDomain, code: codes.ResourceExhausted, metadata: map[string]string{"untrusted": "body"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream, err := status.New(test.code, "private-upstream-text").WithDetails(&errdetails.ErrorInfo{Domain: test.domain, Reason: managedkubernetes.ReasonStagingQuotaExceeded, Metadata: test.metadata})
			if err != nil {
				t.Fatal(err)
			}
			writer := httptest.NewRecorder()
			writeRPCProblem(writer, upstream.Err())
			var problem struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &problem) != nil {
				t.Fatal("invalid problem")
			}
			if test.quota {
				if writer.Code != http.StatusInsufficientStorage || problem.Code != managedkubernetes.ReasonStagingQuotaExceeded || problem.Retryable || writer.Header().Get("Retry-After") != "" {
					t.Fatal("storage capacity was mislabeled as automatic retry")
				}
			} else if problem.Code == managedkubernetes.ReasonStagingQuotaExceeded {
				t.Fatal("foreign error minted owner quota evidence")
			}
		})
	}
}
