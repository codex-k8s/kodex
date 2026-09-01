package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/google/uuid"
)

const (
	maximumJSONLLineBytes  = 1 << 20
	maximumJSONLMessages   = 100_000
	maximumFinalBytes      = 480_000
	maximumDiagnosticBytes = 16 << 10
)

var nativeToolCallIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
var nativeSafeLabelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type Result struct {
	SessionID           string
	FinalMessage        string
	Outcome             string
	FailureCode         string
	ArchivePath         string
	ArchiveRelativePath string
	ArchiveSHA256       string
	ArchiveSizeBytes    int64
	Usage               runtimecontract.TokenUsage
	ToolCalls           []runtimecontract.NativeToolCall
}

type messageKind uint8

const (
	messageResponse messageKind = iota + 1
	messageError
	messageNotification
	messageRequest
)

type wireMessage struct {
	kind    messageKind
	id      json.RawMessage
	method  string
	payload json.RawMessage
}

type objectSchema struct {
	allowed  map[string]struct{}
	required map[string]struct{}
}

func schema(required []string, allowed ...string) objectSchema {
	value := objectSchema{allowed: make(map[string]struct{}, len(allowed)), required: make(map[string]struct{}, len(required))}
	for _, field := range allowed {
		value.allowed[field] = struct{}{}
	}
	for _, field := range required {
		value.required[field] = struct{}{}
	}
	return value
}

func parseWireMessage(raw []byte) (wireMessage, error) {
	fields, err := decodeObject(raw, schema(nil, "id", "method", "params", "result", "error", "trace", "emittedAtMs"))
	if err != nil {
		return wireMessage{}, errors.New("Codex app-server JSON-RPC message is invalid")
	}
	id, hasID := fields["id"]
	methodRaw, hasMethod := fields["method"]
	params, hasParams := fields["params"]
	result, hasResult := fields["result"]
	errorValue, hasError := fields["error"]
	_, hasTrace := fields["trace"]
	emittedAtMS, hasEmittedAtMS := fields["emittedAtMs"]
	if hasEmittedAtMS {
		var timestamp int64
		if strictDecode(emittedAtMS, &timestamp) != nil || timestamp < 0 {
			return wireMessage{}, errors.New("Codex app-server notification timestamp is invalid")
		}
	}
	if hasMethod {
		method, decodeErr := decodeBoundedString(methodRaw, 256)
		if decodeErr != nil || method == "" || hasResult || hasError {
			return wireMessage{}, errors.New("Codex app-server JSON-RPC method is invalid")
		}
		if hasID {
			if hasEmittedAtMS || !validRequestID(id) || !hasParams {
				return wireMessage{}, errors.New("Codex app-server JSON-RPC request is invalid")
			}
			return wireMessage{kind: messageRequest, id: id, method: method, payload: params}, nil
		}
		if hasTrace || !hasParams {
			return wireMessage{}, errors.New("Codex app-server JSON-RPC notification is invalid")
		}
		return wireMessage{kind: messageNotification, method: method, payload: params}, nil
	}
	if !hasID || hasParams || hasTrace || hasEmittedAtMS || hasResult == hasError || !validRequestID(id) {
		return wireMessage{}, errors.New("Codex app-server JSON-RPC response is invalid")
	}
	if hasError {
		if _, decodeErr := decodeRPCError(errorValue); decodeErr != nil {
			return wireMessage{}, decodeErr
		}
		return wireMessage{kind: messageError, id: id, payload: errorValue}, nil
	}
	return wireMessage{kind: messageResponse, id: id, payload: result}, nil
}

func validRequestID(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 256 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil && decoder.Decode(&struct{}{}) == io.EOF {
		_, err = number.Int64()
		return err == nil
	}
	_, err := decodeBoundedString(raw, 128)
	return err == nil
}

func numericRequestID(raw json.RawMessage) (int64, error) {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, errors.New("Codex app-server response id is invalid")
	}
	value, err := number.Int64()
	if err != nil || value <= 0 {
		return 0, errors.New("Codex app-server response id is invalid")
	}
	return value, nil
}

func decodeRPCError(raw json.RawMessage) (int64, error) {
	fields, err := decodeObject(raw, schema([]string{"code", "message"}, "code", "message", "data"))
	if err != nil {
		return 0, errors.New("Codex app-server JSON-RPC error is invalid")
	}
	var code int64
	if strictDecode(fields["code"], &code) != nil {
		return 0, errors.New("Codex app-server JSON-RPC error code is invalid")
	}
	if _, err := decodeBoundedString(fields["message"], maximumDiagnosticBytes); err != nil {
		return 0, errors.New("Codex app-server JSON-RPC error diagnostic is invalid")
	}
	return code, nil
}

type protocolState struct {
	expectedSessionID string
	threadID          string
	threadPath        string
	turnID            string
	turnStarted       uint32
	terminals         uint32
	result            Result
	latestUsage       runtimecontract.TokenUsage
	usageBaseline     runtimecontract.TokenUsage
	baselineCaptured  bool
	agentMessages     map[string]agentMessage
	toolCalls         map[string]runtimecontract.NativeToolCall
	toolCallOrder     []string
	itemStartedAtMS   map[string]int64
	workspaceRoot     string
	finalID           string
	fallbackID        string
}

type agentMessage struct {
	text  string
	phase string
}

func newProtocolState(expectedSessionID string) *protocolState {
	return &protocolState{expectedSessionID: expectedSessionID, agentMessages: make(map[string]agentMessage),
		toolCalls: make(map[string]runtimecontract.NativeToolCall), itemStartedAtMS: make(map[string]int64)}
}

func (state *protocolState) captureUsageBaseline() error {
	if state.baselineCaptured || state.turnID != "" || state.latestUsage.Validate() != nil {
		return errors.New("Codex app-server token usage baseline is invalid")
	}
	state.usageBaseline = state.latestUsage
	state.baselineCaptured = true
	return nil
}

func (state *protocolState) initialize(raw json.RawMessage, expectedHome string) error {
	fields, err := decodeObject(raw, schema([]string{"codexHome", "platformFamily", "platformOs", "userAgent"},
		"codexHome", "platformFamily", "platformOs", "userAgent"))
	if err != nil {
		return errors.New("Codex app-server initialize response is invalid")
	}
	home, homeErr := decodeBoundedString(fields["codexHome"], 4096)
	family, familyErr := decodeBoundedString(fields["platformFamily"], 64)
	operatingSystem, osErr := decodeBoundedString(fields["platformOs"], 64)
	userAgent, agentErr := decodeBoundedString(fields["userAgent"], 512)
	if homeErr != nil || familyErr != nil || osErr != nil || agentErr != nil || home != expectedHome ||
		family != "unix" || operatingSystem != "linux" || userAgent == "" {
		return errors.New("Codex app-server initialize binding is invalid")
	}
	return nil
}

func (state *protocolState) bindThread(raw json.RawMessage, expectedModel, expectedWorkspace, expectedApproval string) error {
	fields, err := decodeObject(raw, schema([]string{"approvalPolicy", "approvalsReviewer", "cwd", "model", "modelProvider", "sandbox", "thread"},
		"activePermissionProfile", "approvalPolicy", "approvalsReviewer", "cwd", "initialTurnsPage", "instructionSources",
		"model", "modelProvider", "multiAgentMode", "reasoningEffort", "runtimeWorkspaceRoots", "sandbox", "serviceTier", "thread"))
	if err != nil {
		return errors.New("Codex app-server thread response is invalid")
	}
	model, modelErr := decodeBoundedString(fields["model"], 128)
	cwd, cwdErr := decodeBoundedString(fields["cwd"], 4096)
	approval, approvalErr := decodeBoundedString(fields["approvalPolicy"], 64)
	if modelErr != nil || cwdErr != nil || approvalErr != nil || model != expectedModel || cwd != expectedWorkspace ||
		approval != expectedApproval {
		return errors.New("Codex app-server thread binding is invalid")
	}
	threadID, path, threadErr := parseThread(fields["thread"])
	if threadErr != nil || (state.expectedSessionID != "" && threadID != state.expectedSessionID) ||
		(state.threadID != "" && state.threadID != threadID) {
		return errors.New("Codex app-server thread identity is invalid")
	}
	state.threadID = threadID
	state.threadPath = path
	state.workspaceRoot = expectedWorkspace
	state.result.SessionID = threadID
	return nil
}

