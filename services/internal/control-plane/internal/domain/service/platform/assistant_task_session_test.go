package platform

import (
	"context"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
)

// taskSessionRepository counts synthetic assistant task-session reads without loading a real run
// or session history.
// taskSessionRepository подсчитывает синтетические чтения сессии задачи помощника без загрузки
// настоящего выполнения либо истории сессии.
type taskSessionRepository struct {
	platformrepo.Repository
	calls int
}

// ResolvePrincipal returns the local task-session fixture principal unchanged for exact workload
// and read-permission assertions.
// ResolvePrincipal возвращает локальный тестовый principal сессии задачи без изменений для
// проверок точных workload и permission чтения.
func (r *taskSessionRepository) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, nil
}

// ReadAssistantTaskSession counts the owner read and returns a synthetic failed task-session state
// without performing a lifecycle transition.
// ReadAssistantTaskSession подсчитывает чтение владельца и возвращает синтетическое ошибочное
// состояние сессии задачи без перехода lifecycle.
func (r *taskSessionRepository) ReadAssistantTaskSession(context.Context, value.Principal, string, string, int64, query.AssistantTaskSessionRead) (query.AssistantTaskSessionPage, error) {
	r.calls++
	return query.AssistantTaskSessionPage{State: "FAILED"}, nil
}

// TestReadAssistantTaskSessionUsesClosedReadPermission checks exact runtime read authority and
// valid lease/fence/generation/run pins before any owner task-session read.
// TestReadAssistantTaskSessionUsesClosedReadPermission проверяет точные runtime-полномочия чтения
// и корректные pins lease, fence, поколения и выполнения до чтения сессии задачи владельцем.
func TestReadAssistantTaskSessionUsesClosedReadPermission(t *testing.T) {
	p := providerTestPrincipal()
	p.CallerWorkload = "runtime-controller"
	p.Permission = "platform.runtime.assistant.resources.search"
	for _, test := range []struct {
		name              string
		principal         value.Principal
		lease, fence, ref string
		generation        int64
		valid             bool
	}{
		{"read", p, "lse_current123", "fence", "run_selected123", 1, true},
		{"empty lease", p, "", "fence", "run_selected123", 1, false},
		{"empty fence", p, "lse_current123", "", "run_selected123", 1, false},
		{"stale generation", p, "lse_current123", "fence", "run_selected123", 0, false},
		{"unsafe locator", p, "lse_current123", "fence", "../run", 1, false},
		{"owner transport", providerTestPrincipal(), "lse_current123", "fence", "run_selected123", 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &taskSessionRepository{}
			service, _ := New(r)
			_, err := service.ReadAssistantTaskSession(t.Context(), test.principal, test.lease, test.fence, test.generation, query.AssistantTaskSessionRead{RunRef: test.ref})
			if (err == nil) != test.valid || r.calls != map[bool]int{true: 1, false: 0}[test.valid] {
				t.Fatal("invalid read reached owner repository")
			}
		})
	}
}
