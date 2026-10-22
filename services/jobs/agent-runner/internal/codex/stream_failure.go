package codex

import "errors"

// appServerStreamFailureReason names internal stream subtypes while public diagnostic v1 remains
// STREAM_INVALID; no reader/parser text or raw JSON is carried by this type.
// appServerStreamFailureReason задаёт внутренние подтипы потока при сохранении публичного diagnostic v1
// как STREAM_INVALID; текст reader/parser и исходный JSON этот тип не переносит.
type appServerStreamFailureReason string

const (
	// streamFailureFrameTooLarge identifies an encoded frame exceeding the transport size bound.
	// streamFailureFrameTooLarge обозначает превышение транспортного лимита размера кодированного кадра.
	streamFailureFrameTooLarge appServerStreamFailureReason = "FRAME_TOO_LARGE"
	// streamFailureMessageLimit identifies too many JSONL messages independently of each frame's size.
	// streamFailureMessageLimit обозначает превышение числа JSONL-сообщений независимо от размера кадра.
	streamFailureMessageLimit appServerStreamFailureReason = "MESSAGE_LIMIT"
	// streamFailureEmptyFrame identifies an empty logical JSONL frame rather than ordinary stream EOF.
	// streamFailureEmptyFrame обозначает пустой логический JSONL-кадр, а не обычный EOF потока.
	streamFailureEmptyFrame appServerStreamFailureReason = "EMPTY_FRAME"
	// streamFailureWireInvalid identifies rejected JSON/wire structure without preserving the offending payload.
	// streamFailureWireInvalid обозначает отклонённую JSON/wire структуру без сохранения проблемной нагрузки.
	streamFailureWireInvalid appServerStreamFailureReason = "WIRE_INVALID"
	// streamFailureReadError identifies an underlying read failure without exposing its original error text.
	// streamFailureReadError обозначает ошибку исходного reader без раскрытия её первоначального текста.
	streamFailureReadError appServerStreamFailureReason = "READ_ERROR"
)

// appServerStreamFailure stores only an internal reason and exposes a fixed public Error string;
// normalization to the closed log vocabulary happens in safeAppServerStreamFailureReason.
// appServerStreamFailure хранит лишь внутреннюю причину и выдаёт фиксированный публичный Error;
// приведение к закрытому словарю логов выполняет safeAppServerStreamFailureReason.
type appServerStreamFailure struct {
	reason appServerStreamFailureReason
}

// Error returns a fixed stream-failure message without interpolating even an unknown reason.
// Error возвращает фиксированное сообщение отказа потока без подстановки даже неизвестной причины.
func (failure *appServerStreamFailure) Error() string {
	return "Codex app-server response stream is invalid"
}

// safeAppServerStreamFailureReason normalizes a wrapped typed failure to the closed log vocabulary.
// It returns NONE when absent and UNKNOWN for an unregistered reason; typed failure pointers must be nonnil.
// safeAppServerStreamFailureReason приводит вложенный типизированный отказ к закрытому словарю логов.
// Она возвращает NONE при отсутствии и UNKNOWN для неизвестной причины; указатель типизированной ошибки не должен быть nil.
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
