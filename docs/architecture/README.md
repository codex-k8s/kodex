---
id: ARCH-MC-001
title: Архитектурная основа Kodex
type: architecture-index
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-25
---

# Архитектурная основа Kodex

Kodex строится как независимая от поставщика моделей web-платформа
управления ИИ-сотрудниками. Control Center предоставляет полный пользовательский
интерфейс, а Kubernetes исполняет изолированные ролевые окружения агентов.

## Основные принципы

- Компоненты реализуются полными самостоятельно развертываемыми unit по
  правилам `project-template`.
- Fresh install использует единую baseline schema без compatibility, dual-write,
  legacy migration и cutover paths.
- PostgreSQL является источником истины для metadata, bounded artifact content,
  desired state, queues, graph/events и audit fresh web-only профиля.
- Kubernetes исполняет рабочую нагрузку, но не хранит бизнес-состояние.
- Control Center работает без внешних интеграций. Mattermost может принимать
  входящие сообщения, доставлять уведомления, зеркалировать результаты и
  решения Human Gate как четыре независимые optional capabilities.
- Среда выполнения ИИ, GitHub, Kubernetes и внешние бизнес-системы подключаются через контракты поставщиков и интеграций.
- Любое внешнее изменение имеет ключ идемпотентности, явно выданное право, политику риска и запись аудита.
- Shell не используется как слой оркестрации прикладной логики.
- Внутренние синхронные границы используют Proto/gRPC, mTLS/SPIFFE и
  workload-local `internal-rpc-authority`.
- Доменные события доставляются через transactional outbox, broker-neutral
  relay, NATS JetStream и durable inbox.
- POST-MVP расширения используют универсальный пакет приложения и локальный
  каталог; магазин, plugin или пакет руководств не создают обходных runtime,
  access, secret или audit границ.

## Документы раздела

| Код | Файл | Назначение |
| --- | --- | --- |
| `ARCH-MC-001` | `docs/architecture/README.md` | Индекс и принципы. |
| `ARCH-DOC-002` | `docs/architecture/technology-stack.md` | Нормативный технологический профиль. |
| `ARCH-MC-002` | `docs/architecture/high-level-architecture.md` | Компоненты и потоки. |
| `ARCH-MC-003` | `docs/architecture/domain-map.md` | Доменные контексты и зависимости. |
| `ARCH-MC-004` | `docs/architecture/service-boundaries.md` | Границы компонентов и переход. |
| `ARCH-MC-005` | `docs/architecture/integration-map.md` | Внешние системы и режимы интеграций. |
| `ARCH-MC-006` | `docs/architecture/data-model.md` | Сущности, владение данными и инварианты. |
| `ARCH-MC-007` | `docs/architecture/runtime-and-sessions.md` | Сессии, ходы и привязка учетной записи. |
| `ARCH-MC-008` | `docs/architecture/attachments-and-artifacts.md` | Входные и выходные файлы. |
| `ARCH-MC-009` | `docs/architecture/automations-and-playbooks.md` | Расписания, процессы и обратные вызовы. |
| `ARCH-MC-010` | `docs/architecture/runtime-controller.md` | Materialization role Pod и warm assistant runtime. |
| `ARCH-MC-011` | `docs/architecture/web-first-platform-reset.md` | Нормативная архитектура product reset. |
| `ARCH-MC-012` | `docs/architecture/installable-applications.md` | Будущая архитектура пакетов приложений, каталогов и установок. |

## Технологическая основа

- Go для серверных приложений, контроллеров, шлюзов и запуска агента.
- Vue 3 и TypeScript для центра управления.
- PostgreSQL для транзакционного состояния.
- Kubernetes для платформы и рабочих нагрузок агентов.
- OpenAPI owner API и resumable WebSocket stream для Control Center.
- Официальный Mattermost client используется только optional adapter-ом.
- OpenAPI для внешних HTTP-контрактов.
- AsyncAPI для долговечных событий.
- Protobuf/gRPC для всех типизированных внутренних синхронных контрактов.
- Redis и S3 могут добавляться специализированными cache/storage adapters, но не
  являются обязательными зависимостями fresh web-only профиля.
- NATS JetStream для доменных событий за broker-neutral relay/inbox API.
- OpenTelemetry, Prometheus и Grafana для наблюдаемости.
- BuildKit для сборки образов ролей.

Конкретные версии зависимостей фиксируются в каталоге зависимостей и lock-файлах, а не в архитектурных документах.