func (state *protocolState) bindThreadRead(raw json.RawMessage) error {
	fields, err := decodeObject(raw, schema([]string{"thread"}, "thread"))
	if err != nil {
		return errors.New("Codex app-server thread read response is invalid")
	}
	threadID, path, err := parseThread(fields["thread"])
	if err != nil || threadID != state.threadID || path == "" {
		return errors.New("Codex app-server rollout path is invalid")
	}
	state.threadPath = path
	return nil
}

func parseThread(raw json.RawMessage) (string, string, error) {
	fields, err := decodeObject(raw, schema([]string{"cliVersion", "createdAt", "cwd", "ephemeral", "id", "modelProvider",
		"preview", "sessionId", "source", "status", "turns", "updatedAt"}, "agentNickname", "agentRole", "canAcceptDirectInput",
		"cliVersion", "createdAt", "cwd", "ephemeral", "extra", "forkedFromId", "gitInfo", "historyMode", "id", "modelProvider",
		"name", "parentThreadId", "path", "preview", "projectId", "recencyAt", "section", "sectionEnteredAt", "sessionId",
		"source", "status", "threadSource", "turns", "updatedAt"))
	if err != nil {
		return "", "", err
	}
	id, idErr := decodeBoundedString(fields["id"], 128)
	sessionID, sessionErr := decodeBoundedString(fields["sessionId"], 128)
	var ephemeral bool
	if idErr != nil || sessionErr != nil || strictDecode(fields["ephemeral"], &ephemeral) != nil || ephemeral ||
		uuid.Validate(id) != nil || uuid.Validate(sessionID) != nil {
		return "", "", errors.New("Codex app-server thread is invalid")
	}
	path := ""
	if rawPath, present := fields["path"]; present && !bytes.Equal(rawPath, []byte("null")) {
		path, err = decodeBoundedString(rawPath, 4096)
		if err != nil {
			return "", "", errors.New("Codex app-server thread path is invalid")
		}
	}
	return id, path, nil
}

func (state *protocolState) bindTurn(raw json.RawMessage) error {
	fields, err := decodeObject(raw, schema([]string{"turn"}, "turn"))
	if err != nil {
		return errors.New("Codex app-server turn response is invalid")
	}
	turn, err := parseTurn(fields["turn"])
	if err != nil || turn.status != "inProgress" || turn.errorValue != nil || state.turnID != "" {
		return errors.New("Codex app-server turn start is invalid")
	}
	state.turnID = turn.id
	return state.consumeItems(turn.items, false, 0)
}

func (state *protocolState) notification(method string, raw json.RawMessage) error {
	if _, allowed := serverNotificationMethods[method]; !allowed {
		return errors.New("Codex app-server notification method is not allowed")
	}
	if _, err := decodeObject(raw, notificationSchema(method)); err != nil {
		return errors.New("Codex app-server notification is invalid")
	}
	switch method {
	case "thread/started":
		fields, _ := decodeObject(raw, notificationSchema(method))
		threadID, path, err := parseThread(fields["thread"])
		if err != nil || state.threadID == "" || threadID != state.threadID ||
			(state.threadPath != "" && path != "" && path != state.threadPath) {
			return errors.New("Codex app-server thread started notification is invalid")
		}
	case "turn/started":
		fields, _ := decodeObject(raw, notificationSchema(method))
		threadID, err := decodeBoundedString(fields["threadId"], 128)
		turn, turnErr := parseTurn(fields["turn"])
		if err != nil || turnErr != nil || threadID != state.threadID || turn.id != state.turnID ||
			turn.status != "inProgress" || state.turnStarted != 0 {
			return errors.New("Codex app-server turn started notification is invalid")
		}
		state.turnStarted++
		return state.consumeItems(turn.items, false, 0)
	case "thread/tokenUsage/updated":
		fields, _ := decodeObject(raw, notificationSchema(method))
		if err := state.validateUsageTuple(fields); err != nil {
			return err
		}
		usage, err := parseTokenUsage(fields["tokenUsage"])
		if err != nil {
			return err
		}
		state.latestUsage = usage
	case "item/started", "item/completed":
		fields, _ := decodeObject(raw, notificationSchema(method))
		if err := state.validateTurnTuple(fields); err != nil {
			return err
		}
		timestampField := "startedAtMs"
		if method == "item/completed" {
			timestampField = "completedAtMs"
		}
		var timestamp int64
		if strictDecode(fields[timestampField], &timestamp) != nil || timestamp < 0 {
			return errors.New("Codex app-server item timestamp is invalid")
		}
		return state.consumeItem(fields["item"], method == "item/completed", timestamp)
	case "turn/completed":
		fields, _ := decodeObject(raw, notificationSchema(method))
		threadID, err := decodeBoundedString(fields["threadId"], 128)
		turn, turnErr := parseTurn(fields["turn"])
		if err != nil || turnErr != nil || threadID != state.threadID || turn.id != state.turnID || state.terminals != 0 {
			return errors.New("Codex app-server terminal notification is invalid")
		}
		state.terminals++
		if err := state.consumeItems(turn.items, true, 0); err != nil {
			return err
		}
		return state.complete(turn)
	case "error":
		fields, _ := decodeObject(raw, notificationSchema(method))
		if err := state.validateTurnTuple(fields); err != nil {
			return err
		}
		if _, err := parseTurnError(fields["error"]); err != nil {
			return errors.New("Codex app-server error notification is invalid")
		}
		var willRetry bool
		if strictDecode(fields["willRetry"], &willRetry) != nil {
			return errors.New("Codex app-server error retry flag is invalid")
		}
	case "warning":
		fields, _ := decodeObject(raw, notificationSchema(method))
		_, err := decodeBoundedString(fields["message"], maximumDiagnosticBytes)
		return err
	case "configWarning":
		fields, _ := decodeObject(raw, notificationSchema(method))
		_, err := decodeBoundedString(fields["summary"], maximumDiagnosticBytes)
		return err
	}
	return nil
}

func (state *protocolState) validateUsageTuple(fields map[string]json.RawMessage) error {
	threadID, threadErr := decodeBoundedString(fields["threadId"], 128)
	turnID, turnErr := decodeBoundedString(fields["turnId"], 128)
	if threadErr != nil || turnErr != nil || uuid.Validate(turnID) != nil || threadID != state.threadID ||
		(state.turnID != "" && turnID != state.turnID) {
		return errors.New("Codex app-server token usage tuple is invalid")
	}
	return nil
}

func parseTokenUsage(raw json.RawMessage) (runtimecontract.TokenUsage, error) {
	fields, err := decodeObject(raw, schema([]string{"last", "total"}, "last", "modelContextWindow", "total"))
	if err != nil {
		return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage is invalid")
	}
	var contextWindow int64
	if rawWindow, present := fields["modelContextWindow"]; present && !bytes.Equal(rawWindow, []byte("null")) {
		if strictDecode(rawWindow, &contextWindow) != nil || contextWindow < 0 {
			return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage is invalid")
		}
	}
	total, err := parseTokenUsageBreakdown(fields["total"], contextWindow)
	if err != nil {
		return runtimecontract.TokenUsage{}, err
	}
	last, err := parseTokenUsageBreakdown(fields["last"], contextWindow)
	if err != nil || last.TotalTokens > total.TotalTokens || last.InputTokens > total.InputTokens ||
		last.CachedInputTokens > total.CachedInputTokens || last.CacheWriteInputTokens > total.CacheWriteInputTokens ||
		last.OutputTokens > total.OutputTokens || last.ReasoningOutputTokens > total.ReasoningOutputTokens {
		return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage is invalid")
	}
	return total, nil
}

