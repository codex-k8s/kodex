package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// assistantLeaseContextFixture checks exact owner/run pins and returns a positional synthetic row.
// assistantLeaseContextFixture проверяет точные owner/run pins и возвращает позиционную строку.
type assistantLeaseContextFixture struct {
	pgx.Tx
	t       *testing.T
	failure error
}

// QueryRow checks that the same locking SQL and machine-scoped run are used.
// QueryRow проверяет тот же locking SQL и run в области machine.
func (tx assistantLeaseContextFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.t.Helper()
	if sql != queryRuntimeProposeassistantplanSelectContext || len(args) != 2 || args[0] != "org-id" || args[1] != "run-id" {
		tx.t.Fatal("lease context query or owner pins changed")
	}
	return tx
}

// Scan fills distinct values in every selected field, or returns a synthetic read failure.
// Scan заполняет разные значения во всех выбранных полях либо возвращает ошибку чтения.
func (tx assistantLeaseContextFixture) Scan(dest ...any) error {
	if tx.failure != nil {
		return tx.failure
	}
	if len(dest) != 14 {
		tx.t.Fatal("lease context column count changed")
	}
	values := []string{"conversation-id", "conversation-ref", "project-id", "project-ref", "kind", "context-ref", "assistant-ref", "actor-id", "actor-ref", "actor-name", "role", "org-ref"}
	positions := []int{0, 1, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13}
	for i, position := range positions {
		*dest[position].(*string) = values[i]
	}
	*dest[2].(*int64) = 17
	*dest[5].(*[]string) = []string{"operation"}
	return nil
}

// TestAssistantLeaseContext preserves query pins, every column, correlation and closed failures.
// TestAssistantLeaseContext сохраняет query pins, все столбцы, correlation и закрытые ошибки.
func TestAssistantLeaseContext(t *testing.T) {
	machine := scope{organizationID: "org-id", correlationRef: "correlation", actorID: "machine-actor"}
	got, err := readAssistantLeaseContext(t.Context(), assistantLeaseContextFixture{t: t}, machine, "run-id")
	want := assistantLeaseContext{
		conversationID: "conversation-id", conversationRef: "conversation-ref", conversationVersion: 17,
		projectID: "project-id", projectRef: "project-ref", allowedOperations: []string{"operation"},
		contextKind: "kind", contextRef: "context-ref", assistantRef: "assistant-ref",
		actorScope: scope{correlationRef: "correlation", actorID: "actor-id", actorRef: "actor-ref", actorName: "actor-name", role: "role", organizationRef: "org-ref"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("lease context fields or authority origin changed")
	}
	for _, failure := range []error{pgx.ErrNoRows, errors.New("synthetic driver error")} {
		got, err := readAssistantLeaseContext(t.Context(), assistantLeaseContextFixture{t: t, failure: failure}, machine, "run-id")
		if !errors.Is(err, errs.ErrForbidden) || !reflect.DeepEqual(got, assistantLeaseContext{}) {
			t.Fatal("lease context error did not fail closed")
		}
	}
}
