package httptransport

import (
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// errOwnerGateShape is the fixed public-projection rejection error, avoiding inclusion of gate
// payloads or integration details in diagnostics.
// errOwnerGateShape задаёт фиксированную ошибку отклонения публичной проекции без включения gate
// payload и деталей интеграции в диагностику.
var errOwnerGateShape = errors.New("owner gate integration projection is invalid")

// integrationEffectKeyPattern accepts only the canonical eff_ prefix and 32 lowercase hexadecimal
// characters for an integration intent effect key; matching is shape validation, not proof of a
// receipt.
// integrationEffectKeyPattern допускает только канонический eff_ и 32 шестнадцатеричных символа
// нижнего регистра для ключа эффекта integration intent; совпадение проверяет форму, а не наличие
// receipt.
var integrationEffectKeyPattern = regexp.MustCompile(`^eff_[a-f0-9]{32}$`)

// validateOwnerGateProjection checks the already normalized gate projection, scope/version,
// decision/consequence correspondence and optional integration intent metadata. It bounds
// preview/scope containers without executing an effect or creating an owner decision.
// validateOwnerGateProjection проверяет уже нормализованную проекцию шлюза, область/версию,
// соответствие решений и последствий и необязательные метаданные integration intent. Размеры
// preview/scope ограничиваются без исполнения эффекта и создания решения владельца.
func validateOwnerGateProjection(gate map[string]any) error {
	ref, _ := gate["ref"].(string)
	project, _ := gate["projectRef"].(string)
	organization, _ := gate["organizationRef"].(string)
	version, _ := gate["version"].(float64)
	if !fileTargetRef(ref) || !fileTargetRef(organization) || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
		return errOwnerGateShape
	}
	switch gate["scopeKind"] {
	case "ORGANIZATION":
		if _, present := gate["projectRef"]; present {
			return errOwnerGateShape
		}
	case "PROJECT":
		if !fileTargetRef(project) {
			return errOwnerGateShape
		}
	default:
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
	if !fileTargetRef(ref) || !integrationEffectKeyPattern.MatchString(effectKey) || !gateText(intent["connectionName"], 0, 160) ||
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
	case "SYNTHETIC_JOURNAL", "GITHUB_REPOSITORY", "MATTERMOST_CHANNEL", "GITLAB_PROJECT", "JIRA_PROJECT", "CONFLUENCE_SPACE", "EMAIL_SENDER", "HTTPS_RESOURCE", "SSH_TARGET":
	default:
		return errOwnerGateShape
	}
	values, ok := scope["values"].(map[string]any)
	if !ok || len(values) > 9 || scope["kind"] != "SSH_TARGET" && len(values) > 8 {
		return errOwnerGateShape
	}
	if scope["kind"] == "SSH_TARGET" {
		pins := make(map[string]string, len(values))
		for key, value := range values {
			text, valid := value.(string)
			if !valid {
				return errOwnerGateShape
			}
			pins[key] = text
		}
		if _, err := sshcompose.TargetFromResourceScope(pins); err != nil {
			return errOwnerGateShape
		}
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

// gateText requires a UTF-8 string within inclusive rune-count bounds. It does not trim, localize
// or sanitize text and rejects nonstring values.
// gateText требует UTF-8 строку в включённых границах числа рун. Текст не обрезается, не
// переводится и не очищается; нестроковые значения отклоняются.
func gateText(value any, minimum, maximum int) bool {
	text, ok := value.(string)
	return ok && utf8.ValidString(text) && utf8.RuneCountInString(text) >= minimum && utf8.RuneCountInString(text) <= maximum
}
