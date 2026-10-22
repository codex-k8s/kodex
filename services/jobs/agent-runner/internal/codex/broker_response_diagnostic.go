package codex

import "errors"

// brokerResponseFailureCategory names a diagnostic string, not a validated enum by itself; the public classifier applies the closed allowlist before a category reaches logs.
// brokerResponseFailureCategory именует diagnostic string, не являясь проверенным enum сама по себе; public classifier применяет закрытый allowlist до передачи категории в логи.
type brokerResponseFailureCategory string

const (
	// brokerResponseUnknown is the safe fallback for absent, nil, untyped or unrecognized response failure categories without exposing their original cause.
	// brokerResponseUnknown — безопасный fallback отсутствующей, nil, untyped или неизвестной response failure category без раскрытия исходной cause.
	brokerResponseUnknown brokerResponseFailureCategory = "UNKNOWN"
	// brokerResponseCallbacks identifies invalid callback arity at response-reader setup before any broker frame can be consumed.
	// brokerResponseCallbacks обозначает неверную callback arity при подготовке response reader до чтения broker frames.
	brokerResponseCallbacks brokerResponseFailureCategory = "CALLBACKS"
	// brokerResponsePostTerminal records any frame encountered after the terminal envelope, preserving the one-terminal stream boundary.
	// brokerResponsePostTerminal фиксирует frame после terminal envelope, сохраняя границу одного terminal в потоке.
	brokerResponsePostTerminal brokerResponseFailureCategory = "POST_TERMINAL"
	// brokerResponseFrameLimit records exhaustion of the closed frame-count budget rather than a raw stream or payload diagnostic.
	// brokerResponseFrameLimit фиксирует исчерпание закрытого frame-count budget вместо сырой stream- или payload-диагностики.
	brokerResponseFrameLimit brokerResponseFailureCategory = "FRAME_LIMIT"
	// brokerResponseFrameSchema identifies duplicate/unknown fields or malformed frame structure before semantic use of the decoded frame.
	// brokerResponseFrameSchema обозначает duplicate/unknown fields или malformed frame structure до семантического использования декодированного frame.
	brokerResponseFrameSchema brokerResponseFailureCategory = "FRAME_SCHEMA"
	// brokerResponseFrameVersion identifies a broker frame whose protocol version differs from the single accepted version.
	// brokerResponseFrameVersion обозначает broker frame с protocol version вне единственной принимаемой версии.
	brokerResponseFrameVersion brokerResponseFailureCategory = "FRAME_VERSION"
	// brokerResponseFrameSequence identifies a frame that does not advance the expected sequence by exactly one, without returning its private contents.
	// brokerResponseFrameSequence обозначает frame без увеличения ожидаемой sequence ровно на один без возврата приватного содержимого.
	brokerResponseFrameSequence brokerResponseFailureCategory = "FRAME_SEQUENCE"
	// brokerResponseFrameKind records an unrecognized frame kind after structural/version/sequence checks, not a free-form kind label.
	// brokerResponseFrameKind фиксирует неизвестный frame kind после structural/version/sequence checks, не возвращая произвольный kind label.
	brokerResponseFrameKind brokerResponseFailureCategory = "FRAME_KIND"
	// brokerResponseActivitySchema identifies an invalid or mutually conflicting activity/terminal payload in an ACTIVITY frame.
	// brokerResponseActivitySchema обозначает неверный или взаимно конфликтующий activity/terminal payload в ACTIVITY frame.
	brokerResponseActivitySchema brokerResponseFailureCategory = "ACTIVITY_SCHEMA"
	// brokerResponseProviderTuple records a provider-process observation whose peer/input/ordering/cardinality binding is not valid for this stream.
	// brokerResponseProviderTuple фиксирует provider-process observation с неверной peer/input/ordering/cardinality binding данного потока.
	brokerResponseProviderTuple brokerResponseFailureCategory = "PROVIDER_PROCESS_TUPLE"
	// brokerResponseMessageReplay identifies a repeated published message item identifier within one broker stream.
	// brokerResponseMessageReplay обозначает повторный published message item identifier внутри одного broker stream.
	brokerResponseMessageReplay brokerResponseFailureCategory = "MESSAGE_REPLAY"
	// brokerResponseMessageLimit records exhaustion of the bounded published-message count, keeping item identifiers out of the category.
	// brokerResponseMessageLimit фиксирует исчерпание ограниченного published-message count без включения item identifiers в категорию.
	brokerResponseMessageLimit brokerResponseFailureCategory = "MESSAGE_LIMIT"
	// brokerResponseToolLifecycle identifies a tool-call kind or revision transition that contradicts the previously observed lifecycle for that call.
	// brokerResponseToolLifecycle обозначает tool-call kind или revision transition, противоречащие ранее наблюдавшемуся lifecycle данного вызова.
	brokerResponseToolLifecycle brokerResponseFailureCategory = "TOOL_LIFECYCLE"
	// brokerResponseToolLimit records an attempt to add another tool call after the stream's native-tool-count budget is exhausted.
	// brokerResponseToolLimit фиксирует добавление очередного tool call после исчерпания native-tool-count budget потока.
	brokerResponseToolLimit brokerResponseFailureCategory = "TOOL_LIMIT"
	// brokerResponseTerminalSchema identifies malformed or conflicting terminal/result fields before outcome, diagnostic or archive provenance is accepted.
	// brokerResponseTerminalSchema обозначает malformed или conflicting terminal/result fields до принятия outcome, diagnostic или archive provenance.
	brokerResponseTerminalSchema brokerResponseFailureCategory = "TERMINAL_SCHEMA"
	// brokerResponseDiagnostic identifies a diagnostic tuple with wrong peer/input pins, class or terminal-outcome binding; its source text is not included.
	// brokerResponseDiagnostic обозначает diagnostic tuple с неверными peer/input pins, class или terminal-outcome binding без включения исходного текста.
	brokerResponseDiagnostic brokerResponseFailureCategory = "DIAGNOSTIC_TUPLE"
	// brokerResponseArchive identifies missing, malformed, foreign or unverifiable rollout capture proof, including archive metadata without a proof envelope.
	// brokerResponseArchive обозначает missing, malformed, foreign или unverifiable rollout capture proof, включая archive metadata без proof envelope.
	brokerResponseArchive brokerResponseFailureCategory = "ARCHIVE_PROOF"
	// brokerResponseStreamRead records a scanner/I/O or bounded-stream read failure without returning the raw frame or reader error.
	// brokerResponseStreamRead фиксирует scanner/I/O либо bounded-stream read failure без возврата сырого frame или reader error.
	brokerResponseStreamRead brokerResponseFailureCategory = "STREAM_READ"
	// brokerResponseNoTerminal records a stream that ends without a terminal envelope even if earlier activity frames were valid.
	// brokerResponseNoTerminal фиксирует поток, завершившийся без terminal envelope, даже при корректных прежних activity frames.
	brokerResponseNoTerminal brokerResponseFailureCategory = "TERMINAL_ABSENT"
	// brokerResponseTerminalResult identifies failure from terminal-result validation or projection after the single terminal frame has been read.
	// brokerResponseTerminalResult обозначает отказ terminal-result validation или projection после чтения единственного terminal frame.
	brokerResponseTerminalResult brokerResponseFailureCategory = "TERMINAL_RESULT"
	// brokerResponseActivityFailed records unsuccessful activity delivery while preserving the response reader's failed-result projection rather than claiming the activity was delivered.
	// brokerResponseActivityFailed фиксирует неуспешную activity delivery с сохранением failed-result projection response reader вместо заявления успешной доставки.
	brokerResponseActivityFailed brokerResponseFailureCategory = "ACTIVITY_DELIVERY"
	// brokerResponseFailureMessage is the fixed Error string for every typed response failure, deliberately excluding category inputs and wrapped cause text.
	// brokerResponseFailureMessage — фиксированная строка Error всех typed response failures, намеренно исключающая category inputs и wrapped cause text.
	brokerResponseFailureMessage = "Codex provider broker response failed"
)

