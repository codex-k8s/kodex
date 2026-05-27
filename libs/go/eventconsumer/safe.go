package eventconsumer

import "strings"

func safeToken(text string, fallback string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fallback
	}
	var builder strings.Builder
	for _, char := range trimmed {
		if char >= 'a' && char <= 'z' {
			builder.WriteRune(char)
			continue
		}
		if char >= 'A' && char <= 'Z' {
			builder.WriteRune(char)
			continue
		}
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
			continue
		}
		switch char {
		case '-', '_', '.', ':', '/', '@':
			builder.WriteRune(char)
		default:
			builder.WriteByte('_')
		}
	}
	value := builder.String()
	if value == "" {
		return fallback
	}
	if len(value) > 160 {
		return value[:160]
	}
	return value
}

func safeSummary(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if limit < 1 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
