---
id: OPS-DOC-RESUMABLE-SESSIONS-1046
title: Каталог доступных продолжений Session
type: implementation-guide
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Каталог доступных продолжений Session

Источники: Issue #1046, Epic #1018, PWA #1022 и исходный MVP-UI-05.
Аддитивный режим существующего ListRuns согласован manager: одна последняя
доступная Run на Session, поиск и total принадлежат control-plane.

## Сквозная карта

| Инициатор и authority | Внешний путь и mapping | Владелец и состояние | Результат и consumer |
| --- | --- | --- | --- |
| Владелец/оператор через проверенную HTTP session, signed context внутреннего RPC | GET /api/v1/runs?resumableSessionsOnly=true → PlatformQueryService.ListRuns.resumable_sessions_only | CP PostgreSQL: run.view, точный agent.launch/workflow.launch, project scope; ACTIVE Session без QUEUED/RUNNING turn; текущие target, image/runtime contract, account/catalog pins и overlay | RunPage с одним представителем на Session, distinct total и курсором; Home selector |
| Тот же actor, выбор результата | Существующий AddSessionTurn → owner command → защищённый nested LaunchRun | Повтор target authority в command transaction, блокировка Session, свежая runtime/catalog проверка, существующие version/idempotency/Run events | Новый Run и существующий authoritative read/rejoin; каталог не гарантирует успешную будущую mutation |

Чтение ничего не изменяет: idempotency и события отсутствуют. Авторитетный
read path — повтор ListRuns. Browser не объединяет проектные страницы и не
вычисляет total или полномочия по загруженным Run.

## Границы снимка

Один read-only REPEATABLE READ снимок читает кандидатов порциями по 100.
Страница и total возвращаются только после полного прохода. Общий бюджет —
5 секунд; timeout, ошибка чтения и переполнение означают отказ без частичного
успеха. В памяти остаются только текущая порция и запрошенная страница.

Курсор связывает actor, organization, authority project, фильтр и режим со
снимком доступного состава. Изменение состава/версии между страницами даёт
VersionMismatch: consumer начинает с первой страницы. Обычный ListRuns
сохраняет прежнюю семантику. States и resumable mode несовместимы.

NewRun передаёт парные target_type/target_ref (Proto 6/7, HTTP targetType/targetRef)
только в resumable режиме. Допустимы AGENT и WORKFLOW. Owner сначала разрешает
target в точном project/tenant scope и проверяет launch authority, затем
применяет target filter до distinct/count/page. Курсор связан с парой; Home
использует тот же каталог без неё. Browser не фильтрует страницы по target.

Продолжение отдельно проверяет agent.launch или workflow.launch. Run.view и
organization.view недостаточны для nested LaunchRun. Неизвестный target,
недоступный runtime и отозванный account не превращаются в доступное действие.

## Проверки

Исполняемые результаты фиксируются только после завершения локальных запусков.
HTTP/SDK consumer, PWA consumer и live acceptance до их выполнения — NOT RUN.
Новый PR для contribution не создаётся; код входит в полный unit #1046.

Context7 в текущем наборе инструментов недоступен. Проверены официальные
[pgx v5.10.0 StrictNamedArgs](https://github.com/jackc/pgx/blob/v5.10.0/named_args.go)
и [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html).
