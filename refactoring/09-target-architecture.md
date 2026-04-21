---
doc_id: REF-CK8S-0009
type: target-architecture
title: "kodex — целевая архитектура новой платформы"
status: active
owner_role: SA
created_at: 2026-04-21
updated_at: 2026-04-21
related_issues: [281, 282, 309, 376, 470, 488]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave2"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Целевая архитектура новой платформы

## TL;DR
- Новая архитектура строится вокруг набора доменных сервисов-владельцев, а не вокруг одного `control-plane`.
- `api-gateway` и `web-console` остаются thin-edge и интерфейсным слоем, без владения доменной логикой и каноническим состоянием.
- Канонический первый набор сервисов-владельцев: `access-manager`, `project-catalog`, `provider-hub`, `agent-manager`, `runtime-manager`, `interaction-hub`, `operations-hub`.
- `worker` и `agent-runner` остаются исполнительными компонентами, но не владельцами доменной правды.
- `control-plane` в целевом состоянии должен исчезнуть как доменное ядро; допускается только временное существование как переходного thin shell без собственного доменного центра.

## 1. Архитектурные драйверы
Архитектура должна одновременно поддержать:
1. provider-first модель работы через `Issue`, `PR/MR`, комментарии, mentions и связи;
2. central agent-manager как основной управляющий интерфейс продукта;
3. slot/runtime платформу с `code-only`, `full-env`, prewarmed slots и platform jobs;
4. MCP-контур для опасных операций, approvals и расширяемых инструментов;
5. настраиваемые внешние каналы взаимодействия и уведомлений;
6. постепенное вытеснение старого `control-plane` без сохранения его как "новой стабильной формы".

## 2. Базовые архитектурные принципы

### 2.1. Один домен — один владелец истины
Каждый тип данных и каждый бизнес-процесс должны иметь один сервис-владелец.

### 2.2. Edge остаётся thin-edge
`services/external/*` и `services/staff/*` не становятся доменными сервисами.

### 2.3. Исполнители не владеют доменной правдой
`worker` и `agent-runner` выполняют задачи и reconciliations, но не определяют каноническое состояние бизнеса.

### 2.4. Provider-специфика изолируется
GitHub- и GitLab-особенности не должны расползаться по нескольким доменам. Для этого нужен отдельный owner-контур провайдера.

### 2.5. Runtime — отдельный тяжёлый operational контур
Слоты, build/deploy/cleanup jobs, реестр образов, Kubernetes orchestration и retention нельзя размазывать по агентной оркестрации.

### 2.6. Console и operator UX питаются от проекций чтения
Интерфейс не должен собирать бизнес-смысл напрямую из нескольких чужих БД и не должен превращать gateway в новый доменный центр.

## 3. Целевая карта контейнеров и сервисов

### 3.1. Edge и UI
- `services/staff/web-console`
- `services/external/api-gateway`

### 3.2. Доменные сервисы-владельцы
- `access-manager`
- `project-catalog`
- `provider-hub`
- `agent-manager`
- `runtime-manager`
- `interaction-hub`
- `operations-hub`

### 3.3. Исполнительные компоненты
- `services/jobs/worker`
- `services/jobs/agent-runner`

### 3.4. Хранилище
- PostgreSQL-контур как общий инфраструктурный кластер с database-per-service моделью

## 4. Роли сервисов в целевой архитектуре

### `access-manager`
Отвечает за:
- пользователей платформы;
- allowlist и вход;
- memberships и права доступа;
- системные настройки;
- административный аудит.

### `project-catalog`
Отвечает за:
- проекты;
- репозитории;
- привязку документации и project rules;
- `services.yaml` и связанный config-контур;
- onboarding/preflight репозиториев.

### `provider-hub`
Отвечает за:
- provider accounts;
- webhook normalization;
- нативные сущности провайдера в нормализованном виде;
- provider mirrors/enrichment;
- mentions, relationships, provider metadata;
- rate limits и ограничения внешних API;
- выполнение provider-операций через интерфейсы.

Это отдельный сервис, потому что новая платформа уже не может позволить себе размазывать GitHub/GitLab semantics по `agent-manager`, runtime и UI одновременно.

### `agent-manager`
Отвечает за:
- разбор пользовательского намерения;
- выбор flow;
- запуск role-агентов;
- lifecycle run и session;
- acceptance machine;
- handover и follow-up логику;
- остановку процесса при недостающих данных, лимитах или policy-блокировках.

