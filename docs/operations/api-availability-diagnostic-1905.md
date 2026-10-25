---
id: OPS-DOC-API-AVAILABILITY-1905
title: Диагностика HTTP503 при обновлении API
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-26
---

# Диагностика HTTP503 при обновлении API

Задача [#1905](https://github.com/codex-k8s/kodex/issues/1905), приёмка
[#1223](https://github.com/codex-k8s/kodex/issues/1223), QA
[#1830](https://github.com/codex-k8s/kodex/issues/1830).
Причина наблюдавшихся на a304 HTTP503 пока UNKNOWN. Эта диагностика позволяет
выделить отказавшую границу; само наличие логов не исправляет доступность.

## Карта запроса и полномочий

GET runs проходит прежние cookie → revocation → family → OIDC → verified
identity → owner RPC → HTTP projection. POST session/ticket проходит ту же
аутентификацию, затем family → ledger read → bounded CAS. Ticket не вызывает
control-plane RPC. Payload, trace и correlation не дают полномочий.

Native NATS owners `browserstate` и `sessionrevocation` классифицируют ошибку
до замены публичным sentinel. Общий небольшой value в существующем eventing
module сохраняет только закрытый класс и прежнюю публичную ошибку. Исходный
SDK error, ответ брокера, subject, ciphertext и credentials не сохраняются
в диагностическом значении. Классификатор не использует текст ошибки.

Gateway добавляет закрытый этап к прежней публичной ошибке; `errors.Is` и
порядок HTTP mapping сохраняются. Ошибки cookie/CSRF не становятся503.
В `problemresponse` metadata достигают уже существующего request-local
localized writer, который выдаёт одну ERROR-запись только для фактического503.
Correlation назначается один раз сервером и совпадает с публичным Problem.
Штатный telemetry logger добавляет существующие trace/span ID.
Flush учитывает неявный HTTP200 после успешной отправки; успешный Hijack
передаёт соединение и прекращает HTTP-диагностику. Неподдерживаемые операции
не фиксируют заголовки. После ошибки Flush статус мог уже уйти в сеть,
поэтому поздний Problem не объявляется доказанным HTTP503.

Зарегистрированные этапы: `auth_revocation_read`, `auth_family_read`,
`auth_oidc_verify`, `session_renewal`, `ticket_family_read`, `ticket_ledger_read`,
`ticket_ledger_cas`, `ticket_capacity`, `ticket_cas_exhausted`, `upstream_rpc`,
`gateway_stopping`. Непокрытый путь остаётся `unknown`.
`upstream_rpc` не объявляется автоматически отказом CP: общий mapper также
используется другими owner clients. Точный owner определяется маршрутом и
соответствующим RPC span. Ticket не получает этот этап без настоящего RPC.

Классы ограничены `canceled`, `deadline`, `connection_closed`, `disconnected`,
`no_responders`, `timeout`, `authorization`, `cas_conflict`, `record_invalid`,
`signing_keys_unavailable`, `pending`, `unavailable`, `unknown`.
Raw errors, URL/query, headers, bodies, actor/session/family IDs в лог не входят.
Маршрут берётся из существующего нормализатора; method имеет закрытый список.
Метрики, Sentry observer, runtime grants и deployment env не расширяются.

## Сохранённый жизненный цикл

| Путь | Сохранённая семантика |
| --- | --- |
| Нет записи revocation | `false,nil`; запрос проходит следующие прежние проверки |
| Нет family | Прежний отказ повторной аутентификации, без fabricated503 |
| Нет ticket ledger | Прежний create с sequence0 |
| Отказ native read | Прежний закрытый отказ, теперь с классом |
| Потерянный ACK | Только прежний exact ciphertext readback; совпадение остаётся успехом без диагностической ошибки |
| Неизвестный publish/readback | Прежний отказ; класс последнего readback не доказывает отсутствие внешней записи |
| CAS conflict | Прежний лимит3 попытки; исчерпание отделено от capacity16 |
| HTTP401/403/200 | Availability ERROR не создаётся |
| HTTP503 | Одна диагностическая запись; body/status/retryable/Retry-After сохраняются |
| Shutdown/restart | Прежние drain, contexts, deadlines, close/join; лог не добавляет retry |

Новых доменных событий и фоновых задач нет. Авторитетные read paths,
idempotency receipts и исходы команд сохраняются. Диагностические поля не
участвуют в решении о повторе или допуске.

## Проверка и поставка

Тесты двух изменённых Go modules проверяют native NATS causes, lost ACK без
повторной публикации, настоящие HTTP routes с cookie crypto и memory CAS,
разделение CP refusal и успешного ticket, capacity/CAS, сохранение Problem,
одну запись при конкурентных отметках, trace/correlation и отсутствие утечки.
Настоящий локальный TCP-сервер проверяет Flush/Hijack и отсутствие ложного503;
отдельная проверка сохраняет отказ неподдерживаемых потоковых операций.
Они не заменяют живой broker либо browser acceptance. Результаты привязываются
к точному SHA отдельно; наличие теста не означает PASS.

После сборки штатного API-образа оператор собирает bounded logs старых и новых
API Pod во время одного адресного rollout. Каждое чтение закрепляется точными
Pod UID/source/image до и после; неизвестная смена UID даёт UNKNOWN. Проба
сохраняет только status/code/correlation и время. По correlation связываются
ответ, закрытый этап и конкретная реплика. Ни cookies, ни ticket response,
ни raw error в отчёт не попадают. Требуются реальные Console/Network и
сохранность истории/файлов. Повторный WRITE при неизвестном ответе запрещён.

После определения причины исправляется именно подтверждённый failure path.
Успешный rollout или green Pods не закрывают исторические503. Возврат этой
диагностики возможен прежним совместимым API-образом a304; миграций и новых
обязательных env нет. Для включённой политики TTL Gate совместимость a304
остаётся нижней границей: более старый несовместимый API не допускается.

Через Context7 проверены NATS Go Client `jetstream/stream.go` и
`jetstream/README.md`; реализация сверена с закреплённым nats.go v1.52.0.
