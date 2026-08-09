package botidentity

import (
	"context"
	"testing"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTypedErrorsUseClosedSafeDetails(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err       error
		grpcCode  codes.Code
		reason    interactiongatewayv1.ErrorReason
		safeCode  string
		retryable bool
	}{
		"idempotency": {domainerrs.ErrIdempotencyConflict, codes.AlreadyExists,
			interactiongatewayv1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT, "IDEMPOTENCY_CONFLICT", false},
		"version": {domainerrs.ErrVersionMismatch, codes.Aborted,
			interactiongatewayv1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, "VERSION_MISMATCH", true},
		"ambiguous": {domainerrs.ErrAmbiguousEffect, codes.Unavailable,
			interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE, "MATTERMOST_BOT_EFFECT_AMBIGUOUS", true},
		"repair": {domainerrs.ErrRepairRequired, codes.FailedPrecondition,
			interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT, "MATTERMOST_BOT_REPAIR_REQUIRED", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := status.Convert(transportError(context.Background(), test.err))
			if got.Code() != test.grpcCode || len(got.Details()) != 1 {
				t.Fatalf("typed status mismatch: %s %#v", got.Code(), got.Details())
			}
			detail, ok := got.Details()[0].(*interactiongatewayv1.ErrorDetail)
			if !ok || detail.GetReason() != test.reason || detail.GetCode() != test.safeCode ||
				detail.GetRetryable() != test.retryable || uuid.Validate(detail.GetCorrelationId()) != nil {
				t.Fatalf("typed detail mismatch: %#v", detail)
			}
		})
	}
}
