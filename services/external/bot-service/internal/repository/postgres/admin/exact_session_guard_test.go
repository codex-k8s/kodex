package admin

import (
	"errors"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func TestNormalizeExactAgentSessionBindingsOrdersAndDeduplicates(t *testing.T) {
	first := entity.AgentSession{ID: 1, SessionKey: "source", ProjectID: 1, ChatID: 1, RoleID: 1, MattermostChannelID: "source-channel"}
	second := entity.AgentSession{ID: 2, SessionKey: "child", ProjectID: 1, ChatID: 2, RoleID: 2, MattermostChannelID: "child-channel"}
	bindings, err := normalizeExactAgentSessionBindings([]entity.AgentSession{second, first, second})
	if err != nil {
		t.Fatalf("normalizeExactAgentSessionBindings() error = %v", err)
	}
	if len(bindings) != 2 || bindings[0].ID != first.ID || bindings[1].ID != second.ID {
		t.Fatalf("normalized bindings = %#v", bindings)
	}

	conflict := second
	conflict.ChatID++
	if _, err := normalizeExactAgentSessionBindings([]entity.AgentSession{second, conflict}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
}
