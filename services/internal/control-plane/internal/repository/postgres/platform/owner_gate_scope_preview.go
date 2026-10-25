package platform

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// integrationScopedGatePreview resolves exact approval scope from the pinned capability and input,
// bounds selected JSON at64KiB and derives sorted mutable schema paths outside selected subtrees.
// Without showValues it returns path/type only but preserves scope digest. Errors are Unavailable;
// this display map neither grants mutable-field authority nor submits an approval.
// integrationScopedGatePreview разрешает точный approval scope по pinned capability и input,
// ограничивает selected JSON до64KiB и выводит сортированные mutable schema paths вне выбранных
// subtrees. Без showValues возвращает лишь path/type, сохраняя scope digest. Ошибки дают
// Unavailable; display map не выдаёт право менять поля и не отправляет approval.
func integrationScopedGatePreview(capability integrationpackage.Capability, input []byte, paths []string, showValues bool) (map[string]any, error) {
	resolved, err := capability.ResolveApprovalScope(paths, input)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	selectedJSON, err := json.Marshal(resolved.Values)
	if err != nil || len(selectedJSON) > 65536 {
		return nil, errs.ErrUnavailable
	}
	rawSchema, err := capability.InputSchema()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var schema map[string]any
	if json.Unmarshal(rawSchema, &schema) != nil {
		return nil, errs.ErrUnavailable
	}
	declared := make([]string, 0, 16)
	if err := collectApprovalSchemaPaths(schema, "", 0, &declared); err != nil {
		return nil, err
	}
	mutable := make([]string, 0, len(declared))
	for _, declaredPath := range declared {
		covered := false
		for _, selectedPath := range paths {
			if declaredPath == selectedPath || strings.HasPrefix(declaredPath, selectedPath+"/") {
				covered = true
				break
			}
		}
		if !covered {
			mutable = append(mutable, declaredPath)
		}
	}
	sort.Strings(mutable)
	selected := any(resolved.Values)
	if !showValues {
		redacted := make([]map[string]string, 0, len(resolved.Values))
		for _, value := range resolved.Values {
			redacted = append(redacted, map[string]string{"path": value.Path, "type": value.Type})
		}
		selected = redacted
	}
	return map[string]any{"selected": selected, "mutablePaths": mutable, "scopeDigest": resolved.Digest}, nil
}

// collectApprovalSchemaPaths recursively walks sorted object properties, escaping JSON Pointer
// segments and treating nonobjects/nested missing properties as leaves. Root shape errors,
// malformed children, depth>8 or an already accumulated count>256 reject; partial paths remain on
// error. It does not expand array items or alternatives, and the entry-time count guard is not an
// exact final256-leaf cap.
// collectApprovalSchemaPaths рекурсивно обходит сортированные object properties, экранирует JSON
// Pointer segments и считает nonobjects и nested missing properties листьями. Неверный root,
// повреждённые children, depth>8 либо уже накопленное число>256 дают отказ; частичные paths
// сохраняются при ошибке. Не разворачивает array items и alternatives, входная count-проверка не
// является точным итоговым лимитом256 листьев.
func collectApprovalSchemaPaths(schema map[string]any, prefix string, depth int, paths *[]string) error {
	if depth > 8 || len(*paths) > 256 {
		return errs.ErrUnavailable
	}
	if schema["type"] != "object" {
		if prefix == "" {
			return errs.ErrUnavailable
		}
		*paths = append(*paths, prefix)
		return nil
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		if prefix == "" {
			return errs.ErrUnavailable
		}
		*paths = append(*paths, prefix)
		return nil
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, ok := properties[key].(map[string]any)
		if !ok {
			return errs.ErrUnavailable
		}
		segment := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
		if err := collectApprovalSchemaPaths(child, prefix+"/"+segment, depth+1, paths); err != nil {
			return err
		}
	}
	return nil
}
