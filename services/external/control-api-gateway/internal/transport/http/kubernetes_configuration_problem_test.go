package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestKubernetesConfigurationPlanProblem requires trusted empty-metadata ErrorInfo and keeps
// the unsupported configuration workflow nonretryable without reflecting upstream text.
// TestKubernetesConfigurationPlanProblem требует trusted empty-metadata ErrorInfo и сохраняет
// unsupported configuration workflow nonretryable без отражения upstream text.
func TestKubernetesConfigurationPlanProblem(t *testing.T) {
	for _, scenario := range []string{"trusted", "foreign", "metadata", "wrong-status"} {
		t.Run(scenario, func(t *testing.T) {
			info := &errdetails.ErrorInfo{Domain: controlPlaneErrorDomain, Reason: managedkubernetes.ConfigurationPlanUnsupportedReason}
			code, want := codes.FailedPrecondition, managedkubernetes.ConfigurationPlanUnsupportedReason
			switch scenario {
			case "foreign":
				info.Domain = "foreign.invalid"
				want = "STATE_CONFLICT"
			case "metadata":
				info.Metadata = map[string]string{"private": "private-input-canary"}
				want = "STATE_CONFLICT"
			case "wrong-status":
				code = codes.InvalidArgument
				want = "INVALID_REQUEST"
			}
			upstream, err := status.New(code, "private-input-canary").WithDetails(info)
			if err != nil {
				t.Fatal("construct typed configuration failure")
			}
			recorder := httptest.NewRecorder()
			writeRPCProblem(recorder, upstream.Err())
			var result struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &result) != nil || result.Code != want || result.Retryable || strings.Contains(recorder.Body.String(), "private-input-canary") {
				t.Fatal("configuration error trust/echo boundary failed")
			}
		})
	}
}
