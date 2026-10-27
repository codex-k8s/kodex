package platform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestFinishRehydratedAssistantOperation verifies snapshot and selection handling.
// TestFinishRehydratedAssistantOperation проверяет обработку снимка и признака выбора.
func TestFinishRehydratedAssistantOperation(t *testing.T) {
	cause := errors.New("hydrate failed")
	cases := []struct {
		name     string
		before   map[string]any
		hydrated entity.AssistantPlanOperation
		selected bool
		cause    error
		wantErr  error
	}{
		{name: "nested equal selected", before: map[string]any{"x": []any{map[string]any{"y": 1}}}, hydrated: entity.AssistantPlanOperation{Key: "key", Before: map[string]any{"x": []any{map[string]any{"y": 1}}}}, selected: true},
		{name: "nil equal deselected", hydrated: entity.AssistantPlanOperation{Key: "key", Selected: true}},
		{name: "nil differs from empty", before: map[string]any{}, hydrated: entity.AssistantPlanOperation{Key: "key"}, wantErr: errs.ErrConflict},
		{name: "snapshot drift", before: map[string]any{"x": 1}, hydrated: entity.AssistantPlanOperation{Key: "key", Before: map[string]any{"x": 2}}, wantErr: errs.ErrConflict},
		{name: "hydration error precedes drift", before: map[string]any{"x": 1}, hydrated: entity.AssistantPlanOperation{Key: "key"}, cause: cause, wantErr: cause},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.hydrated
			got, err := finishRehydratedAssistantOperation(tc.before, tc.selected, tc.hydrated, tc.cause)
			if err != tc.wantErr {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if !reflect.DeepEqual(got, entity.AssistantPlanOperation{}) {
					t.Fatalf("error returned nonzero operation: %#v", got)
				}
				return
			}
			original.Selected = tc.selected
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("operation = %#v, want %#v", got, original)
			}
		})
	}
}
