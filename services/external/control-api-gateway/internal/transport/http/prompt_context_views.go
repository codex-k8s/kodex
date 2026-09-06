package httptransport

import (
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func promptContextPinView(v *cp.PromptContextPin) (map[string]any, bool) {
	if v == nil {
		return nil, true
	}
	if !validManagedDigest(v.Digest) {
		return nil, false
	}
	result := map[string]any{"digest": v.Digest}
	for key, ref := range map[string]string{"agentRef": v.AgentRef, "workflowRef": v.WorkflowRef, "workflowRevisionRef": v.WorkflowRevisionRef, "runtimeConfigurationRef": v.RuntimeConfigurationRef, "environmentBindingRef": v.EnvironmentBindingRef, "environmentVersionRef": v.EnvironmentVersionRef, "attachmentSetRef": v.AttachmentSetRef, "previousRuntimeRevisionRef": v.PreviousRuntimeRevisionRef} {
		if ref != "" {
			if !fileTargetRef(ref) {
				return nil, false
			}
			result[key] = ref
		}
	}
	for key, digest := range map[string]string{"runtimeConfigurationDigest": v.RuntimeConfigurationDigest, "environmentDigest": v.EnvironmentDigest, "attachmentManifestDigest": v.AttachmentManifestDigest} {
		if digest != "" {
			if !validManagedDigest(digest) {
				return nil, false
			}
			result[key] = digest
		}
	}
	for key, version := range map[string]int64{"agentVersion": v.AgentVersion, "workflowVersion": v.WorkflowVersion, "environmentBindingVersion": v.EnvironmentBindingVersion} {
		if version != 0 {
			if !validManagedVersion(version) {
				return nil, false
			}
			result[key] = version
		}
	}
	if (v.AgentRef == "") != (v.AgentVersion == 0) || (v.WorkflowRef == "") != (v.WorkflowVersion == 0) || (v.WorkflowRef == "") != (v.WorkflowRevisionRef == "") || (v.WorkflowRef == "") != (v.WorkflowStageKey == "") || (v.RuntimeConfigurationRef == "") != (v.RuntimeConfigurationDigest == "") || (v.EnvironmentBindingRef == "") != (v.EnvironmentBindingVersion == 0) || (v.EnvironmentBindingRef == "") != (v.EnvironmentVersionRef == "") || (v.EnvironmentBindingRef == "") != (v.EnvironmentDigest == "") || (v.AttachmentSetRef == "") != (v.AttachmentManifestDigest == "") {
		return nil, false
	}
	if v.WorkflowStageKey != "" {
		if !promptText(v.WorkflowStageKey, 128) {
			return nil, false
		}
		result["workflowStageKey"] = v.WorkflowStageKey
	}
	return result, true
}

func promptDiagnosticViews(values []*cp.PromptTemplateDiagnostic) ([]any, bool) {
	if len(values) > 100 {
		return nil, false
	}
	result := make([]any, 0, len(values))
	for _, v := range values {
		if v == nil || v.Severity != "ERROR" && v.Severity != "WARNING" || !validPromptDiagnosticCode(v.Code) || !promptText(v.Message, 500) || v.Line < 1 || v.Column < 1 || !promptText(v.VariableName, 160) {
			return nil, false
		}
		item := map[string]any{"severity": v.Severity, "code": v.Code, "message": v.Message, "line": v.Line, "column": v.Column}
		if v.VariableName != "" {
			item["variableName"] = v.VariableName
		}
		result = append(result, item)
	}
	return result, true
}

func validPromptDiagnosticCode(value string) bool {
	if len(value) < 1 || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func promptValidationView(v *cp.ValidatePromptTemplateResponse) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	diagnostics, ok := promptDiagnosticViews(v.Diagnostics)
	if !ok {
		return nil, false
	}
	pin, ok := promptContextPinView(v.ContextPin)
	if !ok {
		return nil, false
	}
	if v.Valid {
		for _, d := range v.Diagnostics {
			if d.Severity == "ERROR" {
				return nil, false
			}
		}
	}
	result := map[string]any{"valid": v.Valid, "diagnostics": diagnostics}
	if pin != nil {
		result["contextPin"] = pin
	}
	return result, true
}

func promptSlotName(v cp.PromptSemanticSlot) (string, bool) {
	name, ok := cp.PromptSemanticSlot_name[int32(v)]
	name = strings.TrimPrefix(name, "PROMPT_SEMANTIC_SLOT_")
	if !ok {
		return "", false
	}
	switch name {
	case "WORKFLOW", "STAGE", "PURPOSE", "EXPECTED_RESULT", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS", "RUNTIME_CHANGES":
		return name, true
	default:
		return "", false
	}
}

func promptSourceName(v cp.PromptSectionSource) (string, bool) {
	switch v {
	case cp.PromptSectionSource_PROMPT_SECTION_SOURCE_PLATFORM:
		return "PLATFORM", true
	case cp.PromptSectionSource_PROMPT_SECTION_SOURCE_USER_TEMPLATE:
		return "USER_TEMPLATE", true
	default:
		return "", false
	}
}

func promptPreviewView(v *cp.PreviewPromptTemplateResponse, full bool) (map[string]any, bool) {
	if v == nil || !fileTargetRef(v.TemplateRef) || !promptText(v.SafePreview, 256<<10) || !promptText(v.FullMaterializedPrompt, 1<<20) || !full && v.FullMaterializedPrompt != "" || full && v.FullMaterializedPrompt == "" || !promptText(v.ServiceTemplateRevision, 128) || v.ServiceTemplateRevision == "" || !promptText(v.Locale, 16) || v.Locale == "" || len(v.Slots) < 1 || len(v.Slots) > 11 || len(v.Sections) < 1 || len(v.Sections) > 32 || len(v.EffectiveCapabilities) > 128 {
		return nil, false
	}
	for _, digest := range []string{v.TemplateDigest, v.MaterializationDigest, v.ServiceTemplateDigest, v.VariableSnapshotDigest} {
		if !validManagedDigest(digest) {
			return nil, false
		}
	}
	diagnostics, ok := promptDiagnosticViews(v.Diagnostics)
	if !ok {
		return nil, false
	}
	pin, ok := promptContextPinView(v.ContextPin)
	if !ok {
		return nil, false
	}
	if v.Complete {
		for _, d := range v.Diagnostics {
			if d.Severity == "ERROR" || d.Code == "RUNTIME_CONTEXT_REQUIRED" {
				return nil, false
			}
		}
	}
	seen := map[string]bool{}
	capabilities := make([]string, 0, len(v.EffectiveCapabilities))
	for _, capability := range v.EffectiveCapabilities {
		if capability == "" || !promptText(capability, 128) || seen[capability] {
			return nil, false
		}
		seen[capability] = true
		capabilities = append(capabilities, capability)
	}
	slots := make([]any, 0, len(v.Slots))
	slotSources := map[string]string{}
	for i, item := range v.Slots {
		if item == nil || item.Position != int32(i+1) {
			return nil, false
		}
		slot, valid := promptSlotName(item.Slot)
		source, sourceValid := promptSourceName(item.Source)
		if !valid || !sourceValid || slotSources[slot] != "" {
			return nil, false
		}
		slotSources[slot] = source
		slots = append(slots, map[string]any{"slot": slot, "source": source, "position": item.Position})
	}
	sections := make([]any, 0, len(v.Sections))
	implicitTailStarted := false
	sectionSlots := map[string]bool{}
	userOrder := map[string]int{"BASE_TEMPLATE": 0, "WORKFLOW_CONTEXT": 1, "AUTOMATION_TASK": 2}
	lastUser := -1
	userPins := map[string]string{}
	for _, item := range v.Sections {
		if item == nil || !promptText(item.Content, 1<<20) {
			return nil, false
		}
		source, valid := promptSourceName(item.Source)
		if !valid {
			return nil, false
		}
		section := map[string]any{"source": source, "content": item.Content}
		if source == "PLATFORM" {
			slot, valid := promptSlotName(item.Slot)
			if !valid || slotSources[slot] == "" || sectionSlots[slot] || item.UserKind != 0 || item.TemplateRef != "" || item.TemplateDigest != "" {
				return nil, false
			}
			if len(sectionSlots) >= len(v.Slots) || v.Slots[len(sectionSlots)].Slot != item.Slot {
				return nil, false
			}
			if slotSources[slot] == "PLATFORM" {
				implicitTailStarted = true
			} else if implicitTailStarted {
				return nil, false
			}
			sectionSlots[slot] = true
			section["slot"] = slot
		} else {
			if implicitTailStarted || item.Slot != 0 || !fileTargetRef(item.TemplateRef) || !validManagedDigest(item.TemplateDigest) {
				return nil, false
			}
			kind, exists := cp.PromptUserSectionKind_name[int32(item.UserKind)]
			if !exists || item.UserKind == 0 {
				return nil, false
			}
			kind = strings.TrimPrefix(kind, "PROMPT_USER_SECTION_KIND_")
			if kind != "BASE_TEMPLATE" && kind != "WORKFLOW_CONTEXT" && kind != "AUTOMATION_TASK" {
				return nil, false
			}
			if userOrder[kind] < lastUser || kind == "BASE_TEMPLATE" && (item.TemplateRef != v.TemplateRef || item.TemplateDigest != v.TemplateDigest) {
				return nil, false
			}
			lastUser = userOrder[kind]
			binding := item.TemplateRef + "/" + item.TemplateDigest
			if previous := userPins[kind]; previous != "" && previous != binding {
				return nil, false
			}
			userPins[kind] = binding
			section["userKind"] = kind
			section["templateRef"] = item.TemplateRef
			section["templateDigest"] = item.TemplateDigest
		}
		sections = append(sections, section)
	}
	for slot := range slotSources {
		if !sectionSlots[slot] {
			return nil, false
		}
	}
	result := map[string]any{"safePreview": v.SafePreview, "complete": v.Complete, "diagnostics": diagnostics, "templateRef": v.TemplateRef, "templateDigest": v.TemplateDigest, "materializationDigest": v.MaterializationDigest, "effectiveCapabilities": capabilities, "serviceTemplateRevision": v.ServiceTemplateRevision, "serviceTemplateDigest": v.ServiceTemplateDigest, "variableSnapshotDigest": v.VariableSnapshotDigest, "locale": v.Locale, "slots": slots, "sections": sections}
	if full {
		result["fullMaterializedPrompt"] = v.FullMaterializedPrompt
	}
	if pin != nil {
		result["contextPin"] = pin
	}
	if v.RuntimeDiff != nil {
		if v.ContextPin != nil && v.ContextPin.PreviousRuntimeRevisionRef != "" && v.RuntimeDiff.PreviousRevisionRef != v.ContextPin.PreviousRuntimeRevisionRef {
			return nil, false
		}
		diff, ok := promptRuntimeDiffView(v.RuntimeDiff)
		if !ok {
			return nil, false
		}
		result["runtimeDiff"] = diff
	}
	return result, true
}
