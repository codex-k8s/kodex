package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
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

func TestWriteRPCProblemDistinguishesLocalAuthorityTransientFromAuthRejection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantStatus    int
		wantCode      string
		wantRetryable bool
	}{
		{
			name: "local authority canceled", err: localAuthorityFailure(t, codes.Canceled),
			wantStatus: http.StatusServiceUnavailable, wantCode: "UNAVAILABLE", wantRetryable: true,
		},
		{
			name: "local authority rejected", err: localAuthorityFailure(t, codes.PermissionDenied),
			wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHENTICATED", wantRetryable: false,
		},
		{
			name: "untrusted bare canceled", err: status.Error(codes.Canceled, "request canceled"),
			wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL", wantRetryable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			writeRPCProblem(recorder, test.err)
			var body struct {
				Status    int    `json:"status"`
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if recorder.Code != test.wantStatus || body.Status != test.wantStatus || body.Code != test.wantCode || body.Retryable != test.wantRetryable {
				t.Fatalf("problem = HTTP %d body=%+v, want status=%d code=%q retryable=%t", recorder.Code, body, test.wantStatus, test.wantCode, test.wantRetryable)
			}
		})
	}
}

func TestWriteRPCProblemAlwaysReturnsLocalizedStrictProblem(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		code       codes.Code
		wantStatus int
		wantCode   string
	}{
		{code: codes.NotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{code: codes.Internal, wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL"},
		{code: codes.Unavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "UNAVAILABLE"},
	} {
		recorder := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
		writeRPCProblem(recorder, status.Error(test.code, "raw upstream details must not leak"))
		var body struct {
			Type          string `json:"type"`
			Title         string `json:"title"`
			Code          string `json:"code"`
			CorrelationID string `json:"correlationId"`
			Status        int    `json:"status"`
			Retryable     bool   `json:"retryable"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode strict problem: %v", err)
		}
		if recorder.Code != test.wantStatus || body.Status != test.wantStatus || body.Code != test.wantCode || body.Title != "localized:"+test.wantCode || body.Type == "" || body.CorrelationID == "" || strings.Contains(recorder.Body.String(), "raw upstream") {
			t.Fatalf("strict problem = status %d body %s", recorder.Code, recorder.Body.String())
		}
	}
}

type testOperationResolver map[string]string

func (resolver testOperationResolver) OperationID(method string) (string, bool) {
	operation, ok := resolver[method]
	return operation, ok
}

type testProofProvider struct{ err error }

func (provider testProofProvider) AuthorityProof(context.Context, string, string) (string, string, error) {
	return "", "correlation", provider.err
}

func localAuthorityFailure(t *testing.T, code codes.Code) error {
	t.Helper()
	const method = "/example.v1.Service/Method"
	interceptor := authorityclient.IssuerUnaryClientInterceptor(nil, testOperationResolver{method: "example.read"}, testProofProvider{
		err: status.Error(code, "authority failure"),
	})
	return interceptor(context.Background(), method, &emptypb.Empty{}, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			t.Fatal("downstream RPC was opened after failed authority proof")
			return nil
		},
	)
}

func rpcStatusWithErrorInfo(t *testing.T, code codes.Code, domain, reason string) error {
	t.Helper()
	value, err := status.New(code, "rpc failure").WithDetails(&errdetails.ErrorInfo{Domain: domain, Reason: reason})
	if err != nil {
		t.Fatalf("attach ErrorInfo: %v", err)
	}
	return value.Err()
}
