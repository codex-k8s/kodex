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
