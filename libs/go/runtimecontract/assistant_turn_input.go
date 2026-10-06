package runtimecontract

import (
	"strings"
	"unicode/utf8"
)

// MaximumAssistantTurnCodepoints соответствует OpenAPI content и PostgreSQL runs.task.
const MaximumAssistantTurnCodepoints = 32768

const (
	MaximumControlPlaneStreamMessageBytes = 256 << 10
	// Резерв покрывает Nats-Msg-Id/ожидаемый stream и protocol headers.
	MaximumControlPlaneRunEventPayloadBytes = MaximumControlPlaneStreamMessageBytes - (4 << 10)
)

// ValidAssistantTurnContent проверяет исходный текст без нормализации или усечения.
func ValidAssistantTurnContent(content string) bool {
	return utf8.ValidString(content) && utf8.RuneCountInString(content) <= MaximumAssistantTurnCodepoints &&
		strings.TrimSpace(content) != "" && !strings.ContainsRune(content, 0)
}
