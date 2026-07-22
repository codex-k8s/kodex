---
id: ARCH-MC-004
title: Границы сервисов и структура репозитория
type: architecture
status: approved
owner: architect
version: 0.3.0
updated: 2026-07-22
---

# Границы сервисов и структура репозитория

## Целевая структура

```text
apps/
  control-center/
services/
  external/
    interaction-gateway/
  internal/
    control-plane/
    runtime-controller/
    integration-gateway/
  jobs/
    automation-scheduler/
    agent-runner/
  dev/
libs/go/
proto/
specs/
  openapi/
  asyncapi/
config/catalog/
  roles/
  integrations/
  playbooks/
deploy/
  helm/
  gitops/
docs/
```

Каждый сервис Go использует локальные `cmd`, `internal/app`, `internal/domain`, `internal/repository`, `internal/clients` и `internal/transport`. `libs/go/**` содержит только наблюдаемость, контекст авторизации, типизированные идентификаторы, часы и другие действительно сквозные примитивы с несколькими реальными потребителями. Подробная структура определена в `docs/design-guidelines/go/services_design_requirements.md`.

## Границы компонентов

### control-plane

- CRUD и проверка бизнес-сущностей;
- вычисление итоговой конфигурации и политик;
- OpenAPI для Control Center;
- транзакционный исходящий журнал;
- миграции собственных схем.

Не выполняет сверку Kubernetes, запуск ИИ и внешние изменения.

### interaction-gateway

- Mattermost WebSocket/REST;
- карточки, диалоги, реакции и учетные записи ботов;
- входные файлы и исходящие доставки;
- преобразование сообщений и обсуждений в команды платформы;
- идемпотентность входных событий.

Не владеет сессиями, расписаниями и интеграциями.

### runtime-controller

- сверка pod, PVC, секретов и конфигурационных ресурсов сессий;
- применение `RuntimeRevision`;
- контроль ресурсов, допуск, вытеснение простаивающих pod и TTL;
- жизненный цикл и состояние ресурсов Kubernetes.

### integration-gateway

- транспорт MCP;
- выбор соединения;
- права, риски и согласования;
- изоляция учетных данных;
- идемпотентное выполнение инструментов.

### automation-scheduler

- получение наступивших расписаний;
- идемпотентность экземпляров расписания;
- политика пропусков и параллельности;
- постановка `ScheduledRun` в очередь;
- вычисление следующего запуска.

До выделения самостоятельного сервиса текущий bot-service также владеет узким контрактом ручного шлюза автоматизации: атомарной записью `waiting_owner` и точного `OwnerAttentionRequest`, server-owned публикацией с устойчивой identity, ограниченным восстановлением несохранённого post binding при старте и атомарным закрытием связи `ScheduledRun → attention`. Общий watchdog, heartbeat/deadline/lease, callback outbox, Kubernetes health и retry среды выполнения не входят в эту границу.

### agent-runner

- получение и завершение хода;
- жизненный цикл процесса среды выполнения ИИ;
- восстановление и снимок сессии;
- материализация рабочей области;
- локальный мост публикации файлов.

## Переход от текущего bot-service

1. Зафиксировать характеристические тесты существующих сценариев.
2. Ввести пакеты доменных контекстов и отдельные интерфейсы репозиториев внутри текущего процесса.
3. Разделить общий `admin.Repository` по владельцам данных.
4. Вынести транспорт Mattermost из доменных сервисов.
5. Ввести исходящий журнал и идемпотентные обработчики команд.
6. Первым самостоятельным сервисом выделить `runtime-controller`.
7. Выделить `integration-gateway` после появления модели интеграций.
8. Выделить interaction-gateway и control-plane после стабилизации OpenAPI.
9. Удалить фасад совместимости только после миграции интерфейса и рабочих данных.

Нельзя одновременно менять модель данных, транспорт, границы сервисов и пользовательское поведение без промежуточного совместимого состояния.

## Внутренние контракты

- Внешний API и API управления: OpenAPI.
- Обратные вызовы Mattermost: типизированные HTTP-модели на основе официального SDK.
- Доменные события и команды: AsyncAPI и версионируемые конверты.
- Внутренняя потоковая передача с высокой пропускной способностью: Protobuf/gRPC только после измеренной необходимости.
- MCP: официальный Model Context Protocol Go SDK.

До выделения `integration-gateway` текущий bot-service владеет внешней HTTP-границей MCP. Для POST он ограничивает полный JSON envelope до передачи в `go-sdk`, чтения session/token и доменного допуска: oversized `Content-Length` и превысивший предел chunked body получают транспортный отказ. Полный server-owned `ReadTimeout`, `ReadHeaderTimeout`, `IdleTimeout` и `MaxHeaderBytes` ограничивают медленные неаутентифицированные соединения. GET/SSE и допустимый POST сохраняют семантику SDK.

До выделения `interaction-gateway` текущий bot-service также владеет доставкой двух обязательных callback audit publications. Доменная транзакция владеет ровно двумя неизменяемыми строками outbox и манифестом их точного множества; `CallbackRunID` не фиксируется без одной публикации каждой обязательной destination и полного плана. Mattermost adapter владеет post-commit сетевой попыткой, детерминированной внешней identity и точной сверкой существующего post: client-owned payload сравнивается полностью, а из server-owned props разрешено только документированное Mattermost 11.6 поле `from_bot: "true"`. Нельзя считать `CallbackRunID`, непустое подмножество `delivered`, успешный HTTP-ответ без DB mark или кратковременный `pending_post_id` доказательством завершения всей доставки. Переход к отдельному gateway обязан сохранить ключ `(delegation, callback run, destination, publication)`, манифест, payload hash, lease, final binding fence и монотонное подтверждение `delivered`.
