---
doc_id: ARC-CK8S-SERVICE-BOUNDARIES-0001
type: design-doc
title: kodex — границы сервисов
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
related_adrs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Границы сервисов

## TL;DR

Новая реализация пишется как набор owner-сервисов, а не как новый большой `control-plane`. Каждый owner-сервис владеет своим состоянием, контрактами, миграциями, событиями и бизнес-правилами. Edge, worker и agent-runner не получают доменного владения.

## Цели

- Зафиксировать, какой сервис владеет каждым классом решений.
- Запретить прямое чтение и изменение чужих БД.
- Не допустить повторного появления доменного монолита под видом gateway, worker или operations-сервиса.
- Сохранить provider-first модель без внутреннего заменителя `Issue` и `PR/MR`.
- Разделить agent run, runtime job, slot и provider artifact.

## Не-цели

- Не фиксировать физическую структуру каталогов реализации.
- Не описывать полные SQL-схемы каждого сервиса.
- Не проектировать GitLab или платёжные провайдеры детально.
- Не переносить старый код из `deprecated/**` в новые сервисы.

## Базовые правила границ

### Один owner для канонической истины

Если два сервиса одновременно меняют одно и то же каноническое состояние, граница неверна. Read-проекция может дублировать данные для UI, но не может принимать бизнес-решения вместо owner-сервиса.

### Сервис владеет правилами и состоянием

Нельзя оставить данные в одном сервисе, а бизнес-переходы в другом. Если переход меняет каноническое состояние, его выполняет owner-сервис.

### Edge остаётся тонким

`api-gateway`, `web-console` и `platform-mcp-server` не должны становиться местом, где живут правила проекта, доступа, run lifecycle, job lifecycle или provider state.

### Исполнители не владеют доменом

`worker` и `agent-runner` исполняют задачи по поручению owner-сервисов. Они не вводят собственные статусы, которые становятся важнее доменного состояния.

### Межсервисные связи не являются SQL-связями

Ссылки на чужие агрегаты хранятся как внешние идентификаторы. Целостность между owner-сервисами поддерживается через API, доменные события, read-проекции и reconciliation, а не через `FOREIGN KEY` между БД.

## Owner-сервисы и запреты

| Сервис | Владеет | Не владеет |
|---|---|---|
| `access-manager` | Пользователи, организации, группы, membership, allowlist, access decisions, административный аудит. | Проекты, provider mirror, run/job lifecycle, UI-проекции. |
| `project-catalog` | Проекты, репозитории, `services.yaml`, источники документации, branch rules, release policy, placement policy. | Provider webhooks, roles/prompts, slot lifecycle, notifications. |
| `provider-hub` | Provider accounts, webhooks, mirror, synchronization, limits, provider operations. | Flow, role selection, slot/build/deploy jobs, release policy. |
| `package-hub` | Package catalog, installed packages, available packages, package sources, verification, secret schemas, price metadata. | Runtime lifecycle plugin workloads, Git provider truth, user membership. |
| `agent-manager` | Flow, stage, role, prompts, run/session, automation rules, acceptance, wait states. | Provider API details, slot/job execution, notification delivery. |
| `fleet-manager` | Servers, Kubernetes clusters, connectivity, health, placement policy. | Run lifecycle, provider state, job status as runtime truth. |
| `runtime-manager` | Slot lifecycle, platform jobs, build/deploy/mirror/cleanup, runtime status, retention. | Product meaning of tasks, approvals, provider artifacts. |
| `billing-hub` | Billing accounts, cost records, allocation, invoice basis, package economics. | Runtime truth, provider truth, access graph. |
| `interaction-hub` | Dialogs, approvals, notifications, subscriptions, external callbacks, delivery attempts. | Flow business logic, canonical run/job statuses, project state. |
| `operations-hub` | Read models, timelines, operator queues, aggregated statuses. | Primary user, project, provider, run or job truth. |

## Edge-компоненты

### `web-console`

Отвечает за UI, routing, client-side state и доступность операторских экранов. Не рассчитывает доменные переходы и не соединяется напрямую с owner-БД.

### `api-gateway`

Отвечает за HTTP ingress, webhook edge, authn/authz на edge, базовый rate limiting и routing. Не хранит бизнесовые статусы и не реализует доменные use-cases.

### `platform-mcp-server`

Отвечает за MCP-поверхность инструментов, policy-проверки, аудит MCP-вызовов и маршрутизацию к owner-сервисам. Не хранит каноническое состояние run, job, project, package или provider artifact.

## Исполнительные компоненты

### `worker`

Исполняет background tasks, retries, outbox delivery, inbox processing и reconciliation. Работает через контракты owner-сервисов и не читает чужие БД.

### `agent-runner`

Исполняет ролевого агента в slot. Может работать с кодом, проектной документацией, provider-native артефактами и MCP-инструментами. Не владеет flow, run lifecycle, acceptance или job lifecycle.

## Типы взаимодействия

| Тип | Когда использовать | Ограничение |
|---|---|---|
| Синхронный command | Нужно изменить каноническое состояние owner-сервиса. | Команда идёт только к owner-сервису. |
| Authoritative read | Нужно проверить актуальную истину owner-сервиса. | Не заменяется чтением read-проекции. |
| Read projection | Нужно быстро собрать UI, timeline, фильтр или поиск. | Не является источником истины. |
| Domain event | Нужно уведомить другие домены о факте изменения. | Подписчик обрабатывает событие идемпотентно. |
| Reconciliation | Нужно восстановить состояние после потери события или drift. | Выполняется владельцем состояния или по его поручению. |

## Инварианты для реализации

- Новый сервис создаётся только с явным owner-контуром.
- У сервиса есть свой контур миграций и свой доступ к БД.
- Нельзя добавлять SQL-зависимость на таблицы другого owner-сервиса.
- Общие библиотеки не содержат доменной логики конкретного сервиса.
- Если UI требует сложный агрегат, сначала проектируется read-проекция, а не временная сборка в gateway.
- Если агенту нужна опасная операция, она идёт через MCP-инструмент с policy и audit.

## Апрув

- request_id: `owner-2026-04-26-platform-architecture-frame`
- Решение: approved
- Комментарий: границы сервисов входят в сквозной архитектурный каркас платформы.
