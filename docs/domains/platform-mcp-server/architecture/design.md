---
doc_id: DSG-CK8S-PLATFORM-MCP-0001
type: design-doc
title: kodex — дизайн platform-mcp-server
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues: [747]
related_prs: []
related_adrs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-platform-mcp-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Дизайн platform-mcp-server

## TL;DR

- Что меняем: выделяем `platform-mcp-server` как тонкую MCP-поверхность платформы.
- Почему: `agent-manager`, slot-агенты и hook emitter должны обращаться к платформе через управляемую policy/auth boundary, а не напрямую к каждому сервису.
- Основные компоненты: MCP transport, source verifier, tool catalog, policy boundary, router к сервисам-владельцам, sanitizer, bounded diagnostics и audit emitter.
- Риски: превратить MCP в доменный монолит, хранить сырые данные вызовов или начать обходить `provider-hub` при операциях провайдера.

## Цели

- Зафиксировать границу сервиса до контрактов и кода.
- Разделить MCP-поверхность и доменное владение.
- Описать связь входного контура hook-событий с hook emitter без подмены реализации hooks.
- Подготовить будущие инструменты `agent-manager` без переноса состояния run/session/gate в MCP.
- Подготовить provider tools без обхода `provider-hub` и его provider-native pipeline.

## Не-цели

- Не проектировать полную proto/AsyncAPI спецификацию.
- Не реализовывать сервисный код.
- Не создавать БД-модель.
- Не проектировать `staff-gateway` или `interaction-hub`.
- Не переносить бизнес-состояние из сервисов-владельцев.

## Граница сервиса

| Владеет `platform-mcp-server` | Не владеет |
|---|---|
| MCP-поверхность инструментов, граница приёма hook-событий, нормализация вызова, проверка источника, минимальная tool-policy, маршрутизация, очистка данных, ограниченная диагностика, idempotency/correlation на границе. | `Run`, session, flow, stage, role, prompt, gates, slot, job, workspace, provider projections, provider write truth, project policy, package installation, dialogue, notification, billing, UI. |

Главное правило: `platform-mcp-server` отвечает на вопрос «можно ли этому источнику вызвать этот инструмент в этом контексте и как безопасно передать вызов владельцу». Он не отвечает на вопрос «как меняется бизнес-состояние домена».

## Ответственность соседних сервисов

| Сервис | Ответственность | Роль MCP |
|---|---|---|
| `agent-manager` | `Run`, session, flow, role, prompt, acceptance, gates, agent lifecycle. | MCP вызывает только типизированные agent-инструменты и не хранит состояние run/gate. |
| `runtime-manager` | Slot, workspace, job, cleanup, prewarm и runtime refs. | MCP маршрутизирует чтения и разрешённые команды runtime, не выбирает slot и не меняет job state сам. |
| `fleet-manager` | Серверы, Kubernetes-кластеры, health и placement decision. | MCP маршрутизирует административные чтения и будущие fleet tools без собственной placement-логики. |
| `provider-hub` | Provider projections, webhook, reconciliation, лимиты, provider write pipeline. | MCP вызывает provider tools только через `provider-hub`, не через GitHub/GitLab напрямую. |
| `project-catalog` | Проекты, репозитории, `services.yaml`, workspace policy, release/placement policy. | MCP читает проектную политику только через `project-catalog`. |
| `package-hub` | Пакеты, manifest, установки, catalog, store connections. | MCP читает package/install/manifest через `package-hub`. |
| `interaction-hub` | Диалоги, owner feedback, approval delivery, notifications, callbacks. | MCP создаёт запросы обратной связи через `interaction-hub`, когда контракт готов. |

## Компоненты

| Компонент | Назначение |
|---|---|
| MCP transport | Принимает tool calls и возвращает нормализованные ответы. |
| Адаптер приёма hook-событий | Принимает события Codex hooks из slot emitter или локального sidecar. |
| Source verifier | Проверяет actor, source type, run id, session id, slot id, project/repository scope и подпись или токен вызова. |
| Tool catalog | Хранит список разрешённых tool groups, версий и владельцев маршрута. |
| Policy boundary | Делает минимальную проверку права на инструмент и риск-профиля вызова. Доменную проверку выполняет сервис-владелец. |
| Router | Вызывает сервис-владелец по внутреннему gRPC-контракту. |
| Sanitizer | Удаляет секреты, сырые данные вызова, большие логи и небезопасные поля до маршрутизации, аудита и ответа. |
| Diagnostics guard | Ограничивает размер и тип диагностических ответов. |
| Audit emitter | Фиксирует решения, risky operations, отказы и permission/gate сценарии без сырых данных. |

## Основные потоки

### Hook-событие из slot

