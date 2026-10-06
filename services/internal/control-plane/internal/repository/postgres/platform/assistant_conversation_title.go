package platform

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const assistantConversationTitleMaximumRunes = 80

var assistantTitleProtectedInput = regexp.MustCompile(`(?i)(?:https?://|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|bearer\s+\S+|(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|token|password|secret|пароль|секрет|ключ)\s*[=:]|-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:sk-|ghp_|github_pat_)[a-z0-9_-]+|<\s*(?:protected|secret|credential)[_-]?(?:input|value)?\b)`)

// Название выводится только из обычного публичного текста USER, не из
// attachments, protected forms, materialized prompt или runtime credentials.
// Сомнительный текст не переносится в более широко видимую метадату диалога.
func assistantUserMessageTitle(content string) string {
	text := assistantPublicTitleText(content)
	if text == "" {
		text = assistantUserMessageLeadingTitle(content)
	}
	if len([]rune(text)) < 8 || genericAssistantConversationTitle(text) {
		return ""
	}
	return boundedAssistantConversationTitle(text, assistantConversationTitleMaximumRunes)
}

// Ссылка не скрывает безопасное начало запроса. Остальные запреты
// проверяются по всему исходнику, а общий и модельный фильтры не ослабляются.
func assistantUserMessageLeadingTitle(content string) string {
	if len(content) > 64<<10 || !utf8.ValidString(content) ||
		strings.ContainsAny(content, "{}") || strings.Contains(content, "```") {
		return ""
	}
	text := strings.TrimSpace(strings.TrimLeft(strings.Join(strings.Fields(content), " "), "#*->"))
	if strings.HasPrefix(text, "i18n:") || strings.IndexFunc(text, func(value rune) bool {
		return unicode.IsControl(value) || unicode.In(value, unicode.Cf)
	}) >= 0 {
		return ""
	}
	matches := assistantTitleProtectedInput.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return ""
	}
	for _, match := range matches {
		switch strings.ToLower(text[match[0]:match[1]]) {
		case "http://", "https://":
		default:
			return ""
		}
	}
	prefix := boundedAssistantConversationTitle(text[:matches[0][0]], assistantConversationTitleMaximumRunes)
	return assistantPublicTitleText(prefix)
}

func assistantPublicTitleText(content string) string {
	if len(content) > 64<<10 || !utf8.ValidString(content) || assistantTitleProtectedInput.MatchString(content) ||
		strings.ContainsAny(content, "{}") || strings.Contains(content, "```") {
		return ""
	}
	text := strings.TrimSpace(strings.TrimLeft(strings.Join(strings.Fields(content), " "), "#*->"))
	if strings.HasPrefix(text, "i18n:") || strings.IndexFunc(text, func(value rune) bool {
		return unicode.IsControl(value) || unicode.In(value, unicode.Cf)
	}) >= 0 {
		return ""
	}
	return text
}

func genericAssistantConversationTitle(text string) bool {
	text = strings.ToLower(strings.TrimFunc(text, func(value rune) bool {
		return unicode.IsSpace(value) || unicode.IsPunct(value) || unicode.IsSymbol(value)
	}))
	switch text {
	case "", "готов", "готово", "всё готово", "все готово", "выполнено", "завершено", "сделано", "успешно", "ок", "okay", "ok", "да", "нет", "привет", "здравствуйте", "спасибо", "новый диалог", "новый разговор",
		"план готов", "план подготовлен", "настройка завершена", "настройки сохранены", "изменения применены", "всё настроено", "все настроено",
		"done", "ready", "completed", "success", "hello", "thanks", "thank you", "new chat", "new conversation", "plan ready", "plan is ready", "configuration complete", "changes applied":
		return true
	default:
		return false
	}
}

func boundedAssistantConversationTitle(text string, maximum int) string {
	runes := []rune(text)
	if len(runes) <= maximum {
		return text
	}
	return strings.TrimSpace(string(runes[:maximum]))
}