func parseTokenUsageBreakdown(raw json.RawMessage, contextWindow int64) (runtimecontract.TokenUsage, error) {
	fields, err := decodeObject(raw, schema(
		[]string{"cachedInputTokens", "inputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens"},
		"cachedInputTokens", "inputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens",
	))
	if err != nil {
		return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage breakdown is invalid")
	}
	usage := runtimecontract.TokenUsage{ModelContextWindow: contextWindow}
	values := []struct {
		raw    json.RawMessage
		target *int64
	}{
		{fields["totalTokens"], &usage.TotalTokens},
		{fields["inputTokens"], &usage.InputTokens},
		{fields["cachedInputTokens"], &usage.CachedInputTokens},
		{fields["outputTokens"], &usage.OutputTokens},
		{fields["reasoningOutputTokens"], &usage.ReasoningOutputTokens},
	}
	for _, value := range values {
		if strictDecode(value.raw, value.target) != nil {
			return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage breakdown is invalid")
		}
	}
	if usage.Validate() != nil {
		return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage breakdown is invalid")
	}
	return usage, nil
}

func tokenUsageDelta(final, baseline runtimecontract.TokenUsage) (runtimecontract.TokenUsage, error) {
	delta := runtimecontract.TokenUsage{
		TotalTokens:           nonNegativeDelta(final.TotalTokens, baseline.TotalTokens),
		InputTokens:           nonNegativeDelta(final.InputTokens, baseline.InputTokens),
		CachedInputTokens:     nonNegativeDelta(final.CachedInputTokens, baseline.CachedInputTokens),
		CacheWriteInputTokens: nonNegativeDelta(final.CacheWriteInputTokens, baseline.CacheWriteInputTokens),
		OutputTokens:          nonNegativeDelta(final.OutputTokens, baseline.OutputTokens),
		ReasoningOutputTokens: nonNegativeDelta(final.ReasoningOutputTokens, baseline.ReasoningOutputTokens),
		ModelContextWindow:    final.ModelContextWindow,
	}
	if delta.Validate() != nil {
		return runtimecontract.TokenUsage{}, errors.New("Codex app-server token usage delta is invalid")
	}
	return delta, nil
}

func nonNegativeDelta(final, baseline int64) int64 {
	if final <= baseline {
		return 0
	}
	return final - baseline
}

func (state *protocolState) validateTurnTuple(fields map[string]json.RawMessage) error {
	threadID, threadErr := decodeBoundedString(fields["threadId"], 128)
	turnID, turnErr := decodeBoundedString(fields["turnId"], 128)
	if threadErr != nil || turnErr != nil || threadID != state.threadID || turnID != state.turnID {
		return errors.New("Codex app-server notification tuple is invalid")
	}
	return nil
}

type parsedTurn struct {
	id         string
	status     string
	items      []json.RawMessage
	errorValue json.RawMessage
}

func parseTurn(raw json.RawMessage) (parsedTurn, error) {
	fields, err := decodeObject(raw, schema([]string{"id", "items", "status"}, "completedAt", "durationMs", "error", "id",
		"items", "itemsView", "startedAt", "status"))
	if err != nil {
		return parsedTurn{}, err
	}
	id, idErr := decodeBoundedString(fields["id"], 128)
	status, statusErr := decodeBoundedString(fields["status"], 64)
	if idErr != nil || statusErr != nil || uuid.Validate(id) != nil || !closedTurnStatus(status) {
		return parsedTurn{}, errors.New("Codex app-server turn is invalid")
	}
	var items []json.RawMessage
	if strictDecode(fields["items"], &items) != nil || len(items) > 10_000 {
		return parsedTurn{}, errors.New("Codex app-server turn items are invalid")
	}
	var errorValue json.RawMessage
	if rawError, present := fields["error"]; present && !bytes.Equal(rawError, []byte("null")) {
		errorValue = rawError
	}
	return parsedTurn{id: id, status: status, items: items, errorValue: errorValue}, nil
}

func (state *protocolState) consumeItems(items []json.RawMessage, authoritative bool, timestampMS int64) error {
	for _, item := range items {
		if err := state.consumeItem(item, authoritative, timestampMS); err != nil {
			return err
		}
	}
	return nil
}

func (state *protocolState) consumeItem(raw json.RawMessage, authoritative bool, timestampMS int64) error {
	base, err := decodeObject(raw, schema([]string{"id", "type"}, itemFieldUniverse...))
	if err != nil {
		return errors.New("Codex app-server thread item is invalid")
	}
	typeName, typeErr := decodeBoundedString(base["type"], 128)
	itemSchema, allowed := threadItemSchemas[typeName]
	if typeErr != nil || !allowed {
		return errors.New("Codex app-server thread item type is invalid")
	}
	fields, err := decodeObject(raw, itemSchema)
	if err != nil {
		return errors.New("Codex app-server tagged thread item is invalid")
	}
	id, idErr := decodeBoundedString(fields["id"], 256)
	if idErr != nil {
		return errors.New("Codex app-server thread item id is invalid")
	}
	if typeName == "mcpToolCall" {
		// MCP callbacks уже проецируются runtime-controller вместе с capability
		// и grant. Повторная запись app-server item создала бы дубль аудита.
		return nil
	}
	if typeName != "agentMessage" {
		if _, supported := nativeToolItemKinds[typeName]; !supported {
			return nil
		}
		if !nativeToolCallIDPattern.MatchString(id) {
			return errors.New("Codex app-server native tool item id is invalid")
		}
		if !authoritative {
			if timestampMS > 0 {
				state.itemStartedAtMS[id] = timestampMS
			}
			return nil
		}
		call, terminal, parseErr := state.parseNativeToolCall(typeName, id, fields, timestampMS)
		if parseErr != nil {
			return parseErr
		}
		if !terminal {
			return nil
		}
		return state.recordNativeToolCall(call)
	}
	var text string
	textErr := strictDecode(fields["text"], &text)
	if len(text) > maximumFinalBytes || !utf8.ValidString(text) {
		textErr = errors.New("Codex app-server agent message text is invalid")
	}
	phase := ""
	if rawPhase, present := fields["phase"]; present && !bytes.Equal(rawPhase, []byte("null")) {
		phase, err = decodeBoundedString(rawPhase, 64)
		if err != nil || (phase != "commentary" && phase != "final_answer") {
			return errors.New("Codex app-server agent message phase is invalid")
		}
	}
	if idErr != nil || textErr != nil || id == "" {
		return errors.New("Codex app-server agent message is invalid")
	}
	if !authoritative || text == "" {
		return nil
	}
	value := agentMessage{text: text, phase: phase}
	if previous, duplicate := state.agentMessages[id]; duplicate && previous != value {
		return errors.New("Codex app-server agent message changed after completion")
	}
	state.agentMessages[id] = value
	if phase == "final_answer" {
		if state.finalID != "" && state.finalID != id {
			return errors.New("Codex app-server emitted duplicate final messages")
		}
		state.finalID = id
	} else if phase == "" {
		state.fallbackID = id
	}
	return nil
}

