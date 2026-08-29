// Package runtimesecret содержит общие безопасные идентификаторы Runtime Secret.
package runtimesecret

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

var secretReferencePattern = regexp.MustCompile(`^sec_[A-Za-z0-9_-]{8,92}$`)

// VersionedKubernetesName возвращает стабильное имя immutable Kubernetes Secret.
// Стабильность позволяет повторному запуску и revoke обнаружить материализацию,
// даже если процесс завершился между записью в Kubernetes и фиксацией metadata.
func VersionedKubernetesName(secretRef string, revision int64) (string, error) {
	if !secretReferencePattern.MatchString(secretRef) || revision < 1 {
		return "", errors.New("runtime secret identity is invalid")
	}
	digest := sha256.Sum256([]byte(secretRef))
	return fmt.Sprintf("runtime-secret-%s-r%d", hex.EncodeToString(digest[:8]), revision), nil
}
