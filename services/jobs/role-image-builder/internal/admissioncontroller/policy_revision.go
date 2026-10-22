package admissioncontroller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"

	corev1 "k8s.io/api/core/v1"
)

// versionedPolicyNamePattern restricts a versioned policy name to the fixed base and 32 lowercase hexadecimal suffix characters.
// versionedPolicyNamePattern ограничивает версионированное имя политики фиксированной базой и 32 шестнадцатеричными символами суффикса нижнего регистра.
var versionedPolicyNamePattern = regexp.MustCompile(`^kodex-image-admission-policy-[a-f0-9]{32}$`)

// validPolicyName accepts the existing fixed name or a digest-shaped versioned name without choosing or loading a policy.
// validPolicyName допускает существующее фиксированное имя либо версионированное имя с формой дайджеста без выбора или загрузки политики.
func validPolicyName(name string) bool {
	return name == policyName || versionedPolicyNamePattern.MatchString(name)
}

// versionedPolicyMatches checks a nonnil policy's name against the first half of its full SHA-256 payload digest.
// It hashes sorted-key JSON plus newline with HTML escaping disabled, excluding orchestrationRevision and policySHA256.
// ConfigMap identity/immutability and accepted fixed-name behavior are enforced by callers, not by this helper.
// versionedPolicyMatches проверяет имя ненулевой политики по первой половине полного SHA-256 дайджеста её содержимого.
// Хешируется JSON с упорядоченными ключами и конечным переводом строки без HTML-экранирования, исключая orchestrationRevision и policySHA256.
// Идентичность и immutable-признак ConfigMap и принятие фиксированного имени проверяет вызывающий, а не этот помощник.
func versionedPolicyMatches(policy *corev1.ConfigMap) bool {
	digest := policy.Data["policySHA256"]
	if len(digest) != 64 || policy.Name != policyName+"-"+digest[:32] {
		return false
	}
	payload := make(map[string]string, len(policy.Data))
	for key, value := range policy.Data {
		if key != "orchestrationRevision" && key != "policySHA256" {
			payload[key] = value
		}
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(payload) != nil {
		return false
	}
	// Match the canonical renderer's sorted JSON followed by a newline.
	// Сохранить JSON с упорядоченными ключами и переводом строки, как в каноническом renderer.
	actual := sha256.Sum256(encoded.Bytes())
	return hex.EncodeToString(actual[:]) == digest
}
