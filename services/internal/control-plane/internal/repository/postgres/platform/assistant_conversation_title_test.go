package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAssistantUserMessageTitle checks literal ordinary-topic, whitespace, minimum-length,
// generic/i18n, UTF-8/control and source-size cases plus bounded long Cyrillic text. All inputs
// are synthetic and the test exercises title projection only, without runtime execution or
// persistence.
// TestAssistantUserMessageTitle проверяет буквальные темы, пробелы, минимальную длину, generic и
// i18n, UTF-8, control и размер источника, а также длинный кириллический текст. Ввод
// синтетический; проверяется только проекция заголовка без runtime и сохранения.
func TestAssistantUserMessageTitle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, input, want string }{
		{"request", "Настрой системного помощника и подключи Context7", "Настрой системного помощника и подключи Context7"},
		{"whitespace", "  ## Настрой\n\tсистемного   помощника  ", "Настрой системного помощника"},
		{"short meaningful", "Настрой MCP", "Настрой MCP"},
		{"empty", " \n\t ", ""},
		{"ack", "Готов!", ""},
		{"generic", "Новый диалог", ""},
		{"localized placeholder", "i18n:NEW_ASSISTANT_CONVERSATION", ""},
		{"other localized diagnostic", "i18n:PROTECTED_INPUT", ""},
		{"invalid encoding", "Настрой " + string([]byte{0xff}), ""},
		{"invisible control", "Настрой\u202eпомощника", ""},
		{"bounded source", strings.Repeat("я", (64<<10)+1), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := assistantUserMessageTitle(test.input); got != test.want {
				t.Fatalf("unexpected title: got %q want %q", got, test.want)
			}
		})
	}
	title := assistantUserMessageTitle(strings.Repeat("Настройка ", 40))
	if !utf8.ValidString(title) || len([]rune(title)) > assistantConversationTitleMaximumRunes || !strings.HasPrefix(title, "Настройка") {
		t.Fatal("title did not preserve bounded UTF-8 request text")
	}
}

// TestAssistantUserMessageTitleKeepsEarlyProseBeforeTechnicalBody requires the same human topic
// before fenced JSON, typed refs, digests and a large benign structured body. It proves only the
// explicit leading-prose fallback, not acceptance of technical content as metadata.
// TestAssistantUserMessageTitleKeepsEarlyProseBeforeTechnicalBody требует одинаковую человеческую
// тему перед fenced JSON, типизированными ссылками, хешами и большим безопасным структурированным
// телом. Проверяет только явный fallback начала, не допуск технического тела в метаданные.
func TestAssistantUserMessageTitleKeepsEarlyProseBeforeTechnicalBody(t *testing.T) {
	t.Parallel()
	const topic = "Настрой окружение помощника"
	for _, input := range []string{
		topic + ":\n```json\n{\"imageArtifactRef\":\"imgart_0123456789abcdefghijklmn\"}\n```",
		topic + ": renv_0123456789abcdefghijklmn; далее параметры конфигурации",
		topic + ": sha256:" + strings.Repeat("a", 64),
		topic + ": {\"description\":\"" + strings.Repeat("詳", 28000) + "\"}",
	} {
		if got := assistantUserMessageTitle(input); got != topic {
			t.Fatalf("safe early topic = %q, want %q", got, topic)
		}
	}
}

// TestAssistantUserMessageTechnicalPrefixPreservesWholeInputGuards rejects whole-source synthetic
// protected fields, password markers, URLs, oversized content, control/format characters and
// structure-first input even when a normal topic appears elsewhere. No real credential or external
// request is used.
// TestAssistantUserMessageTechnicalPrefixPreservesWholeInputGuards отклоняет синтетические
// protected поля, password маркеры, URL, слишком большой ввод, control/format символы и структуру
// в начале даже при наличии обычной темы. Реальные credentials и внешние запросы не используются.
func TestAssistantUserMessageTechnicalPrefixPreservesWholeInputGuards(t *testing.T) {
	t.Parallel()
	const topic = "Настрой окружение помощника"
	for _, input := range []string{
		topic + ":\n```json\n{\"token\":\"SYNTHETIC_PRIVATE_VALUE\"}\n```",
		topic + ": renv_0123456789abcdefghijklmn password=SYNTHETIC_PRIVATE_VALUE",
		topic + ": {\"url\":\"https://fixture.invalid/private\"}",
		topic + ": {\"description\":\"" + strings.Repeat("詳", 32768) + "\"}",
		topic + ": {\"description\":\"value\"}\u0000",
		topic + ": {\"description\":\"value\"}\u202e",
		"```json\n{\"name\":\"Настрой окружение помощника\"}\n```",
	} {
		if assistantUserMessageTitle(input) != "" {
			t.Fatal("invalid or protected technical input reached title metadata")
		}
	}
}

