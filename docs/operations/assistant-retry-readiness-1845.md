---
id: QA-OPS-1845
title: Полный снимок повторной попытки помощника
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-25
---

# Повтор помощника и чтение результата

Источник: #1845, полный QA #1830. На SHA
`84c6768acd47c671d3e290cd7569ddf76e4b7f01` повтор отменённого помощника
завершился успешно, но его страница оставалась в загрузке. HTTP графа и
событий отвечал 200; `RUN_UNAVAILABLE` WebSocket удалял снимок, после чего
PWA снова выполняла HTTP readback. Reload воспроизвёл тот же цикл.

Причина: канонический assistant retry использует корневые `AGENT_EXECUTION`,
а проверки полного снимка ожидали только `ROOT_PROCESS`.

## Карта сценария и жизненный цикл

| Этап | Полномочия и владелец | Идентичность, результат, событие |
| --- | --- | --- |
| Cancel исходного хода | Проверенный owner session → gateway → Retry/Cancel command, control-plane | Существующие version/idempotency и атомарный terminal lifecycle не меняются |
| Retry | Owner endpoint `/api/v1/runs/{ref}/commands` → control-plane RetryRun | Новый root/attempt, точный retryOf, immutable AssistantRunPin и серверное RETRY_OF; существующие события создания/состояния не меняются |
| HTTP readback | Проверенный owner → GetRunGraph → owner repository | Точный run/root/project; read-only, без события и нового grant |
| WebSocket subscribe/rejoin | Проверенная сессия → GetRunGraph с IncludeRunSnapshots → attachGraphRuns | Полный набор owner-readable run; exact predecessor, session, agent и attempt; закрытый отказ при mismatch |
| Gateway projection | Полный owner snapshot | Те же проверки RETRY_OF до отправки RUN_GRAPH_SNAPSHOT/RUN_READY; никаких новых полномочий |
| PWA | Подписка текущего владельца и exact route | Проверка owner/snapshot до записи cache; история и результат доступны после reload |
| Отмена, истечение lease, повторный retry | Существующий command lifecycle | Не изменяются этой правкой; unit не утверждает новое покрытие expiry или внешнего idempotency |
| Отзыв доступа или удаление проекта | Авторитетный owner read | Отказ не превращается в разрешение по старому cache; межпроектный доступ не расширяется |

Узкое исключение для `AGENT_EXECUTION` применяется только к `RETRY_OF`:
оба узла без родителя, оба запуска SYSTEM_ASSISTANT, exact agent refs,
одинаковые полные pins, одна непустая session, attempt +1. Корни, owner project,
edge ownership, отсутствие дубликатов и циклов по-прежнему проверяются.
Обычный process retry, child retry и CONTINUES сохраняют прежние правила.

## Проверка и возврат

Локальные точки проверки: Go unit для predecessor и WebSocket projection;
`TestAssistantParallelLifecycleComponent` выполняет настоящий RetryRun и
полное чтение GetRunGraphSnapshot; frontend run-snapshot и route rejoin suites.
Фактические результаты и SHA фиксируются в связанном PR, а не считаются
успехом из наличия тестов.

В Chrome на отдельном QA-проекте: запустить помощника, отменить ход, повторить,
дождаться результата; проверить предыдущую попытку, WebSocket и reload.
Проверка полного PDLC остаётся отдельной приёмкой.

Миграции и формат данных не меняются. Для staging использовать штатную
адресную публикацию immutable образов control-plane, control-api-gateway и
control-center с единым source SHA; runtime-controller не затронут.
Возврат — сохранённые digest/Deployment patch предыдущего выпуска этих
компонентов. Историю запусков не переписывать: возврат воспроизводит старый
дефект отображения, но не отменяет уже исполненную попытку.
