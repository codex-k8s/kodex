package controlplane

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func TestLegacyOperationEvidenceUsesDomainEventExpectation(t *testing.T) {
	if !strings.Contains(sqlLegacyGraphOperationVerify, "@event_required") ||
		!strings.Contains(sqlLegacyGraphOperationVerify, "@event_name") {
		t.Fatal("legacy evidence verifier must receive the domain event expectation")
	}
	for _, kind := range []enum.Kind{
		enum.KindProject, enum.KindTeam, enum.KindChat, enum.KindRole,
		enum.KindPromptProfile, enum.KindCredentialBinding, enum.KindRepositoryWorkspace,
		enum.KindIntegration, enum.KindRuntimeRevision, enum.KindSession, enum.KindTurn,
	} {
		name, required := event.EventNameForKind(kind)
		if !required || name != event.RuntimeConfigurationChanged {
			t.Fatalf("legacy materialization kind %s lost its domain event expectation", kind)
		}
	}
}
