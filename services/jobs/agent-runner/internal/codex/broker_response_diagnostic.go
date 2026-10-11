package codex

import "errors"

type brokerResponseFailureCategory string

const (
	brokerResponseUnknown        brokerResponseFailureCategory = "UNKNOWN"
	brokerResponseCallbacks      brokerResponseFailureCategory = "CALLBACKS"
	brokerResponsePostTerminal   brokerResponseFailureCategory = "POST_TERMINAL"
	brokerResponseFrameLimit     brokerResponseFailureCategory = "FRAME_LIMIT"
	brokerResponseFrameSchema    brokerResponseFailureCategory = "FRAME_SCHEMA"
	brokerResponseFrameVersion   brokerResponseFailureCategory = "FRAME_VERSION"
	brokerResponseFrameSequence  brokerResponseFailureCategory = "FRAME_SEQUENCE"
	brokerResponseFrameKind      brokerResponseFailureCategory = "FRAME_KIND"
	brokerResponseActivitySchema brokerResponseFailureCategory = "ACTIVITY_SCHEMA"
	brokerResponseProviderTuple  brokerResponseFailureCategory = "PROVIDER_PROCESS_TUPLE"
	brokerResponseMessageReplay  brokerResponseFailureCategory = "MESSAGE_REPLAY"
	brokerResponseMessageLimit   brokerResponseFailureCategory = "MESSAGE_LIMIT"
	brokerResponseToolLifecycle  brokerResponseFailureCategory = "TOOL_LIFECYCLE"
	brokerResponseToolLimit      brokerResponseFailureCategory = "TOOL_LIMIT"
	brokerResponseTerminalSchema brokerResponseFailureCategory = "TERMINAL_SCHEMA"
	brokerResponseDiagnostic     brokerResponseFailureCategory = "DIAGNOSTIC_TUPLE"
	brokerResponseArchive        brokerResponseFailureCategory = "ARCHIVE_PROOF"
	brokerResponseStreamRead     brokerResponseFailureCategory = "STREAM_READ"
	brokerResponseNoTerminal     brokerResponseFailureCategory = "TERMINAL_ABSENT"
	brokerResponseTerminalResult brokerResponseFailureCategory = "TERMINAL_RESULT"
	brokerResponseActivityFailed brokerResponseFailureCategory = "ACTIVITY_DELIVERY"
	brokerResponseFailureMessage                               = "Codex provider broker response failed"
)

// Диагностика не хранит frame/input и не сериализует исходную цепочку ошибок.
// Unwrap сохраняет прежние sentinel и функциональное отображение отказа.
type brokerResponseFailure struct {
	category brokerResponseFailureCategory
	cause    error
}

func (failure *brokerResponseFailure) Error() string { return brokerResponseFailureMessage }
func (failure *brokerResponseFailure) Unwrap() error { return failure.cause }

func brokerResponseError(category brokerResponseFailureCategory, cause error) error {
	return &brokerResponseFailure{category: category, cause: cause}
}

// BrokerResponseFailureCategory возвращает только закрытую категорию, без
// provider/frame/error payload. Неизвестная причина не угадывается по тексту.
func BrokerResponseFailureCategory(err error) string {
	var failure *brokerResponseFailure
	if !errors.As(err, &failure) || failure == nil {
		return string(brokerResponseUnknown)
	}
	switch failure.category {
	case brokerResponseCallbacks, brokerResponsePostTerminal, brokerResponseFrameLimit,
		brokerResponseFrameSchema, brokerResponseFrameVersion, brokerResponseFrameSequence,
		brokerResponseFrameKind, brokerResponseActivitySchema, brokerResponseProviderTuple,
		brokerResponseMessageReplay, brokerResponseMessageLimit, brokerResponseToolLifecycle,
		brokerResponseToolLimit, brokerResponseTerminalSchema, brokerResponseDiagnostic,
		brokerResponseArchive, brokerResponseStreamRead, brokerResponseNoTerminal,
		brokerResponseTerminalResult, brokerResponseActivityFailed:
		return string(failure.category)
	default:
		return string(brokerResponseUnknown)
	}
}
