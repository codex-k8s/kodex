---
id: OPS-DOC-1175
title: HTTP lifecycle копирования и архивации CFG
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# HTTP lifecycle CFG

Refs #1175, #1174, #1176. Требования CFG-01/02/03: копирование создаёт
отдельную UI-конфигурацию с immutable revision и назначаемым владельцем
`copyProvenance`; экспорт исходника не заменяет эту операцию.

| Инициатор и endpoint POST | RPC владельца Control Plane | Exact precondition |
| --- | --- | --- |
| PWA `/api/v1/role-image-configurations/copies` | `CopyRoleImageConfiguration` | `recipeRef` XOR `configurationRef`, `projectRef`, `name`; If-Match версии источника |
| PWA `/api/v1/integration-definition-configurations/copies` | `CopyIntegrationDefinitionConfiguration` | `shipped {key, definitionVersion, digest}` XOR `configurationRef`, `name`; If-Match версии каталога либо конфигурации |
| PWA `/api/v1/role-image-configurations/{configurationRef}/archive` | `ArchiveRoleImageConfiguration` | If-Match версии конфигурации |
| PWA `/api/v1/integration-definition-configurations/{configurationRef}/archive` | `ArchiveIntegrationDefinitionConfiguration` | If-Match версии конфигурации |

Actor и организация происходят из проверенной browser session, затем из
подписанного межсервисного контекста. Payload не принимает actor, tenant,
готовый provenance или дополнительные source selectors. `projectRef` является
локатором для owner lookup, не выдаёт полномочий. Все четыре команды требуют
CSRF, `Idempotency-Key` и `If-Match`; owner проверяет доступ к источнику и
назначению до OCC/idempotency. Закрытый отказ сохраняет 403/404, stale pins —
412, несовместимый lifecycle или активная сборка — 409. Ошибки и неизвестный
результат не запускают автоматическое повторение команды в gateway.

Copy возвращает 201 `{configuration, revision}` и ETag конфигурации; archive
возвращает 200 `{configuration}` с `archived=true`. Архивирование не удаляет
историю и не отзывает существующие pinned потребления. Владелец атомарно
сохраняет состояние, receipt, аудит и предусмотренный его command event;
HTTP не создаёт собственных событий. Авторитетный get/list owner projection
возвращает `archived`, `copyProvenance` и `nextActions` одинаково для карточки
и детального чтения. PWA использует COPY/ARCHIVE только из `nextActions`,
а не выводит полномочия из происхождения или редактируемости исходника.

`IntegrationDefinition.version` — положительная безопасная JSON integer
для каталожного If-Match. `definitionVersion` остаётся версией shipped
пакета. Generated SDK различает обе версии и два варианта source input.
Недостоверный provenance, неизвестное действие или отсутствующая версия
каталога закрыто отклоняются до выдачи ответа.

Ручная проверка владельца: под обычной session скопировать SHIPPED, UI и GIT
источники, сверить новую UI revision и provenance через get; повторить exact
idempotency, затем stale If-Match и запрос другим actor. Архивировать UI
конфигурацию без активной сборки, сверить get/list и сохранность прежнего
потребителя; для GIT и активной сборки проверить 409. Live-проверки требуют
отдельного развёрнутого producer и разрешения владельца.
