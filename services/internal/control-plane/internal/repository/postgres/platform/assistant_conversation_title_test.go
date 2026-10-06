package platform

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
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
			assistantConversationTitle(command.CompleteExecutionInput{Success: true, ResultSummary: input}) != "" {
			t.Fatal("protected text reached conversation metadata")
		}
	}
}

func TestAssistantUserMessageTitleUsesSafePrefixBeforeLateURL(t *testing.T) {
	t.Parallel()
	const system57 = "QA_SYSTEM_GITHUB_57. Маркер KODEX1797_GEN9_GITHUB_407_A2. Только READ smoke Issue1797. Через фактический native exec выполни отдельно git --version и env GIT_TERMINAL_PROMPT=0 git -c protocol.version=0 -c credential.helper= -c credential.interactive=false ls-remote -- https://github.com/codex-k8s/kodex.git HEAD refs/heads/main. protocol.version=0 нужен для GET-only правил текущего окружения, не меняй policy. Без clone/fetch, записей, credentials и изменений GitHub. Верни exit codes и полученные refs/SHA. При отказе сообщи фактическую ошибку; не заменяй вызов памятью/web."
	const russian = "Проверь настройку помощника и доступность опубликованного репозитория организации перед следующим этапом разработки. Ссылка: https://fixture.invalid/repository"
	const russianTitle = "Проверь настройку помощника и доступность опубликованного репозитория организаци"
	for _, test := range []struct{ name, input, want string }{
		{"system57", system57, "QA_SYSTEM_GITHUB_57. Маркер KODEX1797_GEN9_GITHUB_407_A2. Только READ smoke Issu"},
		{"russian", russian, russianTitle},
		{"uppercase and multiple urls", russian + " HTTP://fixture.invalid/second", russianTitle},
		{"whitespace", "  ## " + strings.ReplaceAll(russian, " ", "\n\t"), russianTitle},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := assistantUserMessageTitle(test.input); got != test.want {
				t.Fatalf("unexpected safe prefix: got %q want %q", got, test.want)
			}
			if assistantPublicTitleText(test.input) != "" || assistantConversationTitle(command.CompleteExecutionInput{Success: true, ResultSummary: test.input}) != "" {
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
			if assistantPublicTitleText(test.input) != "" || assistantConversationTitle(command.CompleteExecutionInput{Success: true, ResultSummary: test.input}) != "" {
				t.Fatal("short prefix changed the shared or terminal title filter")
			}
		})
	}
}

func TestAssistantConversationTitleRejectsGenericTerminalSummary(t *testing.T) {
	t.Parallel()
	for _, summary := range []string{"готов", "Готово.", "Done!", "План готов", "Настройка завершена", "Изменения применены", "Всё готово", "New conversation", "", "i18n:RUN_COMPLETED"} {
		if assistantConversationTitle(command.CompleteExecutionInput{Success: true, ResultSummary: summary}) != "" {
			t.Fatal("generic completion summary became a title")
		}
	}
	for _, title := range []string{"Настройка Context7", "MCP", "Образ для разработки Kodex"} {
		if genericAssistantConversationTitle(title) || assistantPublicTitleText(title) == "" {
			t.Fatal("normal agent-proposed title was rejected")
		}
	}
}

func TestAssistantConversationTitleSQLKeepsExistingNames(t *testing.T) {
	t.Parallel()
	const sourceGuard = "title_source = 'SERVER_DEFAULT' AND title = 'i18n:NEW_ASSISTANT_CONVERSATION'"
	// Один и тот же guard обязателен для title, source и revision: USER_EDITED,
	// AGENT_PROPOSED и уже содержательный SERVER_DEFAULT нельзя перезаписать.
	if strings.Count(queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt, sourceGuard+" AND $2 <> ''") != 3 {
		t.Fatal("terminal update does not preserve existing conversation names")
	}
	if strings.Count(queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt, sourceGuard+" AND $8 <> ''") != 2 {
		t.Fatal("user fallback does not require a meaningful name and exact placeholder")
	}
	if !strings.Contains(queryRuntimeProposeassistantmetadataUpdateConversation, "title_source<>'USER_EDITED'") {
		t.Fatal("explicit agent proposal can overwrite a user-edited title")
	}
}