// TestAssistantConversationTitleDoesNotDiscloseProtectedInput checks synthetic
// password/key/bearer/token-like/header/tag/URL/email/code patterns across USER, public and
// automatic reducers. These are inert fixtures inherited from the source, not live secrets; the
// test does not claim exhaustive secret classification.
// TestAssistantConversationTitleDoesNotDiscloseProtectedInput проверяет синтетические password,
// key, bearer, token-like, header, tag, URL, email и code шаблоны через USER, public и automatic
// редукторы. Это неизменённые неисполняемые fixtures исходника, не живые секреты; полнота
// классификации секретов не заявляется.
func TestAssistantConversationTitleDoesNotDiscloseProtectedInput(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"Настрой пароль: SYNTHETIC_PRIVATE_VALUE",
		"Настрой api_key=SYNTHETIC_PRIVATE_VALUE",
		"Настрой Authorization: Bearer SYNTHETIC_PRIVATE_VALUE",
		"Настрой с sk-syntheticprivatevalue",
		"Настрой с github_pat_syntheticprivatevalue",
		"Настрой <protected_input>SYNTHETIC_PRIVATE_VALUE</protected_input>",
		`Настрой {"credential": "SYNTHETIC_PRIVATE_VALUE"}`,
		"Настрой ```SYNTHETIC_PRIVATE_VALUE```",
		"Настрой https://fixture.invalid/private?value=SYNTHETIC_PRIVATE_VALUE",
		"Настрой synthetic@example.invalid",
		"Настрой -----BEGIN PRIVATE KEY----- SYNTHETIC_PRIVATE_VALUE",
	} {
		if assistantUserMessageTitle(input) != "" || assistantPublicTitleText(input) != "" ||
			assistantAutomaticTitleText(input) != "" {
			t.Fatal("protected text reached conversation metadata")
		}
	}
}

// TestAssistantUserMessageTitleUsesSafePrefixBeforeLateURL checks exact bounded leading topics for
// long synthetic/system and Russian requests, multiple URL cases and whitespace normalization. It
// also confirms the shared public/automatic filters still reject the original URL-bearing source;
// embedded shell text is never executed.
// TestAssistantUserMessageTitleUsesSafePrefixBeforeLateURL проверяет точные ограниченные темы
// длинных синтетических и русских запросов, несколько URL и нормализацию пробелов. Одновременно
// подтверждает отказ общих public и automatic фильтров для всего URL-источника; встроенный текст
// shell не исполняется.
func TestAssistantUserMessageTitleUsesSafePrefixBeforeLateURL(t *testing.T) {
	t.Parallel()
	const system57 = "QA_SYSTEM_GITHUB_57. Маркер KODEX1797_GEN9_GITHUB_407_A2. Только READ smoke Issue1797. Через фактический native exec выполни отдельно git --version и env GIT_TERMINAL_PROMPT=0 git -c protocol.version=0 -c credential.helper= -c credential.interactive=false ls-remote -- https://github.com/codex-k8s/kodex.git HEAD refs/heads/main. protocol.version=0 нужен для GET-only правил текущего окружения, не меняй policy. Без clone/fetch, записей, credentials и изменений GitHub. Верни exit codes и полученные refs/SHA. При отказе сообщи фактическую ошибку; не заменяй вызов памятью/web."
	const russian = "Проверь настройку помощника и доступность опубликованного репозитория организации перед следующим этапом разработки. Ссылка: https://fixture.invalid/repository"
	const russianTitle = "Проверь настройку помощника и доступность опубликованного репозитория"
	for _, test := range []struct{ name, input, want string }{
		{"system57", system57, "QA_SYSTEM_GITHUB_57. Маркер KODEX1797_GEN9_GITHUB_407_A2. Только READ smoke"},
		{"russian", russian, russianTitle},
		{"uppercase and multiple urls", russian + " HTTP://fixture.invalid/second", russianTitle},
		{"whitespace", "  ## " + strings.ReplaceAll(russian, " ", "\n\t"), russianTitle},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := assistantUserMessageTitle(test.input); got != test.want {
				t.Fatalf("unexpected safe prefix: got %q want %q", got, test.want)
			}
			if assistantPublicTitleText(test.input) != "" || assistantAutomaticTitleText(test.input) != "" {
				t.Fatal("late URL changed the shared or terminal title filter")
			}
		})
	}
}

