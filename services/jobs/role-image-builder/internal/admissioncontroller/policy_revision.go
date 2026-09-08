package admissioncontroller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"

	corev1 "k8s.io/api/core/v1"
)

var versionedPolicyNamePattern = regexp.MustCompile(`^kodex-image-admission-policy-[a-f0-9]{32}$`)

func validPolicyName(name string) bool {
	return name == policyName || versionedPolicyNamePattern.MatchString(name)
}

// Прежнее фиксированное имя остаётся читаемым до управляемого перехода.
// Новое имя связывает полный immutable payload с digest, а не выбирает policy из запроса.
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
	// Тот же sorted JSON + newline, что в каноническом renderer.
	actual := sha256.Sum256(encoded.Bytes())
	return hex.EncodeToString(actual[:]) == digest
}
