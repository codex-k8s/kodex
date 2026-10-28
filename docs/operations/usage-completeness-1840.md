---
id: OPS-DOC-USAGE-COMPLETENESS-1840
title: Полнота измеренного расхода токенов
type: operations
status: approved
owner: developer
version: 1.0.1
updated: 2026-10-25
---

# Полнота измеренного расхода

Issue [#1840](https://github.com/codex-k8s/kodex/issues/1840), эпик #1830.
`UNKNOWN`, `PARTIAL`, `COMPLETE` описывают полноту принятых числовых наблюдений
конкретного хода, а не успешность исполнения, цену или полный счёт провайдера.
Размер контекстного окна не является измерением расхода. Аккаунтные финансовые
снимки, их freshness и лимиты параллельности не меняются.
Root агрегирует сохранённые turn receipts, а не будущие PLANNED/queued ходы;
полнота наблюдений не утверждает, что весь граф уже завершён.

## Сквозной контракт и полномочия

| Инициатор → граница → владелец → потребитель | Привязка и результат |
| --- | --- |
| Codex numeric response → parser/broker → runner | Проверенный thread/turn и response receipt; существующий закрытый enum без доверия display totals. |
| Runner → TLS callback `/v1/executions/{lease}/complete` → runtime-controller | Bearer/mTLS, runtime revision digest и attempt; полнота передаётся внутри существующего usage на success и failure. |
| Runtime-controller → `RuntimeService.CompleteExecution` → CP | Серверные lease/fence/generation/runtime revision; tenant/actor из проверенного контекста. Enum не даёт новых полномочий. |
| CP transaction → `runs.usage.turns[turnRef]` и root aggregate | Существующие receipt/idempotency/turnRef dedup, audit и domain/platform events одной owner-транзакции. Нового события/таблицы нет. |
| CP → Proto → gateway REST `/api/v1/runs`, `/{runRef}`, `/{runRef}/graph` и WebSocket snapshot/delta → PWA | Только разрешённые owner/project views; OpenAPI/AsyncAPI и generated adapters сохраняют качество. Отсутствующее legacy-поле означает UNKNOWN. |

## Матрица жизненного цикла

| Сценарий | Счётчики и полнота | Эффект/чтение |
| --- | --- | --- |
| До исполнения, нет numeric receipt, null usage | Подтверждённые значения сохраняются; UNKNOWN | Generic failure не выдумывает COMPLETE. Context показывается отдельно. |
| Единственный terminal и все numeric receipts, включая numeric0 | COMPLETE | Success/failure callback сохраняет полноту независимо от результата. UI показывает измеренный0. |
| Есть numeric receipt и пропуск либо незавершённый ход | PARTIAL и подтверждённый subtotal | UI явно помечает неполноту. |
| Legacy/UNKNOWN с положительными counters | Значения сохраняются, UNKNOWN | Положительный subtotal не повышает качество автоматически. |
| Несколько turns: все COMPLETE | COMPLETE, прежние суммы и max context | TurnRef остаётся ключом dedup. |
| Все turns UNKNOWN или turns отсутствуют | UNKNOWN, прежние значения | Никакой автоматической миграции legacy в COMPLETE. |
| Mixed COMPLETE/PARTIAL с UNKNOWN либо любой PARTIAL | PARTIAL, прежние суммы | Часть графа не объявляется полным расходом. |
| Повтор идентичного completion/lost ACK | Те же counters и качество | Существующий receipt возвращается без двойного учёта/нового события. |
| Тот же key с другим quality/payload; stale/foreign lease, cancel/delete/expiry | Закрытый отказ | Нет новой записи usage, нового grant или возобновления terminal графа. |
| Retry/continuation | Новая существующая attempt/revision | Только её авторизованный completion добавляет собственный turn. |

## Доставка и проверка

Применённые миграции не меняются: качество хранится в существующем usage JSON.
Новый Proto tag additive; optional JSON-поле принимает legacy snapshots,
UNKNOWN материализуется при чтении API. Неизвестный enum отклоняется, а не
маскируется под UNKNOWN. UNKNOWN не записывается новым ключом в сохранённые turn/run/root usage JSON:
отсутствие поля сохраняет прежнее значение и положительные legacy counters.
Поэтому новый CP со старым runner продолжает писать JSON, доступный старому
строгому CP reader; INVALID не опускается и по-прежнему закрыто отклоняется.

Сначала обновляются consumers/runtime-controller/CP/gateway и PWA; до запуска
нового runner требуется авторитетный readback всех CP replicas на новой exact
ревизии. Старую PWA следует обновить. Новый runner при старом strict callback
или при оставшихся старых CP replicas не считается поддерживаемым rollout.
После первой записи PARTIAL/COMPLETE старый строгий CP reader уже несовместим:
восстановление допускает только совместимую новую ревизию CP либо rollforward,
а не откат к reader без этого поля. Качество из receipts/history не удаляется
ради rollback; counters и turnRef dedup сохраняются.

Обязательны parser/broker→настоящий TLS callback сценарии UNKNOWN0/COMPLETE0/
PARTIAL100 на success/failure, lost ACK/replay, native disposable CP claim/
complete с несколькими turns и root aggregate, legacy readback, strict enum,
REST/WebSocket и обе UI-панели. Unit/генерация не заменяют живую приёмку.
Никакие исходные prompts, auth или сырые provider sessions для проверки не нужны.
