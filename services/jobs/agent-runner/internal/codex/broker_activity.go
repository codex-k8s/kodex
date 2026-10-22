package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

const (
	// providerBrokerVersion pins the closed frame format; readers reject every other version.
	// providerBrokerVersion закрепляет закрытый формат кадров; читатель отклоняет любую другую версию.
	providerBrokerVersion = 3
	// maximumPublishedMessages caps distinct message IDs per stream independently of tool activity.
	// maximumPublishedMessages ограничивает число разных сообщений потока отдельно от вызовов инструментов.
	maximumPublishedMessages = 1024
	// maximumBrokerFrameBytes bounds one encoded frame including its newline and reserves terminal space.
	// maximumBrokerFrameBytes ограничивает кадр вместе с переводом строки и задаёт резерв для итога.
	maximumBrokerFrameBytes = 1 << 20
	// maximumBrokerStreamBytes caps the total encoded response, not each individual activity payload.
	// maximumBrokerStreamBytes ограничивает весь кодированный ответ, а не каждое отдельное событие.
	maximumBrokerStreamBytes = 64 << 20
	// maximumBrokerFrames budgets two revisions per tool, messages, process observation and terminal.
	// maximumBrokerFrames отводит место двум ревизиям инструмента, сообщениям, наблюдению процесса и итогу.
	maximumBrokerFrames = 2*runtimecontract.MaximumNativeToolCalls + maximumPublishedMessages + 2
	// brokerFrameActivity identifies a live fact that cannot itself confirm completion of the turn.
	// brokerFrameActivity обозначает текущее событие, которое само по себе не завершает ход.
	brokerFrameActivity = "ACTIVITY"
	// brokerFrameTerminal identifies the single final envelope after which no frame is accepted.
	// brokerFrameTerminal обозначает единственный итоговый конверт, после которого кадры не принимаются.
	brokerFrameTerminal = "TERMINAL"
	// brokerFrameWriteTimeout bounds each net.Conn write; generic writers must provide their own bounds.
	// brokerFrameWriteTimeout ограничивает запись в net.Conn; обычный writer обеспечивает свои ограничения.
	brokerFrameWriteTimeout = 5 * time.Second
)

// brokerFrame carries a versioned sequence and exactly one payload as checked by the reader.
// The Go shape alone does not enforce payload exclusivity, provenance or lifecycle.
// brokerFrame переносит версию, последовательность и одну нагрузку, проверяемую читателем.
// Сама Go-структура не обеспечивает взаимоисключение нагрузок, происхождение или жизненный цикл.
type brokerFrame struct {
	Version  int                              `json:"version"`
	Sequence uint64                           `json:"sequence"`
	Kind     string                           `json:"kind"`
	Activity *runtimecontract.RuntimeActivity `json:"activity,omitempty"`
	Terminal *brokerResponse                  `json:"terminal,omitempty"`
}

// brokerFrameWriter tracks one serialized response, advancing counters only after a successful copy.
// Callers serialize access; input supplies optional binding for producer paths, not a new authority.
// brokerFrameWriter отслеживает один последовательный ответ и меняет счётчики только после успешной записи.
// Доступ упорядочивает вызывающий код; input задаёт привязку путям производителя, но не новые полномочия.
type brokerFrameWriter struct {
	writer   io.Writer
	sequence uint64
	bytes    int
	terminal bool
	input    *model.Input
}

// Write rejects unframed io.Writer use so callers cannot bypass the explicit activity/terminal methods.
// Write запрещает запись через io.Writer в обход явных методов кадров событий и итога.
func (writer *brokerFrameWriter) Write([]byte) (int, error) {
	return 0, errProviderBrokerResponseInvalid
}

