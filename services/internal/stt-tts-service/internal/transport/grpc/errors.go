package grpc

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const errorDomain = "kodex.stt"

func transportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return statusError(codes.Canceled, "transcription request was canceled", "INVALID_REQUEST")
	case errors.Is(err, context.DeadlineExceeded):
		return statusError(codes.DeadlineExceeded, "transcription deadline was exceeded", "UNAVAILABLE")
	case errors.Is(err, errs.ErrAudioTooLarge):
		return statusError(codes.ResourceExhausted, "audio payload exceeds the configured limit", "INVALID_REQUEST")
	case errors.Is(err, errs.ErrAudioTooLong), errors.Is(err, errs.ErrUnsupportedAudio), errors.Is(err, errs.ErrInvalidRequest):
		return statusError(codes.InvalidArgument, "transcription request is invalid", "INVALID_REQUEST")
	case errors.Is(err, errs.ErrPermissionDenied):
		return statusError(codes.PermissionDenied, "transcription is not permitted", "PERMISSION_DENIED")
	case errors.Is(err, errs.ErrGrantRevoked), errors.Is(err, errs.ErrProviderRejected), errors.Is(err, errs.ErrDelegatedProofPending):
		return statusError(codes.FailedPrecondition, "transcription prerequisites changed", "STATE_CONFLICT")
	case errors.Is(err, errs.ErrPolicyUnavailable), errors.Is(err, errs.ErrCredentialUnavailable), errors.Is(err, errs.ErrProviderUnavailable), errors.Is(err, errs.ErrEgressUnavailable):
		return statusError(codes.Unavailable, "transcription is temporarily unavailable", "UNAVAILABLE")
	default:
		return statusError(codes.Internal, "transcription failed", "INTERNAL")
	}
}

func statusError(code codes.Code, message, reason string) error {
	base := status.New(code, message)
	withDetails, err := base.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain})
	if err != nil {
		return base.Err()
	}
	return withDetails.Err()
}