func (state *protocolState) parseNativeToolCall(typeName, id string, fields map[string]json.RawMessage, completedAtMS int64) (runtimecontract.NativeToolCall, bool, error) {
	call := runtimecontract.NativeToolCall{CallID: id}
	var durationPresent bool
	var err error
	switch typeName {
	case "commandExecution":
		call.Kind = runtimecontract.NativeToolKindShell
		statusValue, statusErr := decodeBoundedString(fields["status"], 32)
		call.State, call.SafeResult, _, err = terminalNativeState(statusValue, nil)
		if statusErr != nil || err != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server command status is invalid")
		}
		if statusValue == "inProgress" {
			return runtimecontract.NativeToolCall{}, false, nil
		}
		actions, actionErr := safeCommandActions(fields["commandActions"])
		cwd, cwdErr := decodeBoundedString(fields["cwd"], 4096)
		source := "AGENT"
		if rawSource, present := fields["source"]; present {
			source, err = safeCommandSource(rawSource)
		}
		exitCode := "UNAVAILABLE"
		if rawExit, present := fields["exitCode"]; present && !bytes.Equal(rawExit, []byte("null")) {
			var value int32
			if strictDecode(rawExit, &value) != nil {
				return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server command exit code is invalid")
			}
			if value == 0 {
				exitCode = "ZERO"
			} else {
				exitCode = "NONZERO"
				call.State, call.SafeResult = runtimecontract.NativeToolStateFailed, runtimecontract.NativeToolResultFailed
			}
		}
		if _, commandErr := decodeBoundedString(fields["command"], maximumJSONLLineBytes); commandErr != nil || actionErr != nil || cwdErr != nil || err != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server command item is invalid")
		}
		call.SafeParameters = map[string]any{"action_count": actions.count, "action_kinds": actions.kinds,
			"cwd_scope": state.workspaceScope(cwd), "exit_code": exitCode, "source": source}
		call.DurationMS, durationPresent, err = optionalDuration(fields, "durationMs")
	case "fileChange":
		call.Kind = runtimecontract.NativeToolKindFileChange
		statusValue, statusErr := decodeBoundedString(fields["status"], 32)
		call.State, call.SafeResult, _, err = terminalNativeState(statusValue, nil)
		if statusErr != nil || err != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server file change status is invalid")
		}
		if statusValue == "inProgress" {
			return runtimecontract.NativeToolCall{}, false, nil
		}
		changes, changeErr := state.safeFileChanges(fields["changes"])
		if changeErr != nil {
			return runtimecontract.NativeToolCall{}, false, changeErr
		}
		call.SafeParameters = map[string]any{"change_count": changes.count, "change_kinds": changes.kinds,
			"paths": changes.paths, "paths_truncated": changes.truncated}
	case "webSearch":
		call.Kind, call.State, call.SafeResult = runtimecontract.NativeToolKindWebSearch, runtimecontract.NativeToolStateSucceeded, runtimecontract.NativeToolResultCompleted
		action, count, actionErr := safeWebSearch(fields)
		if actionErr != nil {
			return runtimecontract.NativeToolCall{}, false, actionErr
		}
		call.SafeParameters = map[string]any{"action": action, "query_count": count}
	case "dynamicToolCall":
		call.Kind = runtimecontract.NativeToolKindDynamicTool
		statusValue, statusErr := decodeBoundedString(fields["status"], 32)
		var success *bool
		if rawSuccess, present := fields["success"]; present && !bytes.Equal(rawSuccess, []byte("null")) {
			var value bool
			if strictDecode(rawSuccess, &value) != nil {
				return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server dynamic tool result is invalid")
			}
			success = &value
		}
		call.State, call.SafeResult, _, err = terminalNativeState(statusValue, success)
		if statusErr != nil || err != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server dynamic tool status is invalid")
		}
		if statusValue == "inProgress" {
			return runtimecontract.NativeToolCall{}, false, nil
		}
		tool, toolErr := safeNativeLabel(fields["tool"])
		namespace := "UNSPECIFIED"
		if rawNamespace, present := fields["namespace"]; present && !bytes.Equal(rawNamespace, []byte("null")) {
			namespace, err = safeNativeLabel(rawNamespace)
		}
		shape, count, argumentErr := safeArgumentShape(fields["arguments"])
		if toolErr != nil || err != nil || argumentErr != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server dynamic tool metadata is invalid")
		}
		call.SafeParameters = map[string]any{"argument_count": count, "argument_shape": shape, "namespace": namespace, "tool": tool}
		call.DurationMS, durationPresent, err = optionalDuration(fields, "durationMs")
	case "imageView":
		call.Kind, call.State, call.SafeResult = runtimecontract.NativeToolKindImageView, runtimecontract.NativeToolStateSucceeded, runtimecontract.NativeToolResultCompleted
		pathValue, pathErr := decodeBoundedString(fields["path"], 4096)
		if pathErr != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server image view path is invalid")
		}
		safePath, _ := state.safeWorkspacePath(pathValue)
		call.SafeParameters = map[string]any{"path": safePath}
	case "sleep":
		call.Kind, call.State, call.SafeResult = runtimecontract.NativeToolKindSleep, runtimecontract.NativeToolStateSucceeded, runtimecontract.NativeToolResultCompleted
		var duration uint64
		if strictDecode(fields["durationMs"], &duration) != nil || duration > 86_400_000 {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server sleep duration is invalid")
		}
		call.DurationMS, durationPresent = int64(duration), true
		call.SafeParameters = map[string]any{"requested_duration_ms": int64(duration)}
	case "imageGeneration":
		call.Kind = runtimecontract.NativeToolKindImageGeneration
		statusValue, statusErr := decodeBoundedString(fields["status"], 32)
		call.State, call.SafeResult, _, err = terminalNativeState(statusValue, nil)
		if statusErr != nil || err != nil {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server image generation status is invalid")
		}
		if statusValue == "inProgress" {
			return runtimecontract.NativeToolCall{}, false, nil
		}
		var result string
		if strictDecode(fields["result"], &result) != nil || len(result) > maximumJSONLLineBytes || !utf8.ValidString(result) {
			return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server image generation result is invalid")
		}
		safePath := ""
		if rawPath, present := fields["savedPath"]; present && !bytes.Equal(rawPath, []byte("null")) {
			pathValue, pathErr := decodeBoundedString(rawPath, 4096)
			if pathErr != nil {
				return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server image generation path is invalid")
			}
			safePath, _ = state.safeWorkspacePath(pathValue)
		}
		call.SafeParameters = map[string]any{"output_available": result != "" || safePath != "", "path": safePath}
	default:
		return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server native tool item is unsupported")
	}
	if err != nil {
		return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server native tool duration is invalid")
	}
	if !durationPresent {
		if startedAt, ok := state.itemStartedAtMS[id]; ok && completedAtMS >= startedAt {
			call.DurationMS = completedAtMS - startedAt
		}
	}
	delete(state.itemStartedAtMS, id)
	if call.Validate() != nil {
		return runtimecontract.NativeToolCall{}, false, errors.New("Codex app-server native tool projection is invalid")
	}
	return call, true, nil
}

func (state *protocolState) recordNativeToolCall(call runtimecontract.NativeToolCall) error {
	if previous, exists := state.toolCalls[call.CallID]; exists {
		previousDuration, currentDuration := previous.DurationMS, call.DurationMS
		previous.DurationMS, call.DurationMS = 0, 0
		if !reflect.DeepEqual(previous, call) || previousDuration > 0 && currentDuration > 0 && previousDuration != currentDuration {
			return errors.New("Codex app-server native tool item changed after completion")
		}
		if previousDuration == 0 && currentDuration > 0 {
			call.DurationMS = currentDuration
			state.toolCalls[call.CallID] = call
		}
		return nil
	}
	if len(state.toolCallOrder) >= runtimecontract.MaximumNativeToolCalls {
		return errors.New("Codex app-server native tool item limit exceeded")
	}
	state.toolCalls[call.CallID] = call
	state.toolCallOrder = append(state.toolCallOrder, call.CallID)
	return nil
}

type safeActionSummary struct {
	count int
	kinds []string
}

