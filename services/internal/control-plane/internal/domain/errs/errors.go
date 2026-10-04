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
