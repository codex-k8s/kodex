---
id: OPS-DOC-HTTP-RESUMABLE-SESSIONS-1045
title: HTTP-каталог возобновляемых Session
type: implementation-guide
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# HTTP-каталог возобновляемых Session

Источники: #1045, #1046, #1022, Epic #1018 и MVP-UI-05. Producer contribution
`f9af1bc528197e54ebc549dd582f921f8b762565` проверен отдельно от HTTP consumer.

| Сценарий | Actor и authority | Endpoint → RPC → owner | Результат и consumer |
| --- | --- | --- | --- |
| Home: доступные продолжения | Проверенная HTTP session, signed owner context; CP run.view и current target launch authority | GET /api/v1/runs?resumableSessionsOnly=true → ListRuns.resumable_sessions_only → PostgreSQL REPEATABLE READ, ACTIVE Session без активного turn, текущие runtime/catalog pins | Один последний допустимый Run на Session; total, query и cursor принадлежат owner |
| NewRun: выбор Session точного target | Та же authority; target из payload не предоставляет полномочий | Тот же GET + парные targetType/targetRef → owner target resolution до поиска/count/page | Только owner-eligible Session выбранного AGENT/WORKFLOW; без browser filtering/fan-out |
| Следующая страница | Cursor связан с actor, scope, query, target и снимком доступного состава | ListRuns с прежним pageToken → owner snapshot comparison | Изменившийся состав даёт 412; consumer начинает с первой страницы, не повторяет mutation |
| Продолжение | Свежая owner authority, существующие OCC/idempotency | Существующий AddSessionTurn → nested LaunchRun | Owner повторно проверяет Session/target/runtime; результат каталога не гарантирует будущую mutation |

Чтение не имеет idempotency key и не создаёт событие. Authoritative read path —
повтор ListRuns. State filters несовместимы с resumable mode; target pair разрешена
только в нём. Обычный ListRuns, включая explicit false, сохраняет прежнюю семантику.

HTTP переносит поля без вычисления eligibility. Повреждённые owner count/cursor,
повтор Session, чужой project/target или Run без ADD_TURN закрыто отклоняются целой
страницей; частичный успех и локальный пересчёт total запрещены. Owner budgets
остаются 5 секунд на полный snapshot pass с порциями по 100; timeout не возвращает
частичную страницу. HTTP сохраняет безопасные 400/403/404/409/412/503/504 ошибки;
owner VersionMismatch передаётся как gRPC Aborted → HTTP 412.

## Проверки

Producer exact f9af: полный Bootstrap PostgreSQL PASS 21,403 s, scoped race/vet/build,
Proto lint/build и clean replay PASS. Это отдельное producer evidence. HTTP
mapping, generated SDK и browser проверяются в своих exact checkpoints. До
фактического завершения real protected HTTP→CP/browser и staging — NOT RUN.

Ручная проверка после общей интеграции: создать две доступные Session с несколькими
завершёнными Run, сверить distinct total и pagination Home; выбрать target в NewRun;
закрыть Session либо отозвать launch authority между страницами и проверить fresh
first-page recovery. Обычный Run-каталог продолжает показывать доступные Run.

Context7: `/getkin/kin-openapi` — строгая validation документа перед использованием;
`/protocolbuffers/protobuf-go` — generated oneof wrappers и безопасные getters.
