package httptransport

import (
	"net/http"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRPCErrorMatrixRejectsMismatch(t *testing.T) {
	current := status.New(codes.Aborted, "safe")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	problem, expected := mapRPCError(withDetail.Err(), true)
	if !expected || problem.Status != http.StatusPreconditionFailed || problem.Code != "VERSION_MISMATCH" || !problem.Retryable {
		t.Fatalf("mapped problem = %#v, expected=%v", problem, expected)
	}
	mismatch := status.New(codes.InvalidArgument, "safe")
	mismatchDetail, err := mismatch.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("mismatch detail: %v", err)
	}
	problem, expected = mapRPCError(mismatchDetail.Err(), true)
	if expected || problem.Status != http.StatusInternalServerError || problem.Code != "INTERNAL" {
		t.Fatalf("mismatched detail escaped: %#v, expected=%v", problem, expected)
	}
}

func TestRPCErrorMatrixRejectsMutationConflictForRead(t *testing.T) {
	current := status.New(codes.Aborted, "safe")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	problem, expected := mapRPCError(withDetail.Err(), false)
	if expected || problem.Status != http.StatusInternalServerError || problem.Code != "INTERNAL" {
		t.Fatalf("mutation-only detail escaped read profile: %#v, expected=%v", problem, expected)
	}
}

func TestETagParsingIsExact(t *testing.T) {
	for _, invalid := range []string{"1", "W/\"1\"", "\"0\"", "\"01x\"", "*"} {
		if _, ok := parseETag(invalid); ok {
			t.Fatalf("invalid ETag accepted: %q", invalid)
		}
	}
	if value, ok := parseETag("\"42\""); !ok || value != 42 {
		t.Fatalf("valid ETag rejected: %d, %v", value, ok)
	}
}
