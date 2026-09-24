package systemassistant

import (
	"strings"
	"testing"
)

func TestCorePromptGuidesProjectSwitchAndRunConfirmation(t *testing.T) {
	if CorePromptRevision != "system-assistant-core-v17" {
		t.Fatal("unexpected system assistant prompt revision")
	}
	for _, required := range []string{
		"get_configuration_catalog",
		"find_platform_resources",
		"точным относительным `route`",
		"Пользователь сам нажимает ссылку",
		"план `LAUNCH_RUN`",
		"после подтверждения пользователем",
		"Если результат Run ещё не получен",
		"Никогда не проси значение в диалоге",
		"definition_query",
		"definition_next_offset",
		"credential_secret_key",
		"CREATE_INTEGRATION_CONNECTION",
		"UPDATE_INTEGRATION_CONNECTION",
		"UPDATE_WORKFLOW",
		"PREPARE_RUNTIME_ENVIRONMENT_REVISION",
		"Ручной UI/Git draft сейчас не публикует новый исполняемый adapter",
	} {
		if !strings.Contains(CorePrompt(), required) {
			t.Fatalf("system assistant prompt does not contain required guidance %q", required)
		}
	}
}