// TestAssistantUserMessageLateURLKeepsWholeInputGuards checks both long and short leading prose
// against protected/email/token-like/code/JSON suffixes, malformed encoding, controls and size
// overflow anywhere in the source. Early URLs and generic/reserved prefixes still return no title;
// all credential-looking strings are synthetic fixtures.
// TestAssistantUserMessageLateURLKeepsWholeInputGuards проверяет длинное и короткое начало при
// protected, email, token-like, code и JSON суффиксах, неверной кодировке, control и переполнении
// всего источника. Ранние URL, generic и reserved начала не дают заголовка; credential-like строки
// — синтетические fixtures.
func TestAssistantUserMessageLateURLKeepsWholeInputGuards(t *testing.T) {
	t.Parallel()
	const prefix = "Проверь настройку помощника и доступность опубликованного репозитория организации перед следующим этапом разработки. "
	const lateURL = "https://fixture.invalid/repository"
	for _, test := range []struct{ name, suffix string }{
		{"email", " synthetic@example.invalid"},
		{"secret", " api_key=SYNTHETIC_PRIVATE_VALUE"},
		{"secret in URL", "?token=SYNTHETIC_PRIVATE_VALUE"},
		{"email in URL", "/synthetic@example.invalid"},
		{"bearer", " Bearer SYNTHETIC_PRIVATE_VALUE"},
		{"token", " github_pat_syntheticprivatevalue"},
		{"private key", " -----BEGIN PRIVATE KEY-----"},
		{"protected", " <protected_input>SYNTHETIC_PRIVATE_VALUE</protected_input>"},
		{"json", ` {"credential":"SYNTHETIC_PRIVATE_VALUE"}`},
		{"code", " ```SYNTHETIC_PRIVATE_VALUE```"},
		{"invalid UTF8", string([]byte{0xff})},
		{"control", "\x00"},
		{"format control", "\u202e"},
		{"too large", strings.Repeat("я", 64<<10)},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, leading := range []string{prefix, "Проверь документацию Vue "} {
				if assistantUserMessageTitle(leading+lateURL+test.suffix) != "" {
					t.Fatal("unsafe original input reached the leading title")
				}
			}
		})
	}
	for _, input := range []string{
		lateURL + " Проверь документацию Vue",
		"Настрой " + lateURL,
		"i18n:PROTECTED_INPUT " + prefix + lateURL,
		"Готов! " + lateURL,
		"Новый диалог " + lateURL,
	} {
		if assistantUserMessageTitle(input) != "" {
			t.Fatal("early URL or reserved title input reached the leading title")
		}
	}
}

// TestAssistantUserMessageTitleUsesShortPrefixBeforeURL checks literal short Russian and
// eight-rune topics, mixed URL case, repeated links and a URL crossing the rune bound. Shared
// public/automatic filtering of the full source must remain strict while only the USER path may
// keep its safe prefix.
// TestAssistantUserMessageTitleUsesShortPrefixBeforeURL проверяет короткую русскую и восьмирунную
// тему, регистр URL, повторные ссылки и URL на границе рун. Общие public и automatic фильтры всего
// источника сохраняют строгость; безопасное начало допускает только USER путь.
func TestAssistantUserMessageTitleUsesShortPrefixBeforeURL(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, input, want string }{
		{"short Russian", "Проверь документацию Vue https://vuejs.org/guide/essentials/computed.html", "Проверь документацию Vue"},
		{"eight runes", "Проверка https://fixture.invalid/repository", "Проверка"},
		{"whitespace", "  ## Проверь\nдокументацию\tVue HTTPS://vuejs.org/guide/ http://fixture.invalid/second", "Проверь документацию Vue"},
		{"URL crosses title limit", strings.Repeat("я", assistantConversationTitleMaximumRunes-1) + "https://fixture.invalid/repository", strings.Repeat("я", assistantConversationTitleMaximumRunes-1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := assistantUserMessageTitle(test.input); got != test.want {
				t.Fatalf("unexpected short prefix: got %q want %q", got, test.want)
			}
			if assistantPublicTitleText(test.input) != "" || assistantAutomaticTitleText(test.input) != "" {
				t.Fatal("short prefix changed the shared or terminal title filter")
			}
		})
	}
}

