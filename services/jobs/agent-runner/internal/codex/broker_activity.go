package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
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
	// nativeSessionRecovering identifies the sole allowed pre-process observation during exact restore.
	// It reports work in progress and never proves process identity or successful recovery.
	// nativeSessionRecovering обозначает единственное допустимое наблюдение до процесса при exact restore.
	// Оно сообщает о работе, но не подтверждает identity процесса или успешное восстановление.
	nativeSessionRecovering = "NATIVE_SESSION_RECOVERING"
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
// Writes serialize concurrent native/HTTP observations; input supplies optional binding for producer paths, not a new authority.
// brokerFrameWriter отслеживает один последовательный ответ и меняет счётчики только после успешной записи.
// Записи упорядочивают concurrent native/HTTP observations; input задаёт привязку путям производителя, но не новые полномочия.
type brokerFrameWriter struct {
	mu       sync.Mutex
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
	response.WorkspaceJoin = nil
	response.NativeCapture = nil
	if writer.input != nil && response.Result.HasVerifiedNativeCheckpoint(*writer.input) {
		response.NativeCapture = response.Result.nativeCapture
	}
	if response.Result.workspaceJoin != nil && response.Result.workspaceJoin.sealed && writer.input != nil && response.Result.workspaceJoin.bound(*writer.input) {
		response.WorkspaceJoin = response.Result.workspaceJoin
	}

	err := writer.frame(brokerFrame{Kind: brokerFrameTerminal, Terminal: &response})
	writer.mu.Lock()
	terminal := writer.terminal
	writer.mu.Unlock()
	if !errors.Is(err, errProviderBrokerResponseInvalid) || terminal {
		return err
	}
	// Ограниченный отказ сохраняет измеренный usage, не подтверждая большой итог.
	failure := brokerResponse{Failure: providerBrokerFailureProvider, Result: failedProviderResult(response.Result), Diagnostic: response.Diagnostic, WorkspaceJoin: response.WorkspaceJoin, NativeCapture: response.NativeCapture}
	return writer.frame(brokerFrame{Kind: brokerFrameTerminal, Terminal: &failure})
}

