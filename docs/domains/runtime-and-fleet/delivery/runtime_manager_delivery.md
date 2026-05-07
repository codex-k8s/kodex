---
doc_id: DLV-CK8S-RUNTIME-MANAGER
type: delivery-plan
title: kodex — поставка runtime-manager
status: active
owner_role: EM
created_at: 2026-05-07
updated_at: 2026-05-07
related_issues: [655, 656, 657, 658, 659, 660, 661, 662]
related_prs: []
related_docsets:
  - docs/domains/runtime-and-fleet/product/requirements.md
  - docs/domains/runtime-and-fleet/architecture/design.md
  - docs/domains/runtime-and-fleet/architecture/data_model.md
  - docs/domains/runtime-and-fleet/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-07-runtime-manager-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-07
---

# Поставка runtime-manager

## TL;DR

`runtime-manager` поставляется малыми PR-срезами: сначала доменная документация, затем контракты, сервисный каркас и БД, жизненный цикл слотов, подготовка workspace, platform jobs, эксплуатационный контур, cleanup/prewarm/reuse. `fleet-manager` остаётся отдельным сервисом-владельцем серверов и кластеров; на старте runtime использует явно описанный default fleet scope.

## Входные артефакты

| Документ | Путь |
|---|---|
| Требования домена | `docs/domains/runtime-and-fleet/product/requirements.md` |
| Дизайн домена | `docs/domains/runtime-and-fleet/architecture/design.md` |
| Модель данных | `docs/domains/runtime-and-fleet/architecture/data_model.md` |
| API-обзор | `docs/domains/runtime-and-fleet/architecture/api_contract.md` |
| Карта Issue | `docs/delivery/issue-map/domains/runtime-and-fleet.md` |

## Срезы поставки

| Срез | Issue | Результат |
|---|---|---|
| RTM-0 | #655 | Доменная документация, границы runtime/fleet, карта Issue и план поставки готовы. |
| RTM-1 | #656 | gRPC и AsyncAPI контракты `runtime-manager`, события и сгенерированные Go-контракты готовы. |
| RTM-2 | #657 | Сервисный каркас, PostgreSQL-модель, миграции, repository, health/readiness, outbox и базовые тесты готовы. |
| RTM-3 | #658 | Жизненный цикл слотов готов: reserve, extend lease, release, fail, MVP default cluster ref и `runtime.slot.*` события. |
| RTM-4 | #659 | Workspace materialization готова: source refs, writable/read-only, local paths, fingerprint и ошибки подготовки. |
| RTM-5 | #660 | Platform job MVP готов: job/step state machine, short log tail, full log ref, executor boundary и `runtime.job.*` события. |
| RTM-6 | #661 | Эксплуатационный контур готов: Dockerfile, manifests, DB bootstrap, migration job, `services.yaml`, smoke path и runbook. |
| RTM-7 | #662 | Cleanup, retention, prewarm pool, deterministic reuse и видимость cleanup failures готовы. |

## Таблица реализации

Контракты зафиксированы в `proto/kodex/runtime/v1/runtime_manager.proto` и `specs/asyncapi/runtime-manager.v1.yaml`; Go-артефакты генерируются в `proto/gen/go/kodex/runtime/v1/**` и `libs/go/platformevents/runtime/events.gen.go`.

| Группа | Контракт | Реализация |
|---|---|---|
| Слоты | Готов: `PrepareRuntime`, `ReserveSlot`, `ExtendSlotLease`, `ReleaseSlot`, `MarkSlotFailed`, `GetSlot`, `ListSlots`, события `runtime.slot.*`. | Базовая таблица и миграция готовы; команды жизненного цикла будут в RTM-3. |
| Workspace materialization | Готов: старт, отчёт прогресса, чтения и события `runtime.workspace.*`. | Не начата; будет в RTM-4. |
| Platform jobs | Готов: создание, claim с `lease_token`, progress, complete/fail/cancel, чтения и события `runtime.job.*`. | Базовые таблицы job/job step готовы; команды и state machine будут в RTM-5. |
| Runtime artifact refs | Готов: запись и чтение ссылок на внешние runtime-артефакты. | Базовая таблица готова; команды записи и чтения будут в RTM-5. |
| Cleanup/prewarm/reuse | Готов: cleanup policy, cleanup batch, prewarm pool и события cleanup/prewarm. | Базовые таблицы cleanup policy и prewarm pool готовы; runtime-логика будет в RTM-7. |
| Deploy/manifests | Не gRPC-группа. | Не начата; будет в RTM-6. |

## Синхронизация с параллельными доменами

| Домен | Когда синхронизироваться | Причина |
|---|---|---|
| `agent-manager` | До RTM-1 и RTM-3 | `Run` остаётся у `agent-manager`; runtime принимает external run refs и возвращает slot/job refs. |
| `project-catalog` | До RTM-1 и RTM-4 | Workspace policy, release policy, placement constraints и source refs должны совпадать с проектным контрактом. |
| `provider-hub` | До RTM-4 и RTM-5 | Provider refs и ускоряющие сигналы после работы slot-агентов. |
| `package-hub` | До RTM-4, RTM-5 и RTM-7 | Руководящие пакеты и runtime-нагрузки плагинов. |
| `access-manager` | До RTM-1 и RTM-2 | Действия доступа для runtime-команд, проверка actor и реакция на блокировки. |
| `fleet-manager` | До RTM-1 и RTM-3 | Поля fleet scope/cluster ref и MVP default cluster boundary. |
| `operations-hub` | До RTM-5 и RTM-6 | Набор полей, который нужен операторским экранам и центру внимания. |
| `billing-hub` | После RTM-5 | Будущие записи затрат по runtime usage. |

## Критерии начала кода

- Принят доменный пакет `runtime-and-fleet`.
- Для каждого кодового PR есть отдельный GitHub Issue.
- Контрактный PR создаёт proto и AsyncAPI до реализации операций.
- Старый код из `deprecated/**` не используется как основа реализации.
- В PR, который закрывает Issue, тело содержит `Closes #...`.

## Критерии завершения домена

- `runtime-manager` имеет собственную БД, миграции, контракты, события и deploy-контур.
- Slot, workspace materialization, job, job step, short log tail, runtime artifact refs, cleanup policy и prewarm pool имеют авторитетные команды и чтения.
- Runtime публикует `runtime.*` события через outbox и `platform-event-log`.
- Полные логи и registry catalog не хранятся в PostgreSQL.
- `agent-manager`, `project-catalog`, `package-hub`, `operations-hub` и будущий release/governance контур могут опираться на runtime-контракты.
- MVP default cluster не блокирует появление `fleet-manager` и multi-cluster.

## Апрув

- request_id: `owner-2026-05-07-runtime-manager-kickoff`
- Решение: approved
- Комментарий: план поставки `runtime-manager` согласован как целевое состояние RTM-0.
