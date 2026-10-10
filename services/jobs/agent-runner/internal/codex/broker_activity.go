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
	providerBrokerVersion    = 3
	maximumPublishedMessages = 1024
	maximumBrokerFrameBytes  = 1 << 20
	maximumBrokerStreamBytes = 64 << 20
	maximumBrokerFrames      = 2*runtimecontract.MaximumNativeToolCalls + maximumPublishedMessages + 2
	brokerFrameActivity      = "ACTIVITY"
	brokerFrameTerminal      = "TERMINAL"
	brokerFrameWriteTimeout  = 5 * time.Second
)

type brokerFrame struct {
	Version  int                              `json:"version"`
	Sequence uint64                           `json:"sequence"`
	Kind     string                           `json:"kind"`
	Activity *runtimecontract.RuntimeActivity `json:"activity,omitempty"`
	Terminal *brokerResponse                  `json:"terminal,omitempty"`
}

type brokerFrameWriter struct {
	writer   io.Writer
	sequence uint64
	bytes    int
	terminal bool
	input    *model.Input
}

func (writer *brokerFrameWriter) Write([]byte) (int, error) {
	return 0, errProviderBrokerResponseInvalid
}

func (writer *brokerFrameWriter) activity(activity runtimecontract.RuntimeActivity) error {
	if activity.Validate() != nil {
		return errProviderBrokerResponseInvalid
	}
	return writer.frame(brokerFrame{Kind: brokerFrameActivity, Activity: &activity})
}

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

func writeBrokerTerminal(writer io.Writer, response brokerResponse) error {
	if stream, ok := writer.(*brokerFrameWriter); ok {
		return stream.finish(response)
	}
	return (&brokerFrameWriter{writer: writer}).finish(response)
}

func bindBrokerPeerContext(ctx context.Context, connection net.Conn, scanner *bufio.Scanner) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = scanner.Scan(); cancel() }()
	return ctx, func() { cancel(); _ = connection.Close(); <-done }
}

func readProviderBrokerResponse(reader io.Reader, callbacks ...func(runtimecontract.RuntimeActivity) error) (Result, error) {
	return readBoundProviderBrokerResponse(reader, nil, 0, callbacks...)
}

