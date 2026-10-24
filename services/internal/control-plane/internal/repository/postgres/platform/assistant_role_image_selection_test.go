package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantImageUpdateSelection checks five omission/template/custom-Dockerfile selection
// cases with exact trailing bytes, explicit wrong-type/empty denials for both fields and the64KiB
// size bound; no catalog, database or image build is used.
// TestAssistantImageUpdateSelection проверяет пять случаев omission/template/custom-Dockerfile
// selection с точными завершающими байтами, явные отказы неверным типам и пустым значениям обоих
// fields и границу64KiB; catalog, БД и сборка образа не используются.
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

// TestAssistantImageOptionInheritance checks same/different/empty environment keys, exact saved
// options, independent populated slices and the previous nil/empty-to-nil shape. Dockerfile bytes
// and caller options for another environment remain unchanged, and the pure helper does not
// validate empty keys or mutate the caller's previous recipe or selection.
// TestAssistantImageOptionInheritance проверяет same/different/empty environment keys, точные
// сохранённые options, независимые заполненные slices и прежнюю nil/empty-to-nil форму. Dockerfile
// bytes и options другого окружения сохраняются; чистый helper не валидирует пустые keys и не
// изменяет прежний recipe либо selection вызывающей стороны.
func TestAssistantImageOptionInheritance(t *testing.T) {
	for _, test := range []struct {
		name, selected, previous, shape string
		packages, tools                 []string
		install                         string
		copied                          bool
	}{
		{"same populated", "promotion", "promotion", "populated", []string{"saved-package", "saved-package"}, []string{"saved-tool"}, "saved install", true},
		{"same nil", "promotion", "promotion", "nil", nil, nil, "saved install", false},
		{"same empty", "promotion", "promotion", "empty", nil, nil, "saved install", false},
		{"different populated", "other", "promotion", "populated", []string{"requested-package"}, []string{"requested-tool"}, "requested install", false},
		{"different nil", "other", "promotion", "nil", []string{"requested-package"}, []string{"requested-tool"}, "requested install", false},
		{"equal empty keys", "", "", "populated", []string{"saved-package", "saved-package"}, []string{"saved-tool"}, "saved install", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection := entity.RoleEnvironmentSelection{EnvironmentKey: test.selected, PackageKeys: []string{"requested-package"}, ToolKeys: []string{"requested-tool"}, InstallationBlock: "requested install", Dockerfile: "\nFROM scratch\n\n"}
			previous := entity.RoleImageRecipeInput{EnvironmentKey: test.previous, InstallationBlock: "saved install", Dockerfile: "old Dockerfile"}
			if test.shape == "populated" {
				previous.PackageKeys, previous.ToolKeys = []string{"saved-package", "saved-package"}, []string{"saved-tool"}
			} else if test.shape == "empty" {
				previous.PackageKeys, previous.ToolKeys = []string{}, []string{}
			}
			want := entity.RoleEnvironmentSelection{EnvironmentKey: test.selected, PackageKeys: test.packages, ToolKeys: test.tools, InstallationBlock: test.install, Dockerfile: "\nFROM scratch\n\n"}
			got := inheritAssistantImageOptions(selection, previous)
			if !reflect.DeepEqual(got, want) || selection.PackageKeys[0] != "requested-package" || selection.ToolKeys[0] != "requested-tool" || selection.InstallationBlock != "requested install" {
				t.Fatal("assistant image option inheritance changed values, empty shape or caller input")
			}
			if test.copied {
				got.PackageKeys[0], got.ToolKeys[0] = "changed-package", "changed-tool"
				if previous.PackageKeys[0] != "saved-package" || previous.ToolKeys[0] != "saved-tool" {
					t.Fatal("inherited assistant options alias the saved recipe")
				}
			}
		})
	}
}
