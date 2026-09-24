package platform

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// Карточка показывает точное выбранное значение и поля схемы, которые
// разрешено менять при следующем вызове без повторного согласования.
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
