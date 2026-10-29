---
id: OPS-DOC-WORKFLOW-EVIDENCE-HISTORY-1972
title: Полная история интеграций в продолжении этапа
type: runbook
status: approved
owner: architect
version: 1.0.0
updated: 2026-10-29
---

# Контракт передачи результатов

Источник требования — [#1972](https://github.com/codex-k8s/kodex/issues/1972)
и реальный PDLC журнала обращений из #1830. После чтения документации и
успешных записей аналитика полный список native outcomes не помещался в task.
Артефакты сохранялись, но root завершался `WORKFLOW_EVIDENCE_DELIVERY_LIMIT`.
Повтор такого запуска без сверки внешних receipts может повторить запись.

Лимит задания — **32768 Unicode codepoints**, а не байтов. Он сохраняется.
Маленькие прежние envelopes остаются inline. Для большой истории сервер
сохраняет исходное поручение и материализует компактный
`kodex.workflow-evidence-manifest.v1`: первый ref в порядке invocation refs,
SHA-256 полного канонического источника, число байтов и outcomes. Manifest
не является полномочием и не подтверждает успех бизнес-этапа.

Полный источник `kodex.workflow-evidence-outcomes.v1` содержит исходный task и
**все** terminal outcomes, включая `FAILED`, `REJECTED`, `CANCELLED` и решения
Gate. Ни результаты обычных READ, ни ошибочные факты не отбрасываются.
Порядок outcomes — по ref; JSON использует одинаковый canonical writer для
manifest и страниц. Верхний предел полного источника — 64MiB, число операций
ограничено прежним `MaximumNativeToolCalls`. Переполнение источника либо самого
поручения сохраняет прежний закрытый отказ. Увеличения лимитов аккаунта нет.

| Участок сценария | Actor, authority и состояние | Контракт и потребитель |
| --- | --- | --- |
| Завершение producer | Control-plane разрешает опубликованный evidence profile, completed lease/capture, все settled native receipts и прежний бюджет продолжений | Оба порядка model/effect completion соединяются прежней owner transaction; один `CONTINUES`, новая lease/RR, прежняя physical attempt не оживает |
| Materialized task | Источник — точные owner rows producer; actor/producer не назначаются текстом модели | Исходный task и полный digest manifest; клиент не выбирает иной источник |
| MCP read | Обычный project turn с integration catalog; текущие lease/fence/generation из проверенного RuntimeRevision | Существующий `get_integration_catalog` получает взаимоисключающий `outcomes_read`; имена инструментов runner не меняются |
| Внутренний вызов | Аутентифицированный runtime-controller с exact permission `platform.runtime.execution.integration.outcomes.read` | `RuntimeWorkService.GetExecutionIntegrationOutcomes` → transport → domain service → repository port → PostgreSQL adapter; новый actor/project из request невозможен |
| Разрешение источника | Live lease, node/turn/RR tuple, активные root/project/agent/actor/membership и текущая видимость project/agent/run | Только непосредственное server-owned ребро `CONTINUES`, тот же run/session/agent/logical origin/business attempt; source completed lease/capture и все native receipts проверяются вновь |
| Страница | Server-resolved producer, первый invocation ref, полный digest и точное совпадение текущего materialized task | Максимум 4096 bytes, UTF-8 boundary, source/page hashes, offset/next/EOF; consumer получает `outcomes_page`, история не пишет внешние эффекты |
| Завершение чтения | Успех требует полного stream/hash и commit owner read transaction | Activity хранит лишь безопасные page commitments, без полного текста, fence и private IDs; отдельного business event/receipt mutation у query нет |

Поля MCP selector: `source_invocation_ref`, `source_sha256`, `offset_bytes`,
`maximum_bytes`. Первый offset — 0, следующий берётся из `next_offset_bytes`.
Собираются **все** страницы до `eof` с проверкой полного SHA-256. Смещение
посередине UTF-8 rune закрыто отклоняется; конец страницы отступает к полной
rune, не меняя байтов источника. Точный EOF можно прочитать повторно.

| Переход или отказ | Ожидаемое поведение |
| --- | --- |
| Create/claim/renew | Прежние команды; чтение не создаёт grant/claim/attempt и не меняет lease |
| Повтор READ | Те же immutable bytes; проверяются свежие lease и eligibility; внешнего WRITE нет |
| Неверный source/hash/page/selector | Закрытый отказ без частичной страницы и без поиска «последнего» результата |
| Другой root/session/agent/business attempt | Отказ независимо от знания ref либо digest |
| Complete/cancel/delete/expiry/revoke | Прежняя owner closure; закрытая аренда не может читать сохранённую историю |
| `WAITING_OWNER`/`CHANGES_REQUESTED` | Reader не утверждает Gate, не меняет verdict и не создаёт новую business attempt |
| Source drift/ошибка транзакции/overflow | Нет частичного успеха; source history и captured files сохраняются прежним owner path |

## Проверка и поставка

Публичные точки проверки: PostgreSQL script с
`TestWorkflowPDLCFileResultOwnerComponent/documentation-large-read-history-`
`effect-first` и `model-first`, unit tests `TestWorkflowEvidencePageUTF8`,
`TestWorkflowEvidenceManifestClosedSource` и прежние малые envelopes;
`scripts/tests/runtime-mcp-catalog-test.sh` передаёт фактический tools/list
runner consumer. Component fixture использует настоящий owner graph/receipts,
но не является доказательством внешней GitHub delivery.

Новый RPC требует согласованной поставки control-plane и runtime-controller
с exact authority/service identity bindings. Регенерация также синхронизирует
`allowed_operation_ids` producer session-archive с четырьмя уже существующими
workspace task bindings из #1953; сами bindings не меняются, включение
`SESSION_WORKSPACE_V2` этим изменением не выполняется. Новое имя MCP и новая миграция
не добавляются. Совместимость прежнего RoleImage доказывается consumer-проверкой,
а не предполагается по наличию работающего Pod. Применяются штатные immutable
сборки и адресный release; фактические SHA/digests и результаты проверок
фиксируются отдельно. Наличие этого документа не означает staging acceptance.

Возврат — адресный возврат прежних image digests и соответствующих generated
policy revisions из приватного preimage штатного release. Данные/receipts не
откатываются. Запуск, завершившийся typed failure, не оживлять SQL-правкой:
сначала сверить внешний результат и использовать штатный разрешённый recovery.
