package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWriteRPCProblemMapsOnlyTrustedFreshAuthenticationReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "fresh authentication",
			err:        rpcStatusWithErrorInfo(t, codes.PermissionDenied, controlPlaneErrorDomain, freshAuthenticationRequiredReason),
			wantStatus: http.StatusForbidden,
			wantCode:   freshAuthenticationRequiredReason,
		},
		{
			name:       "ordinary permission denial",
			err:        status.Error(codes.PermissionDenied, "operation is not permitted"),
			wantStatus: http.StatusForbidden,
			wantCode:   "PERMISSION_DENIED",
		},
		{
			name:       "untrusted reason domain",
			err:        rpcStatusWithErrorInfo(t, codes.PermissionDenied, "untrusted.example", freshAuthenticationRequiredReason),
			wantStatus: http.StatusForbidden,
			wantCode:   "PERMISSION_DENIED",
		},
		{
			name:       "ordinary authentication with fresh reason",
			err:        rpcStatusWithErrorInfo(t, codes.Unauthenticated, controlPlaneErrorDomain, freshAuthenticationRequiredReason),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHENTICATED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			writeRPCProblem(recorder, test.err)
			if recorder.Code != test.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var body struct {
				Status int    `json:"status"`
				Code   string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if body.Status != test.wantStatus || body.Code != test.wantCode {
				t.Fatalf("problem = status %d code %q, want status %d code %q", body.Status, body.Code, test.wantStatus, test.wantCode)
			}
		})
	}
}

func rpcStatusWithErrorInfo(t *testing.T, code codes.Code, domain, reason string) error {
	t.Helper()
	value, err := status.New(code, "rpc failure").WithDetails(&errdetails.ErrorInfo{Domain: domain, Reason: reason})
	if err != nil {
		t.Fatalf("attach ErrorInfo: %v", err)
	}
	return value.Err()
}
