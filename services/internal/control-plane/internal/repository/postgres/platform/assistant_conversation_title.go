package platform

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantConversationTitleMaximumRunes limits automatic conversation titles to 80 Unicode runes,
// with word-aware truncation applied by the shared bounding function. This is a metadata size
// bound rather than a content-safety decision.
// assistantConversationTitleMaximumRunes ограничивает автоматические заголовки 80 Unicode-рунами с
// усечением по словам общим ограничителем. Это предел размера метаданных, а не решение о
// безопасности содержимого.
const assistantConversationTitleMaximumRunes = 80

// assistantTitleProtectedInput detects URL/email, bearer/key/password markers, private-key
// headers, known token-like prefixes and protected-input tags before public title extraction. This
// heuristic rejects the entire source except the dedicated URL-only prefix path; it does not parse
// credentials or prove arbitrary text safe.
// assistantTitleProtectedInput обнаруживает URL, email, bearer, маркеры ключей и паролей,
// заголовки private key, известные token-like префиксы и protected tags до публичного заголовка.
// Эвристика отклоняет весь источник кроме отдельного URL-only пути; не разбирает credentials и не
// доказывает безопасность произвольного текста.
var assistantTitleProtectedInput = regexp.MustCompile(`(?i)(?:https?://|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|bearer\s+\S+|(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|token|password|secret|пароль|секрет|ключ)\s*[=:]|-----BEGIN [A-Z ]*PRIVATE KEY-----|\b(?:sk-|ghp_|github_pat_)[a-z0-9_-]+|<\s*(?:protected|secret|credential)[_-]?(?:input|value)?\b)`)

// assistantTitleTechnicalInput recognizes long typed references and 40-to-64-character hexadecimal
// digest-like text. Automatic titles reject these tokens; the structured USER fallback may use
// only preceding human prose.
// assistantTitleTechnicalInput распознаёт длинные типизированные ссылки и похожие на хеш
// шестнадцатеричные строки длины 40–64. Автоматические заголовки отклоняют такие токены; fallback
// USER может взять лишь предшествующую человеческую тему.
var assistantTitleTechnicalInput = regexp.MustCompile(`\b[a-z][a-z0-9]{1,15}_[A-Za-z0-9_-]{16,}\b|\b(?:sha256:)?[a-fA-F0-9]{40,64}\b`)

// assistantTitleProgressInput recognizes a closed English/Russian completion-report prefix
// followed by whitespace, punctuation or end of text. It prevents automatic execution reports from
// becoming topics without changing manually assigned titles.
// assistantTitleProgressInput распознаёт закрытые английские и русские префиксы отчёта о
// выполнении с пробелом, пунктуацией либо концом строки. Не допускает автоматический отчёт вместо
// темы и не меняет ручные заголовки.
var assistantTitleProgressInput = regexp.MustCompile(`(?i)^(?:готово?|done|completed|ready|success|created|prepared|applied|configured|published|updated|создан[аоы]?|подготовлен[аоы]?|примен[её]н[аоы]?|настроен[аоы]?|проверен[аоы]?|опубликован[аоы]?|выполнен[аоы]?|сохран[её]н[аоы]?)(?:\s|[.!:,—-]|$)`)

// assistantTitleStructuredProtectedInput detects quoted credential/key/token/password field names
// followed by a colon in structured input. The structured fallback applies this guard to the whole
// original source before taking a prose prefix; it is a heuristic, not a JSON parser.
// assistantTitleStructuredProtectedInput обнаруживает имена credential, key, token и password в
// кавычках перед двоеточием в структурированном вводе. Fallback проверяет весь исходник до
// извлечения темы; это эвристика, не JSON-парсер.
var assistantTitleStructuredProtectedInput = regexp.MustCompile(`(?i)["'](?:credential|api[_ -]?key|access[_ -]?token|refresh[_ -]?token|token|password|secret|пароль|секрет|ключ)["']\s*:`)

