package runtimecontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// InitializationFailureSchema versions a closed init observation, not execution authority.
// InitializationFailureSchema версионирует закрытое наблюдение init, не полномочие исполнения.
const InitializationFailureSchema = "kodex.workspace-initialization-failure.v1"

// InitializationFailureCode distinguishes pre-model initialization from provider failure.
// InitializationFailureCode отличает подготовку до модели от отказа провайдера.
const InitializationFailureCode = "RUNTIME_INITIALIZATION_FAILED"

// MaximumInitializationFailureBytes bounds the complete termination record below Kubernetes limits.
// MaximumInitializationFailureBytes ограничивает весь termination record ниже лимита Kubernetes.
const MaximumInitializationFailureBytes = 2048

// initializationReasons closes every supported stage/reason pair, including explicit uncertainty.
// initializationReasons замыкает все пары stage/reason, включая явную неизвестность.
var initializationReasons = map[string][]string{
	"UNKNOWN":           {"UNKNOWN"},
	"INPUT_DECODE":      {"INPUT_INVALID"},
	"EGRESS_TRUST":      {"UNKNOWN", "FILESYSTEM_IO_FAILED"},
	"CONTEXT_SNAPSHOT":  {"INPUT_INVALID"},
	"CALLBACK_SETUP":    {"UNKNOWN"},
	"WORKSPACE_LAYOUT":  {"UNKNOWN", "CONTEXT_CANCELLED", "DEADLINE_EXCEEDED", "READ_ONLY", "QUOTA_EXCEEDED", "FILESYSTEM_IO_FAILED"},
	"INPUT_MANIFEST":    {"INPUT_INVALID"},
	"ARTIFACT_TRANSFER": {"UNKNOWN", "AUTHENTICATION_REJECTED", "AUTHORITY_REJECTED", "SOURCE_UNAVAILABLE", "UPSTREAM_UNAVAILABLE", "TRANSPORT_UNAVAILABLE", "PIN_MISMATCH", "CONTENT_DIGEST_MISMATCH", "CONTEXT_CANCELLED", "DEADLINE_EXCEEDED", "READ_ONLY", "QUOTA_EXCEEDED", "FILESYSTEM_IO_FAILED"},
	"INPUT_PROTECTION":  {"UNKNOWN", "READ_ONLY", "QUOTA_EXCEEDED", "FILESYSTEM_IO_FAILED"},
	"CONTEXT_FILES":     {"UNKNOWN", "CONTEXT_CANCELLED", "DEADLINE_EXCEEDED", "READ_ONLY", "QUOTA_EXCEEDED", "FILESYSTEM_IO_FAILED"},
}

// errInitializationRecordInvalid hides rejected record bytes and private provenance.
// errInitializationRecordInvalid скрывает отклонённые bytes и private provenance записи.
var errInitializationRecordInvalid = errors.New("runtime initialization observation is invalid")

