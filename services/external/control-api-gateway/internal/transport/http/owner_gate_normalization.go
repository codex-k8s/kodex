package httptransport

import (
	"errors"
	"unicode/utf8"
)

var errOwnerGateShape = errors.New("owner gate integration projection is invalid")

func validateOwnerGateProjection(gate map[string]any) error {
	ref, _ := gate["ref"].(string)
	project, _ := gate["projectRef"].(string)
	version, _ := gate["version"].(float64)
	if !fileTargetRef(ref) || !fileTargetRef(project) || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
		return errOwnerGateShape
	}
	switch gate["state"] {
	case "OPEN", "APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED":
	default:
		return errOwnerGateShape
	}
	for _, key := range []string{"sourceAttachmentSetRef", "resolutionAttachmentSetRef"} {
		if value, exists := gate[key]; exists {
			ref, ok := value.(string)
			if !ok || !fileTargetRef(ref) {
				return errOwnerGateShape
			}
		}
	}
	allowed, _ := gate["allowedDecisions"].([]any)
	consequences, _ := gate["decisionConsequences"].([]any)
	if len(allowed) > 4 || len(consequences) > 4 {
		return errOwnerGateShape
	}
	decisions := map[any]bool{}
	for _, decision := range allowed {
		if decisions[decision] {
			return errOwnerGateShape
		}
		decisions[decision] = true
	}
	seen := map[any]bool{}
	for _, item := range consequences {
		consequence, ok := item.(map[string]any)
		if !ok || !decisions[consequence["decision"]] || seen[consequence["decision"]] || !gateText(consequence["safeSummary"], 0, 1000) {
			return errOwnerGateShape
		}
		seen[consequence["decision"]] = true
	}
	if len(seen) != len(decisions) {
		return errOwnerGateShape
	}
	value, exists := gate["integrationIntent"]
	if !exists {
		return nil
	}
	intent, ok := value.(map[string]any)
	if !ok {
		return errOwnerGateShape
	}
	ref, _ = intent["connectionRef"].(string)
	effectKey, _ := intent["effectKey"].(string)
	if !fileTargetRef(ref) || !validManagedDigest(effectKey) || !gateText(intent["connectionName"], 0, 160) ||
		!gateText(intent["definitionKey"], 1, 100) || !gateText(intent["capabilityKey"], 1, 100) || !gateText(intent["operation"], 1, 120) {
		return errOwnerGateShape
	}
	scope, ok := intent["resourceScope"].(map[string]any)
	if !ok {
		return errOwnerGateShape
	}
	digest, _ := scope["digest"].(string)
	if !validManagedDigest(digest) {
		return errOwnerGateShape
	}
	switch scope["kind"] {
	case "SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "MATTERMOST_CHANNEL", "GITLAB_PROJECT", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER":
	default:
		return errOwnerGateShape
	}
	values, ok := scope["values"].(map[string]any)
	if !ok || len(values) > 8 {
		return errOwnerGateShape
	}
	for _, value := range values {
		if !gateText(value, 0, 2048) {
			return errOwnerGateShape
		}
	}
	preview, ok := intent["effectPreview"].(map[string]any)
	if !ok || len(preview) > 32 {
		return errOwnerGateShape
	}
	return nil
}

func gateText(value any, minimum, maximum int) bool {
	text, ok := value.(string)
	return ok && utf8.ValidString(text) && utf8.RuneCountInString(text) >= minimum && utf8.RuneCountInString(text) <= maximum
}
