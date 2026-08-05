---
id: ARCH-MC-002
title: Высокоуровневая архитектура
type: architecture
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-04
---

# Высокоуровневая архитектура

```mermaid
flowchart LR
    U[Пользователь] --> MM[Mattermost]
    U --> CC[Control Center]
    MM --> IG[Шлюз взаимодействия]
    CC --> CAG[Control API Gateway]
    CAG --> CP[Control Plane]
    IG --> CP
    IRA[Internal RPC Authority] -. authorization context .-> CAG
    IRA -. authorization context .-> IG
    IRA -. authorization context .-> RC
    IRA -. authorization context .-> MG
    CP --> PG[(PostgreSQL)]
    CP --> OB[(Transactional Outbox)]
    OB --> NATS[NATS JetStream]
    AS[Планировщик автоматизаций] -- generated protected gRPC --> CP
    NATS --> RC[Контроллер среды выполнения]
    RC --> K8S[Kubernetes API]
    K8S --> AR[Pod агента]
    AR --> AI[Поставщик среды выполнения ИИ]
    AR --> BMCP[Bot Service MCP transport]
    BMCP --> MG[Шлюз интеграций MCP]
    MG --> EXT[Внешние системы]
    MG --> AP[Ручное согласование]
    AR --> IG
    IG --> S3
    IG --> MM
    RIB[Role Image Builder] --> REG[(OCI Registry)]
    REG --> RC
    CP --> OT[OpenTelemetry]
    IG --> OT
    RC --> OT
    MG --> OT
```

## Control Plane

Хранит желаемое состояние и бизнес-модель: организации, рабочие области, агенты, поставщики моделей, интеграции, инструкции, управляемые процессы, расписания, сессии, метаданные файлов, согласования и аудит.

Control Plane не публикует внешний HTTP API и не создает pod Kubernetes
напрямую. Он фиксирует business state, idempotency receipt, audit и обязательные
events одной PostgreSQL-транзакцией.

## Control API Gateway

Предоставляет owner-facing OpenAPI и WebSocket API для Control Center,
аутентифицирует пользователя и преобразует запросы в generated gRPC clients.
Gateway не читает PostgreSQL Control Plane напрямую.

## Шлюз взаимодействия

Обрабатывает события Mattermost, резервные slash-команды, интерактивные карточки, диалоги, учетные записи ботов, реакции, доставку файлов и обновления обсуждений.

Шлюз не владеет бизнес-состоянием агента и сессии. Повторная доставка события Mattermost безопасна благодаря идентификаторам `event_id` и `post_id`.

## Контроллер среды выполнения

Сопоставляет желаемое состояние среды выполнения с ресурсами Kubernetes. Сверка идемпотентна и использует детерминированные имена, метки, ссылки на владельца и условия состояния.

Контроллер решает:

- какой execution-scoped pod сессии должен существовать;
- какую `RuntimeRevision` применить;
- достаточно ли ресурсов;
- какой доказанно terminal pod можно освободить;
- когда guarded удалить terminal pod, не затрагивая PVC;
- когда восстановить ход из очереди после временной ошибки.

## Запуск агента

Компонент запуска агента управляет процессами внутри pod сессии:

- получает и подтверждает ход;
- материализует конфигурацию, авторизацию, инструкции и вложения;
- запускает адаптер среды выполнения ИИ;
- передает прогресс и потребление лимитов;
- вызывает разрешенные инструменты MCP;
- публикует итоговый результат;
- сохраняет архив сессии;
- корректно завершает дочерние процессы и обрабатывает остановку.

Компонент запуска не содержит бизнес-логику Mattermost, создания проектов и согласований.

## Шлюз интеграций

Предоставляет MCP endpoint в области одной сессии. Он аутентифицирует сессию агента, вычисляет права, маскирует данные, создает запросы согласования и выполняет внешние действия от имени `IntegrationConnection`.

Опасные учетные данные остаются в шлюзе или хранилище секретов и не передаются в pod агента.

## Internal RPC Authority

Workload-local issuer формирует короткоживущий signed authorization context
после проверки transport identity. Workload-local verifier проверяет exact RPC,
issuer, audience, actor, project, срок и replay по локальному UDS. Компонент не
становится владельцем пользователей или business permissions.

## Планировщик автоматизаций

Выбирает наступившие `AutomationSchedule`, создает уникальные экземпляры и ставит `ScheduledRun` в общую очередь. Планировщик не запускает pod напрямую и не использует Kubernetes CronJob как бизнес-модель.

## Модель согласованности

- Внутри доменного контекста используется транзакция PostgreSQL.
- Между контекстами используются transactional outbox, broker-neutral relay,
  NATS JetStream, durable PostgreSQL inbox/cursor и идемпотентные consumers.
- Синхронный путь использует Proto/gRPC с deadline, mTLS/SPIFFE и подписанным
  authorization context.
- Kubernetes, Mattermost и внешние API согласуются асинхронно с явным состоянием и повторами.
- Ручное согласование является долговечным состоянием ожидания, а не удержанием HTTP-запроса.
