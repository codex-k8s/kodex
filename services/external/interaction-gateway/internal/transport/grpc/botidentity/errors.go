package botidentity

import (
	"context"
	"errors"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invalidRequest(ctx context.Context) error {
	return typedError(ctx, codes.InvalidArgument, interactiongatewayv1.ErrorReason_ERROR_REASON_INVALID_REQUEST,
		"Agent Mattermost bot identity request is invalid", "INVALID_REQUEST", false)
}

func permissionError(ctx context.Context, message string) error {
	return typedError(ctx, codes.PermissionDenied, interactiongatewayv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED,
		message, "PERMISSION_DENIED", false)
}

func transportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, domainerrs.ErrUnauthorized):
		return permissionError(ctx, "Agent Mattermost bot identity operation is not allowed")
	case errors.Is(err, domainerrs.ErrNotFound):
		return typedError(ctx, codes.NotFound, interactiongatewayv1.ErrorReason_ERROR_REASON_NOT_FOUND,
			"Agent Mattermost bot identity resource is not found", "NOT_FOUND", false)
	case errors.Is(err, domainerrs.ErrIdempotencyConflict):
		return typedError(ctx, codes.AlreadyExists, interactiongatewayv1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT,
			"Agent Mattermost bot identity idempotency key conflicts", "IDEMPOTENCY_CONFLICT", false)
	case errors.Is(err, domainerrs.ErrVersionMismatch):
		return typedError(ctx, codes.Aborted, interactiongatewayv1.ErrorReason_ERROR_REASON_VERSION_MISMATCH,
			"Agent Mattermost bot identity version is stale", "VERSION_MISMATCH", true)
	case errors.Is(err, domainerrs.ErrProviderConflict):
		return typedError(ctx, codes.FailedPrecondition, interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT,
			"Mattermost bot identity conflicts with provider state", "MATTERMOST_BOT_CONFLICT", false)
	case errors.Is(err, domainerrs.ErrProviderDeleted):
		return typedError(ctx, codes.FailedPrecondition, interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT,
			"Mattermost bot identity is not available", "MATTERMOST_BOT_DELETED", false)
	case errors.Is(err, domainerrs.ErrRepairRequired):
		return typedError(ctx, codes.FailedPrecondition, interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT,
			"Agent Mattermost bot identity requires repair", "MATTERMOST_BOT_REPAIR_REQUIRED", false)
	case errors.Is(err, domainerrs.ErrConflict):
		return typedError(ctx, codes.FailedPrecondition, interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT,
			"Agent Mattermost bot identity conflicts with current state", "STATE_CONFLICT", false)
	case errors.Is(err, domainerrs.ErrBusy):
		return typedError(ctx, codes.Unavailable, interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE,
			"Agent Mattermost bot identity operation is already processing", "MATTERMOST_BOT_OPERATION_BUSY", true)
	case errors.Is(err, domainerrs.ErrAmbiguousEffect):
		return typedError(ctx, codes.Unavailable, interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE,
			"Mattermost bot identity effect requires readback", "MATTERMOST_BOT_EFFECT_AMBIGUOUS", true)
	default:
		return typedError(ctx, codes.Unavailable, interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE,
			"Agent Mattermost bot identity dependency is unavailable", "UNAVAILABLE", true)
	}
}

func typedError(ctx context.Context, code codes.Code, reason interactiongatewayv1.ErrorReason,
	message, safeCode string, retryable bool,
) error {
	current := status.New(code, message)
	withDetail, err := current.WithDetails(&interactiongatewayv1.ErrorDetail{
		Reason: reason, Code: safeCode, CorrelationId: correlationID(ctx), Retryable: retryable,
	})
	if err != nil {
		return current.Err()
	}
	return withDetail.Err()
}

func correlationID(ctx context.Context) string {
	if verified, ok := authorityclient.VerifiedAuthorizationContext(ctx); ok && uuid.Validate(verified.GetJti()) == nil {
		return verified.GetJti()
	}
	return uuid.NewString()
}
