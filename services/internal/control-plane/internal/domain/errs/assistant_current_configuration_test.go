package errs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