func readBoundProviderBrokerResponse(reader io.Reader, input *model.Input, writerUID uint32, callbacks ...func(runtimecontract.RuntimeActivity) error) (Result, error) {
	if len(callbacks) > 1 {
		return Result{}, errProviderBrokerResponseInvalid
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
	toolStates := make(map[string]runtimecontract.NativeToolCall)
	providerProcessSeen := false
	for scanner.Scan() {
		if terminal != nil || sequence >= maximumBrokerFrames || rejectDuplicateJSONKeys(scanner.Bytes()) != nil {
			return Result{}, errProviderBrokerResponseInvalid
		}
		var frame brokerFrame
		if strictDecode(scanner.Bytes(), &frame) != nil || frame.Version != providerBrokerVersion || frame.Sequence != sequence+1 {
			return Result{}, errProviderBrokerResponseInvalid
		}
		fields, err := decodeObject(scanner.Bytes(), schema([]string{"version", "sequence", "kind"}, "version", "sequence", "kind", "activity", "terminal"))
		if err != nil {
			return Result{}, errProviderBrokerResponseInvalid
		}
		sequence++
		switch frame.Kind {
		case brokerFrameActivity:
			if _, present := fields["terminal"]; present {
				return Result{}, errProviderBrokerResponseInvalid
			}
			if frame.Activity == nil || frame.Terminal != nil || frame.Activity.Validate() != nil {
				return Result{}, errProviderBrokerResponseInvalid
			}
			if observation := frame.Activity.ProviderProcess; observation != nil {
				if input == nil || writerUID != providerWriterUID || !observation.Matches(*input) || providerProcessSeen || sequence != 1 {
					return Result{}, errProviderBrokerResponseInvalid
				}
				providerProcessSeen = true
			} else if message := frame.Activity.Message; message != nil {
				if _, exists := seenMessages[message.ItemID]; exists || len(seenMessages) >= maximumPublishedMessages {
					return Result{}, errProviderBrokerResponseInvalid
				}
				seenMessages[message.ItemID] = struct{}{}
			} else {
				call := *frame.Activity.ToolCall
				if previous, exists := toolStates[call.CallID]; exists {
					if previous.Kind != call.Kind || previous.Revision != 1 || call.Revision != 2 {
						return Result{}, errProviderBrokerResponseInvalid
					}
				} else if len(toolStates) >= runtimecontract.MaximumNativeToolCalls {
					return Result{}, errProviderBrokerResponseInvalid
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
				return Result{}, errProviderBrokerResponseInvalid
			}
			if frame.Activity != nil || frame.Terminal == nil {
				return Result{}, errProviderBrokerResponseInvalid
			}
			terminalFields, err := decodeObject(fields["terminal"], schema([]string{"result", "ok"}, "result", "ok", "failure", "rollout_capture", "diagnostic"))
			if err != nil {
				return Result{}, errProviderBrokerResponseInvalid
			}
			if _, err := decodeObject(terminalFields["result"], schema(nil, "SessionID", "FinalMessage", "Outcome", "FailureCode", "ArchivePath", "ArchiveRelativePath", "ArchiveSHA256", "ArchiveSizeBytes", "Usage")); err != nil {
				return Result{}, errProviderBrokerResponseInvalid
			}
			terminal = frame.Terminal
			if _, present := terminalFields["diagnostic"]; present {
				if input == nil || writerUID != providerWriterUID || terminal.Diagnostic == nil || !terminal.Diagnostic.Matches(*input) ||
					(terminal.OK && (terminal.Result.Outcome != "FAILED" || terminal.Diagnostic.Kind != "TERMINAL_FAILURE" || terminal.Diagnostic.TerminalCode != terminal.Result.FailureCode)) {
					return Result{}, errProviderBrokerResponseInvalid
				}
				if !terminal.OK {
					class := terminal.Diagnostic.Class
					if class == "ACCOUNT_RESPONSE_SCHEMA" {
						class = "PROVIDER"
					}
					if class != string(terminal.Failure) {
						return Result{}, errProviderBrokerResponseInvalid
					}
				}
				value := *terminal.Diagnostic
				terminal.Result.providerDiagnostic = &value
			}
			if raw, present := terminalFields["rollout_capture"]; present {
				keys := []string{"schema", "execution_binding_digest", "runtime_revision_digest", "input_digest", "attempt", "session_ref", "turn_ref", "codex_session_id", "archive_relative_path", "archive_sha256", "archive_size_bytes"}
				if _, err := decodeObject(raw, schema(keys, keys...)); err != nil || input == nil || terminal.RolloutCapture == nil || verifyRolloutCapture(*input, terminal.Result, terminal.RolloutCapture, writerUID) != nil {
					return Result{}, errProviderBrokerResponseInvalid
				}
				terminal.RolloutCapture.sealed = true
				terminal.Result = withRolloutCapture(terminal.Result, terminal.RolloutCapture)
			} else if terminal.Result.SessionID != "" || terminal.Result.ArchivePath != "" || terminal.Result.ArchiveRelativePath != "" || terminal.Result.ArchiveSHA256 != "" || terminal.Result.ArchiveSizeBytes != 0 {
				return Result{}, errProviderBrokerResponseInvalid
			}
		default:
			return Result{}, errProviderBrokerResponseInvalid
		}
	}
	if scanner.Err() != nil || terminal == nil {
		return Result{}, errProviderBrokerFailed
	}
	result, err := validateBrokerTerminal(*terminal)
	if callbackError != nil && !errors.Is(err, errProviderBrokerResponseInvalid) {
		return failedProviderResult(result), callbackError
	}
	return result, err
}

// Проверяем дубли рекурсивно до typed decode, не принимая восстановленную модель.
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
