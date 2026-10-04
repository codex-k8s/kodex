package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantImageUpdateSelection(t *testing.T) {
	previous := entity.RoleImageRecipeInput{EnvironmentKey: "promotion", Dockerfile: "FROM registry.invalid/base@sha256:" + strings.Repeat("a", 64) + "\n# custom\n"}
	explicit := "FROM registry.invalid/base@sha256:" + strings.Repeat("b", 64) + "\n# explicit\n\n"
	for _, test := range []struct {
		name            string
		parameters      map[string]any
		key, dockerfile string
	}{
		{"name only preserves custom", map[string]any{"name": "Renamed"}, "promotion", previous.Dockerfile},
		{"explicit same key selects fresh template", map[string]any{"environmentKey": "promotion"}, "promotion", ""},
		{"explicit different key selects fresh template", map[string]any{"environmentKey": "other"}, "other", ""},
		{"explicit Dockerfile preserves bytes", map[string]any{"environmentKey": "promotion", "dockerfile": explicit}, "promotion", explicit},
		{"Dockerfile without key preserves bytes", map[string]any{"dockerfile": explicit}, "promotion", explicit},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection, err := assistantImageUpdateSelection(test.parameters, previous)
			if err != nil || selection.EnvironmentKey != test.key || selection.Dockerfile != test.dockerfile {
				t.Fatal("confirmed image selection semantics changed")
			}
		})
	}
	for _, field := range []string{"environmentKey", "dockerfile"} {
		for _, invalid := range []any{nil, "", false, int64(1)} {
			if _, err := assistantImageUpdateSelection(map[string]any{field: invalid}, previous); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("malformed explicit image selection was treated as omitted")
			}
		}
	}
	if _, err := assistantImageUpdateSelection(map[string]any{"dockerfile": strings.Repeat("a", 64<<10+1)}, previous); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("oversized Dockerfile was accepted")
	}
}