// assistantUserMessageTitle derives a bounded topic only from runtime-contract-valid ordinary USER
// text, trying strict automatic text, URL-only leading prose and structured/technical leading
// prose in order. It rejects fewer than eight runes and generic acknowledgments; attachments,
// protected forms, materialized prompts and credentials are not inputs to this reducer.
// assistantUserMessageTitle выводит ограниченную тему из обычного USER текста, допустимого runtime
// contract: сначала строгий автоматический текст, затем начало перед URL, затем перед структурой и
// техническим телом. Отклоняет менее восьми рун и общие подтверждения; вложения, protected forms,
// материализованные prompts и credentials не передаются редуктору.
func assistantUserMessageTitle(content string) string {
	if !runtimecontract.ValidAssistantTurnContent(content) {
		return ""
	}
	text := assistantAutomaticTitleText(content)
	if text == "" {
		text = assistantUserMessageLeadingTitle(content)
	}
	if text == "" {
		text = assistantUserMessageStructuredTitle(content)
	}
	if len([]rune(text)) < 8 || genericAssistantConversationTitle(text) {
		return ""
	}
	return boundedAssistantConversationTitle(text, assistantConversationTitleMaximumRunes)
}

// assistantUserMessageLeadingTitle accepts a UTF-8 source up to 64 KiB without braces/code fences,
// normalizes its prose and requires every protected-pattern match in the full source to be a URL
// prefix. Only text before the first URL is bounded and rechecked by the strict automatic filter;
// any other protected marker rejects the source.
// assistantUserMessageLeadingTitle принимает UTF-8 источник до 64 KiB без фигурных скобок и code
// fences, нормализует текст и требует, чтобы все protected совпадения во всём источнике были
// URL-префиксами. Только начало до первого URL ограничивается и повторно проверяется строгим
// фильтром; иной protected маркер отклоняет источник.
func assistantUserMessageLeadingTitle(content string) string {
	if len(content) > 64<<10 || !utf8.ValidString(content) ||
		strings.ContainsAny(content, "{}") || strings.Contains(content, "```") {
		return ""
	}
	text := assistantNormalizedTitleText(content)
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
	return assistantAutomaticTitleText(prefix)
}

// assistantUserMessageStructuredTitle validates the complete USER source and rejects protected or
// quoted credential markers anywhere before extracting human prose preceding the first
// structure/code marker or technical identifier. Format/control characters fail closed; the prefix
// is bounded and passes the unchanged automatic-topic filter, so JSON content never supplies the
// title.
// assistantUserMessageStructuredTitle проверяет весь USER источник и отклоняет protected и quoted
// credential маркеры до выбора человеческого текста перед структурой, кодом либо техническим ID.
// Управляющие символы вызывают отказ; ограниченный префикс проходит прежний автоматический фильтр,
// содержимое JSON не становится заголовком.
func assistantUserMessageStructuredTitle(content string) string {
	if !runtimecontract.ValidAssistantTurnContent(content) ||
		assistantTitleProtectedInput.MatchString(content) || assistantTitleStructuredProtectedInput.MatchString(content) {
		return ""
	}
	text := strings.Join(strings.Fields(content), " ")
	if strings.IndexFunc(text, func(value rune) bool {
		return unicode.IsControl(value) || unicode.In(value, unicode.Cf)
	}) >= 0 {
		return ""
	}
	index := strings.IndexAny(text, "{[`")
	if technical := assistantTitleTechnicalInput.FindStringIndex(text); technical != nil && (index < 0 || technical[0] < index) {
		index = technical[0]
	}
	if index == 0 {
		return ""
	}
	if index > 0 {
		text = text[:index]
	}
	prefix := boundedAssistantConversationTitle(strings.TrimRight(text, " :"), assistantConversationTitleMaximumRunes)
	return assistantAutomaticTitleText(prefix)
}

