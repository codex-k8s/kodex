package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

const ProviderFailureDiagnosticSchema = "kodex.provider-failure-diagnostic.v1"

var errProviderDiagnostic = errors.New("provider failure diagnostic is invalid")

// ProviderFailureDiagnostic — только наблюдение закрытой причины, не authority,
// ACK провайдера, доказательство результата либо durable CP receipt.
type ProviderFailureDiagnostic struct {
	Schema                 string `json:"schema"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	Kind                   string `json:"kind"`
	Stage                  string `json:"stage"`
	Class                  string `json:"class"`
	Detail                 string `json:"detail"`
	Notification           string `json:"notification"`
	NotificationError      string `json:"notification_error"`
	AccountRead            string `json:"account_read"`
	TerminalCode           string `json:"terminal_code"`
}

func (value ProviderFailureDiagnostic) Validate() error {
	if value.Schema != ProviderFailureDiagnosticSchema || !sha256Pattern.MatchString(value.RuntimeRevisionDigest) ||
		!sha256Pattern.MatchString(value.InputDigest) || !sha256Pattern.MatchString(value.ExecutionBindingDigest) ||
		!opaqueReferencePattern.MatchString(value.SessionRef) || !opaqueReferencePattern.MatchString(value.TurnRef) || value.Attempt < 1 ||
		!slices.Contains(strings.Fields("SELECTION CONTEXT BROKER_REQUEST AUTH_READ MCP_BINDING MCP_BRIDGE HOME_PREPARE ACCOUNT_PIN ARCHIVE_RESTORE PROCESS_START INITIALIZE SKILLS ACCOUNT_READ THREAD_CALL THREAD_BIND MCP_READINESS USAGE_BASELINE TURN_PARAMETERS TURN_START TERMINAL_WAIT THREAD_READ PROCESS_STOP TERMINAL_RESULT ARCHIVE_CAPTURE UNKNOWN"), value.Stage) ||
		!slices.Contains(strings.Fields("AUTHENTICATION AUTHORITY MCP CONFIGURATION PROVIDER ACCOUNT_RESPONSE_SCHEMA"), value.Class) ||
		!slices.Contains(strings.Fields("NONE REQUEST_WRITE CONTEXT_CANCELLED STREAM_CLOSED STREAM_INVALID NOTIFICATION_INVALID REQUEST_REJECTED RESPONSE_CORRELATION MESSAGE_KIND RPC_ERROR RESUME_SOURCE_SCHEMA RESUME_SOURCE_ID RESUME_SOURCE_LOCATOR RESUME_SOURCE_OPEN RESUME_SOURCE_METADATA RESUME_SOURCE_IDENTITY"), value.Detail) ||
		!slices.Contains([]string{"NONE", "UNKNOWN", "thread/tokenUsage/updated", "rawResponse/completed"}, value.Notification) ||
		!slices.Contains(strings.Fields("NONE UNKNOWN METHOD ENVELOPE TUPLE ITEM TIMESTAMP MESSAGE TOKEN_USAGE TERMINAL LIFECYCLE MCP PROVIDER_ERROR TOKEN_USAGE_STRUCTURE TOKEN_USAGE_REQUIRED_MISSING TOKEN_USAGE_REQUIRED_NULL TOKEN_USAGE_REQUIRED_TYPE TOKEN_USAGE_OPTIONAL_NULL TOKEN_USAGE_OPTIONAL_TYPE TOKEN_USAGE_NEGATIVE TOKEN_USAGE_TOTAL_ARITHMETIC TOKEN_USAGE_CACHE_INPUT_BOUND TOKEN_USAGE_REASONING_OUTPUT_BOUND TOKEN_USAGE_LAST_EXCEEDS_TOTAL TOKEN_USAGE_RECEIPT_CONFLICT TOKEN_USAGE_RECEIPT_LIMIT TOKEN_USAGE_OVERFLOW"), value.NotificationError) ||
		!slices.Contains(strings.Fields("NONE UNKNOWN REQUIREMENTS_LOAD MISSING_ACCOUNT_ID DISCOVERY_CANCELLED ACCOUNT_CHANGED DISCOVERY_UNAUTHORIZED DISCOVERY_FAILED MISSING_WORKSPACE DUPLICATE_WORKSPACE REQUIREMENTS_RELOAD CONFIGURATION_CHANGED SHUTDOWN DISCOVERY_TIMEOUT MISSING_BACKEND_ORIGIN INVALID_ROUTING_OVERRIDE BACKEND_IS_NOT_ORIGIN BACKEND_CONFLICT INVALID_BACKEND_URL INVALID_BACKEND_ORIGIN"), value.AccountRead) {
		return errProviderDiagnostic
	}
	if strings.HasPrefix(value.Detail, "RESUME_SOURCE_") && (value.Kind != "REQUEST_FAILURE" || value.Stage != "THREAD_READ" || value.Class != "PROVIDER") {
		return errProviderDiagnostic
	}
	if value.Kind == "TERMINAL_FAILURE" {
		if value.Stage != "TERMINAL_RESULT" || value.Class != "PROVIDER" || value.Detail != "NONE" ||
			value.Notification != "NONE" || value.NotificationError != "NONE" || value.AccountRead != "NONE" ||
			!slices.Contains(strings.Fields("provider_error_info_invalid server_overloaded usage_limit_exceeded unauthorized cyber_policy context_window_exceeded session_budget_exceeded provider_internal_error provider_bad_request thread_rollback_failed provider_sandbox_error provider_other_error active_turn_not_steerable provider_transport_failure provider_interrupted RUNTIME_ARTIFACT_INVALID"), value.TerminalCode) {
			return errProviderDiagnostic
		}
		return nil
	}
	if value.Kind != "REQUEST_FAILURE" || value.TerminalCode != "" ||
		(value.Detail != "NOTIFICATION_INVALID" && (value.Notification != "NONE" || value.NotificationError != "NONE")) ||
		(value.Detail == "NOTIFICATION_INVALID" && (value.Notification == "NONE" || value.NotificationError == "NONE")) ||
		(strings.HasPrefix(value.NotificationError, "TOKEN_USAGE_") && value.Notification != "thread/tokenUsage/updated" && value.Notification != "rawResponse/completed") ||
		(value.AccountRead != "NONE" && (value.Stage != "ACCOUNT_READ" || value.Detail != "RPC_ERROR")) {
		return errProviderDiagnostic
	}
	return nil
}

func (value ProviderFailureDiagnostic) Matches(input RunnerInput) bool {
	return value.Validate() == nil && value.RuntimeRevisionDigest == input.RuntimeRevisionDigest &&
		value.InputDigest == input.InputDigest && value.ExecutionBindingDigest == input.ExecutionBindingDigest &&
		value.SessionRef == input.SessionRef && value.TurnRef == input.TurnRef && value.Attempt == input.Attempt
}

// Существующий recursive duplicate-key guard используется до typed decode.
// Полный canonical JSON исключает неизвестные, отсутствующие и case-alias keys.
func (value *ProviderFailureDiagnostic) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 2) != nil {
		return errProviderDiagnostic
	}
	type wire ProviderFailureDiagnostic
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || decoder.Decode(new(any)) != io.EOF || ProviderFailureDiagnostic(decoded).Validate() != nil {
		return errProviderDiagnostic
	}
	canonical, err := json.Marshal(decoded)
	var compact bytes.Buffer
	if err != nil || json.Compact(&compact, raw) != nil || !bytes.Equal(compact.Bytes(), canonical) {
		return errProviderDiagnostic
	}
	*value = ProviderFailureDiagnostic(decoded)
	return nil
}
