package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestCollectionCreateActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, role, action string
		want               []string
	}{
		{name: "owner", role: "OWNER", action: "CREATE_PROJECT", want: []string{"CREATE_PROJECT"}},
		{name: "administrator", role: "ADMINISTRATOR", action: "CREATE_CONNECTION", want: []string{"CREATE_CONNECTION"}},
		{name: "operator", role: "OPERATOR", action: "CREATE_PROJECT", want: []string{}},
		{name: "member", role: "MEMBER", action: "CREATE_CONNECTION", want: []string{}},
		{name: "auditor", role: "AUDITOR", action: "CREATE_PROJECT", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := collectionCreateActions(test.role, test.action); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("collectionCreateActions(%q, %q)=%v, want %v", test.role, test.action, got, test.want)
			}
		})
	}
}

func TestAssistantActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, role string
		ready      bool
		want       []string
	}{
		{name: "ready owner", role: "OWNER", ready: true, want: []string{"OPEN", "CREATE_CONVERSATION", "ADD_TURN", "EDIT"}},
		{name: "recovering owner", role: "OWNER", want: []string{"OPEN", "EDIT", "RECOVER"}},
		{name: "ready member", role: "MEMBER", ready: true, want: []string{"OPEN", "CREATE_CONVERSATION", "ADD_TURN"}},
		{name: "recovering member", role: "MEMBER", want: []string{"OPEN"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := assistantActions(test.role, test.ready); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("assistantActions(%q, %t)=%v, want %v", test.role, test.ready, got, test.want)
			}
		})
	}
}

func TestProjectResourceActionsArePermissionAware(t *testing.T) {
	t.Parallel()
	agent := entity.Agent{State: "READY", Enabled: true}
	if got := agentActions(agent, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received agent mutations: %v", got)
	}
	if got := agentActions(agent, false, true); !reflect.DeepEqual(got, []string{"OPEN", "LAUNCH"}) {
		t.Fatalf("launcher received unexpected agent actions: %v", got)
	}
	workflow := entity.Workflow{State: "PUBLISHED"}
	if got := workflowActions(workflow, false, true); !reflect.DeepEqual(got, []string{"OPEN", "LAUNCH"}) {
		t.Fatalf("launcher received unexpected workflow actions: %v", got)
	}
	if got := runActions("RUNNING", false, true); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("launcher without cancel permission received cancel: %v", got)
	}
	if got := runActions("SUCCEEDED", true, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("canceller without launch permission received continuation: %v", got)
	}
	if got := gateActions("OPEN", false); len(got) != 0 {
		t.Fatalf("read-only actor received gate resolution: %v", got)
	}
	if got := artifactActions("CLEAN", false); !reflect.DeepEqual(got, []string{"DOWNLOAD"}) {
		t.Fatalf("viewer received unexpected artifact actions: %v", got)
	}
	if got := scheduleActions(entity.Schedule{Enabled: true}, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received schedule mutations: %v", got)
	}
	if got := roleImageActions(entity.RoleImageRecipe{State: "ACTIVE"}, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received role image mutations: %v", got)
	}
	if got := roleImageActions(entity.RoleImageRecipe{State: "ACTIVE"}, true); !reflect.DeepEqual(got, []string{"OPEN", "UPDATE", "REQUEST_BUILD", "ARCHIVE"}) {
		t.Fatalf("role image manager received incorrect actions: %v", got)
	}
	permissions := actorActionPermissions{canResolveGates: true}
	if got := filterNodeActions([]string{"OPEN", "CANCEL", "RESOLVE_GATE", "UNKNOWN"}, permissions); !reflect.DeepEqual(got, []string{"OPEN", "RESOLVE_GATE"}) {
		t.Fatalf("node action filter failed closed incorrectly: %v", got)
	}
}
