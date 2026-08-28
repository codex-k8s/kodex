package authoritygrpc

import (
	"context"
	"fmt"
	"testing"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAuthoritySourcePreservesSupportedPolicySources(t *testing.T) {
	t.Parallel()

	tests := map[string]internalrpcauthorityv1.AuthoritySource{
		"WORKLOAD_READINESS": internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_WORKLOAD_READINESS,
		"PROVIDER_READBACK":  internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_PROVIDER_READBACK,
		"GIT_RECONCILIATION": internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_GIT_RECONCILIATION,
		"RUNTIME_EXECUTION":  internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_RUNTIME_EXECUTION,
	}
	for source, expected := range tests {
		source, expected := source, expected
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if actual := authoritySource(source); actual != expected {
				t.Fatalf("authority source mismatch: got %s, want %s", actual, expected)
			}
		})
	}
}

func TestAuthoritySourceRejectsUnknownValue(t *testing.T) {
	t.Parallel()
	if actual := authoritySource("UNKNOWN"); actual != internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_UNSPECIFIED {
		t.Fatalf("unknown authority source must remain unspecified: %s", actual)
	}
}

func TestMapErrorPreservesRequestContextTermination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cause        error
		expectedCode codes.Code
		expectedText string
	}{
		{
			name:         "direct cancellation",
			cause:        context.Canceled,
			expectedCode: codes.Canceled,
			expectedText: context.Canceled.Error(),
		},
		{
			name: "persistence wrapped cancellation",
			cause: failure.Wrap(
				failure.PersistenceUnavailable,
				"authorization context replay store unavailable",
				fmt.Errorf("accept verified context: %w", context.Canceled),
			),
			expectedCode: codes.Canceled,
			expectedText: context.Canceled.Error(),
		},
		{
			name: "persistence wrapped deadline",
			cause: failure.Wrap(
				failure.PersistenceUnavailable,
				"authority proof replay store unavailable",
				fmt.Errorf("reserve replay identifier: %w", context.DeadlineExceeded),
			),
			expectedCode: codes.DeadlineExceeded,
			expectedText: context.DeadlineExceeded.Error(),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mapped := status.Convert(mapError(test.cause, "correlation-id"))
			if mapped.Code() != test.expectedCode {
				t.Fatalf("context termination code mismatch: got %s, want %s", mapped.Code(), test.expectedCode)
			}
			if mapped.Message() != test.expectedText {
				t.Fatalf("context termination message mismatch: got %q, want %q", mapped.Message(), test.expectedText)
			}
			if len(mapped.Details()) != 0 {
				t.Fatal("context termination must not be reported as an authorization failure")
			}
		})
	}
}

func TestMapErrorKeepsActualPersistenceFailureUnavailable(t *testing.T) {
	t.Parallel()

	mapped := status.Convert(mapError(failure.Wrap(
		failure.PersistenceUnavailable,
		"authorization context replay store unavailable",
		fmt.Errorf("accept verified context: connection reset"),
	), "correlation-id"))
	if mapped.Code() != codes.Unavailable {
		t.Fatalf("persistence failure code mismatch: got %s, want %s", mapped.Code(), codes.Unavailable)
	}
	if mapped.Message() != errorSpecPersistence.message {
		t.Fatalf("persistence failure message mismatch: got %q, want %q", mapped.Message(), errorSpecPersistence.message)
	}
	details := mapped.Details()
	if len(details) != 1 {
		t.Fatalf("persistence failure detail count mismatch: got %d, want 1", len(details))
	}
	detail, ok := details[0].(*internalrpcauthorityv1.AuthorizationErrorDetail)
	if !ok {
		t.Fatalf("persistence failure detail type mismatch: %T", details[0])
	}
	if detail.GetReason() != internalrpcauthorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERSISTENCE_UNAVAILABLE ||
		!detail.GetRetryable() {
		t.Fatalf("persistence failure detail mismatch: reason=%s retryable=%t", detail.GetReason(), detail.GetRetryable())
	}
}
