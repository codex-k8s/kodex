package platform

import (
	"strings"
	"unicode/utf8"
)

// normalizedIntegrationConnectionName applies one Unicode contract before creation or update:
// valid UTF-8, at most 160 input codepoints, no CR/LF/NUL and a nonempty trimmed display name.
// It validates presentation data only and never resolves a resource, actor or permission.
// normalizedIntegrationConnectionName применяет единый Unicode contract перед create/update:
// допустимый UTF-8, до160 исходных codepoints, без CR/LF/NUL и непустое имя после trim.
// Проверяет лишь данные представления, не разрешая ресурс, actor либо permission.
func normalizedIntegrationConnectionName(raw string) (string, bool) {
	if !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > 160 || strings.ContainsAny(raw, "\x00\r\n") {
		return "", false
	}
	name := strings.TrimSpace(raw)
	return name, name != ""
}
