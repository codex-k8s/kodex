---
id: OPS-INTEGRATION-GRANT-1046
title: Выбор соединения и адресная выдача разрешения
type: operations
status: approved
owner: control-plane
version: 1.0.0
updated: 2026-09-06
---

# Выбор соединения и адресная выдача разрешения

Источники: Issue #1046, MVP-UI-39/40 из принятого backlog, GUIDE-DOC-006,
GUIDE-DOC-003. Producer реализован в существующем unit control-plane/PR #1071.

## Полномочия и карта сценариев

Обычный actor и организация происходят из проверенного контекста gateway.
Поля запроса задают только искомый контекст. Control plane разрешает каждую
ссылку внутри tenant и повторяет свежую проверку перед OCC и receipt replay.

| Сценарий | HTTP GET | RPC PlatformQueryService | Правило владельца |
| --- | --- | --- | --- |
| Соединение | `/integration-grant-candidates/connections` | `ListIntegrationGrantConnectionCandidates` | GRANT: право выдачи; USE: право исполнения точной операции |
| Проект | `/integration-grant-candidates/projects` | `ListIntegrationGrantProjectCandidates` | Доступный проект с адресатом, которым actor вправе управлять, для точного соединения |
| Адресат | `/integration-grant-candidates/recipients` | `ListIntegrationGrantRecipientCandidates` | Точные connection/project и закрытый тип AGENT либо WORKFLOW |
| Capability | `/integration-grant-candidates/capabilities` | `ListIntegrationGrantCapabilityCandidates` | Точные connection/project/recipient, фактически привязанный пакет и право выдачи |

Все четыре чтения используют policy72 и разные типизированные ответы. Ответ
возвращает выбранный prefix, версии зависимостей и context digest. Поиск
буквальный; page, count и eligibility относятся к одной owner-транзакции.
Cursor связан с actor, tenant, purpose, полным prefix и поиском. Изменение
контекста требует первой страницы. Скрытые строки не входят в total.
Дополнительные read permissions скрывают имена проектов и адресатов, которые
actor вправе изменять по отдельному назначению, но не вправе читать.
В `RECIPIENT` обязателен выбранный `recipientKind`; mixed-page с последующей
фильтрацией в браузере отсутствует. USE возвращает только `usable=true`,
GRANT никогда не выдаёт этот флаг вместо `grantable`.

GRANT не требует существующего grant: иначе создание имело бы циклическую
предпосылку. Проверяются `integration.manage` для соединения и `agent.manage`
либо `workflow.manage` для адресата. USE проверяет существующий включённый
AGENT grant и пересечение полномочий actor, фактического пакета и требований
точного этапа Workflow. Самостоятельного Workflow runtime нет. Неполный USE
контекст не превращается в GRANT.

`ChangeIntegrationGrant` создаёт либо переключает адресный grant.
`ChangeAgentGrant` переключает тот же `integration_grants.enabled` и его
версию; это не отдельная независимая запись opt-in. Выборки не меняют grants.
Команда повторяет общий predicate, затем OCC, затем записывает business state,
receipt, audit и существующий `INTEGRATION_GRANT_CHANGED` в owner-транзакции.
Receipt не продлевает отозванную authority. Existing immutable попытки не
переписываются; новые исполнения используют свежий owner snapshot.

## Обязательные отрицательные проверки

- Чужой tenant, скрытые project/recipient/connection и неизвестный тип.
- Отзыв authority перед повтором существующего receipt и перед OCC.
- Отключённые connection/definition/recipient, устаревшие package pins.
- Один capability key у разных соединений и суженный managed package.
- USE без действующего grant, actor permission либо включения в Workflow step.
- Literal search, total без скрытых строк, cursor другого actor/prefix/query.
- GRANT без предварительного grant и отсутствие скрытых эффектов чтения.

## Проверка и ограничения

Полный disposable PostgreSQL `TestBootstrapComponent`: **PASS**, 30.061s.
Оснастка включает managed package с более узким input schema, два соединения
с одинаковым capability key, четыре типизированные выборки, pagination/count,
буквальный поиск, отозванные полномочия до receipt replay, чужой tenant,
недоступное соединение до OCC и реальный Workflow grant/step intersection.
Общий `ListIntegrationConnections` также использует authoritative read ACL,
query и cursor; `definitionKey` больше не теряется в repository path.
Single/list/mutation readback вычисляют connection actions по точной
`integration.manage`, а сведения о grants — по текущему праву читать адресата;
legacy role и membership не подменяют эти permissions. Повреждённый digest
managed package исключается общим SQL predicate до page/count; отрицательная
проверка откатывает изменение fixture в отдельной транзакции.

Repository/transport race, полный CP vet/build, SQL boundary, Proto lint/build
и canonical replay, policy72 replay и authority ABI render: **PASS**.

Исторические **FAIL** сохранены: старый Workflow grant SQL ссылался на
несуществующий `workflow.lifecycle`; используется фактический `state`.
Классификация неизвестного capability сохранена как Invalid только после
проверки exact authority, скрытый адресат остаётся NotFound. Новая Workflow
fixture первоначально не выдала требуемую platform capability; исправлена
реальной owner-командой до Validate/Publish, production guard не ослаблялся.

Публичная точка проверки: `make test-control-plane-postgres` в безопасной
локальной disposable среде. При ручной проверке интегрированного HTTP/PWA
выбрать соединение, Project, тип адресата, адресата и capability; затем отозвать
право управления соединением и повторить прежний mutation intent. Повтор не
должен создавать эффект либо возвращать разрешённый прежним actor receipt.
Новая выборка не должна включать скрытое соединение в items или total.

HTTP/PWA consumer и live-приёмка нового selector не входят в этот локальный
producer PASS; их evidence фиксируют владельцы соответствующих unit.
Новых событий у четырёх read RPC нет. Переходы grant сохраняют существующий
owner audit/receipt и `INTEGRATION_GRANT_CHANGED`; чтения не создают tasks.
