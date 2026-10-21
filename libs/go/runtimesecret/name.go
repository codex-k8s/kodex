// Package runtimesecret validates server-owned secret scope tuples and derives
// stable names for immutable Kubernetes materializations without using a
// caller-provided path or revealing the secret reference in the object name.
// RU: Пакет runtimesecret проверяет серверные кортежи области секрета и
// создаёт стабильные имена immutable-материализаций Kubernetes без пути от
// вызывающего кода и без раскрытия ссылки на секрет в имени объекта.
package runtimesecret

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

// secretReferencePattern admits bounded opaque secret references with a fixed
// prefix and excludes separators or characters that could change a path.
// RU: secretReferencePattern принимает ограниченные непрозрачные ссылки с
// заданным префиксом и исключает разделители и символы, меняющие путь.
var secretReferencePattern = regexp.MustCompile(`^sec_[A-Za-z0-9_-]{8,92}$`)

// VersionedKubernetesName derives a deterministic object name from the first
// eight SHA-256 bytes of a valid secret reference and a positive revision.
// Retry and revoke can locate the same immutable materialization even after a
// crash between the Kubernetes write and metadata persistence.
// RU: VersionedKubernetesName создаёт детерминированное имя из первых восьми
// байтов SHA-256 допустимой ссылки и положительной ревизии. Повтор и отзыв
// находят ту же immutable-материализацию после сбоя между записью в Kubernetes
// и сохранением метаданных.
func VersionedKubernetesName(secretRef string, revision int64) (string, error) {
	if !secretReferencePattern.MatchString(secretRef) || revision < 1 {
		return "", errors.New("runtime secret identity is invalid")
	}
	digest := sha256.Sum256([]byte(secretRef))
	return fmt.Sprintf("runtime-secret-%s-r%d", hex.EncodeToString(digest[:8]), revision), nil
}
