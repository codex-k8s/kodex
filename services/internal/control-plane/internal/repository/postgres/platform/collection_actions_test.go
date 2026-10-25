package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestCollectionCreateActions checks only owner/administrator role fixtures receive collection
// creation actions while operator/member/auditor fixtures receive none.
// TestCollectionCreateActions проверяет выдачу действий создания коллекции только тестовым ролям
// владельца и администратора и их отсутствие у оператора, участника и аудитора.
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

// TestAssistantActions checks assistant conversation/turn actions depend on readiness while
// edit/recovery actions remain limited to the owner role.
// TestAssistantActions проверяет зависимость действий беседы и реплики ассистента от готовности и
// ограничение редактирования и восстановления ролью владельца.
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

// TestProjectResourceActionsArePermissionAware checks ordered mutation/launch/publication/trash
// actions across resource states and explicit permissions, closing unknown node actions; these
// presentation predicates do not authenticate transport.
// TestProjectResourceActionsArePermissionAware проверяет порядок действий изменения, запуска,
// публикации и корзины по состояниям ресурсов и явным правам, закрывая неизвестные действия узла;
// эти предикаты представления не аутентифицируют transport.
func TestProjectResourceActionsArePermissionAware(t *testing.T) {
	t.Parallel()
	agent := entity.Agent{State: "READY", Enabled: true}
	if got := agentActions(agent, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received agent mutations: %v", got)
	}
	if got := agentActions(agent, false, true); !reflect.DeepEqual(got, []string{"OPEN", "LAUNCH"}) {
		t.Fatalf("launcher received unexpected agent actions: %v", got)
	}
	managedDraft := entity.Agent{
		State: "READY", Enabled: true,
		DraftInstructions:            &entity.InstructionVersion{State: "DRAFT"},
		PublishedInstructionVersions: []entity.InstructionVersion{{VersionNumber: 2}, {VersionNumber: 1}},
	}
	if got := agentActions(managedDraft, true, false); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "MANAGE_CAPABILITIES", "DISABLE", "ARCHIVE", "VALIDATE", "ROLLBACK"}) {
		t.Fatalf("instruction draft actions are incorrect: %v", got)
	}
	managedDraft.DraftInstructions.State = "VALID"
	if got := agentActions(managedDraft, true, false); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "MANAGE_CAPABILITIES", "DISABLE", "ARCHIVE", "PUBLISH", "ROLLBACK"}) {
		t.Fatalf("validated instruction actions are incorrect: %v", got)
	}
	workflow := entity.Workflow{State: "PUBLISHED"}
	if got := workflowActions(workflow, false, true); !reflect.DeepEqual(got, []string{"OPEN", "LAUNCH"}) {
		t.Fatalf("launcher received unexpected workflow actions: %v", got)
	}
	workflowCases := []struct {
		state string
		want  []string
	}{
		{state: "DRAFT", want: []string{"OPEN", "EDIT", "VALIDATE", "ARCHIVE"}},
		{state: "VALID", want: []string{"OPEN", "EDIT", "PUBLISH", "ARCHIVE"}},
		{state: "PUBLISHED", want: []string{"OPEN", "EDIT", "ARCHIVE", "LAUNCH"}},
		{state: "ARCHIVED", want: []string{"OPEN"}},
	}
	for _, test := range workflowCases {
		if got := workflowActions(entity.Workflow{State: test.state}, true, true); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("workflowActions(%q)=%v, want %v", test.state, got, test.want)
		}
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
	if got := artifactActions("CLEAN", "ACTIVE", false); !reflect.DeepEqual(got, []string{"DOWNLOAD"}) {
		t.Fatalf("viewer received unexpected artifact actions: %v", got)
	}
	if got := artifactActions("CLEAN", "ACTIVE", true); !reflect.DeepEqual(got, []string{"DOWNLOAD", "BIND", "DELETE"}) {
		t.Fatalf("artifact manager received incorrect active actions: %v", got)
	}
	if got := artifactActions("CLEAN", "DELETED", true); !reflect.DeepEqual(got, []string{"RESTORE", "PURGE"}) {
		t.Fatalf("artifact manager received incorrect trash actions: %v", got)
	}
	if got := artifactActions("CLEAN", "DELETED", false); len(got) != 0 {
		t.Fatalf("viewer received trash mutations: %v", got)
	}
	if got := artifactActions("CLEAN", "PURGE_PENDING", true); len(got) != 0 {
		t.Fatalf("artifact pending purge exposed mutations: %v", got)
	}
	if got := artifactActions("CLEAN", "PURGED", true); len(got) != 0 {
		t.Fatalf("purged artifact exposed mutations: %v", got)
	}
	if got := scheduleActions(entity.Schedule{State: "ACTIVE", Enabled: true}, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received schedule mutations: %v", got)
	}
	if got := scheduleActions(entity.Schedule{State: "ACTIVE", Enabled: true}, true); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "ARCHIVE", "DISABLE"}) {
		t.Fatalf("active schedule manager received incorrect actions: %v", got)
	}
	if got := scheduleActions(entity.Schedule{State: "ARCHIVED"}, true); !reflect.DeepEqual(got, []string{"OPEN", "DELETE"}) {
		t.Fatalf("archived schedule exposed mutations: %v", got)
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
