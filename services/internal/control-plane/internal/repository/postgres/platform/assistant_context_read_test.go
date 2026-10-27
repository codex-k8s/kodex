package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantContextRow returns a synthetic result or driver error for one query.
// assistantContextRow возвращает синтетический результат либо ошибку драйвера одного запроса.
type assistantContextRow struct{ index, fail int }

// Scan fills the requested output using the ordered synthetic result.
// Scan заполняет результат согласно порядку синтетических запросов.
func (row assistantContextRow) Scan(dest ...any) error {
	if row.index == row.fail {
		return errors.New("synthetic read failure")
	}
	switch row.index {
	case 0:
		*dest[0].(*bool) = true
	case 1:
		*dest[0].(*[]string) = []string{"shared", "grant"}
	case 2:
		*dest[0].(*[]string) = []string{"shared", "connection"}
	case 3:
		*dest[0].(*[]string) = []string{"not-permitted", createProjectFileRevision}
	}
	return nil
}

// assistantContextTx checks query ordering and all authority pins.
// assistantContextTx проверяет порядок запросов и все параметры полномочий.
type assistantContextTx struct {
	pgx.Tx
	t           *testing.T
	count, fail int
}

// QueryRow rejects a changed query, order or pinned scope.
// QueryRow отклоняет изменённые запрос, порядок или закреплённую область.
func (tx *assistantContextTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	i := tx.count
	tx.count++
	queries := []string{queryProjectAssistantContextCreationAllowed, querySystemAssistantIntegrationGrantOperations, queryProjectAssistantConnectionOperations, queryAssistantFileRevisionOperations}
	pins := []pgx.StrictNamedArgs{
		{"organization_id": "org", "actor_id": "actor", "project_ref": "project", "assistant_scope": "PROJECT", "context_kind": "PROJECT"},
		{"organization_id": "org", "actor_id": "actor", "assistant_agent_id": "agent-id", "assistant_scope": "PROJECT", "authority_project": "authority"},
		{"organization_id": "org", "actor_id": "actor", "assistant_agent_id": "agent-id", "assistant_scope": "PROJECT", "authority_project": "authority"},
		{"organization_id": "org", "actor_id": "actor", "authority_project": "authority", "context_kind": "PROJECT", "context_ref": "context", "project_ref": "project", "helper_scope": "PROJECT"},
	}
	if i >= len(queries) || sql != queries[i] || len(args) != 1 || !reflect.DeepEqual(args[0], pins[i]) {
		tx.t.Fatal("query order or scope changed")
	}
	return assistantContextRow{index: i, fail: tx.fail}
}

// TestExpandAssistantContextReadOrder checks success, deduplication, error mapping and partial hints.
// TestExpandAssistantContextReadOrder проверяет успех, уникальность, обработку ошибок и частичные подсказки.
func TestExpandAssistantContextReadOrder(t *testing.T) {
	for fail := -1; fail < 4; fail++ {
		t.Run(string(rune('0'+fail+1)), func(t *testing.T) {
			tx := &assistantContextTx{t: t, fail: fail}
			descriptor := &entity.AssistantContextDescriptor{EntityKind: "PROJECT", EntityRef: "context", AllowedOperations: []string{"base"}}
			err := (&Repository{}).expandAssistantContext(t.Context(), tx, scope{organizationID: "org", actorID: "actor", authorityProjectID: "authority"}, conversationAssistant{ID: "agent-id", Scope: "PROJECT"}, "project", descriptor)
			want := []string{"base"}
			if fail != 0 {
				want = append(want, "CREATE_PROJECT_ASSISTANT")
			}
			if fail < 0 || fail > 1 {
				want = append(want, "shared", "grant")
			}
			if fail < 0 || fail > 2 {
				want = append(want, "connection")
			}
			if fail < 0 {
				want = append(want, createProjectFileRevision)
			}
			if !reflect.DeepEqual(descriptor.AllowedOperations, want) {
				t.Fatalf("incorrect ordered partial hints: %v", descriptor.AllowedOperations)
			}
			if fail < 0 {
				if err != nil || tx.count != 4 {
					t.Fatal("expected four successful ordered reads")
				}
			} else if !errors.Is(err, errs.ErrUnavailable) || tx.count != fail+1 {
				t.Fatal("expected fail-closed read")
			}
		})
	}
}
