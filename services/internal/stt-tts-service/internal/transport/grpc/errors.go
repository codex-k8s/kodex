package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// errorDomain pins structured STT error details to the shared canonical domain rather than arbitrary provider diagnostics.
// RU: errorDomain закрепляет структурированные ошибки STT за общим каноническим доменом, а не произвольной диагностикой провайдера.
const errorDomain = errorprofile.Domain

// transportError preserves cancellation, maps known domain failures to safe gRPC statuses and exposes only a validated whole-second rate-limit hint.
// RU: transportError сохраняет отмену, отображает известные доменные отказы в безопасные gRPC статусы и выдаёт только проверенную целосекундную подсказку ограничения частоты.
// Unknown or joined provider errors never expose raw payloads through the returned status text.
// RU: Неизвестные или объединённые ошибки провайдера не раскрывают сырые payload через возвращаемый текст статуса.
func transportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return statusError(codes.Canceled, "transcription request was canceled", "INVALID_REQUEST")
	case errors.Is(err, context.DeadlineExceeded):
		return statusError(codes.DeadlineExceeded, "transcription deadline was exceeded", "UNAVAILABLE")
	case errors.Is(err, errs.ErrProviderRateLimited):
		base := status.New(codes.ResourceExhausted, errs.ErrProviderRateLimited.Error())
		withDetails, detailErr := base.WithDetails(&errdetails.ErrorInfo{Reason: errorprofile.TranscriptionRateLimited, Domain: errorDomain})
		if detailErr != nil {
			return base.Err()
		}
		var limited *errs.ProviderRateLimit
		if errors.As(err, &limited) && limited != nil && limited.RetryAfter >= time.Second && limited.RetryAfter <= errorprofile.MaximumRetryAfter && limited.RetryAfter%time.Second == 0 {
			if hinted, hintErr := withDetails.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(limited.RetryAfter)}); hintErr == nil {
				withDetails = hinted
			}
		}
		return withDetails.Err()
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

// statusError adds closed ErrorInfo reason/domain details to a safe status and falls back to that base status if detail serialization fails.
// RU: statusError добавляет закрытые ErrorInfo reason/domain к безопасному статусу и возвращает базовый статус при отказе сериализации деталей.
func statusError(code codes.Code, message, reason string) error {
	base := status.New(code, message)
	withDetails, err := base.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain})
	if err != nil {
		return base.Err()
	}
	return withDetails.Err()
}
