---
id: ROAD-MC-003
title: Review cycles и human gate полного unit
type: process
status: approved
owner: manager
version: 1.0.0
updated: 2026-07-29
---

# Review cycles и human gate полного unit

## Обязательный цикл

```text
Issue полного unit
  -> implementation + docs + deploy + manual acceptance в одном PR
  -> product-manager + security + reviewer параллельно на одном SHA
  -> developer исправляет все подтвержденные замечания и системные аналоги
  -> три направления параллельно проверяют новый SHA
  -> повтор до нуля замечаний, но не более пяти циклов автоматически
  -> manager проверяет exact SHA и reviewThreads
  -> human gate владельца
  -> merge только после отдельного OK
```

Направления review не подменяют друг друга:

- `product-manager`: продукт, сценарии, роли, полномочия, состояния, ошибки и
  acceptance;
- `security`: trust boundaries, authentication, authorization, isolation,
  replay, secrets, network и supply chain;
- `reviewer`: архитектура, contracts, data/event flow, lifecycle,
  observability, deploy и maintainability.

## Ограничение циклов

Manager автоматически проводит не более пяти полных циклов. Шестой цикл
запрещён без `mattermost_request_owner_attention`: менеджер показывает причины
повторения, открытые threads, варианты и рекомендацию.

Неблокирующее новое требование можно вынести в отдельный Issue только если оно
не является дефектом текущего scope. Подтвержденный дефект unit нельзя
перенести ради ускорения merge.

## Условия human gate

Manager передает владельцу результат только когда:

- нет unresolved review threads по GitHub GraphQL;
- все три reviewer явно подтвердили текущий exact SHA;
- фактический diff соответствует Issue;
- применимые проверки активного профиля выполнены на финальном SHA;
- PR описывает ручную проверку, риски и rollback.

Manager не сливает PR. После замечаний владельца developer исправляет
системные аналоги, а все три направления выполняют новый цикл перед повторным
human gate.

## Mattermost coordination

Корневой manager работает с владельцем в канале `coordination` и держит не
более двух активных unit. Для каждого unit он через
`mattermost_start_agent_thread` запускает дочернего manager в отдельном треде
`development`.

Дочерний manager запускает исполнителя и reviewer в своём треде через
`mattermost_request_agent`. Все дочерние роли возвращают результат через
`mattermost_return_to_requester`; дочерний manager возвращает консолидированный
результат корневому manager. Упоминания не заменяют MCP delegation.
