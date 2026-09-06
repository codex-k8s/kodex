package httptransport

import (
	"slices"
	"strings"
	"unicode"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func promptRuntimeDiffView(v *cp.PromptRuntimeDiff) (map[string]any, bool) {
	if v == nil || !fileTargetRef(v.PreviousRevisionRef) || !fileTargetRef(v.SessionRef) || !validManagedDigest(v.Digest) || len(v.Changes) > 13 {
		return nil, false
	}
	result := map[string]any{"previousRevisionRef": v.PreviousRevisionRef, "sessionRef": v.SessionRef, "digest": v.Digest}
	if v.CurrentRevisionRef == "" {
		if v.TurnRef != "" || v.Attempt != 0 {
			return nil, false
		}
	} else {
		if !fileTargetRef(v.CurrentRevisionRef) || v.CurrentRevisionRef == v.PreviousRevisionRef || !fileTargetRef(v.TurnRef) || v.Attempt < 1 {
			return nil, false
		}
		result["currentRevisionRef"], result["turnRef"], result["attempt"] = v.CurrentRevisionRef, v.TurnRef, v.Attempt
	}
	order := []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"}
	changes := make([]any, 0, len(v.Changes))
	last := -1
	for _, change := range v.Changes {
		if change == nil || change.Action != cp.PromptRuntimeAction_PROMPT_RUNTIME_ACTION_USE_CURRENT_CONTEXT {
			return nil, false
		}
		component := strings.TrimPrefix(change.Component.String(), "PROMPT_RUNTIME_COMPONENT_")
		position := slices.Index(order, component)
		if position <= last {
			return nil, false
		}
		last = position
		previous, ok := promptRuntimeDescriptors(change.Previous)
		if !ok {
			return nil, false
		}
		current, ok := promptRuntimeDescriptors(change.Current)
		if !ok {
			return nil, false
		}
		changes = append(changes, map[string]any{"component": component, "previous": previous, "current": current, "action": "USE_CURRENT_CONTEXT"})
	}
	result["changes"] = changes
	return result, true
}

func promptRuntimeDescriptors(values []*cp.PromptRuntimeDescriptor) ([]any, bool) {
	if len(values) > 256 {
		return nil, false
	}
	result := make([]any, 0, len(values))
	for _, value := range values {
		if value == nil || !validSearchText(value.Ref, 0, 128) || !promptText(value.Value, 256) || strings.ContainsFunc(value.Ref+value.Value, unicode.IsControl) || value.Version < 0 || value.Version > maximumSafeJSONInteger || value.Digest != "" && !validManagedDigest(value.Digest) {
			return nil, false
		}
		item := map[string]any{}
		if value.Ref != "" {
			item["ref"] = value.Ref
		}
		if value.Version != 0 {
			item["version"] = value.Version
		}
		if value.Digest != "" {
			item["digest"] = value.Digest
		}
		if value.Value != "" {
			item["value"] = value.Value
		}
		result = append(result, item)
	}
	return result, true
}