// activity validates the public event shape before emitting it within the reserved stream budget.
// Cross-event ordering and trusted process identity are checked separately by the consumer.
// activity проверяет публичную структуру события перед записью с сохранением резерва потока.
// Порядок между событиями и доверенную идентичность процесса отдельно проверяет потребитель.
func (writer *brokerFrameWriter) activity(activity runtimecontract.RuntimeActivity) error {
	if activity.Validate() != nil {
		return errProviderBrokerResponseInvalid
	}
	return writer.frame(brokerFrame{Kind: brokerFrameActivity, Activity: &activity})
}

// finish omits already-streamed calls and forwards only a sealed capture matching the result.
// An oversized/invalid frame gets one bounded failure envelope preserving measurements and proof;
// transport errors and an already-written terminal are not retried here.
// finish исключает уже переданные вызовы и переносит только закрытый захват, совпадающий с результатом.
// Слишком большой или некодируемый кадр заменяется одним ограниченным отказом с измерениями и доказательством;
// транспортная ошибка и уже записанный итог здесь не повторяются.
func (writer *brokerFrameWriter) finish(response brokerResponse) error {
	// Факты tool call уже доставлены в ACTIVITY; terminal переносит только итог.
	response.Result.ToolCalls = nil
	response.RolloutCapture = nil
	if response.Result.rolloutCapture != nil && response.Result.rolloutCapture.sealed && response.Result.matchesCapture(response.Result.rolloutCapture) {
		response.RolloutCapture = response.Result.rolloutCapture
	}
	err := writer.frame(brokerFrame{Kind: brokerFrameTerminal, Terminal: &response})
	if !errors.Is(err, errProviderBrokerResponseInvalid) || writer.terminal {
		return err
	}
	// Ограниченный отказ сохраняет измеренный usage, не подтверждая большой итог.
	failure := brokerResponse{Failure: providerBrokerFailureProvider, Result: failedProviderResult(response.Result), RolloutCapture: response.RolloutCapture, Diagnostic: response.Diagnostic}
	return writer.frame(brokerFrame{Kind: brokerFrameTerminal, Terminal: &failure})
}

// frame assigns sequence/version, enforces encoded limits and reserves one frame plus byte budget
// for completion. It sets a net.Conn deadline and commits counters only after copying all bytes;
// a failed partial write is not evidence that the peer received nothing.
// frame назначает последовательность и версию, проверяет размер и оставляет кадр с байтовым резервом
// для завершения. Он задаёт deadline net.Conn и меняет счётчики лишь после полной записи;
// ошибка частичной записи не доказывает, что другая сторона ничего не получила.
func (writer *brokerFrameWriter) frame(frame brokerFrame) error {
	if writer.terminal || writer.sequence >= maximumBrokerFrames {
		return errProviderBrokerResponseInvalid
	}
	frame.Version, frame.Sequence = providerBrokerVersion, writer.sequence+1
	encoded, err := json.Marshal(frame)
	byteLimit := maximumBrokerStreamBytes
	if frame.Kind == brokerFrameActivity {
		byteLimit -= maximumBrokerFrameBytes
		if writer.sequence >= maximumBrokerFrames-1 {
			return errProviderBrokerResponseInvalid
		}
	}
	if err != nil || len(encoded)+1 > maximumBrokerFrameBytes || writer.bytes+len(encoded)+1 > byteLimit {
		return errProviderBrokerResponseInvalid
	}
	encoded = append(encoded, '\n')
	if connection, ok := writer.writer.(net.Conn); ok {
		if connection.SetWriteDeadline(time.Now().Add(brokerFrameWriteTimeout)) != nil {
			return errProviderBrokerFailed
		}
	}
	if _, err := io.Copy(writer.writer, bytes.NewReader(encoded)); err != nil {
		return errProviderBrokerFailed
	}
	writer.sequence++
	writer.bytes += len(encoded)
	writer.terminal = frame.Kind == brokerFrameTerminal
	return nil
}