// InitializationFailure records only closed reasons and exact immutable input commitments.
// Pod identity belongs to the observing controller and must never be supplied by the runner.
// InitializationFailure содержит закрытые причины и точные immutable input commitments.
// Pod identity назначает наблюдающий controller, runner её не передаёт.
type InitializationFailure struct {
	Schema                 string `json:"schema"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	Stage                  string `json:"stage"`
	Reason                 string `json:"reason"`
}

// Validate rejects unknown pairs and malformed commitments without granting access or retry.
// Validate отклоняет неизвестные пары и повреждённые commitments без выдачи доступа либо retry.
func (value InitializationFailure) Validate() error {
	if value.Schema != InitializationFailureSchema || !sha256Pattern.MatchString(value.RuntimeRevisionDigest) || !sha256Pattern.MatchString(value.InputDigest) || !sha256Pattern.MatchString(value.ExecutionBindingDigest) || !opaqueReferencePattern.MatchString(value.SessionRef) || !opaqueReferencePattern.MatchString(value.TurnRef) || value.Attempt < 1 || !slices.Contains(initializationReasons[value.Stage], value.Reason) {
		return errInitializationRecordInvalid
	}
	return nil
}

// Matches binds a validated observation to the controller's trusted execution input.
// Matches связывает проверенное наблюдение с доверенным входом исполнения controller.
func (value InitializationFailure) Matches(input RunnerInput) bool {
	return value.Validate() == nil && value.RuntimeRevisionDigest == input.RuntimeRevisionDigest && value.InputDigest == input.InputDigest && value.ExecutionBindingDigest == input.ExecutionBindingDigest && value.SessionRef == input.SessionRef && value.TurnRef == input.TurnRef && value.Attempt == input.Attempt
}

// NewInitializationObservation copies only exact commitments and normalized stage/reason.
// NewInitializationObservation копирует лишь exact commitments и нормализованную пару stage/reason.
func NewInitializationObservation(input RunnerInput, stage, reason string) InitializationFailure {
	if !slices.Contains(initializationReasons[stage], reason) {
		stage, reason = "UNKNOWN", "UNKNOWN"
	}
	return InitializationFailure{InitializationFailureSchema, input.RuntimeRevisionDigest, input.InputDigest, input.ExecutionBindingDigest, input.SessionRef, input.TurnRef, input.Attempt, stage, reason}
}

// InitializationFailureSummary returns one closed localized owner summary; bad input stays unknown.
// InitializationFailureSummary возвращает закрытый localized owner summary; неверный ввод остаётся unknown.
func InitializationFailureSummary(stage, reason string) string {
	if !slices.Contains(initializationReasons[stage], reason) {
		stage, reason = "UNKNOWN", "UNKNOWN"
	}
	return "i18n:RUNTIME_INIT_" + stage + "_" + reason
}

// ValidInitializationFailureSummary recognizes only supported complete owner summary tokens.
// ValidInitializationFailureSummary распознаёт только полные поддерживаемые owner summary tokens.
func ValidInitializationFailureSummary(summary string) bool {
	for stage, reasons := range initializationReasons {
		for _, reason := range reasons {
			if summary == InitializationFailureSummary(stage, reason) {
				return true
			}
		}
	}
	return false
}

// DecodeInitializationFailure accepts one bounded canonical JSON object without extra fields.
// DecodeInitializationFailure принимает один bounded canonical JSON object без лишних полей.
func DecodeInitializationFailure(raw []byte) (InitializationFailure, error) {
	var value InitializationFailure
	if len(raw) == 0 || len(raw) > MaximumInitializationFailureBytes || !decodeCanonicalProviderObservation(raw, &value, func() bool { return value.Validate() == nil }) {
		return InitializationFailure{}, errInitializationRecordInvalid
	}
	return value, nil
}

// EncodeInitializationFailure emits only validated bounded bytes for the fixed termination file.
// EncodeInitializationFailure выдаёт лишь проверенные bounded bytes для фиксированного termination file.
func EncodeInitializationFailure(value InitializationFailure) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errInitializationRecordInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumInitializationFailureBytes {
		return nil, errInitializationRecordInvalid
	}
	return raw, nil
}

// initializationError retains the original cause for errors.Is while formatting only fixed text.
// initializationError сохраняет исходную cause для errors.Is, но форматирует лишь fixed text.
type initializationError struct {
	stage, reason string
	cause         error
}

// Error never reflects private causes, filenames, credential paths or response bodies.
// Error не отражает private causes, имена файлов, credential paths либо response bodies.
func (failure *initializationError) Error() string { return "runtime workspace initialization failed" }

// Unwrap preserves typed error identity for existing caller classification, not log serialization.
// Unwrap сохраняет typed error identity для существующей классификации, не сериализации логов.
func (failure *initializationError) Unwrap() error { return failure.cause }

// Format closes every fmt verb and flag so private cause fields cannot bypass Error.
// Format закрывает все fmt verbs и flags, чтобы private cause fields не обходили Error.
func (failure *initializationError) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, failure.Error())
}

// InitializationError wraps a source-selected pair and preserves private sentinel identity.
// InitializationError оборачивает назначенную источником пару, сохраняя private sentinel identity.
func InitializationError(stage, reason string, cause error) error {
	if !slices.Contains(initializationReasons[stage], reason) {
		stage, reason = "UNKNOWN", "UNKNOWN"
	}
	return &initializationError{stage, reason, cause}
}

// InitializationErrorDetails extracts only typed validated pairs; it never parses Error text.
// InitializationErrorDetails извлекает лишь typed validated pairs, не разбирая Error text.
func InitializationErrorDetails(err error) (string, string) {
	var failure *initializationError
	if errors.As(err, &failure) && slices.Contains(initializationReasons[failure.stage], failure.reason) {
		return failure.stage, failure.reason
	}
	return "UNKNOWN", "UNKNOWN"
}

// IsInitializationFailureSummary reserves the diagnostic namespace against inconsistent owner codes.
// IsInitializationFailureSummary резервирует namespace диагностики против противоречивых owner codes.
func IsInitializationFailureSummary(value string) bool {
	return strings.HasPrefix(value, "i18n:RUNTIME_INIT_")
}