// brokerResponseFailure privately stores category and original cause so sentinel matching survives, while its Error and default JSON surface do not serialize that cause. Consumers must use the closed classifier for logging rather than unwrapping private errors.
// brokerResponseFailure приватно хранит category и исходную cause для сохранения sentinel matching, но Error и default JSON surface не сериализуют cause. Для логов следует использовать closed classifier, не разворачивать private errors.
type brokerResponseFailure struct {
	category brokerResponseFailureCategory
	cause    error
}

// Error returns only the fixed broker-response failure message and never formats the wrapped cause or category payload.
// Error возвращает лишь фиксированное broker-response failure message без форматирования wrapped cause и category payload.
func (failure *brokerResponseFailure) Error() string { return brokerResponseFailureMessage }

// Unwrap exposes the retained cause to errors.Is/errors.As so existing functional failure classification is preserved; the returned error is not a safe diagnostic string.
// Unwrap предоставляет сохранённую cause errors.Is/errors.As для прежней functional failure classification; возвращённая ошибка не безопасная diagnostic string.
func (failure *brokerResponseFailure) Unwrap() error { return failure.cause }

// brokerResponseError attaches a category to a cause without changing that cause or validating arbitrary category strings; output normalization remains the public classifier's responsibility.
// brokerResponseError прикрепляет category к cause без изменения cause и без проверки произвольных category strings; output normalization остаётся обязанностью public classifier.
func brokerResponseError(category brokerResponseFailureCategory, cause error) error {
	return &brokerResponseFailure{category: category, cause: cause}
}

// BrokerResponseFailureCategory finds the first typed response wrapper through error chains, safely handles typed nil and returns only its recognized category or UNKNOWN. It never guesses from err.Error or returns provider/frame/cause payloads.
// BrokerResponseFailureCategory находит первый typed response wrapper в error chains, безопасно обрабатывает typed nil и возвращает лишь известную category либо UNKNOWN. Не угадывает по err.Error и не возвращает provider/frame/cause payloads.
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