func safeCommandActions(raw json.RawMessage) (safeActionSummary, error) {
	var items []json.RawMessage
	if strictDecode(raw, &items) != nil || len(items) > 256 {
		return safeActionSummary{}, errors.New("Codex app-server command actions are invalid")
	}
	seen := make(map[string]struct{})
	result := safeActionSummary{count: len(items)}
	for _, item := range items {
		fields, err := decodeObject(item, schema([]string{"command", "type"}, "command", "name", "path", "query", "type"))
		if err != nil {
			return safeActionSummary{}, errors.New("Codex app-server command action is invalid")
		}
		if _, err := decodeBoundedString(fields["command"], maximumJSONLLineBytes); err != nil {
			return safeActionSummary{}, errors.New("Codex app-server command action is invalid")
		}
		kind, err := decodeBoundedString(fields["type"], 32)
		mapped := map[string]string{"read": "READ", "listFiles": "LIST_FILES", "search": "SEARCH", "unknown": "UNKNOWN"}[kind]
		if err != nil || mapped == "" {
			return safeActionSummary{}, errors.New("Codex app-server command action kind is invalid")
		}
		for _, optional := range []string{"name", "path", "query"} {
			if value, present := fields[optional]; present && !bytes.Equal(value, []byte("null")) {
				if _, err := decodeBoundedString(value, 4096); err != nil {
					return safeActionSummary{}, errors.New("Codex app-server command action metadata is invalid")
				}
			}
		}
		seen[mapped] = struct{}{}
	}
	for kind := range seen {
		result.kinds = append(result.kinds, kind)
	}
	sort.Strings(result.kinds)
	return result, nil
}

type safeFileChangeSummary struct {
	count     int
	kinds     []string
	paths     []string
	truncated bool
}

func (state *protocolState) safeFileChanges(raw json.RawMessage) (safeFileChangeSummary, error) {
	var items []json.RawMessage
	if strictDecode(raw, &items) != nil || len(items) > 10_000 {
		return safeFileChangeSummary{}, errors.New("Codex app-server file changes are invalid")
	}
	result := safeFileChangeSummary{count: len(items)}
	seenKinds := make(map[string]struct{})
	for _, item := range items {
		fields, err := decodeObject(item, schema([]string{"diff", "kind", "path"}, "diff", "kind", "path"))
		if err != nil {
			return safeFileChangeSummary{}, errors.New("Codex app-server file change is invalid")
		}
		var diff string
		pathValue, pathErr := decodeBoundedString(fields["path"], 4096)
		if strictDecode(fields["diff"], &diff) != nil || len(diff) > maximumJSONLLineBytes || !utf8.ValidString(diff) || pathErr != nil {
			return safeFileChangeSummary{}, errors.New("Codex app-server file change is invalid")
		}
		kindFields, kindErr := decodeObject(fields["kind"], schema([]string{"type"}, "move_path", "type"))
		kind, typeErr := decodeBoundedString(kindFields["type"], 32)
		mapped := map[string]string{"add": "ADD", "update": "UPDATE", "delete": "DELETE"}[kind]
		if kindErr != nil || typeErr != nil || mapped == "" {
			return safeFileChangeSummary{}, errors.New("Codex app-server file change kind is invalid")
		}
		if movePath, present := kindFields["move_path"]; present && !bytes.Equal(movePath, []byte("null")) {
			if kind != "update" {
				return safeFileChangeSummary{}, errors.New("Codex app-server file move is invalid")
			}
			if _, err := decodeBoundedString(movePath, 4096); err != nil {
				return safeFileChangeSummary{}, errors.New("Codex app-server file move is invalid")
			}
		}
		seenKinds[mapped] = struct{}{}
		if safePath, ok := state.safeWorkspacePath(pathValue); ok {
			if len(result.paths) < 20 {
				result.paths = append(result.paths, safePath)
			} else {
				result.truncated = true
			}
		}
	}
	for kind := range seenKinds {
		result.kinds = append(result.kinds, kind)
	}
	sort.Strings(result.kinds)
	return result, nil
}

func safeWebSearch(fields map[string]json.RawMessage) (string, int, error) {
	query, err := decodeBoundedString(fields["query"], 64<<10)
	if err != nil {
		return "", 0, errors.New("Codex app-server web search query is invalid")
	}
	action, count := "UNSPECIFIED", 1
	rawAction, present := fields["action"]
	if !present || bytes.Equal(rawAction, []byte("null")) {
		_ = query
		return action, count, nil
	}
	actionFields, err := decodeObject(rawAction, schema([]string{"type"}, "pattern", "queries", "query", "type", "url"))
	if err != nil {
		return "", 0, errors.New("Codex app-server web search action is invalid")
	}
	typeValue, err := decodeBoundedString(actionFields["type"], 32)
	action = map[string]string{"search": "SEARCH", "openPage": "OPEN_PAGE", "findInPage": "FIND_IN_PAGE", "other": "OTHER"}[typeValue]
	if err != nil || action == "" {
		return "", 0, errors.New("Codex app-server web search action kind is invalid")
	}
	for _, key := range []string{"pattern", "query", "url"} {
		if value, exists := actionFields[key]; exists && !bytes.Equal(value, []byte("null")) {
			if _, err := decodeBoundedString(value, 64<<10); err != nil {
				return "", 0, errors.New("Codex app-server web search action metadata is invalid")
			}
		}
	}
	if values, exists := actionFields["queries"]; exists && !bytes.Equal(values, []byte("null")) {
		var queries []string
		if strictDecode(values, &queries) != nil || len(queries) > 100 {
			return "", 0, errors.New("Codex app-server web search queries are invalid")
		}
		for _, value := range queries {
			if value == "" || len(value) > 64<<10 || !utf8.ValidString(value) {
				return "", 0, errors.New("Codex app-server web search queries are invalid")
			}
		}
		if len(queries) > 0 {
			count = len(queries)
		}
	}
	return action, count, nil
}

func safeArgumentShape(raw json.RawMessage) (string, int, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || ensureEOF(decoder) != nil {
		return "", 0, errors.New("Codex app-server dynamic tool arguments are invalid")
	}
	switch typed := value.(type) {
	case nil:
		return "NULL", 0, nil
	case map[string]any:
		if len(typed) > 10_000 {
			return "", 0, errors.New("Codex app-server dynamic tool arguments are invalid")
		}
		return "OBJECT", len(typed), nil
	case []any:
		if len(typed) > 10_000 {
			return "", 0, errors.New("Codex app-server dynamic tool arguments are invalid")
		}
		return "ARRAY", len(typed), nil
	default:
		return "SCALAR", 1, nil
	}
}

func safeNativeLabel(raw json.RawMessage) (string, error) {
	value, err := decodeBoundedString(raw, 128)
	if err != nil || !nativeSafeLabelPattern.MatchString(value) {
		return "", errors.New("Codex app-server native tool label is invalid")
	}
	return value, nil
}

func safeCommandSource(raw json.RawMessage) (string, error) {
	value, err := decodeBoundedString(raw, 64)
	mapped := map[string]string{"agent": "AGENT", "userShell": "USER_SHELL", "unifiedExecStartup": "UNIFIED_EXEC_STARTUP", "unifiedExecInteraction": "UNIFIED_EXEC_INTERACTION"}[value]
	if err != nil || mapped == "" {
		return "", errors.New("Codex app-server command source is invalid")
	}
	return mapped, nil
}

func terminalNativeState(statusValue string, success *bool) (string, string, bool, error) {
	switch statusValue {
	case "inProgress":
		return "", "", false, nil
	case "completed":
		if success != nil && !*success {
			return runtimecontract.NativeToolStateFailed, runtimecontract.NativeToolResultFailed, true, nil
		}
		return runtimecontract.NativeToolStateSucceeded, runtimecontract.NativeToolResultCompleted, true, nil
	case "failed":
		return runtimecontract.NativeToolStateFailed, runtimecontract.NativeToolResultFailed, true, nil
	case "declined":
		return runtimecontract.NativeToolStateFailed, runtimecontract.NativeToolResultDeclined, true, nil
	default:
		return "", "", false, errors.New("native tool status is invalid")
	}
}

func optionalDuration(fields map[string]json.RawMessage, key string) (int64, bool, error) {
	raw, present := fields[key]
	if !present || bytes.Equal(raw, []byte("null")) {
		return 0, false, nil
	}
	var value int64
	if strictDecode(raw, &value) != nil || value < 0 || value > 86_400_000 {
		return 0, false, errors.New("native tool duration is invalid")
	}
	return value, true, nil
}