```mermaid
sequenceDiagram
  participant C as Codex runtime
  participant E as hook emitter
  participant MCP as platform-mcp-server
  participant AM as agent-manager
  participant R as runtime-manager
  participant PH as provider-hub
  C->>E: hook event
  E->>E: normalize + redact + attach run/session/slot
  E->>MCP: hooks.* event
  MCP->>MCP: verify source + sanitize envelope
  alt lifecycle или permission
    MCP->>AM: record lifecycle/gate signal
  else runtime diagnostics
    MCP->>R: record bounded runtime signal
  else provider signal
    MCP->>PH: RegisterProviderArtifactSignal
  end
```

Приём hook-событий не означает постоянное хранение каждого события. `platform-mcp-server` пропускает только нормализованный безопасный envelope и маршрутизирует его владельцу, который решает, что хранить.

### Provider-инструмент

```mermaid
sequenceDiagram
  participant A as agent-manager или slot agent
  participant MCP as platform-mcp-server
  participant PH as provider-hub
  participant ACC as access-manager
  participant P as provider
  A->>MCP: provider.issue.create typed tool
  MCP->>MCP: verify source + policy boundary
  MCP->>PH: typed provider command + policy context + gate ref
  PH->>ACC: ResolveExternalAccountUsage
  PH->>P: provider API через adapter
  PH-->>MCP: safe ProviderOperationResponse
  MCP-->>A: safe tool result
```

MCP не выбирает токен провайдера, не хранит секрет и не сохраняет исходные данные провайдера.

### Agent-manager инструмент

```mermaid
sequenceDiagram
  participant A as manager-agent
  participant MCP as platform-mcp-server
  participant AM as agent-manager
  participant IH as interaction-hub
  A->>MCP: agent.gate.submit / agent.run.start
  MCP->>MCP: source binding + tool policy
  MCP->>AM: typed agent command
  alt требуется доставка решения человеку
    AM->>IH: request feedback or approval
  end
  AM-->>MCP: agent command result
```

`agent-manager` остаётся владельцем `Run`, session и gate. MCP только проверяет инструментальную границу.

### Ограниченная диагностика

```mermaid
sequenceDiagram
  participant O as operator или manager-agent
  participant MCP as platform-mcp-server
  participant S as owner service
  O->>MCP: diagnostics.status.read
  MCP->>S: readiness/status query
  S-->>MCP: bounded status
  MCP-->>O: короткий безопасный результат
```

Диагностика не возвращает большие логи, секреты, kubeconfig, исходные данные провайдера, полный stdout/stderr или сырые session files.

## Безопасность

### Контекст вызова

Каждый вызов должен иметь:

- `actor_id` и `actor_type`;
- `source_type`: `agent_manager`, `slot_agent`, `hook_emitter`, `plugin_workload`, `operator`;
- `source_instance_id`;
- `organization_id`, `project_id`, `repository_id`, если применимо;
- `agent_run_id`, `session_id`, `slot_id`, если вызов связан с агентной работой;
- `correlation_id`;
- `command_id` или idempotency key для изменяющих операций;
- `tool_name` и `tool_version`.

Вызов отклоняется, если source не может быть связан с ожидаемым run/slot/session или если область проекта не совпадает с политикой запуска.

### Очистка данных

Запрещено хранить и передавать дальше без отдельного доменного решения:

- значения секретов;
- `Authorization` headers, tokens, private keys;
- полный `tool_input` и `tool_response`;
- полный prompt, если он не является частью диалогового контура `interaction-hub`;
- большие stdout/stderr;
- исходные данные провайдера;
- kubeconfig и Kubernetes objects;
- бинарные данные и вложения.

Разрешённый минимум: тип события, безопасная категория инструмента, hash/digest, object ref, короткая безопасная сводка, exit status, bounded error code, timestamps и correlation id.

### Rate limits и backpressure

- Лимиты задаются по actor, source type, tool group, project scope и dependency route.
- При переполнении очереди MCP возвращает явный retryable error, а не создаёт скрытую фоновую работу без владельца.
- Большие данные вызова отклоняются до маршрутизации.
- Timeout вызова владельца меньше, чем общий timeout MCP-call, чтобы вернуть контролируемую ошибку.

### Аудит

Аудит пишется только для:

- решений policy/gate;
- risky operations;
- отказов доступа;
- provider write operations;
- permission requests;
- изменения статуса run/session через MCP;
- диагностических запросов с повышенным доступом.

Массовые успешные read-only вызовы и allow-события hook не пишутся как полный аудит, но отражаются в метриках и короткой операционной истории с retention.

## Наблюдаемость

| Область | Что измерять |
|---|---|
| Tool calls | Количество, задержка, статус, tool group, owner service. |
| Policy boundary | Allow/deny/ask, причина отказа, риск-класс без данных вызова. |
| Hooks | Количество по типам, срабатывания очистки, отказы по размеру, результат маршрутизации. |
| Dependencies | Ошибки gRPC, timeout, unavailable, latency per owner service. |
| Safety | Количество удалённых секретоподобных значений, отказы из-за размера данных вызова, срабатывания rate limit. |

## Апрув

- request_id: `owner-2026-05-14-platform-mcp-kickoff`
- Решение: approved
- Комментарий: дизайн `platform-mcp-server` согласован как целевое состояние.
