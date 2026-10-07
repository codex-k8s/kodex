package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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
