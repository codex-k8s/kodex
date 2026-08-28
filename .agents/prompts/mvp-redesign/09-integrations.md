# Интеграции, подключения и Human Gate

## Источники

- `docs/design/mockups/15_integrations_desktop.dc.html`.
- `services/staff/control-center/src/pages/IntegrationsPage.vue`.
- `docs/domains/integrations-approvals.md`.
- `docs/architecture/integration-map.md`.
- `docs/decisions/0005-integrations-and-approvals.md`.
- `/home/s/Рабочий стол/PoC/kodex-poc-ooo-zvuk-confluence.txt` только как
  функциональный источник планов. Не переноси из него название или сведения
  конкретной организации.

## Результат

Создай:

- `integrations-catalog-desktop.html`;
- `integration-package-desktop.html`;
- `integration-connection-desktop.html`;
- `integration-grants-desktop.html`;
- `integration-approval-desktop.html`;
- `integrations-mobile.html`.

Каталог содержит first-party packages GitHub, GitLab, Jira, Confluence и Email.
Покажи versioned YAML package как управляемое описание возможностей, а не как
поле для arbitrary shell. Для каждой capability видны назначение, risk,
read/write, требование Human Gate, bounded result и доступные resource scopes.

Подключение проходит понятный onboarding: способ авторизации, безопасный ввод
credential, проверка, выбор разрешённых ресурсов, enable/disable и health.
Secret value после сохранения не возвращается.

Модель доступа:

- чтение может использовать resource-scoped service connection;
- изменения используют user-delegated credential или отдельно одобренную
  service identity;
- grants назначаются сотруднику или published Workflow version;
- опасная операция сначала создаёт immutable intent и `WAITING_APPROVAL`;
- Human Gate показывает effect preview, actor, root initiator, connection,
  resource scope, expiry и решение;
- после выполнения доступен безопасный receipt и audit link.

Кнопки в карточках выровнены по нижнему краю независимо от объёма описания.
Основная платформа не становится неготовой из-за отсутствующей интеграции.
