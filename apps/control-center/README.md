# Control Center: история автоматизаций

Интерфейс показывает сохранённую сервером историю `ScheduledRun`. Vue загружает её через сгенерированный из [`control-center.v1.yaml`](../../specs/openapi/control-center.v1.yaml) клиент и `GET /api/control-center/v1/automation-runs`, а затем обновляет каждые пять секунд. Production-путь не читает данные из `window`, local storage или встроенных mock-объектов.

Read-only token вводится пользователем после открытия страницы и хранится только в памяти вкладки. Сервер проверяет его значение из `MATTERCODEX_CONTROL_CENTER_READ_TOKEN`, а история ограничивается настроенным `MATTERCODEX_OWNER_MATTERMOST_USERNAME`. Значения этих ключей в документации и журналах не выводятся.

Для `requires_human` интерфейс различает:

- `waiting_owner` + `human_decision_status: open` — решение ожидается;
- `succeeded` + `human_decision_status: resolved` — сохранённый ручной шлюз закрыт.

Проверки:

```bash
npm ci
npm run typecheck
npm test
npm run build
```

После изменения OpenAPI-контракта оба клиента пересоздаются из корня репозитория:

```bash
make gen-openapi
```

## Ручная проверка

1. Убедиться, что для bot-service заданы `MATTERCODEX_CONTROL_CENTER_READ_TOKEN` и `MATTERCODEX_OWNER_MATTERMOST_USERNAME`.
2. Открыть `/control-center/`, ввести read-only token и проверить загрузку сохранённых запусков без JavaScript-переменных в `window`.
3. Довести автоматизацию до `requires_human`: одна строка должна показать `waiting_owner`, доставленную карточку и ожидание решения.
4. Ответить от имени сохранённого корневого инициатора в точном Mattermost-треде. Не позднее следующего обновления строка должна перейти в `succeeded` и показать принятое решение.
5. Перезагрузить страницу, снова ввести token и убедиться, что разрешённое состояние восстановилось из PostgreSQL.