func (state *protocolState) workspaceScope(pathValue string) string {
	if _, ok := state.safeWorkspacePath(pathValue); ok {
		return "WORKSPACE"
	}
	return "OUTSIDE_WORKSPACE"
}

func (state *protocolState) safeWorkspacePath(pathValue string) (string, bool) {
	if state.workspaceRoot == "" || pathValue == "" || !utf8.ValidString(pathValue) {
		return "", false
	}
	cleanRoot := filepath.Clean(state.workspaceRoot)
	cleanPath := filepath.Clean(pathValue)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(cleanRoot, cleanPath)
	}
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	relative = filepath.ToSlash(relative)
	if len(relative) > 240 || strings.ContainsRune(relative, '\x00') {
		return "", false
	}
	return relative, true
}

func (state *protocolState) complete(turn parsedTurn) error {
	if state.turnStarted != 1 {
		return errors.New("Codex app-server turn lifecycle is incomplete")
	}
	switch turn.status {
	case "completed":
		if turn.errorValue != nil {
			return errors.New("Codex app-server successful turn carries an error")
		}
		messageID := state.finalID
		if messageID == "" {
			messageID = state.fallbackID
		}
		message, exists := state.agentMessages[messageID]
		if !exists || message.text == "" {
			return errors.New("Codex app-server completed without a final message")
		}
		state.result.FinalMessage = message.text
		state.result.Outcome = "SUCCEEDED"
	case "failed":
		if turn.errorValue == nil {
			state.result.FailureCode = "provider_error_info_invalid"
		} else {
			errorValue, err := parseTurnError(turn.errorValue)
			if err != nil {
				state.result.FailureCode = "provider_error_info_invalid"
			} else {
				state.result.FailureCode = classifyCodexErrorInfo(errorValue.codexErrorInfo)
			}
		}
		state.result.Outcome = "FAILED"
	case "interrupted":
		state.result.Outcome = "FAILED"
		state.result.FailureCode = "provider_interrupted"
	default:
		return errors.New("Codex app-server terminal status is invalid")
	}
	return nil
}

type parsedTurnError struct {
	codexErrorInfo json.RawMessage
}

func parseTurnError(raw json.RawMessage) (parsedTurnError, error) {
	fields, err := decodeObject(raw, schema([]string{"message"}, "additionalDetails", "codexErrorInfo", "message"))
	if err != nil {
		return parsedTurnError{}, err
	}
	if _, err := decodeBoundedString(fields["message"], maximumDiagnosticBytes); err != nil {
		return parsedTurnError{}, err
	}
	if details, present := fields["additionalDetails"]; present && !bytes.Equal(details, []byte("null")) {
		if _, err := decodeBoundedString(details, maximumDiagnosticBytes); err != nil {
			return parsedTurnError{}, err
		}
	}
	info := fields["codexErrorInfo"]
	if bytes.Equal(info, []byte("null")) {
		info = nil
	}
	return parsedTurnError{codexErrorInfo: info}, nil
}

func classifyCodexErrorInfo(raw json.RawMessage) string {
	code, valid := parseCodexErrorInfo(raw)
	if !valid {
		return "provider_error_info_invalid"
	}
	return code
}

func parseCodexErrorInfo(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	if value, err := decodeBoundedString(raw, 128); err == nil {
		switch value {
		case "serverOverloaded":
			return "server_overloaded", true
		case "usageLimitExceeded":
			return "usage_limit_exceeded", true
		case "unauthorized":
			return "unauthorized", true
		case "cyberPolicy":
			return "cyber_policy", true
		case "contextWindowExceeded":
			return "context_window_exceeded", true
		case "sessionBudgetExceeded":
			return "session_budget_exceeded", true
		case "internalServerError":
			return "provider_internal_error", true
		case "badRequest":
			return "provider_bad_request", true
		case "threadRollbackFailed":
			return "thread_rollback_failed", true
		case "sandboxError":
			return "provider_sandbox_error", true
		case "other":
			return "provider_other_error", true
		default:
			return "", false
		}
	}
	fields, err := decodeObject(raw, schema(nil, "activeTurnNotSteerable", "httpConnectionFailed",
		"responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts"))
	if err != nil || len(fields) != 1 {
		return "", false
	}
	for name, value := range fields {
		switch name {
		case "activeTurnNotSteerable":
			details, decodeErr := decodeObject(value, schema([]string{"turnKind"}, "turnKind"))
			kind, kindErr := decodeBoundedString(details["turnKind"], 64)
			if decodeErr != nil || kindErr != nil || (kind != "review" && kind != "compact") {
				return "", false
			}
			return "active_turn_not_steerable", true
		default:
			details, decodeErr := decodeObject(value, schema(nil, "httpStatusCode"))
			if decodeErr != nil {
				return "", false
			}
			if status, present := details["httpStatusCode"]; present && !bytes.Equal(status, []byte("null")) {
				var code uint16
				if strictDecode(status, &code) != nil {
					return "", false
				}
			}
			return "provider_transport_failure", true
		}
	}
	return "", false
}

func (state *protocolState) terminalResult() (Result, error) {
	if state.terminals != 1 || state.result.SessionID == "" || state.result.Outcome == "" || state.threadPath == "" || !state.baselineCaptured {
		return Result{}, errors.New("Codex app-server lifecycle is incomplete")
	}
	usage, err := tokenUsageDelta(state.latestUsage, state.usageBaseline)
	if err != nil {
		return Result{}, err
	}
	state.result.Usage = usage
	state.result.ToolCalls = make([]runtimecontract.NativeToolCall, 0, len(state.toolCallOrder))
	for _, callID := range state.toolCallOrder {
		state.result.ToolCalls = append(state.result.ToolCalls, state.toolCalls[callID])
	}
	return state.result, nil
}

func closedTurnStatus(value string) bool {
	return value == "completed" || value == "interrupted" || value == "failed" || value == "inProgress"
}

