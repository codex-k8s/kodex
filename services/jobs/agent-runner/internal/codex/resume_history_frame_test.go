package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// Fixture использует настоящий stdout reader, но не запускает Codex, сеть
// или модель. JSONL-история и thread identity исключительно синтетические.
type resumeHistoryFixture struct {
	t            *testing.T
	input        model.Input
	path         string
	events       chan streamEvent
	history      []any
	resumeParams map[string]any
	frameBytes   int
}

func (fixture *resumeHistoryFixture) Close() error { return nil }
func (fixture *resumeHistoryFixture) Write(raw []byte) (int, error) {
	var request struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if json.Unmarshal(raw, &request) != nil {
		fixture.t.Fatal("synthetic request invalid")
	}
	thread := codex160ThreadFixture(fixture.t)
	thread["id"], thread["sessionId"], thread["path"], thread["cwd"] = fixture.input.CodexSessionID, fixture.input.CodexSessionID, fixture.path, fixture.input.WorkspaceRoot
	result := map[string]any{"thread": thread}
	if request.Method == "thread/resume" {
		fixture.resumeParams = request.Params
		if request.Params["excludeTurns"] != true {
			thread["turns"] = fixture.history
		}
		result["approvalPolicy"], result["approvalsReviewer"], result["cwd"], result["model"], result["modelProvider"], result["sandbox"] = "never", "user", fixture.input.WorkspaceRoot, fixture.input.Model, "openai", map[string]any{"type": "readOnly"}
	} else if request.Method != "thread/read" || request.Params["includeTurns"] != false {
		fixture.t.Fatal("synthetic pre-read changed")
	}
	frame := marshalProtocolFixture(fixture.t, map[string]any{"id": request.ID, "result": result})
	if request.Method == "thread/resume" {
		fixture.frameBytes = len(frame)
	}
	readerEvents := make(chan streamEvent, 2)
	readAppServerMessages(bytes.NewReader(append(frame, '\n')), readerEvents)
	for event := range readerEvents {
		fixture.events <- event
	}
	return len(raw), nil
}

func syntheticResumeHistory(t *testing.T) (*resumeHistoryFixture, *protocolState, [sha256.Size]byte) {
	t.Helper()
	input, before, _ := resumeSourceFixture(t)
	history := make([]any, 20)
	for index := range history {
		history[index] = map[string]any{"id": "synthetic", "status": "completed", "items": []any{map[string]any{"type": "agentMessage", "id": "message", "text": strings.Repeat("x", 60<<10), "phase": "final_answer"}}}
	}
	// Файл остаётся большим, даже когда ответ resume выдаёт только metadata.
	archive := bytes.Repeat([]byte("{\"type\":\"event_msg\",\"payload\":\"synthetic history\"}\n"), 24000)
	if err := os.WriteFile(before.ArchivePath, archive, 0o640); err != nil {
		t.Fatal("synthetic archive setup failed")
	}
	fixture := &resumeHistoryFixture{t: t, input: input, path: before.ArchivePath, history: history, events: make(chan streamEvent, 4)}
	return fixture, newProtocolState(input.CodexSessionID), sha256.Sum256(archive)
}

func TestResumeLongHistoryMetadataDoesNotOverflowFrame(t *testing.T) {
	fixture, state, sourceSHA := syntheticResumeHistory(t)
	server := &appServer{stdin: fixture, messages: fixture.events}
	err := server.bindExecutionThread(t.Context(), state, fixture.input)
	if state.resumeSource != nil {
		defer state.resumeSource.file.Close()
	}
	if err != nil {
		var failure *appServerCallFailure
		if errors.As(err, &failure) {
			t.Fatalf("synthetic resume rejected: stage=%s detail=%s frame_bytes=%d frame_bound=%d", providerStageOf(err), failure.detail, fixture.frameBytes, maximumJSONLLineBytes)
		}
		t.Fatal("synthetic resume rejected before successful binding")
	}
	if fixture.resumeParams["excludeTurns"] != true || fixture.frameBytes >= maximumJSONLLineBytes {
		t.Fatal("resume did not request bounded metadata-only reply")
	}
	if _, present := fixture.resumeParams["includeTurns"]; present {
		t.Fatal("resume used an unpinned flag")
	}
	if _, present := fixture.resumeParams["omitHistory"]; present {
		t.Fatal("resume used an unsupported history flag")
	}
	if fixture.resumeParams["model"] != fixture.input.Model || fixture.resumeParams["cwd"] != fixture.input.WorkspaceRoot || fixture.resumeParams["approvalPolicy"] != fixture.input.CodexApprovalPolicy {
		t.Fatal("metadata-only resume changed configuration pins")
	}
	if state.threadID != fixture.input.CodexSessionID || state.threadPath != fixture.path || state.resumeSource == nil || state.resumeSource.verifyIdentity(fixture.input) != nil || state.baselineCaptured || state.result.Usage.TotalTokens != 0 || state.turnID != "" {
		t.Fatal("resume changed identity, authority or usage")
	}
	archive, err := os.ReadFile(fixture.path)
	if err != nil || len(archive) <= maximumJSONLLineBytes || sha256.Sum256(archive) != sourceSHA {
		t.Fatal("metadata projection altered or truncated retained history")
	}
	if err := state.captureUsageBaseline(); err != nil {
		t.Fatal("metadata-only resume prevented fresh usage baseline")
	}
	t.Logf("synthetic metadata-only resume: frame_bytes=%d retained_history_bytes=%d", fixture.frameBytes, len(archive))
}

func TestResumeHistoryFrameOverflowIsClosed(t *testing.T) {
	fixture, state, _ := syntheticResumeHistory(t)
	server := &appServer{stdin: fixture, messages: fixture.events}
	_, err := server.call(t.Context(), state, "thread/resume", map[string]any{"threadId": fixture.input.CodexSessionID})
	var failure *appServerCallFailure
	if !errors.As(err, &failure) || failure.detail != "STREAM_INVALID" || fixture.frameBytes <= maximumJSONLLineBytes {
		t.Fatal("oversized history frame did not preserve closed stream refusal")
	}
	t.Logf("synthetic overflow: detail=%s frame_bytes=%d frame_bound=%d", failure.detail, fixture.frameBytes, maximumJSONLLineBytes)
}

func TestResumeHistoryTransportBudgetIsPerFrame(t *testing.T) {
	frame := marshalProtocolFixture(t, map[string]any{"id": 1, "result": map[string]any{"padding": strings.Repeat("x", 100<<10)}})
	stream := bytes.Repeat(append(frame, '\n'), 12)
	if len(stream) <= maximumJSONLLineBytes {
		t.Fatal("synthetic aggregate did not cross frame budget")
	}
	events := make(chan streamEvent, 13)
	readAppServerMessages(bytes.NewReader(stream), events)
	count := 0
	for event := range events {
		if event.err != nil {
			t.Fatal("aggregate history size incorrectly became per-frame failure")
		}
		count++
	}
	if count != 12 {
		t.Fatal("valid bounded frames were lost")
	}
}