// writeBrokerTerminal preserves an existing frame writer's sequence, or starts a terminal-only stream.
// writeBrokerTerminal сохраняет последовательность существующего writer либо создаёт поток только с итогом.
func writeBrokerTerminal(writer io.Writer, response brokerResponse) error {
	if stream, ok := writer.(*brokerFrameWriter); ok {
		return stream.finish(response)
	}
	return (&brokerFrameWriter{writer: writer}).finish(response)
}

// bindBrokerPeerContext cancels on the next scanned input or EOF without interpreting its content.
// The returned cleanup cancels, closes the connection to unblock Scan, and joins the reader goroutine.
// bindBrokerPeerContext отменяет контекст при следующем вводе или EOF без разбора содержимого.
// Возвращённая очистка отменяет контекст, закрывает соединение для выхода из Scan и ждёт goroutine.
func bindBrokerPeerContext(ctx context.Context, connection net.Conn, scanner *bufio.Scanner) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = scanner.Scan(); cancel() }()
	return ctx, func() { cancel(); _ = connection.Close(); <-done }
}

// readProviderBrokerResponse reads an unbound stream with at most one consumer; bound process,
// diagnostic and rollout evidence still require the explicit input/peer-aware reader.
// readProviderBrokerResponse читает непривязанный поток максимум с одним потребителем; доказательства
// процесса, диагностики и архива требуют отдельного читателя с точным input и идентичностью peer.
func readProviderBrokerResponse(reader io.Reader, callbacks ...func(runtimecontract.RuntimeActivity) error) (Result, error) {
	return readBoundProviderBrokerResponse(reader, nil, 0, callbacks...)
}

