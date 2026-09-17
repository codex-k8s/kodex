package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRuntimeEnvironmentActionsFollowStateAndAuthority(t *testing.T) {
	tests := []struct {
		name                    string
		state                   string
		revision                int64
		manage, disable, delete bool
		want                    []string
	}{
		{name: "viewer", state: "ACTIVE", revision: 1, want: []string{"OPEN"}},
		{name: "active manager", state: "ACTIVE", revision: 1, manage: true, disable: true, delete: true, want: []string{"OPEN", "UPDATE", "DISABLE"}},
		{name: "published revisions", state: "ACTIVE", revision: 2, manage: true, want: []string{"OPEN", "UPDATE", "ROLLBACK"}},
		{name: "disabled administrator", state: "DISABLED", revision: 2, manage: true, disable: true, delete: true, want: []string{"OPEN", "UPDATE", "ROLLBACK", "ENABLE", "DELETE"}},
		{name: "deleted", state: "DELETED", revision: 2, manage: true, disable: true, delete: true, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := entity.RuntimeEnvironmentSet{State: test.state, CurrentVersion: entity.RuntimeEnvironmentVersion{Revision: test.revision}}
			if got := runtimeEnvironmentActions(item, test.manage, test.disable, test.delete); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("actions=%v want=%v", got, test.want)
			}
		})
	}
}