func decodeObject(raw []byte, expected objectSchema) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("JSON value is not an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok {
			return nil, errors.New("JSON object key is invalid")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("JSON object key is duplicated")
		}
		if _, allowed := expected.allowed[key]; !allowed {
			return nil, fmt.Errorf("JSON object field %q is not allowed", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || len(value) == 0 {
			return nil, errors.New("JSON object value is invalid")
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("JSON object is incomplete")
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	for required := range expected.required {
		if _, present := fields[required]; !present {
			return nil, fmt.Errorf("JSON object field %q is required", required)
		}
	}
	return fields, nil
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func ensureEOF(decoder *json.Decoder) error {
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("JSON value has trailing data")
	}
	return nil
}

func decodeBoundedString(raw []byte, maximum int) (string, error) {
	var value string
	if strictDecode(raw, &value) != nil || value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return "", errors.New("JSON string is invalid")
	}
	return value, nil
}

func notificationSchema(method string) objectSchema {
	return notificationSchemas[method]
}

func CapacityFailure(code string) bool {
	return code == "server_overloaded"
}

// TerminalPresentation is a closed mapping from typed app-server outcome.
// Provider diagnostics never participate in a lifecycle transition or user text.
func TerminalPresentation(code string) (outcome, markdown, nextAction string) {
	switch code {
	case "unauthorized", "authentication_required", "authentication_expired":
		return "BLOCKED", "i18n:PROVIDER_AUTHENTICATION_REQUIRED", "REAUTH_DEVICE_CODE"
	case "usage_limit_exceeded":
		return "BLOCKED", "i18n:PROVIDER_USAGE_LIMIT_EXCEEDED", "CHECK_PROVIDER_QUOTA"
	case "server_overloaded":
		return "FAILED", "i18n:PROVIDER_OVERLOADED", "RETRY_LATER"
	case "cyber_policy", "policy_denied":
		return "BLOCKED", "i18n:PROVIDER_POLICY_DENIED", "REVIEW_POLICY"
	case "invalid_configuration", "stale_grant":
		return "BLOCKED", "i18n:RUNTIME_CONFIGURATION_STALE", "CREATE_FRESH_TURN"
	case "provider_error_info_invalid", "provider_interrupted", "":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "RETRY_FRESH_TURN"
	case "provider_other_error":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "RETRY_FRESH_TURN"
	case "provider_internal_error", "provider_transport_failure":
		return "FAILED", "i18n:PROVIDER_OVERLOADED", "RETRY_LATER"
	case "provider_bad_request", "provider_sandbox_error":
		return "BLOCKED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "REVIEW_CONFIGURATION"
	case "context_window_exceeded", "session_budget_exceeded":
		return "BLOCKED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "CREATE_FRESH_SESSION"
	case "thread_rollback_failed", "active_turn_not_steerable":
		return "FAILED", "i18n:PROVIDER_RESULT_UNVERIFIABLE", "CREATE_FRESH_TURN"
	default:
		return "FAILED", "i18n:PROVIDER_RESULT_UNKNOWN", "RETRY_FRESH_TURN"
	}
}

func BlockedFailure(code string) bool {
	switch code {
	case "unauthorized", "cyber_policy", "usage_limit_exceeded", "provider_error_info_invalid",
		"authentication_required", "authentication_expired", "policy_denied", "invalid_configuration", "stale_grant":
		return true
	default:
		return false
	}
}

var serverNotificationMethods = stringSet(
	"error", "thread/started", "thread/status/changed", "thread/archived", "thread/deleted", "thread/unarchived",
	"thread/closed", "skills/changed", "thread/name/updated", "thread/goal/updated", "thread/goal/cleared",
	"thread/settings/updated", "thread/tokenUsage/updated", "turn/started", "hook/started", "turn/completed",
	"hook/completed", "turn/diff/updated", "turn/plan/updated", "item/started", "item/autoApprovalReview/started",
	"item/autoApprovalReview/completed", "item/completed", "item/agentMessage/delta", "item/plan/delta",
	"command/exec/outputDelta", "process/outputDelta", "process/exited", "item/commandExecution/outputDelta",
	"item/commandExecution/terminalInteraction", "item/fileChange/outputDelta", "item/fileChange/patchUpdated",
	"serverRequest/resolved", "item/mcpToolCall/progress", "mcpServer/oauthLogin/completed",
	"mcpServer/startupStatus/updated", "account/updated", "account/rateLimits/updated", "app/list/updated",
	"remoteControl/status/changed", "externalAgentConfig/import/progress", "externalAgentConfig/import/completed",
	"fs/changed", "item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded", "item/reasoning/textDelta",
	"thread/compacted", "model/rerouted", "model/verification", "turn/moderationMetadata",
	"model/safetyBuffering/updated", "warning", "guardianWarning", "deprecationNotice", "configWarning",
	"fuzzyFileSearch/sessionUpdated", "fuzzyFileSearch/sessionCompleted", "thread/realtime/started",
	"thread/realtime/itemAdded", "thread/realtime/transcript/delta", "thread/realtime/transcript/done",
	"thread/realtime/outputAudio/delta", "thread/realtime/sdp", "thread/realtime/error", "thread/realtime/closed",
	"windows/worldWritableWarning", "windowsSandbox/setupCompleted", "account/login/completed",
)

var serverRequestMethods = stringSet(
	"item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/tool/requestUserInput",
	"mcpServer/elicitation/request", "item/permissions/requestApproval", "item/tool/call",
	"account/chatgptAuthTokens/refresh", "attestation/generate", "applyPatchApproval", "execCommandApproval",
)

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var itemFieldUniverse = []string{
	"action", "agentPath", "agentThreadId", "agentsStates", "aggregatedOutput", "appContext", "arguments", "changes", "clientId",
	"command", "commandActions", "content", "contentItems", "cwd", "delivery", "durationMs", "error", "exitCode", "failure", "fragments", "id",
	"kind", "memoryCitation", "model", "mcpAppResourceUri", "namespace", "path", "phase", "pluginId", "processId",
	"prompt", "query", "readOnlyHint", "reasoningEffort", "receiverThreadIds", "result", "results", "review", "revisedPrompt", "savedPath", "scriptPath", "server",
	"senderThreadId", "source", "status", "success", "summary", "text", "tool", "transparentBackground", "type",
}

var notificationSchemas = map[string]objectSchema{
	"error":                                     schema([]string{"error", "threadId", "turnId", "willRetry"}, "error", "threadId", "turnId", "willRetry"),
	"thread/started":                            schema([]string{"thread"}, "thread"),
	"thread/status/changed":                     schema([]string{"status", "threadId"}, "status", "threadId"),
	"thread/archived":                           schema([]string{"threadId"}, "threadId"),
	"thread/deleted":                            schema([]string{"threadId"}, "threadId"),
	"thread/unarchived":                         schema([]string{"threadId"}, "threadId"),
	"thread/closed":                             schema([]string{"threadId"}, "threadId"),
	"skills/changed":                            schema(nil),
	"thread/name/updated":                       schema([]string{"threadId"}, "threadId", "threadName"),
	"thread/goal/updated":                       schema([]string{"goal", "threadId"}, "goal", "threadId", "turnId"),
	"thread/goal/cleared":                       schema([]string{"threadId"}, "threadId"),
	"thread/settings/updated":                   schema([]string{"threadId", "threadSettings"}, "threadId", "threadSettings"),
	"thread/tokenUsage/updated":                 schema([]string{"threadId", "tokenUsage", "turnId"}, "threadId", "tokenUsage", "turnId"),
	"turn/started":                              schema([]string{"threadId", "turn"}, "threadId", "turn"),
	"hook/started":                              schema([]string{"run", "threadId"}, "run", "threadId", "turnId"),
	"turn/completed":                            schema([]string{"threadId", "turn"}, "threadId", "turn"),
	"hook/completed":                            schema([]string{"run", "threadId"}, "run", "threadId", "turnId"),
	"turn/diff/updated":                         schema([]string{"diff", "threadId", "turnId"}, "diff", "threadId", "turnId"),
	"turn/plan/updated":                         schema([]string{"plan", "threadId", "turnId"}, "explanation", "plan", "threadId", "turnId"),
	"item/started":                              schema([]string{"item", "startedAtMs", "threadId", "turnId"}, "item", "startedAtMs", "threadId", "turnId"),
	"item/autoApprovalReview/started":           schema([]string{"action", "review", "reviewId", "startedAtMs", "threadId", "turnId"}, "action", "review", "reviewId", "startedAtMs", "targetItemId", "threadId", "turnId"),
	"item/autoApprovalReview/completed":         schema([]string{"action", "completedAtMs", "decisionSource", "review", "reviewId", "startedAtMs", "threadId", "turnId"}, "action", "completedAtMs", "decisionSource", "review", "reviewId", "startedAtMs", "targetItemId", "threadId", "turnId"),
	"item/completed":                            schema([]string{"completedAtMs", "item", "threadId", "turnId"}, "completedAtMs", "item", "threadId", "turnId"),
	"item/agentMessage/delta":                   schema([]string{"delta", "itemId", "threadId", "turnId"}, "delta", "itemId", "threadId", "turnId"),
	"item/plan/delta":                           schema([]string{"delta", "itemId", "threadId", "turnId"}, "delta", "itemId", "threadId", "turnId"),
	"command/exec/outputDelta":                  schema([]string{"capReached", "deltaBase64", "processId", "stream"}, "capReached", "deltaBase64", "processId", "stream"),
	"process/outputDelta":                       schema([]string{"capReached", "deltaBase64", "processHandle", "stream"}, "capReached", "deltaBase64", "processHandle", "stream"),
	"process/exited":                            schema([]string{"exitCode", "processHandle", "stderr", "stderrCapReached", "stdout", "stdoutCapReached"}, "exitCode", "processHandle", "stderr", "stderrCapReached", "stdout", "stdoutCapReached"),
	"item/commandExecution/outputDelta":         schema([]string{"delta", "itemId", "threadId", "turnId"}, "delta", "itemId", "threadId", "turnId"),
	"item/commandExecution/terminalInteraction": schema([]string{"itemId", "processId", "stdin", "threadId", "turnId"}, "itemId", "processId", "stdin", "threadId", "turnId"),
	"item/fileChange/outputDelta":               schema([]string{"delta", "itemId", "threadId", "turnId"}, "delta", "itemId", "threadId", "turnId"),
	"item/fileChange/patchUpdated":              schema([]string{"changes", "itemId", "threadId", "turnId"}, "changes", "itemId", "threadId", "turnId"),
	"serverRequest/resolved":                    schema([]string{"requestId", "threadId"}, "requestId", "threadId"),
	"item/mcpToolCall/progress":                 schema([]string{"itemId", "message", "threadId", "turnId"}, "itemId", "message", "threadId", "turnId"),
	"mcpServer/oauthLogin/completed":            schema([]string{"name", "success"}, "error", "name", "success", "threadId"),
	"mcpServer/startupStatus/updated":           schema([]string{"name", "status"}, "error", "failureReason", "name", "status", "threadId"),
	"account/updated":                           schema(nil, "authMode", "planType"),
	"account/rateLimits/updated":                schema([]string{"rateLimits"}, "rateLimits"),
	"app/list/updated":                          schema([]string{"data"}, "data"),
	"remoteControl/status/changed":              schema([]string{"installationId", "serverName", "status"}, "environmentId", "installationId", "serverName", "status"),
	"externalAgentConfig/import/progress":       schema([]string{"importId", "itemTypeResults"}, "importId", "itemTypeResults"),
	"externalAgentConfig/import/completed":      schema([]string{"importId", "itemTypeResults"}, "importId", "itemTypeResults"),
	"fs/changed":                                schema([]string{"changedPaths", "watchId"}, "changedPaths", "watchId"),
	"item/reasoning/summaryTextDelta":           schema([]string{"delta", "itemId", "summaryIndex", "threadId", "turnId"}, "delta", "itemId", "summaryIndex", "threadId", "turnId"),
	"item/reasoning/summaryPartAdded":           schema([]string{"itemId", "summaryIndex", "threadId", "turnId"}, "itemId", "summaryIndex", "threadId", "turnId"),
	"item/reasoning/textDelta":                  schema([]string{"contentIndex", "delta", "itemId", "threadId", "turnId"}, "contentIndex", "delta", "itemId", "threadId", "turnId"),
	"thread/compacted":                          schema([]string{"threadId", "turnId"}, "threadId", "turnId"),
	"model/rerouted":                            schema([]string{"fromModel", "reason", "threadId", "toModel", "turnId"}, "fromModel", "reason", "threadId", "toModel", "turnId"),
	"model/verification":                        schema([]string{"threadId", "turnId", "verifications"}, "threadId", "turnId", "verifications"),
	"turn/moderationMetadata":                   schema([]string{"metadata", "threadId", "turnId"}, "metadata", "threadId", "turnId"),
	"model/safetyBuffering/updated":             schema([]string{"model", "reasons", "showBufferingUi", "threadId", "turnId", "useCases"}, "fasterModel", "model", "reasons", "showBufferingUi", "threadId", "turnId", "useCases"),
	"warning":                                   schema([]string{"message"}, "message", "threadId"),
	"guardianWarning":                           schema([]string{"message", "threadId"}, "message", "threadId"),
	"deprecationNotice":                         schema([]string{"summary"}, "details", "summary"),
	"configWarning":                             schema([]string{"summary"}, "details", "path", "range", "summary"),
	"fuzzyFileSearch/sessionUpdated":            schema([]string{"files", "query", "sessionId"}, "files", "query", "sessionId"),
	"fuzzyFileSearch/sessionCompleted":          schema([]string{"sessionId"}, "sessionId"),
	"thread/realtime/started":                   schema([]string{"threadId", "version"}, "realtimeSessionId", "threadId", "version"),
	"thread/realtime/itemAdded":                 schema([]string{"item", "threadId"}, "item", "threadId"),
	"thread/realtime/transcript/delta":          schema([]string{"delta", "role", "threadId"}, "delta", "role", "threadId"),
	"thread/realtime/transcript/done":           schema([]string{"role", "text", "threadId"}, "role", "text", "threadId"),
	"thread/realtime/outputAudio/delta":         schema([]string{"audio", "threadId"}, "audio", "threadId"),
	"thread/realtime/sdp":                       schema([]string{"sdp", "threadId"}, "sdp", "threadId"),
	"thread/realtime/error":                     schema([]string{"message", "threadId"}, "message", "threadId"),
	"thread/realtime/closed":                    schema([]string{"threadId"}, "reason", "threadId"),
	"windows/worldWritableWarning":              schema([]string{"extraCount", "failedScan", "samplePaths"}, "extraCount", "failedScan", "samplePaths"),
	"windowsSandbox/setupCompleted":             schema([]string{"mode", "success"}, "error", "mode", "success"),
	"account/login/completed":                   schema([]string{"success"}, "error", "loginId", "success"),
}

var threadItemSchemas = map[string]objectSchema{
	"userMessage":         schema([]string{"content", "id", "type"}, "clientId", "content", "id", "type"),
	"hookPrompt":          schema([]string{"fragments", "id", "type"}, "fragments", "id", "type"),
	"agentMessage":        schema([]string{"id", "text", "type"}, "delivery", "id", "memoryCitation", "phase", "text", "type"),
	"plan":                schema([]string{"id", "text", "type"}, "id", "text", "type"),
	"reasoning":           schema([]string{"id", "type"}, "content", "id", "summary", "type"),
	"commandExecution":    schema([]string{"command", "commandActions", "cwd", "id", "status", "type"}, "aggregatedOutput", "command", "commandActions", "cwd", "durationMs", "exitCode", "id", "pluginId", "processId", "scriptPath", "source", "status", "type"),
	"fileChange":          schema([]string{"changes", "id", "status", "type"}, "changes", "id", "status", "type"),
	"mcpToolCall":         schema([]string{"arguments", "id", "server", "status", "tool", "type"}, "appContext", "arguments", "durationMs", "error", "id", "mcpAppResourceUri", "pluginId", "readOnlyHint", "result", "server", "status", "tool", "type"),
	"dynamicToolCall":     schema([]string{"arguments", "id", "status", "tool", "type"}, "arguments", "contentItems", "durationMs", "id", "namespace", "status", "success", "tool", "type"),
	"collabAgentToolCall": schema([]string{"agentsStates", "id", "receiverThreadIds", "senderThreadId", "status", "tool", "type"}, "agentsStates", "id", "model", "prompt", "reasoningEffort", "receiverThreadIds", "senderThreadId", "status", "tool", "type"),
	"subAgentActivity":    schema([]string{"agentPath", "agentThreadId", "id", "kind", "type"}, "agentPath", "agentThreadId", "id", "kind", "type"),
	"webSearch":           schema([]string{"id", "query", "type"}, "action", "id", "query", "results", "type"),
	"imageView":           schema([]string{"id", "path", "type"}, "id", "path", "type"),
	"sleep":               schema([]string{"durationMs", "id", "type"}, "durationMs", "id", "type"),
	"imageGeneration":     schema([]string{"id", "result", "status", "type"}, "failure", "id", "result", "revisedPrompt", "savedPath", "status", "transparentBackground", "type"),
	"enteredReviewMode":   schema([]string{"id", "review", "type"}, "id", "review", "type"),
	"exitedReviewMode":    schema([]string{"id", "review", "type"}, "id", "review", "type"),
	"contextCompaction":   schema([]string{"id", "type"}, "id", "type"),
}

var nativeToolItemKinds = stringSet(
	"commandExecution", "fileChange", "dynamicToolCall", "webSearch", "imageView", "sleep", "imageGeneration",
)