// assistantAutomaticTitleText normalizes strict public text, rejects generic acknowledgments,
// code/bracket syntax, technical references/digests and completion-report prefixes, then bounds
// the topic. This reducer handles automatic sources only; manual user titles do not pass through
// it.
// assistantAutomaticTitleText нормализует строгий публичный текст, отклоняет общие подтверждения,
// код и квадратные скобки, технические ссылки, хеши и отчёты о выполнении, затем ограничивает
// тему. Обрабатывает автоматические источники; ручной заголовок пользователя через него не
// проходит.
func assistantAutomaticTitleText(content string) string {
	text := assistantPublicTitleText(content)
	if genericAssistantConversationTitle(text) || strings.ContainsAny(text, "`[]") ||
		assistantTitleTechnicalInput.MatchString(text) || assistantTitleProgressInput.MatchString(text) {
		return ""
	}
	return boundedAssistantConversationTitle(text, assistantConversationTitleMaximumRunes)
}

// assistantPublicTitleText rejects oversized or invalid UTF-8 sources, protected-pattern matches,
// braces and code fences before shared prose normalization. It returns an empty string on
// rejection and does not persist or log the input; acceptance alone is not an authorization or
// universal redaction guarantee.
// assistantPublicTitleText отклоняет слишком большой либо неверный UTF-8 источник, protected
// совпадения, фигурные скобки и code fences до общей нормализации. При отказе возвращает пустую
// строку, не сохраняет и не логирует ввод; допуск не является полномочиями и универсальной
// гарантией редактирования секретов.
func assistantPublicTitleText(content string) string {
	if len(content) > 64<<10 || !utf8.ValidString(content) || assistantTitleProtectedInput.MatchString(content) ||
		strings.ContainsAny(content, "{}") || strings.Contains(content, "```") {
		return ""
	}
	return assistantNormalizedTitleText(content)
}

// assistantNormalizedTitleText collapses whitespace, removes leading heading/list marker
// characters and surrounding whitespace, then rejects the reserved i18n prefix and remaining
// Unicode control/format characters. It preserves the caller-specific full-source
// size/UTF-8/protected checks outside this helper and performs no truncation, secret detection or
// persistence.
// assistantNormalizedTitleText схлопывает пробелы, убирает начальные маркеры заголовка и списка и
// внешние пробелы, затем отклоняет i18n-префикс и оставшиеся Unicode control/format символы.
// Проверки размера, UTF-8 и protected содержимого всего источника остаются у callers; helper не
// усекает текст, не ищет секреты и не сохраняет данные.
func assistantNormalizedTitleText(content string) string {
	text := strings.TrimSpace(strings.TrimLeft(strings.Join(strings.Fields(content), " "), "#*->"))
	if strings.HasPrefix(text, "i18n:") || strings.IndexFunc(text, func(value rune) bool {
		return unicode.IsControl(value) || unicode.In(value, unicode.Cf)
	}) >= 0 {
		return ""
	}
	return text
}

// genericAssistantConversationTitle case-folds and trims outer whitespace, punctuation and
// symbols, then matches the existing closed set of acknowledgments, placeholders and completion
// summaries. It does not infer topic quality or change an explicit title.
// genericAssistantConversationTitle переводит текст в нижний регистр, убирает внешние пробелы,
// пунктуацию и символы и сравнивает с прежним закрытым перечнем подтверждений, placeholders и
// итоговых фраз. Качество темы не выводит и явный заголовок не меняет.
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

// boundedAssistantConversationTitle returns text unchanged within the rune limit; otherwise cuts
// at the limit or the latest whitespace in its latter half when the next rune would split a word,
// then trims outer whitespace. Callers supply a nonnegative maximum; it does not screen content or
// change UTF-8 bytes inside retained runes.
// boundedAssistantConversationTitle сохраняет текст в пределах рун; иначе обрезает по пределу либо
// последнему пробелу во второй половине, если следующая руна продолжает слово, затем убирает
// внешние пробелы. Вызывающий задаёт неотрицательный максимум; содержимое не фильтруется, байты
// сохранённых рун не меняются.
func boundedAssistantConversationTitle(text string, maximum int) string {
	runes := []rune(text)
	if len(runes) <= maximum {
		return text
	}
	limit := maximum
	if !unicode.IsSpace(runes[maximum]) {
		for index := maximum - 1; index >= maximum/2; index-- {
			if unicode.IsSpace(runes[index]) {
				limit = index
				break
			}
		}
	}
	return strings.TrimSpace(string(runes[:limit]))
}
