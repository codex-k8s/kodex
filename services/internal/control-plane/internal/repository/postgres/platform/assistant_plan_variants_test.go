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