// TestAssistantModelTitleRejectsGenericSummary rejects the closed generic/completion/i18n examples
// while retaining explicit normal topic examples through the relevant public/generic checks. It
// performs no model call or evaluation of arbitrary generated output.
// TestAssistantModelTitleRejectsGenericSummary отклоняет фиксированные generic, completion и i18n
// примеры, сохраняя обычные темы соответствующими public и generic проверками. Вызов модели и
// оценка произвольного вывода отсутствуют.
func TestAssistantModelTitleRejectsGenericSummary(t *testing.T) {
	t.Parallel()
	for _, summary := range []string{"готов", "Готово.", "Done!", "План готов", "Настройка завершена", "Изменения применены", "Всё готово", "New conversation", "", "i18n:RUN_COMPLETED"} {
		if assistantAutomaticTitleText(summary) != "" {
			t.Fatal("generic completion summary became a title")
		}
	}
	for _, title := range []string{"Настройка Context7", "MCP", "Образ для разработки Kodex"} {
		if genericAssistantConversationTitle(title) || assistantPublicTitleText(title) == "" {
			t.Fatal("normal agent-proposed title was rejected")
		}
	}
}

// TestAssistantModelTitleRejectsTechnicalAndProgressResults rejects literal plan/completion
// reports, typed refs, digests and bracketed technical summaries from the automatic reducer. The
// examples are local strings, not accepted model receipts or execution evidence.
// TestAssistantModelTitleRejectsTechnicalAndProgressResults отклоняет буквальные plan и completion
// отчёты, типизированные ссылки, хеши и технические списки автоматическим редуктором. Примеры —
// локальные строки, не квитанции модели и не доказательство исполнения.
func TestAssistantModelTitleRejectsTechnicalAndProgressResults(t *testing.T) {
	t.Parallel()
	for _, summary := range []string{
		"Создан ровно один подтверждаемый typed plan по вашему JSON: `pln_0123456789abcdefghijklmn`, версия 1. Подтвердите изменения.",
		"Подготовлен подробный план настройки проекта. Проверьте изменения.",
		"Создан план настройки сети помощника. Следующий шаг — подтверждение.",
		"Created a configuration plan for the project. Confirm the proposed changes.",
		"Настройка окружения renv_0123456789abcdefghijklmn",
		"Настройка образа sha256:" + strings.Repeat("a", 64),
		`Настройка окружения ["renv_0123456789abcdefghijklmn"]`,
	} {
		if title := assistantAutomaticTitleText(summary); title != "" {
			t.Fatalf("technical or progress result became title: %q", title)
		}
	}
}

// TestAssistantAutomaticTitlePreservesTopicsAndRejectsReports checks literal useful topics versus
// progress/technical/protected/control reports, plus word-aware Cyrillic truncation, a single long
// word and an exact whole-word boundary. It preserves existing automatic-title policy without
// testing manual edits.
// TestAssistantAutomaticTitlePreservesTopicsAndRejectsReports проверяет буквальные полезные темы
// против progress, technical, protected и control отчётов, кириллическое усечение по словам, одно
// длинное слово и точную границу целого слова. Сохраняет автоматическую политику; ручные правки не
// проверяются.
func TestAssistantAutomaticTitlePreservesTopicsAndRejectsReports(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ input, want string }{
		{"  ## Настройка   Context7  ", "Настройка Context7"},
		{"MCP", "MCP"},
		{"Создание образа для разработки Kodex", "Создание образа для разработки Kodex"},
		{"Проверка доступности GitHub", "Проверка доступности GitHub"},
		{"Настройка Context7 и окружения проекта " + strings.Repeat("дополнения ", 15), "Настройка Context7 и окружения проекта дополнения дополнения дополнения"},
		{"Созданы новые сотрудники проекта", ""},
		{"UPDATED project configuration", ""},
		{"Настройка окружения agt_0123456789abcdefghijklmn", ""},
		{"Настройка диалога cnv_0123456789abcdefghijklmn", ""},
		{"Настройка образа " + strings.Repeat("a", 64), ""},
		{"Проверка `gh --version`", ""},
		{"Настройка api_key=SYNTHETIC_PRIVATE_VALUE", ""},
		{"Настройка\u202eпроекта", ""},
	} {
		if got := assistantAutomaticTitleText(test.input); got != test.want {
			t.Fatalf("automatic title = %q, want %q", got, test.want)
		}
	}
	if got := assistantAutomaticTitleText(strings.Repeat("я", 100)); got != strings.Repeat("я", assistantConversationTitleMaximumRunes) {
		t.Fatal("single-word title did not preserve a bounded UTF-8 value")
	}
	const exactBoundary = "Настройка окружения помощника"
	if got := boundedAssistantConversationTitle(exactBoundary+" и образа", len([]rune(exactBoundary))); got != exactBoundary {
		t.Fatal("whole final word was lost at the exact title boundary")
	}
}

