package resource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func TestProtectedConfigurationRegistriesAreSpecialized(t *testing.T) {
	t.Parallel()

	want := map[enum.Kind][]string{
		enum.KindRoleDefinition:    {"archive", "create", "delete", "update"},
		enum.KindAgent:             {"archive", "create", "delete", "update"},
		enum.KindAgentAssignment:   {"assign", "unassign"},
		enum.KindInstructionSet:    {"archive", "copy", "create", "delete", "detach", "publish", "rollback", "update", "validate"},
		enum.KindProviderReference: {"archive", "refresh", "register"},
		enum.KindProviderPool:      {"archive", "create", "delete", "update"},
	}
	if len(protectedConfigurationActions) != len(want) {
		t.Fatalf("unexpected protected kind count: %d", len(protectedConfigurationActions))
	}
	for kind, actions := range want {
		for _, action := range actions {
			if _, ok := protectedConfigurationActions[kind][action]; !ok {
				t.Fatalf("specialized action %s/%s is absent", kind, action)
			}
		}
		for _, forbidden := range []string{"transition", "manage", "grant", "escalate"} {
			if _, ok := protectedConfigurationActions[kind][forbidden]; ok {
				t.Fatalf("generic authority-bearing action %s/%s is present", kind, forbidden)
			}
		}
	}
}

func TestProtectedConfigurationDoesNotDeclareFalseEventConsumer(t *testing.T) {
	t.Parallel()

	for _, kind := range []enum.Kind{enum.KindRoleDefinition, enum.KindAgent,
		enum.KindAgentAssignment, enum.KindInstructionSet, enum.KindProviderReference,
		enum.KindProviderPool, enum.KindWorkspaceBackup, enum.KindWorkspaceRestore,
		enum.KindWorkspaceMapping} {
		if name, published := event.EventNameForKind(kind); published {
			t.Fatalf("protected kind %s declares unsupported event %s", kind, name)
		}
	}
	if name, published := event.EventNameForKind(enum.KindRuntimeRevision); !published || name != event.RuntimeConfigurationChanged {
		t.Fatal("materialized runtime revision lost its existing consumer event")
	}
}

func TestProtectedConfigurationStableKeysAreRecognized(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]entity.Spec{
		"role definition": entity.RoleDefinitionSpec{StableKey: "stable"},
		"agent":           entity.AgentSpec{StableKey: "stable"},
		"instruction":     entity.InstructionSetSpec{StableKey: "stable"},
		"provider ref":    entity.ProviderConnectionReferenceSpec{StableKey: "stable"},
		"provider pool":   entity.ProviderPoolSpec{StableKey: "stable"},
	} {
		t.Run(name, func(t *testing.T) {
			key, ok := protectedConfigurationStableKey(spec)
			if !ok || key != "stable" {
				t.Fatalf("protected stable key is not recognized: %q, %t", key, ok)
			}
		})
	}
	if _, ok := protectedConfigurationStableKey(entity.AgentAssignmentSpec{}); ok {
		t.Fatal("server-owned assignment unexpectedly exposes a stable key")
	}
}

func TestRuntimeIncidentTransitionRegistryIsClosed(t *testing.T) {
	t.Parallel()

	want := map[string]map[string]string{
		"OPEN":         {"acknowledge": "ACKNOWLEDGED", "retry": "RETRYING"},
		"ACKNOWLEDGED": {"retry": "RETRYING", "release": "RELEASED", "close": "CLOSED"},
		"RETRYING":     {"close": "CLOSED"},
		"RELEASED":     {"retry": "RETRYING", "close": "CLOSED"},
	}
	if !reflect.DeepEqual(runtimeIncidentTransitions, want) {
		t.Fatalf("unexpected incident lifecycle: %#v", runtimeIncidentTransitions)
	}
	for _, terminal := range []string{"CLOSED", "UNKNOWN"} {
		if len(runtimeIncidentTransitions[terminal]) != 0 {
			t.Fatalf("terminal incident state %s accepts an action", terminal)
		}
	}
}

func TestWorkspaceRecoveryActionRegistriesAreExact(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"create": {}, "cancel": {}, "retry": {}, "complete": {}, "fail": {}, "expire": {},
	}
	if !reflect.DeepEqual(workspaceBackupActions, want) {
		t.Fatalf("unexpected workspace backup actions: %#v", workspaceBackupActions)
	}
	if !reflect.DeepEqual(workspaceRestoreActions, want) {
		t.Fatalf("unexpected workspace restore actions: %#v", workspaceRestoreActions)
	}
	if enum.TransitionAllowed(enum.KindWorkspaceMapping, enum.StateArchived, enum.StateActive) {
		t.Fatal("unlinked workspace mapping can be reopened without a fresh provider receipt")
	}
	if !enum.TransitionAllowed(enum.KindTurn, enum.StateCancelled, enum.StateQueued) {
		t.Fatal("full-envelope workspace restore cannot create a fresh cancelled attempt")
	}
}

func TestTargetScheduleDigestPinsPromptAndEverySelection(t *testing.T) {
	t.Parallel()

	spec := entity.ScheduleSpec{
		AgentID: "agent", AgentVersion: 1, AgentSHA256: strings.Repeat("1", 64),
		InstructionSetID: "instruction", InstructionSetVersion: 2, InstructionSetSHA256: strings.Repeat("2", 64),
		RuntimeSelectionRef: "runtime://standard", RuntimeSelectionVersion: 3,
		RuntimeSelectionSHA256: strings.Repeat("3", 64), ProviderPoolID: "pool",
		ProviderPoolVersion: 4, ProviderPoolSHA256: strings.Repeat("4", 64),
		TargetType: "AGENT", SessionPolicy: "NEW",
	}
	base, err := targetScheduleEffectiveInput(spec, strings.Repeat("5", 64))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*entity.ScheduleSpec) string
	}{
		{"agent version", func(value *entity.ScheduleSpec) string { value.AgentVersion++; return strings.Repeat("5", 64) }},
		{"instruction digest", func(value *entity.ScheduleSpec) string {
			value.InstructionSetSHA256 = strings.Repeat("6", 64)
			return strings.Repeat("5", 64)
		}},
		{"runtime version", func(value *entity.ScheduleSpec) string {
			value.RuntimeSelectionVersion++
			return strings.Repeat("5", 64)
		}},
		{"pool digest", func(value *entity.ScheduleSpec) string {
			value.ProviderPoolSHA256 = strings.Repeat("7", 64)
			return strings.Repeat("5", 64)
		}},
		{"prompt digest", func(_ *entity.ScheduleSpec) string { return strings.Repeat("8", 64) }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			changed := spec
			digest, digestErr := targetScheduleEffectiveInput(changed, test.edit(&changed))
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			if digest == base {
				t.Fatal("changed target selection reused schedule digest")
			}
		})
	}
}