// readBoundProviderBrokerResponse rejects duplicate keys, unknown fields, bad sequence/version,
// replayed messages, invalid tool revisions and frames following the sole terminal. Trusted process,
// diagnostic and capture claims additionally require their exact input/peer bindings. Callback failure
// suppresses later delivery and interrupts a connection but continues validation through terminal so
// measured usage can survive; schema failure never exposes a partially trusted result.
// readBoundProviderBrokerResponse отвергает дубли ключей, лишние поля, неверную последовательность/версию,
// повтор сообщений, неверные ревизии инструментов и кадры после единственного итога. Заявления о процессе,
// диагностике и архиве требуют точной привязки к input и peer. Отказ callback прекращает дальнейшую
// доставку и прерывает соединение, но разбор продолжается до итога ради сохранения usage;
// ошибка схемы не выдаёт частично доверенный результат.
func readBoundProviderBrokerResponse(reader io.Reader, input *model.Input, writerUID uint32, callbacks ...func(runtimecontract.RuntimeActivity) error) (Result, error) {
	if len(callbacks) > 1 {
		return Result{}, brokerResponseError(brokerResponseCallbacks, errProviderBrokerResponseInvalid)
	}
	var callback func(runtimecontract.RuntimeActivity) error
	if len(callbacks) == 1 {
		callback = callbacks[0]
	}
	scanner := bufio.NewScanner(&boundedReader{reader: reader, remaining: maximumBrokerStreamBytes})
	scanner.Buffer(make([]byte, 64<<10), maximumBrokerFrameBytes)
	var terminal *brokerResponse
	var callbackError error
	var sequence uint64
	seenMessages := make(map[string]struct{})
	var financialSequence int64
	toolStates := make(map[string]runtimecontract.NativeToolCall)
	providerProcessSeen := false
	for scanner.Scan() {
		if terminal != nil {
			return Result{}, brokerResponseError(brokerResponsePostTerminal, errProviderBrokerResponseInvalid)
		}
		if sequence >= maximumBrokerFrames {
			return Result{}, brokerResponseError(brokerResponseFrameLimit, errProviderBrokerResponseInvalid)
		}
		if rejectDuplicateJSONKeys(scanner.Bytes()) != nil {
			return Result{}, brokerResponseError(brokerResponseFrameSchema, errProviderBrokerResponseInvalid)
		}
		var frame brokerFrame
		if strictDecode(scanner.Bytes(), &frame) != nil {
			return Result{}, brokerResponseError(brokerResponseFrameSchema, errProviderBrokerResponseInvalid)
		}
		if frame.Version != providerBrokerVersion {
			return Result{}, brokerResponseError(brokerResponseFrameVersion, errProviderBrokerResponseInvalid)
		}
		if frame.Sequence != sequence+1 {
			return Result{}, brokerResponseError(brokerResponseFrameSequence, errProviderBrokerResponseInvalid)
		}
		fields, err := decodeObject(scanner.Bytes(), schema([]string{"version", "sequence", "kind"}, "version", "sequence", "kind", "activity", "terminal"))
		if err != nil {
			return Result{}, brokerResponseError(brokerResponseFrameSchema, errProviderBrokerResponseInvalid)
		}
		sequence++
		switch frame.Kind {
		case brokerFrameActivity:
			if _, present := fields["terminal"]; present {
				return Result{}, brokerResponseError(brokerResponseActivitySchema, errProviderBrokerResponseInvalid)
			}
			if frame.Activity == nil || frame.Terminal != nil || frame.Activity.Validate() != nil {
				return Result{}, brokerResponseError(brokerResponseActivitySchema, errProviderBrokerResponseInvalid)
			}
			if observation := frame.Activity.ProviderProcess; observation != nil {
				if input == nil || writerUID != providerWriterUID || !observation.Matches(*input) || providerProcessSeen || sequence != 1 {
					return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
				}
				providerProcessSeen = true
			} else if message := frame.Activity.Message; message != nil {
				if _, exists := seenMessages[message.ItemID]; exists {
					return Result{}, brokerResponseError(brokerResponseMessageReplay, errProviderBrokerResponseInvalid)
				}
				if len(seenMessages) >= maximumPublishedMessages {
					return Result{}, brokerResponseError(brokerResponseMessageLimit, errProviderBrokerResponseInvalid)
				}
				seenMessages[message.ItemID] = struct{}{}
			} else if financial := frame.Activity.Financial; financial != nil {
				if financial.Sequence != financialSequence+1 {
					return Result{}, errProviderBrokerResponseInvalid
				}
				financialSequence = financial.Sequence
			} else {
				call := *frame.Activity.ToolCall
				if previous, exists := toolStates[call.CallID]; exists {
					if previous.Kind != call.Kind || previous.Revision != 1 || call.Revision != 2 {
						return Result{}, brokerResponseError(brokerResponseToolLifecycle, errProviderBrokerResponseInvalid)
					}
				} else if len(toolStates) >= runtimecontract.MaximumNativeToolCalls {
					return Result{}, brokerResponseError(brokerResponseToolLimit, errProviderBrokerResponseInvalid)
				}
				toolStates[call.CallID] = call
			}
			if callbackError == nil && callback != nil && callback(*frame.Activity) != nil {
				callbackError = errors.New("Codex runtime activity delivery failed")
				if connection, ok := reader.(net.Conn); ok {
					interruptBrokerRequest(connection)
				}
			}
		case brokerFrameTerminal:
			if _, present := fields["activity"]; present {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			if frame.Activity != nil || frame.Terminal == nil {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			terminalFields, err := decodeObject(fields["terminal"], schema([]string{"result", "ok"}, "result", "ok", "failure", "rollout_capture", "diagnostic"))
			if err != nil {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			if _, err := decodeObject(terminalFields["result"], schema(nil, "SessionID", "FinalMessage", "Outcome", "FailureCode", "ArchivePath", "ArchiveRelativePath", "ArchiveSHA256", "ArchiveSizeBytes", "Usage", "UsageCompleteness")); err != nil {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			terminal = frame.Terminal
			if _, present := terminalFields["diagnostic"]; present {
				if input == nil || writerUID != providerWriterUID || terminal.Diagnostic == nil || !terminal.Diagnostic.Matches(*input) ||
					(terminal.OK && (terminal.Result.Outcome != "FAILED" || terminal.Diagnostic.Kind != "TERMINAL_FAILURE" || terminal.Diagnostic.TerminalCode != terminal.Result.FailureCode)) {
					return Result{}, brokerResponseError(brokerResponseDiagnostic, errProviderBrokerResponseInvalid)
				}
				if !terminal.OK {
					class := terminal.Diagnostic.Class
					if class == "ACCOUNT_RESPONSE_SCHEMA" {
						class = "PROVIDER"
					}
					if class != string(terminal.Failure) {
						return Result{}, brokerResponseError(brokerResponseDiagnostic, errProviderBrokerResponseInvalid)
					}
				}
				value := *terminal.Diagnostic
				terminal.Result.providerDiagnostic = &value
			}
			if raw, present := terminalFields["rollout_capture"]; present {
				keys := []string{"schema", "execution_binding_digest", "runtime_revision_digest", "input_digest", "attempt", "session_ref", "turn_ref", "codex_session_id", "archive_relative_path", "archive_sha256", "archive_size_bytes"}
				if _, err := decodeObject(raw, schema(keys, keys...)); err != nil || input == nil || terminal.RolloutCapture == nil || verifyRolloutCapture(*input, terminal.Result, terminal.RolloutCapture, writerUID) != nil {
					return Result{}, brokerResponseError(brokerResponseArchive, errProviderBrokerResponseInvalid)
				}
				terminal.RolloutCapture.sealed = true
				terminal.Result = withRolloutCapture(terminal.Result, terminal.RolloutCapture)
			} else if terminal.Result.SessionID != "" || terminal.Result.ArchivePath != "" || terminal.Result.ArchiveRelativePath != "" || terminal.Result.ArchiveSHA256 != "" || terminal.Result.ArchiveSizeBytes != 0 {
				return Result{}, brokerResponseError(brokerResponseArchive, errProviderBrokerResponseInvalid)
			}
		default:
			return Result{}, brokerResponseError(brokerResponseFrameKind, errProviderBrokerResponseInvalid)
		}
	}
	if scanner.Err() != nil {
		return Result{}, brokerResponseError(brokerResponseStreamRead, errProviderBrokerFailed)
	}
	if terminal == nil {
		return Result{}, brokerResponseError(brokerResponseNoTerminal, errProviderBrokerFailed)
	}
	result, err := validateBrokerTerminal(*terminal)
	if callbackError != nil && !errors.Is(err, errProviderBrokerResponseInvalid) {
		return failedProviderResult(result), brokerResponseError(brokerResponseActivityFailed, callbackError)
	}
	if err != nil {
		err = brokerResponseError(brokerResponseTerminalResult, err)
	}
	return result, err
}

// rejectDuplicateJSONKeys walks decoded keys before typed decoding, including nested objects/arrays,
// with depth and token-visit limits and a final EOF check. It does not require an object root or
// impose its own byte limit; those constraints belong to the surrounding frame reader.
// rejectDuplicateJSONKeys обходит декодированные ключи до типизированного разбора, включая вложенные
// объекты и массивы, с пределами глубины и посещений и проверкой EOF. Корневой объект и байтовый
// лимит проверяет окружающий читатель кадров, а не эта функция.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	count := 0
	walk = func(depth int) error {
		count++
		if depth > 64 || count > 100_000 {
			return errProviderBrokerResponseInvalid
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]struct{}{}
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok {
					return errProviderBrokerResponseInvalid
				}
				if _, exists := keys[key]; exists {
					return errProviderBrokerResponseInvalid
				}
				keys[key] = struct{}{}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errProviderBrokerResponseInvalid
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errProviderBrokerResponseInvalid
			}
		default:
			return errProviderBrokerResponseInvalid
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	return ensureEOF(decoder)
}
