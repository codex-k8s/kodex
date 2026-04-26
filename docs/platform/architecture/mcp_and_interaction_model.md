---
doc_id: ARC-CK8S-MCP-INTERACTION-0001
type: api-contract
title: kodex — MCP и модель взаимодействий
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# MCP и модель взаимодействий

## TL;DR

Платформенный MCP-сервер является тонкой инструментальной поверхностью для agent-manager, slot-агентов и будущих интеграций. Он не владеет доменными данными, а проверяет policy, пишет audit и маршрутизирует вызовы к owner-сервисам. Взаимодействия с человеком, approvals, уведомления и внешние каналы принадлежат `interaction-hub`.

## Роли MCP-контура

| Контур | Назначение |
|---|---|
| `platform-mcp-server` | Авторизация MCP-клиента, policy-проверка, аудит, маршрутизация инструментов к owner-сервисам. |
| `agent-manager` | Быстрый управляющий агент, который использует MCP как основной инструментальный интерфейс платформы. |
| Slot-агент | Использует MCP для платформенных операций, feedback, approvals, runtime и безопасных действий. |
| Owner-сервисы | Реализуют фактические команды и чтения; MCP не подменяет их доменную логику. |
| `interaction-hub` | Владеет диалогами, запросами к человеку, уведомлениями, callbacks и внешними каналами. |

## Что идёт через MCP

Через MCP должны проходить операции, где нужны:
- policy и audit;
- доступ к платформенным owner-сервисам;
- управление run, feedback, Human gate или notification;
- работа с runtime, slot, job или fleet;
- provider operation, если она выполняется как платформенная операция, а не как обычная работа slot-агента через `gh`;
- запрос к человеку из slot или agent-manager.

Через MCP не надо пропускать обычную работу slot-агента с provider-native артефактами, если роль и policy разрешают `gh` или нативный API провайдера.

## Категории инструментов

| Категория | Owner-сервис | Примеры инструментов |
|---|---|---|
| Access tools | `access-manager` | Проверить principal, получить допустимые действия, проверить membership. |
| Project tools | `project-catalog` | Получить состав workspace, источники документации, release policy, placement policy. |
| Provider tools | `provider-hub` | Создать `Issue`, обновить labels, перечитать артефакт, получить rate limit state. |
| Package tools | `package-hub` | Найти пакет, проверить установку, получить guidance package manifest. |
| Agent tools | `agent-manager` | Создать run, продолжить сессию, записать acceptance result, запросить follow-up. |
| Runtime tools | `fleet-manager`, `runtime-manager` | Получить slot, запросить job, продлить lease, освободить slot. |
| Interaction tools | `interaction-hub` | Запросить feedback, создать approval request, отправить уведомление, обработать callback. |
| Operations tools | `operations-hub` | Получить operator timeline, очереди, блокировки и сводку статуса. |

## Security и policy

Каждый MCP-вызов должен иметь:
- идентификатор caller: agent-manager, slot-agent, plugin workload, user session или service account;
- project/org scope;
- цель операции;
- policy decision;
- audit entry;
- correlation id для связи с run, job, slot, provider artifact или approval.

Правило: MCP-сервер не должен сам вычислять бизнес-решение, если это решение принадлежит owner-сервису. Он проверяет доступ к инструменту и вызывает owner-сервис.

## Взаимодействие с человеком

`interaction-hub` владеет:
- диалоговыми ветками;
- пользовательскими сообщениями;
- запросами обратной связи;
- approvals;
- уведомлениями;
- подписками;
- попытками доставки;
- callbacks из внешних каналов;
- ссылками на временные voice/media attachments.

Взаимодействие может начаться из:
- web-console;
- голосового ввода;
- provider-native mention или comment;
- внешнего канала через пакет;
- автоматического события, alert или schedule rule;
- slot-агента, которому нужен feedback.

## Внешние каналы

Список внешних каналов не фиксируется в ядре. Каналы подключаются через пакетную платформу и общий контракт interaction package.

Минимальный контракт канала:
- принять сообщение или request;
- доставить уведомление;
- вернуть callback или resolution;
- связать delivery attempt с исходным approval, run, job или dialog thread;
- передать ошибки доставки в `interaction-hub`;
- объявить требуемые permissions и secrets в manifest пакета.

Примеры каналов:
- Telegram approval package;
- Telegram feedback package;
- email package;
- корпоративный messenger package;
- webhook package для внешних систем.

## Типовые потоки

### Запрос из UI или голоса

1. Пользователь отправляет текст или голос в web-console.
2. `api-gateway` проверяет сессию и маршрутизирует запрос.
3. `interaction-hub` создаёт или продолжает dialog thread.
4. `agent-manager` интерпретирует намерение через MCP-инструменты.
5. При необходимости `provider-hub` создаёт или обновляет provider-native артефакт.
6. `operations-hub` обновляет read-проекцию для UI.

### Slot-агент просит обратную связь

1. Slot-агент вызывает MCP-инструмент feedback request.
2. `platform-mcp-server` проверяет caller, role policy, project scope и run context.
3. `interaction-hub` создаёт request и выбирает канал доставки.
4. Пользователь отвечает через UI или внешний канал.
5. `interaction-hub` связывает callback с request.
6. `agent-manager` продолжает session или переводит run в wait/error по policy.

### Human gate перед релизом

1. `agent-manager` формирует gate request по release policy.
2. `interaction-hub` доставляет Owner уведомление и собирает решение.
3. `runtime-manager` не запускает deploy job до принятого решения.
4. `operations-hub` показывает gate в operator queue.
5. После решения owner-сервис, владеющий переходом, меняет состояние и публикует событие.

## Наблюдаемость

Для MCP и interactions обязательно хранить:
- audit trail по каждому вызову инструмента;
- policy decision и причину отказа;
- latency и error class;
- delivery attempt status;
- связь с run, job, slot, provider artifact и approval;
- короткое описание для operator timeline.

Полные вложения и медиа не хранятся в PostgreSQL. В БД хранится ссылка, metadata, retention и связь с dialog thread.

## Апрув

- request_id: `owner-2026-04-26-platform-architecture-frame`
- Решение: approved
- Комментарий: MCP и модель взаимодействий входят в сквозной архитектурный каркас платформы.
