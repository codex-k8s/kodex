package runtimecontract

import (
	"strings"
	"unicode/utf8"
)

// MaximumAssistantTurnCodepoints aligns the user-content limit with OpenAPI content and PostgreSQL runs.task, counting Unicode codepoints rather than bytes.
// MaximumAssistantTurnCodepoints согласует лимит пользовательского текста с OpenAPI content и PostgreSQL runs.task, считая кодовые точки Unicode, а не байты.
const MaximumAssistantTurnCodepoints = 32768

const (
	// MaximumControlPlaneStreamMessageBytes sets the broker message ceiling shared by bounded control-plane producers.
	// MaximumControlPlaneStreamMessageBytes задаёт общий предел брокерного сообщения для ограниченных отправителей control plane.
	MaximumControlPlaneStreamMessageBytes = 256 << 10
	// MaximumControlPlaneRunEventPayloadBytes reserves four KiB for Nats-Msg-Id, expected-stream and other protocol headers outside the event payload.
	// MaximumControlPlaneRunEventPayloadBytes оставляет четыре КиБ для Nats-Msg-Id, ожидаемого stream и прочих протокольных заголовков вне тела события.
	MaximumControlPlaneRunEventPayloadBytes = MaximumControlPlaneStreamMessageBytes - (4 << 10)
)

// ValidAssistantTurnContent rejects invalid UTF-8, NUL, blank text and codepoint overflow without normalizing or truncating the caller's content.
// ValidAssistantTurnContent отклоняет некорректный UTF-8, NUL, пустой текст и превышение лимита кодовых точек без нормализации и усечения содержимого.
func ValidAssistantTurnContent(content string) bool {
	return utf8.ValidString(content) && utf8.RuneCountInString(content) <= MaximumAssistantTurnCodepoints &&
		strings.TrimSpace(content) != "" && !strings.ContainsRune(content, 0)
}
