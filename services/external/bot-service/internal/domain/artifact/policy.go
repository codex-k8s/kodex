package artifact

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"

	"github.com/codex-k8s/matter-codex/libs/go/artifacttype"
)

const (
	DefaultMaxFilesPerTurn = 8
	DefaultMaxObjectBytes  = int64(8 << 20)
	DefaultMaxTurnBytes    = int64(32 << 20)
)

var syntheticSecretPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|auth[_-]?token|password|secret)\s*[:=]\s*["']?[A-Za-z0-9_./+%:@-]{16,}`)

func DetectMediaType(sample []byte) (string, error) {
	detected, err := artifacttype.DetectBytes(sample)
	if err != nil {
		return "", ErrMediaTypeDenied
	}
	return detected, nil
}

func DetectMediaTypeReader(reader io.ReaderAt, size int64) (string, error) {
	detected, err := artifacttype.Detect(reader, size)
	if err != nil {
		return "", ErrMediaTypeDenied
	}
	return detected, nil
}

func SafeExtension(mediaType string) (string, error) {
	extension, err := artifacttype.Extension(mediaType)
	if err != nil {
		return "", ErrMediaTypeDenied
	}
	return extension, nil
}

func SafeLocalName(ordinal int, versionID string, mediaType string) (string, error) {
	if ordinal <= 0 || !validOpaqueID(versionID) {
		return "", fmt.Errorf("artifact local identity is invalid")
	}
	extension, err := SafeExtension(mediaType)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s%s", ordinal, versionID, extension), nil
}

func SafeMetadataName(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	var body strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			body.WriteRune('�')
			continue
		}
		body.WriteRune(r)
		if body.Len() >= 512 {
			break
		}
	}
	if body.Len() == 0 {
		return "unnamed"
	}
	return body.String()
}

func SafeDeliveryName(versionID string, mediaType string) (string, error) {
	extension, err := SafeExtension(mediaType)
	if err != nil {
		return "", err
	}
	if !validOpaqueID(versionID) {
		return "", fmt.Errorf("artifact delivery identity is invalid")
	}
	return "artifact-" + versionID + extension, nil
}

func ContainsSyntheticSecret(mediaType string, body []byte) bool {
	if !artifacttype.IsText(mediaType) {
		return false
	}
	return syntheticSecretPattern.Match(body)
}

func validOpaqueID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
