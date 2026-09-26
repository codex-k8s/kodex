package platform

import (
	"strings"
	"testing"
)

func TestAssistantPlanVariantsRemainIndependentAcrossTurns(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"UPDATE control_plane.assistant_plans",
		"superseded-by-new-turn",
		"state = 'STALE'",
	} {
		if strings.Contains(queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt, forbidden) {
			t.Errorf("new assistant turn still supersedes an earlier plan: %q", forbidden)
		}
	}
	for _, required := range []string{
		"c.ref=p.conversation_ref",
		"ORDER BY p.created_at,p.id",
	} {
		if !strings.Contains(queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef, required) {
			t.Errorf("assistant plan history query lacks %q", required)
		}
	}
}

func TestAssistantTurnContextIsSnapshottedPerRun(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"run insert":    queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType,
		"runtime claim": queryRuntimeClaimexecutionSelectAssistantContext,
	} {
		for _, required := range []string{
			"assistant_context_route",
			"assistant_context_entity_kind",
			"assistant_context_entity_ref",
		} {
			if !strings.Contains(query, required) {
				t.Errorf("%s query lacks immutable turn context field %q", name, required)
			}
		}
	}
	for _, required := range []string{
		"run.assistant_context_entity_kind",
		"run.assistant_context_entity_ref",
	} {
		if !strings.Contains(queryRuntimeProposeassistantplanSelectContext, required) {
			t.Errorf("plan proposal query lacks immutable turn context field %q", required)
		}
	}
	if strings.Contains(queryRuntimeClaimexecutionSelectAssistantContext, "conversation.context_entity_kind") ||
		strings.Contains(queryRuntimeProposeassistantplanSelectContext, "conversation.context_entity_kind") {
		t.Fatal("assistant runtime still reads mutable conversation context")
	}
	for _, required := range []string{
		"context_route = $2",
		"context_entity_kind = $3",
		"context_entity_ref = $4",
		"context_entity_name = $5",
		"context_entity_version = $6",
		"allowed_operations = $7",
	} {
		if !strings.Contains(queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt, required) {
			t.Errorf("conversation context refresh query lacks %q", required)
		}
	}
}