// frame assigns sequence/version, enforces encoded limits and reserves one frame plus byte budget
// for completion. It sets a net.Conn deadline and commits counters only after copying all bytes;
// a failed partial write is not evidence that the peer received nothing.
// frame назначает последовательность и версию, проверяет размер и оставляет кадр с байтовым резервом
// для завершения. Он задаёт deadline net.Conn и меняет счётчики лишь после полной записи;
// ошибка частичной записи не доказывает, что другая сторона ничего не получила.
func (writer *brokerFrameWriter) frame(frame brokerFrame) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	nativeStream := writer.input != nil && writer.input.Provider == "opencode"
	if writer.terminal || writer.sequence == ^uint64(0) || !nativeStream && writer.sequence >= maximumBrokerFrames {
		return errProviderBrokerResponseInvalid
	}
	frame.Version, frame.Sequence = providerBrokerVersion, writer.sequence+1
	encoded, err := json.Marshal(frame)
	byteLimit := maximumBrokerStreamBytes
	if frame.Kind == brokerFrameActivity {
		byteLimit -= maximumBrokerFrameBytes
		if !nativeStream && writer.sequence >= maximumBrokerFrames-1 {
			return errProviderBrokerResponseInvalid
		}
	}
	if err != nil || len(encoded)+1 > maximumBrokerFrameBytes || !nativeStream && writer.bytes+len(encoded)+1 > byteLimit {
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
	if !nativeStream {
		writer.bytes += len(encoded)
	}
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
	// The native production path streams under its existing deadline/cancellation and owner callback.
	// The owner retains replay receipts; this transport must not accumulate the lifetime transcript.
	// Native production path ограничен deadline/cancellation и owner callback.
	// Replay receipts хранит owner; transport не накапливает lifetime transcript.
	nativeStream := input != nil && input.Provider == "opencode" && writerUID == providerWriterUID && callback != nil
	stream := reader
	if !nativeStream {
		stream = &boundedReader{reader: reader, remaining: maximumBrokerStreamBytes}
	}
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64<<10), maximumBrokerFrameBytes)
	var terminal *brokerResponse
	var callbackError error
	var sequence uint64
	seenMessages := make(map[string]struct{})
	var financialSequence int64
	toolStates := make(map[string]runtimecontract.NativeToolCall)
	providerProcessSeen := false
	nativeRecoverySeen := false
	for scanner.Scan() {
		if terminal != nil {
			return Result{}, brokerResponseError(brokerResponsePostTerminal, errProviderBrokerResponseInvalid)
		}
		if sequence == ^uint64(0) || !nativeStream && sequence >= maximumBrokerFrames {
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
			if frame.Activity.Progress == nativeSessionRecovering {
				if input == nil || input.Provider != "opencode" || writerUID != providerWriterUID || input.NativeSession == nil || input.NativeSession.Validate() != nil || sequence != 1 || nativeRecoverySeen || providerProcessSeen {
					return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
				}
				nativeRecoverySeen = true
			} else if nativeRecoverySeen && !providerProcessSeen && frame.Activity.ProviderProcess == nil {
				return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
			}
			if observation := frame.Activity.ProviderProcess; observation != nil {
				expectedSequence := uint64(1)
				if nativeRecoverySeen {
					expectedSequence = 2
				}
				if input == nil || writerUID != providerWriterUID || !observation.Matches(*input) || providerProcessSeen || sequence != expectedSequence {
					return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
				}
				providerProcessSeen = true
			} else if message := frame.Activity.Message; message != nil {
				if _, exists := seenMessages[message.ItemID]; !nativeStream && exists {
					return Result{}, brokerResponseError(brokerResponseMessageReplay, errProviderBrokerResponseInvalid)
				}
				if !nativeStream && len(seenMessages) >= maximumPublishedMessages {
					return Result{}, brokerResponseError(brokerResponseMessageLimit, errProviderBrokerResponseInvalid)
				}
				if !nativeStream {
					seenMessages[message.ItemID] = struct{}{}
				}
			} else if financial := frame.Activity.Financial; financial != nil {
				if financial.Sequence != financialSequence+1 {
					return Result{}, errProviderBrokerResponseInvalid
				}
				financialSequence = financial.Sequence
			} else if tool := frame.Activity.ToolCall; tool != nil {
				call := *tool
				if previous, exists := toolStates[call.CallID]; exists {
					if previous.Kind != call.Kind || previous.Revision != 1 || call.Revision != 2 {
						return Result{}, brokerResponseError(brokerResponseToolLifecycle, errProviderBrokerResponseInvalid)
					}
				} else if len(toolStates) >= runtimecontract.MaximumNativeToolCalls {
					return Result{}, brokerResponseError(brokerResponseToolLimit, errProviderBrokerResponseInvalid)
				}
				toolStates[call.CallID] = call
				if nativeStream && call.Revision == 2 {
					delete(toolStates, call.CallID)
				}
			} else if frame.Activity.Progress == "" && frame.Activity.ModelMeasurement == nil {
				return Result{}, brokerResponseError(brokerResponseActivitySchema, errProviderBrokerResponseInvalid)
			}
			// Validated progress and measurements only reach the callback; they neither
			// enter the tool ledger nor confirm terminal completion.
			// Проверенные progress и measurements передаются только callback: они не
			// входят в журнал инструментов и не подтверждают завершение исполнения.
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
			terminalFields, err := decodeObject(fields["terminal"], schema([]string{"result", "ok"}, "result", "ok", "failure", "diagnostic", "workspace_join", "native_capture"))
			if err != nil {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			if _, err := decodeObject(terminalFields["result"], schema(nil, "FinalMessage", "Outcome", "FailureCode", "Usage", "UsageCompleteness", "NativeSession")); err != nil {
				return Result{}, brokerResponseError(brokerResponseTerminalSchema, errProviderBrokerResponseInvalid)
			}
			terminal = frame.Terminal
			if nativeRecoverySeen && !providerProcessSeen && terminal.OK && terminal.Result.Outcome != "FAILED" {
				return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
			}
			if raw, present := terminalFields["native_capture"]; present {
				keys := []string{"schema", "execution_binding_digest", "runtime_revision_digest", "input_digest", "lease_ref", "lease_generation", "organization_ref", "project_ref", "agent_ref", "session_ref", "turn_ref", "attempt", "checkpoint"}
				if _, err := decodeObject(raw, schema(keys, keys...)); err != nil || input == nil || verifyNativeCapture(*input, terminal.Result, terminal.NativeCapture, writerUID) != nil {
					return Result{}, brokerResponseError(brokerResponseArchive, errProviderBrokerResponseInvalid)
				}
				terminal.NativeCapture.sealed = true
				terminal.Result.nativeCapture = terminal.NativeCapture
			} else if terminal.Result.NativeSession != nil {
				return Result{}, brokerResponseError(brokerResponseArchive, errProviderBrokerResponseInvalid)
			}
			if raw, present := terminalFields["workspace_join"]; present {
				keys := []string{"schema", "execution_binding_digest", "runtime_revision_digest", "input_digest", "lease_ref", "lease_generation", "attempt", "joined"}
				if _, err := decodeObject(raw, schema(keys, keys...)); err != nil || input == nil || writerUID != providerWriterUID || !terminal.WorkspaceJoin.bound(*input) {
					return Result{}, brokerResponseError(brokerResponseProviderTuple, errProviderBrokerResponseInvalid)
				}
				terminal.WorkspaceJoin.sealed = true
				terminal.Result.workspaceJoin = terminal.WorkspaceJoin
			}
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
