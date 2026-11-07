package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestPersonalPreflightHTTPValidatesWireDetails exercises real protobuf status serialization and
// HTTP projection. Synthetic typed owner input is not evidence of a live JWT or PostgreSQL RPC.
// TestPersonalPreflightHTTPValidatesWireDetails проверяет реальную protobuf status serialization
// и HTTP projection. Synthetic typed owner input не доказывает live JWT либо PostgreSQL RPC.
func TestPersonalPreflightHTTPValidatesWireDetails(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*cp.IntegrationCredentialPreflightDetail)
		code   codes.Code
		extra  bool
		want   int
	}{
		{name: "valid", code: codes.FailedPrecondition, want: 409},
		{name: "unknown", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) { d.Issues[0].Problem = 999 }},
		{name: "duplicate", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) { d.Issues = append(d.Issues, d.Issues[0]) }},
		{name: "arbitrary endpoint", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) {
			d.Issues[0].ConnectionRef = "https://other.example.invalid"
		}},
		{name: "empty", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) { d.Issues = nil }},
		{name: "unknown detail fields", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) {
			d.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		}},
		{name: "unknown issue fields", code: codes.FailedPrecondition, want: 502, change: func(d *cp.IntegrationCredentialPreflightDetail) {
			d.Issues[0].ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		}},
		{name: "wrong status", code: codes.Aborted, want: 502},
		{name: "extra metadata", code: codes.FailedPrecondition, want: 502, extra: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			detail := &cp.IntegrationCredentialPreflightDetail{Issues: []*cp.IntegrationCredentialPreflightIssue{{ConnectionRef: "int_fixture01", CapabilityKey: "gitlab.project.metadata.read", Problem: cp.IntegrationCredentialPreflightProblem_INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_CREDENTIAL_REVOKED}, {ConnectionRef: "int_fixture02", CapabilityKey: "jira.issue.read", Problem: cp.IntegrationCredentialPreflightProblem_INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_CREDENTIAL_MISSING}}}
			if test.change != nil {
				test.change(detail)
			}
			upstream, err := status.New(test.code, "synthetic-private-provider-response").WithDetails(detail)
			if err != nil {
				t.Fatal(err)
			}
			if test.extra {
				upstream, err = upstream.WithDetails(&errdetails.ErrorInfo{Reason: "synthetic-private-provider-response"})
				if err != nil {
					t.Fatal(err)
				}
			}
			wire, err := proto.Marshal(upstream.Proto())
			if err != nil {
				t.Fatal(err)
			}
			decoded := upstream.Proto()
			proto.Reset(decoded)
			if err := proto.Unmarshal(wire, decoded); err != nil {
				t.Fatal(err)
			}
			writer := httptest.NewRecorder()
			writeRPCProblem(writer, status.FromProto(decoded).Err())
			if writer.Code != test.want || strings.Contains(writer.Body.String(), "synthetic-private-provider-response") {
				t.Fatalf("preflight HTTP status or redaction failed: status=%d", writer.Code)
			}
			var body map[string]any
			if json.Unmarshal(writer.Body.Bytes(), &body) != nil {
				t.Fatal("preflight problem is not JSON")
			}
			if test.want == 409 {
				list, ok := body["integrationCredentialIssues"].([]any)
				if !ok || len(list) != 2 || body["code"] != personalCredentialPreflightRequired || body["retryable"] != false || writer.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("preflight lost full safe list or became retryable")
				}
			} else if _, exists := body["integrationCredentialIssues"]; exists {
				t.Fatal("invalid upstream leaked a partial problem list")
			}
		})
	}
}