### `runtime-manager`
Отвечает за:
- slot lifecycle;
- reuse и prewarming;
- build/deploy/mirror jobs;
- cleanup/retention jobs;
- orchestration Kubernetes и реестра образов;
- технический статус среды.

### `interaction-hub`
Отвечает за:
- approvals;
- уведомления;
- delivery attempts;
- callback handling;
- внешний канал связи с человеком;
- subscriptions и маршрутизацию событий.

### `operations-hub`
Отвечает за:
- operator-facing read-модели;
- timeline, очереди, статусы, рабочие представления;
- проекции для web-console;
- единый операционный срез по run, job, блокировкам и событиям.

Это read-heavy сервис. Он не должен становиться владельцем первичных бизнес-данных.

## 5. Исполнительные компоненты

### `worker`
Общий фоновой исполнитель доменных задач и reconciliations.

Он может:
- подбирать ожидающие задачи;
- вызывать сервисы-владельцы;
- исполнять retry/expiry/reconciliation loops.

Он не должен:
- владеть доменной моделью;
- иметь собственную "скрытую правду";
- напрямую решать бизнес-переходы в обход сервисов-владельцев.

### `agent-runner`
Исполнитель агентных сессий и role-агентов в slot/runtime.

Он может:
- выполнять инструкции;
- работать с кодом, runtime и MCP;
- возвращать результаты сервисам-владельцам.

Он не должен:
- определять канонический lifecycle run;
- сам становиться источником истины для flows, approvals, provider state или jobs.

## 6. Ключевые потоки между сервисами

### 6.1. Входящий webhook или mention из GitHub/GitLab
1. `api-gateway` валидирует запрос и маршрутизирует его.
2. `provider-hub` нормализует событие и обновляет provider state.
3. `agent-manager` получает нормализованный сигнал и решает, нужен ли run, follow-up или блокировка.
4. `runtime-manager` поднимает или переиспользует slot и platform jobs, если это требуется.
5. `interaction-hub` уведомляет человека, если нужен approval, feedback или операционное внимание.
6. `operations-hub` строит видимую оператору проекцию.

### 6.2. Запуск задачи из UI/голоса
1. Пользователь идёт через `web-console` и `api-gateway`.
2. `agent-manager` интерпретирует запрос и решает, создать ли `Issue`, продолжить ли существующую задачу или спросить уточнение.
3. Если требуется работа с провайдером, `agent-manager` идёт в `provider-hub`.
4. Если требуется slot/runtime, `agent-manager` идёт в `runtime-manager`.
5. Если нужно уведомление, human gate или внешний канал, используется `interaction-hub`.
6. Оператор видит агрегированное состояние через `operations-hub`.

### 6.3. Build/deploy/cleanup
1. `runtime-manager` создаёт и ведёт platform jobs.
2. `worker` или специализированные исполнители выполняют нужные действия.
3. Статус и краткий хвост лога возвращаются в контур-владелец `runtime-manager`.
4. `interaction-hub` и `operations-hub` распространяют это в уведомления и UI-проекции.

## 7. Что происходит со старым `control-plane`

### Целевое состояние
Старый `services/internal/control-plane` удаляется как единый доменный центр.

Его обязанности разъезжаются по новым сервисам:
- auth/admin -> `access-manager`
- project/repo config -> `project-catalog`
- provider semantics/webhooks/rate limits -> `provider-hub`
- run/session/acceptance/flow -> `agent-manager`
- slots/build/deploy/cleanup -> `runtime-manager`
- approvals/notifications/callbacks -> `interaction-hub`
- projections/operator views -> `operations-hub`

### Допустимое переходное состояние
На переходном этапе допустимо, что часть кода ещё физически живёт в старом каталоге, но только если:
- владение уже переписано в документации;
- старый слой не остаётся каноническим доменом;
- новый сервисный контур описан как целевой владелец;
- это рассматривается как временный migration shell, а не как финальная архитектура.

## 8. Что не нужно фиксировать преждевременно
В этой волне не нужно зацементировать:
- точную структуру таблиц и миграций;
- точные gRPC методы;
- точный формат watermark;
- точные event/outbox контракты;
- точный выбор механизма расширения внешних каналов.

Это пойдёт следующими волнами поверх уже зафиксированной service map.

## 9. Deferred seams
Нужно уже сейчас держать в уме, но не выделять в отдельные сервисы-владельцы без необходимости:
- knowledge storage и vector lifecycle;
- расширенный governance-read model beyond basic operator projections;
- дополнительные provider implementations beyond GitHub-first;
- специализированные background executors по отдельным доменам, если общий `worker` станет узким местом.
