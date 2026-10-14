package codex

import "errors"

// Подтипы остаются внутренними: публичный diagnostic v1 сохраняет STREAM_INVALID.
// Ни текст ошибки reader/parser, ни содержимое JSON не выводятся в диагностику.
type appServerStreamFailureReason string

const (
	streamFailureFrameTooLarge appServerStreamFailureReason = "FRAME_TOO_LARGE"
	streamFailureMessageLimit  appServerStreamFailureReason = "MESSAGE_LIMIT"
	streamFailureEmptyFrame    appServerStreamFailureReason = "EMPTY_FRAME"
	streamFailureWireInvalid   appServerStreamFailureReason = "WIRE_INVALID"
	streamFailureReadError     appServerStreamFailureReason = "READ_ERROR"
)

type appServerStreamFailure struct {
	reason appServerStreamFailureReason
}

func (failure *appServerStreamFailure) Error() string {
	return "Codex app-server response stream is invalid"
}

func safeAppServerStreamFailureReason(err error) string {
	var failure *appServerStreamFailure
	if !errors.As(err, &failure) {
		return "NONE"
	}
	switch failure.reason {
	case streamFailureFrameTooLarge, streamFailureMessageLimit, streamFailureEmptyFrame, streamFailureWireInvalid, streamFailureReadError:
		return string(failure.reason)
	default:
		return "UNKNOWN"
	}
}
