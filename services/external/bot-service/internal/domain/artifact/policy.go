package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultMaxFilesPerTurn = 8
	DefaultMaxObjectBytes  = int64(8 << 20)
	DefaultMaxTurnBytes    = int64(32 << 20)
)

var syntheticSecretPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|auth[_-]?token|password|secret)\s*[:=]\s*["']?[A-Za-z0-9_./+%:@-]{16,}`)

var allowedMediaTypes = map[string]string{
	"text/plain":       ".txt",
	"text/markdown":    ".md",
	"text/csv":         ".csv",
	"application/json": ".json",
	"application/pdf":  ".pdf",
	"image/png":        ".png",
	"image/jpeg":       ".jpg",
	"image/webp":       ".webp",
	"image/gif":        ".gif",
}

func DetectMediaType(sample []byte) (string, error) {
	if len(sample) == 0 {
		return "text/plain", nil
	}
	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(sample), ";")[0]))
	if bytes.HasPrefix(sample, []byte("%PDF-")) {
		detected = "application/pdf"
	} else if len(sample) >= 12 && string(sample[:4]) == "RIFF" && string(sample[8:12]) == "WEBP" {
		detected = "image/webp"
	} else if utf8.Valid(sample) && !bytes.ContainsRune(sample, '\x00') {
		if json.Valid(bytes.TrimSpace(sample)) {
			detected = "application/json"
		} else if textualSample(sample) {
			detected = "text/plain"
		}
	}
	if _, ok := allowedMediaTypes[detected]; !ok {
		return "", ErrMediaTypeDenied
	}
	return detected, nil
}

func SafeExtension(mediaType string) (string, error) {
	extension, ok := allowedMediaTypes[strings.ToLower(strings.TrimSpace(mediaType))]
	if !ok {
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
	if mediaType != "text/plain" && mediaType != "text/markdown" && mediaType != "text/csv" && mediaType != "application/json" {
		return false
	}
	return syntheticSecretPattern.Match(body)
}

func textualSample(sample []byte) bool {
	for _, r := range string(sample) {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			continue
		}
		return false
	}
	return true
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
