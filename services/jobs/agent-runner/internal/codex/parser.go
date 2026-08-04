package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maximumJSONLLineBytes  = 1 << 20
	maximumJSONLMessages   = 100_000
	maximumFinalBytes      = 480_000
	maximumDiagnosticBytes = 16 << 10
)

type Result struct {
	SessionID           string
	FinalMessage        string
	Outcome             string
	FailureCode         string
	ArchivePath         string
	ArchiveRelativePath string
	ArchiveSHA256       string
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
	fields, err := decodeObject(raw, schema(nil, "id", "method", "params", "result", "error", "trace"))
	if err != nil {
		return wireMessage{}, errors.New("Codex app-server JSON-RPC message is invalid")
	}
	id, hasID := fields["id"]
	methodRaw, hasMethod := fields["method"]
	params, hasParams := fields["params"]
	result, hasResult := fields["result"]
	errorValue, hasError := fields["error"]
	_, hasTrace := fields["trace"]
	if hasMethod {
		method, decodeErr := decodeBoundedString(methodRaw, 256)
		if decodeErr != nil || method == "" || hasResult || hasError {
			return wireMessage{}, errors.New("Codex app-server JSON-RPC method is invalid")
		}
		if hasID {
			if !validRequestID(id) || !hasParams {
				return wireMessage{}, errors.New("Codex app-server JSON-RPC request is invalid")
			}
			return wireMessage{kind: messageRequest, id: id, method: method, payload: params}, nil
		}
		if hasTrace || !hasParams {
			return wireMessage{}, errors.New("Codex app-server JSON-RPC notification is invalid")
		}
		return wireMessage{kind: messageNotification, method: method, payload: params}, nil
	}
	if !hasID || hasParams || hasTrace || hasResult == hasError || !validRequestID(id) {
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
	agentMessages     map[string]agentMessage
	finalID           string
	fallbackID        string
}

type agentMessage struct {
	text  string
	phase string
}

func newProtocolState(expectedSessionID string) *protocolState {
	return &protocolState{expectedSessionID: expectedSessionID, agentMessages: make(map[string]agentMessage)}
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
		"approvalPolicy", "approvalsReviewer", "cwd", "instructionSources", "model", "modelProvider", "serviceTier",
		"reasoningEffort", "sandbox", "thread"))
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
		"preview", "sessionId", "source", "status", "turns", "updatedAt"}, "agentNickname", "agentRole", "cliVersion",
		"createdAt", "cwd", "ephemeral", "forkedFromId", "gitInfo", "id", "modelProvider", "name", "parentThreadId",
		"path", "preview", "recencyAt", "sessionId", "source", "status", "threadSource", "turns", "updatedAt"))
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
	return state.consumeItems(turn.items, false)
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
		return state.consumeItems(turn.items, false)
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
		return state.consumeItem(fields["item"], method == "item/completed")
	case "turn/completed":
		fields, _ := decodeObject(raw, notificationSchema(method))
		threadID, err := decodeBoundedString(fields["threadId"], 128)
		turn, turnErr := parseTurn(fields["turn"])
		if err != nil || turnErr != nil || threadID != state.threadID || turn.id != state.turnID || state.terminals != 0 {
			return errors.New("Codex app-server terminal notification is invalid")
		}
		state.terminals++
		if err := state.consumeItems(turn.items, true); err != nil {
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

func (state *protocolState) consumeItems(items []json.RawMessage, authoritative bool) error {
	for _, item := range items {
		if err := state.consumeItem(item, authoritative); err != nil {
			return err
		}
	}
	return nil
}

func (state *protocolState) consumeItem(raw json.RawMessage, authoritative bool) error {
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
	if typeName != "agentMessage" {
		return nil
	}
	id, idErr := decodeBoundedString(fields["id"], 256)
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
	if state.terminals != 1 || state.result.SessionID == "" || state.result.Outcome == "" || state.threadPath == "" {
		return Result{}, errors.New("Codex app-server lifecycle is incomplete")
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
	"command", "commandActions", "content", "contentItems", "cwd", "durationMs", "error", "exitCode", "fragments", "id",
	"kind", "memoryCitation", "model", "mcpAppResourceUri", "namespace", "path", "phase", "pluginId", "processId",
	"prompt", "query", "reasoningEffort", "receiverThreadIds", "result", "review", "revisedPrompt", "savedPath", "server",
	"senderThreadId", "source", "status", "success", "summary", "text", "tool", "type",
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
	"agentMessage":        schema([]string{"id", "text", "type"}, "id", "memoryCitation", "phase", "text", "type"),
	"plan":                schema([]string{"id", "text", "type"}, "id", "text", "type"),
	"reasoning":           schema([]string{"id", "type"}, "content", "id", "summary", "type"),
	"commandExecution":    schema([]string{"command", "commandActions", "cwd", "id", "status", "type"}, "aggregatedOutput", "command", "commandActions", "cwd", "durationMs", "exitCode", "id", "processId", "source", "status", "type"),
	"fileChange":          schema([]string{"changes", "id", "status", "type"}, "changes", "id", "status", "type"),
	"mcpToolCall":         schema([]string{"arguments", "id", "server", "status", "tool", "type"}, "appContext", "arguments", "durationMs", "error", "id", "mcpAppResourceUri", "pluginId", "result", "server", "status", "tool", "type"),
	"dynamicToolCall":     schema([]string{"arguments", "id", "status", "tool", "type"}, "arguments", "contentItems", "durationMs", "id", "namespace", "status", "success", "tool", "type"),
	"collabAgentToolCall": schema([]string{"agentsStates", "id", "receiverThreadIds", "senderThreadId", "status", "tool", "type"}, "agentsStates", "id", "model", "prompt", "reasoningEffort", "receiverThreadIds", "senderThreadId", "status", "tool", "type"),
	"subAgentActivity":    schema([]string{"agentPath", "agentThreadId", "id", "kind", "type"}, "agentPath", "agentThreadId", "id", "kind", "type"),
	"webSearch":           schema([]string{"id", "query", "type"}, "action", "id", "query", "type"),
	"imageView":           schema([]string{"id", "path", "type"}, "id", "path", "type"),
	"sleep":               schema([]string{"durationMs", "id", "type"}, "durationMs", "id", "type"),
	"imageGeneration":     schema([]string{"id", "result", "status", "type"}, "id", "result", "revisedPrompt", "savedPath", "status", "type"),
	"enteredReviewMode":   schema([]string{"id", "review", "type"}, "id", "review", "type"),
	"exitedReviewMode":    schema([]string{"id", "review", "type"}, "id", "review", "type"),
	"contextCompaction":   schema([]string{"id", "type"}, "id", "type"),
}
