package errs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAssistantCurrentConfigurationStagePreservesClassWithoutDisclosingCause checks all configuration labels, unknown-stage fallback, ErrUnavailable matching and private-cause suppression. Nil, cancellation, deadlines and other error classes must pass through unchanged; fixtures are synthetic errors, not storage failures.
// TestAssistantCurrentConfigurationStagePreservesClassWithoutDisclosingCause проверяет все метки конфигурации, запасное значение неизвестного этапа, соответствие ErrUnavailable и скрытие закрытой причины. Nil, отмена, сроки и другие классы должны проходить неизменными; оснастка содержит синтетические ошибки, а не сбои хранилища.
func TestAssistantCurrentConfigurationStagePreservesClassWithoutDisclosingCause(t *testing.T) {
	for _, stage := range []string{AssistantCurrentPromptContext, AssistantCurrentConfigurationView, AssistantCurrentOwnerCoreRead,
		AssistantCurrentOwnerCoreVersion, AssistantCurrentTemplateProjection, AssistantCurrentUnclassified} {
		wrapped := WithAssistantCurrentConfigurationStage(errors.Join(ErrUnavailable, errors.New("PRIVATE_SQL_PAYLOAD")), stage)
		if !errors.Is(wrapped, ErrUnavailable) || AssistantCurrentConfigurationStage(wrapped) != stage || strings.Contains(wrapped.Error(), "PRIVATE_SQL_PAYLOAD") {
			t.Fatal("closed diagnostic lost its class or disclosed private cause")
		}
	}
	if AssistantCurrentConfigurationStage(WithAssistantCurrentConfigurationStage(ErrUnavailable, "PRIVATE_SQL_PAYLOAD")) != AssistantCurrentUnclassified {
		t.Fatal("unknown diagnostic stage did not remain bounded")
	}
	for _, err := range []error{nil, ErrNotFound, ErrForbidden, ErrInvalid, context.Canceled, context.DeadlineExceeded} {
		if WithAssistantCurrentConfigurationStage(err, AssistantCurrentPromptContext) != err {
			t.Fatal("diagnostic changed an existing error class")
		}
	}
}
