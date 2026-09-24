package systemassistant

import (
	"strings"
	"testing"
)

func TestCorePromptGuidesProjectSwitchAndRunConfirmation(t *testing.T) {
	if CorePromptRevision != "system-assistant-core-v21" {
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
		"BIND_AGENT_RUNTIME_ENVIRONMENT",
		"UPDATE_ROLE_IMAGE_RECIPE",
		"[импорт OpenAPI](/configurations/INTEGRATION_DEFINITION)",
		"Создание черновика не публикует определение",
		"Для исполнения создай отдельное подключение по поставленному шаблону `openapi-mcp`",
		"Новые типы адаптеров вне поставленного реестра остаются недоступными",
	} {
		if !strings.Contains(CorePrompt(), required) {
			t.Fatalf("system assistant prompt does not contain required guidance %q", required)
		}
	}
}
