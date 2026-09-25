package systemassistant

import (
	"strings"
	"testing"
)

func TestCorePromptGuidesProjectSwitchAndRunConfirmation(t *testing.T) {
	if CorePromptRevision != "system-assistant-core-v30" {
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
		"Импорт создаёт только черновик",
		"PUBLISH_INTEGRATION_DEFINITION",
		"только `configurationRef` и `revisionRef`",
		"отдельное подключение по поставленному шаблону `openapi-mcp`",
		"Новые типы адаптеров вне поставленного реестра недоступны",
		"карточка показывает сборку, допуск и публикацию",
		"не повторяй команду при неопределённом результате",
		"`publicValues`",
		"`secretBindings`",
		"создаёт только редактируемый черновик",
		"полный желаемый список",
		"обычной форме окружения, которая открывается рядом с чатом",
		"сам `secretSuggestions` не создаёт Secret и не является привязкой",
		"`CREATE_INSTRUCTION_DRAFT` только сохраняет черновик",
		"Не составляй полный список `steps` или `inputFields` по памяти",
		"предложи владельцу просмотреть его в штатной форме",
		"Не переписывай его по памяти",
		"полный Dockerfile в том же редакторе",
	} {
		if !strings.Contains(CorePrompt(), required) {
			t.Fatalf("system assistant prompt does not contain required guidance %q", required)
		}
	}
	for _, forbidden := range []string{
		"Граф этапов, назначенные сотрудники, входные поля и опубликованная версия остаются прежними",
		"Пользовательский Dockerfile и Git-owned рецепт помощник не перезаписывает",
		"Операция меняет только название, описание и ссылку на проверенный образ",
		"Уточнение к",
	} {
		if strings.Contains(CorePrompt(), forbidden) {
			t.Fatalf("system assistant prompt contains stale guidance %q", forbidden)
		}
	}
}
