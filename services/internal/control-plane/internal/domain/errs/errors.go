// Package errs содержит стабильные доменные классы ошибок control-plane.
package errs

import "errors"

var (
	ErrInvalid                     = errors.New("invalid input")
	ErrUnauthorized                = errors.New("unauthorized")
	ErrFreshAuthenticationRequired = errors.New("fresh authentication required")
	ErrForbidden                   = errors.New("forbidden")
	ErrNotFound                    = errors.New("not found")
	ErrConflict                    = errors.New("conflict")
	ErrCapabilityRequired          = errors.New("required capability is not enabled")
	ErrAlreadyResolved             = errors.New("resource is already resolved")
	ErrVersionMismatch             = errors.New("version mismatch")
	ErrIdempotencyReuse            = errors.New("idempotency key reused with different intent")
	ErrProtected                   = errors.New("protected system resource")
	ErrResourceInUse               = errors.New("resource is in use")
	ErrUnavailable                 = errors.New("temporarily unavailable")
	ErrMailboxPublicationPending   = errors.New("mailbox publication is pending")
)

const (
	AssistantCurrentPromptContext      = "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT"
	AssistantCurrentConfigurationView  = "ASSISTANT_CURRENT_CONFIGURATION_CONFIG_VIEW"
	AssistantCurrentOwnerCoreRead      = "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_READ"
	AssistantCurrentOwnerCoreVersion   = "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_VERSION"
	AssistantCurrentTemplateProjection = "ASSISTANT_CURRENT_CONFIGURATION_TEMPLATE_PROJECTION"
	AssistantCurrentUnclassified       = "ASSISTANT_CURRENT_CONFIGURATION_UNCLASSIFIED"
)

type assistantCurrentConfigurationFailure struct {
	stage string
	cause error
}

func (failure *assistantCurrentConfigurationFailure) Error() string {
	return "assistant current configuration is temporarily unavailable"
}

func (failure *assistantCurrentConfigurationFailure) Unwrap() error { return failure.cause }

// WithAssistantCurrentConfigurationStage сохраняет класс ошибки, но не раскрывает
// диагностические строки adapter. Неизвестный этап остаётся закрытым fallback.
func WithAssistantCurrentConfigurationStage(err error, stage string) error {
	if !errors.Is(err, ErrUnavailable) {
		return err
	}
	return &assistantCurrentConfigurationFailure{stage: assistantCurrentConfigurationStage(stage), cause: err}
}

// AssistantCurrentConfigurationStage возвращает только серверный закрытый enum.
func AssistantCurrentConfigurationStage(err error) string {
	var failure *assistantCurrentConfigurationFailure
	if errors.As(err, &failure) {
		return assistantCurrentConfigurationStage(failure.stage)
	}
	return AssistantCurrentUnclassified
}

func assistantCurrentConfigurationStage(stage string) string {
	switch stage {
	case AssistantCurrentPromptContext, AssistantCurrentConfigurationView, AssistantCurrentOwnerCoreRead,
		AssistantCurrentOwnerCoreVersion, AssistantCurrentTemplateProjection:
		return stage
	default:
		return AssistantCurrentUnclassified
	}
}

const (
	AssistantPlanHydrate   = "ASSISTANT_PLAN_HYDRATE"
	AssistantPlanNormalize = "ASSISTANT_PLAN_NORMALIZE"
	AssistantPlanBind      = "ASSISTANT_PLAN_BIND"
	AssistantPlanAuthorize = "ASSISTANT_PLAN_AUTHORIZE"
	AssistantPlanEmpty     = "ASSISTANT_PLAN_EMPTY"
	AssistantPlanCommand   = "ASSISTANT_PLAN_COMMAND"
)

type assistantPlanFailure struct {
	stage string
	index int
	cause error
}

func (failure *assistantPlanFailure) Error() string { return "assistant plan preparation failed" }
func (failure *assistantPlanFailure) Unwrap() error { return failure.cause }

// Диагностика не меняет status boundary и не хранит параметры операции.
func WithAssistantPlanStage(err error, stage string, index int) error {
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrVersionMismatch) && !errors.Is(err, ErrInvalid) || !validAssistantPlanStage(stage, index) {
		return err
	}
	return &assistantPlanFailure{stage: stage, index: index, cause: err}
}

// AssistantPlanDiagnostic возвращает только закрытый этап, класс и номер операции.
func AssistantPlanDiagnostic(err error) (stage, category string, index int, ok bool) {
	var failure *assistantPlanFailure
	if !errors.As(err, &failure) || !validAssistantPlanStage(failure.stage, failure.index) {
		return "", "", 0, false
	}
	if errors.Is(failure.cause, ErrVersionMismatch) {
		category = "VERSION"
	} else if errors.Is(failure.cause, ErrConflict) {
		category = "CONFLICT"
	} else if errors.Is(failure.cause, ErrInvalid) {
		category = "INVALID"
	} else {
		return "", "", 0, false
	}
	return failure.stage, category, failure.index, true
}

func validAssistantPlanStage(stage string, index int) bool {
	switch stage {
	case AssistantPlanHydrate, AssistantPlanNormalize, AssistantPlanBind, AssistantPlanAuthorize, AssistantPlanCommand:
		return index >= 1 && index <= 32
	case AssistantPlanEmpty:
		return index == 0
	default:
		return false
	}
}

type assistantPlanFieldFailure struct {
	field string
	cause error
}

func (failure *assistantPlanFieldFailure) Error() string { return "assistant plan input is invalid" }
func (failure *assistantPlanFieldFailure) Unwrap() error { return failure.cause }

// Поле назначает только реальный rejecting guard; caller key не является диагностикой.
func WithAssistantPlanField(err error, field string) error {
	if !errors.Is(err, ErrInvalid) || !validAssistantPlanField(field) {
		return err
	}
	return &assistantPlanFieldFailure{field: field, cause: err}
}

func AssistantPlanField(err error) string {
	var failure *assistantPlanFieldFailure
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &failure) || !validAssistantPlanField(failure.field) {
		return ""
	}
	return failure.field
}

func validAssistantPlanField(field string) bool {
	switch field {
	case "WORKFLOW_SHAPE", "WORKFLOW_REF", "WORKFLOW_VERSION", "WORKFLOW_BINDING",
		"MAX_CONCURRENCY", "TIMEOUT_SECONDS", "WORKFLOW_TEXT", "INPUT_FIELDS", "STEPS",
		"INPUT_FIELD_KEY", "STEP_SHAPE", "STEP_KEY", "STEPS_GRAPH", "WORKFLOW_DRAFT",
		"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS":
		return true
	default:
		return false
	}
}