// TestAssistantUserMessageTitleUsesOnlyHumanPrefixBeforeJSON allows only human prose before
// object/array syntax and rejects structure-first or protected/URL/control-bearing sources. JSON
// payload fields never become the expected title; fixtures do not perform a command or load
// credentials.
// TestAssistantUserMessageTitleUsesOnlyHumanPrefixBeforeJSON разрешает только человеческий текст
// перед объектом и массивом, отклоняя структуру в начале и источники с protected, URL либо
// control. Поля JSON не становятся ожидаемым заголовком; fixtures не исполняют команд и не
// загружают credentials.
func TestAssistantUserMessageTitleUsesOnlyHumanPrefixBeforeJSON(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ input, want string }{
		{`Настрой окружение помощника: {"imageArtifactRef":"imgart_0123456789abcdefghijklmn"}`, "Настрой окружение помощника"},
		{`Настрой окружение помощника: [{"imageArtifactRef":"imgart_0123456789abcdefghijklmn"}]`, "Настрой окружение помощника"},
		{`{"name":"Настройка окружения","imageArtifactRef":"imgart_0123456789abcdefghijklmn"}`, ""},
		{`Настрой окружение помощника: {"token":"SYNTHETIC_PRIVATE_VALUE"}`, ""},
		{`Настрой окружение помощника: {"credential":"SYNTHETIC_PRIVATE_VALUE"}`, ""},
		{`Настрой окружение помощника: {"image":"https://fixture.invalid/private"}`, ""},
		{`[{"name":"Настройка окружения"}]`, ""},
		{"Настрой окружение помощника: {\"image\":\"value\"}\u202e", ""},
	} {
		if got := assistantUserMessageTitle(test.input); got != test.want {
			t.Fatalf("structured input title = %q, want %q", got, test.want)
		}
	}
}

// TestAssistantConversationTitleSQLKeepsExistingNames asserts that USER fallback SQL changes only
// a meaningful exact SERVER_DEFAULT placeholder and that explicit metadata proposals preserve
// USER_EDITED names. This is a literal SQL-shape check, not a database concurrency or persistence
// test.
// TestAssistantConversationTitleSQLKeepsExistingNames проверяет буквальные SQL-условия: USER
// fallback меняет только точный SERVER_DEFAULT placeholder осмысленным названием, явное
// предложение metadata сохраняет USER_EDITED. Это проверка формы SQL, не конкурентного исполнения
// и сохранения в БД.
func TestAssistantConversationTitleSQLKeepsExistingNames(t *testing.T) {
	t.Parallel()
	const sourceGuard = "title_source = 'SERVER_DEFAULT' AND title = 'i18n:NEW_ASSISTANT_CONVERSATION'"
	// Terminal не меняет названия; ранний USER fallback назначается только
	// точному SERVER_DEFAULT placeholder, а explicit proposal сохраняет USER_EDITED.
	if strings.Count(queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt, sourceGuard+" AND $8 <> ''") != 2 {
		t.Fatal("user fallback does not require a meaningful name and exact placeholder")
	}
	if !strings.Contains(queryRuntimeProposeassistantmetadataUpdateConversation, "title_source<>'USER_EDITED'") {
		t.Fatal("explicit agent proposal can overwrite a user-edited title")
	}
}
