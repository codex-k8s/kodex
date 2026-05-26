---
doc_id: PRD-CK8S-CODEX-HOOK-INGRESS-0001
type: prd
title: kodex - требования к codex-hook-ingress
status: active
owner_role: PM
created_at: 2026-05-22
updated_at: 2026-05-26
related_issues: [698, 753, 778, 786, 793, 808, 322]
related_prs: []
related_docsets:
  - docs/domains/codex-hook-ingress/architecture/design.md
  - docs/domains/codex-hook-ingress/architecture/data_model.md
  - docs/domains/codex-hook-ingress/architecture/api_contract.md
  - docs/domains/codex-hook-ingress/architecture/emitter_sidecar_contract.md
  - docs/domains/codex-hook-ingress/delivery/codex_hook_ingress_delivery.md
  - docs/platform/architecture/codex_hooks_and_skills.md
  - docs/platform/architecture/mcp_and_interaction_model.md
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-05-22-codex-hook-ingress-docs"
---

# Требования к codex-hook-ingress

## TL;DR

- Что строим: входной сервисный контур для нормализованных Codex hook events от hook emitter или локального sidecar.
- Для кого: slot-агенты Codex, `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub`, realtime UI и операторский контур.
- Почему: Codex hooks являются lifecycle command hooks, а не MCP tools; смешивание их с `platform-mcp-server` сломает границы протоколов, аудита и владения состоянием.
- MVP: принять и очистить `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`; проверить binding, применить размерные лимиты, отрезать секреты и маршрутизировать безопасные события владельцам.
- Критерии успеха: hook ingress не хранит бизнес-истину, не принимает MCP calls, не пропускает raw secrets/session dumps и даёт соседним сервисам безопасный сигнал для lifecycle, gate, provider signal и realtime-ленты.

## Проблема и цель

Codex запускает hooks как command-обработчики в рабочем пространстве сессии и передаёт JSON на `stdin`. Платформе нужен управляемый канал, который принимает только очищенный envelope, добавляет платформенный контекст, проверяет источник и отдаёт событие сервису-владельцу.

Без отдельного `codex-hook-ingress` появляются риски:

- MCP-сервер начнёт принимать не-MCP transport и станет скрытым монолитом lifecycle-событий.
- В платформу попадут сырые `tool_input`, `tool_response`, stdout/stderr, transcript или session dump.
- `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager` и `interaction-hub` начнут дублировать очистку и проверку источника.
- Поддержка Codex skills смешается с hook transport и создаст новое хранилище capabilities не в своём домене.

Цель `codex-hook-ingress` - быть тонкой входной границей для hook events, а не сервисом бизнес-состояния.

## Пользователи и вызывающие стороны

| Вызывающая сторона | Потребность |
|---|---|
| Hook emitter | Получить JSON от Codex, очистить его, добавить platform context и отправить нормализованное событие. |
| Локальный sidecar | Буферизовать безопасные события, повторять отправку при временной недоступности платформы и не хранить секреты. |
| `agent-manager` | Получать lifecycle, prompt submit, stop checkpoint, ожидание flow и capability usage signals без raw payload. |
| `runtime-manager` | Получать slot/session binding, workspace diagnostics, tool execution summary и materialized capability refs. |
| `provider-hub` | Получать provider artifact signals, rate-limit hints и hot reconciliation hints без stdout `gh` и токенов. |
| `governance-manager` | Получать risk/gate context для `PermissionRequest`, policy-controlled `PreToolUse` и audit-critical decision refs без raw payload. |
| `interaction-hub` | Получать delivery intent для `PermissionRequest`, owner feedback request и notification intent без технических логов и без владения decision state. |
| Realtime UI и оператор | Видеть короткую безопасную ленту действий агента и классы отказов ingress. |

## MVP hook events

| Hook event | Платформенный смысл | Основные получатели |
|---|---|---|
| `SessionStart` | Старт или resume Codex-сессии внутри slot. | `agent-manager`, `runtime-manager` |
| `UserPromptSubmit` | Факт отправки пользовательского prompt и результат pre-check. | `agent-manager`, `interaction-hub` |
| `PreToolUse` | Намерение вызвать поддерживаемый tool и риск-сигнал до выполнения. | `agent-manager`, `governance-manager`, `runtime-manager`, realtime UI |
| `PermissionRequest` | Запрос разрешения Codex на действие, требующее решения. | `governance-manager`, `agent-manager`, `interaction-hub` |
| `PostToolUse` | Итог поддерживаемого tool, bounded error или provider artifact signal. | `agent-manager`, `runtime-manager`, `provider-hub` |
| `Stop` | Завершение хода агента, checkpoint и pending actions. | `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub` |

Контрольные точки сжатия контекста не входят в Codex hook set. Они проектируются только как будущие внутренние события `agent-manager` или `runtime-manager`.

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| CHI-FR-1 | Сервис должен принимать только нормализованные Codex hook events от hook emitter или локального sidecar. | Обязательно |
| CHI-FR-2 | Сервис не должен принимать MCP `tools/list`, `tools/call`, MCP transport или MCP tool discovery. Это зона `platform-mcp-server`. | Обязательно |
| CHI-FR-3 | Envelope события должен содержать `event_id`, `schema_version`, `hook_event_name`, `source`, `actor`, `organization_id`, `project_id`, `repository_id`, `run_id`, `session_id`, `slot_id`, `turn_id`, `correlation_id`, `emitter_version` и время события. | Обязательно |
| CHI-FR-4 | Сервис должен проверять actor/source/run/session/slot binding через авторитетные данные `agent-manager` и `runtime-manager` или их проверенную проекцию. | Обязательно |
| CHI-FR-5 | Сервис должен отклонять событие без совместимого `run_id`, `session_id`, `slot_id`, scope или с неподдерживаемым `hook_event_name`. | Обязательно |
| CHI-FR-6 | Сервис должен поддерживать только MVP-набор: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`. | Обязательно |
| CHI-FR-7 | Сервис должен применять размерные лимиты: нормализованный envelope до 64 KiB, отдельный текстовый preview до 4 KiB, bounded error до 8 KiB, binary payload запрещён. Значения являются стартовыми policy defaults и должны быть настройками платформы. | Обязательно |
| CHI-FR-8 | Сервис должен отбрасывать raw `tool_input`, raw `tool_response`, большие stdout/stderr, transcript, session dump, kubeconfig, provider payload и значения секретов. | Обязательно |
| CHI-FR-9 | Сервис должен сохранять или передавать только hash/digest, безопасный preview, tool category, exit status, bounded error, artifact signal, refs и correlation id. | Обязательно |
| CHI-FR-10 | Сервис должен маршрутизировать `SessionStart`, `UserPromptSubmit` и `Stop` в `agent-manager` как lifecycle/checkpoint-сигналы; для `PermissionRequest` `agent-manager` получает только ожидание flow и refs, если действие связано с агентным переходом. | Обязательно |
| CHI-FR-11 | Сервис должен маршрутизировать runtime diagnostics, workspace refs и slot/session binding в `runtime-manager`, не меняя состояние slot сам. | Обязательно |
| CHI-FR-12 | Сервис должен маршрутизировать provider artifact signals, hot cursor hints и rate-limit hints в `provider-hub`, не выполняя provider read/write operations. | Обязательно |
| CHI-FR-13 | Сервис должен маршрутизировать owner feedback, approval delivery intent и notification intent в `interaction-hub`, не владея диалогами и доставкой. | Обязательно |
| CHI-FR-14 | `PermissionRequest` должен превращаться в risk/gate request у `governance-manager`; `agent-manager` хранит только ожидание flow и refs, а `interaction-hub` доставляет запрос человеку. `codex-hook-ingress` может ждать итоговое allow/deny или `no_decision` только как транспортный bridge с timeout и безопасным отказом. `no_decision` означает отсутствие hook-specific решения и не должен маппиться в неподдерживаемый `permissionDecision: "ask"`. | Обязательно |
| CHI-FR-15 | `PreToolUse` может вернуть deny или дополнительный контекст только после решения владельца политики; ingress не должен самостоятельно принимать бизнес-решения. | Обязательно |
| CHI-FR-16 | `PostToolUse` не должен пытаться откатить side effects уже выполненного tool; он передаёт безопасный итог владельцам. | Обязательно |
| CHI-FR-17 | Сервис должен публиковать короткую операционную ленту и метрики по событиям, отказам, sanitizer, лимитам, latency и retry. | Обязательно |
| CHI-FR-18 | Высокочастотные allow-события должны иметь короткий retention и не должны писаться в БД построчно без доменной причины. | Обязательно |
| CHI-FR-19 | Сервис должен поддерживать идемпотентность по `event_id` и `correlation_id`, чтобы retries emitter/sidecar не создавали дубликаты downstream-событий. | Обязательно |
| CHI-FR-20 | Сервис должен принимать только ссылки на выбранный capability context и skill refs, если они уже выбраны `agent-manager` и материализованы `runtime-manager`; хранение каталога skills, manifest и текстов `SKILL.md` в `codex-hook-ingress` запрещено. | Обязательно |
| CHI-FR-21 | Machine-readable контракт CHI-1 должен быть JSON Schema в `specs/jsonschema/codex-hook-ingress.v1/**`: normalized hook envelope, sanitizer contract и safe examples. Эти схемы не являются proto, OpenAPI, AsyncAPI или MCP tool schema. | Обязательно |
| CHI-FR-22 | Hook emitter должен запускаться как Codex command hook в slot runtime и читать только JSON object из `stdin`; `transcript_path` не является стабильным интерфейсом и не должен читаться, буферизоваться или пересылаться. | Обязательно |
| CHI-FR-23 | Local sidecar может использоваться как локальный процесс рядом с Codex runtime для bounded retry buffer, но он должен принимать и хранить только normalized/sanitized envelope после sanitizer. | Обязательно |
| CHI-FR-24 | Emitter/sidecar должен отправлять события только в логическую операцию `SubmitHookEvent` сервиса `codex-hook-ingress`; `integration-gateway`, MCP transport и бизнес-команды не являются получателями hook events. | Обязательно |
| CHI-FR-25 | Emitter/sidecar должен использовать source binding плюс workload identity, short-lived source token или mTLS service identity; secret material запрещён в config, логах и buffer. | Обязательно |
| CHI-FR-26 | Emitter/sidecar должен поддерживать идемпотентность по `event_id + payload_digest`, best-effort ordering внутри `run_id/session_id` и retry с тем же `event_id`. | Обязательно |
| CHI-FR-27 | Backpressure должен различать audit-critical/decision events и realtime-only events: рискованные события fail-closed или retry until TTL, realtime-only события могут быть отброшены с metric после переполнения buffer. | Обязательно |
| CHI-FR-28 | Compact checkpoints не должны появляться в `supported_hook_events`; будущая поддержка compact оформляется как внутренние события `agent-manager`/`runtime-manager`, а не `PreCompact`/`PostCompact` Codex hooks. | Обязательно |
| CHI-FR-29 | Machine-readable контракт CHI-2 должен быть JSON Schema `hook-emitter-config.v1`, описывающей runtime input, delivery, auth, idempotency, ordering, retry, buffer, backpressure и failure policy без выбора физического транспорта. | Обязательно |

## Поддержка Codex skills как capability layer

Codex skills рассматриваются как управляемые capabilities, которые могут повлиять на hook-события, но не становятся сущностями `codex-hook-ingress`.

| Область | Требование |
|---|---|
| Источник | Источник skill фиксируется как built-in platform source, user/repository source или package source. Для package source авторитетные package/version/install данные отдаёт `package-hub`. |
| Версия | `agent-manager` фиксирует выбранную версию skill или package installation ref в metadata run. `codex-hook-ingress` принимает только ссылку и digest, если они уже есть в run context. |
| Область | Skill может быть доступен на уровне platform, organization, project, repository, flow, stage или role. Решение о применимости принимает `agent-manager` по policy. |
| Manifest/metadata | `package-hub` хранит package manifest и установку. `agents/openai.yaml`, `SKILL.md` metadata и tool dependencies используются при выборе и materialization, но не копируются в hook ingress. |
| Workspace requirements | `runtime-manager` материализует разрешённые skills в рабочее пространство, следит за путями, правами файлов, scripts/assets/references и sandbox profile. |
| Выбор | `agent-manager` выбирает allowed/required/forbidden skills для run, role и stage и передаёт runtime только утверждённый skill set. |
| Materialization | `runtime-manager` получает выбранный skill set и подготавливает локальную структуру для Codex. Hook events могут ссылаться на `capability_context_id` или `skill_ref`, но не передавать содержимое skill. |
| Ограничение прав | Skill не расширяет права роли. Если skill требует MCP tools, scripts или внешние системы, это должно быть разрешено отдельно через policy и MCP/permission контуры. |

## Нефункциональные требования

| Область | Требование |
|---|---|
| Безопасность | Ни raw secrets, ни токены, ни kubeconfig, ни provider credentials, ни session dumps не должны попадать в БД, события, логи, метрики или ответы hook handler. |
| Надёжность | Временная недоступность владельца должна приводить к retry/backoff или безопасному отказу по классу события. `PermissionRequest` не должен молча продолжаться после timeout. |
| Производительность | Входной контур должен быть коротким маршрутом без тяжёлой агрегации, без чтения больших объектов и без синхронного обращения к внешним провайдерам. |
| Наблюдаемость | Нужны метрики по event type, route, sanitizer decision, rejection class, latency, owner timeout, duplicate event и queue depth. |
| Совместимость | Схема нормализованного envelope версионируется отдельно от OpenAI hook input и будущего транспортного контракта ingress. |
| Мультитенантность | Все события несут organization/project/repository scope и не могут маршрутизироваться без проверки scope. |

## Критерии приёмки

| ID | Критерий |
|---|---|
| CHI-AC-1 | Доменный пакет явно отделяет `codex-hook-ingress` от `platform-mcp-server` и не описывает Codex hooks как MCP tools. |
| CHI-AC-2 | MVP hook set содержит только `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`. |
| CHI-AC-3 | Размерные лимиты, redaction, запрет секретов, запрет больших stdout/stderr и запрет session dumps описаны до контрактов и кода. |
| CHI-AC-4 | Для каждого hook event указан сервис-владелец downstream-состояния. |
| CHI-AC-5 | `PermissionRequest` описан как bridge к risk/gate decision у `governance-manager`, ожиданию flow у `agent-manager` и delivery у `interaction-hub`, а не как локальный yes/no без аудита. |
| CHI-AC-6 | Skills описаны как capability layer: source, version, scope, metadata, workspace requirements, selection и materialization, без хранилища skills в hook ingress. |
| CHI-AC-7 | Delivery-план разделяет docs-first, JSON Schema, runtime contract, сервисный каркас и будущий physical transport; proto, OpenAPI и AsyncAPI требуют отдельного транспортного решения. |
| CHI-AC-8 | CHI-1 содержит machine-readable схемы и safe examples, которые валидируются локальной JSON Schema проверкой без генерации сервисного кода. |
| CHI-AC-9 | CHI-2 содержит контракт hook emitter/local sidecar: runtime placement, чтение `stdin`, sanitizer до buffer/send, retry/backpressure/failure policy и логический `SubmitHookEvent` в `codex-hook-ingress`. |
| CHI-AC-10 | CHI-2 не выбирает gRPC/HTTP transport, не создаёт OpenAPI/AsyncAPI/proto, не добавляет `PreCompact`/`PostCompact` в MVP hook set и не хранит raw payload. |
| CHI-AC-11 | Сервисный каркас поддерживает route registry и dispatch только перечисленных safe event parts через owner ports/stubs; disabled, unsupported и failed routes возвращают safe diagnostics и не считаются успешной доставкой. |

## Не-цели

- Не реализовывать physical transport, proto, OpenAPI, AsyncAPI, миграции или Kubernetes manifests без отдельного транспортного или deploy-среза.
- Не добавлять новые Codex hook events сверх MVP-набора.
- Не проектировать hooks как MCP tools.
- Не создавать отдельное хранилище skills, manifest или package catalog внутри `codex-hook-ingress`.
- Не выполнять provider write/read operations из hook ingress.
- Не хранить полный prompt, transcript, raw tool input/output или session JSON в Postgres.
- Не менять код `platform-mcp-server`, `agent-manager`, `package-hub` или соседних сервисов.

## Зависимости

| Зависимость | Зачем нужна |
|---|---|
| `agent-manager` | Run/session binding, ожидание flow, stop checkpoint, skill selection и metadata run. |
| `runtime-manager` | Slot/session binding, workspace, materialized skills, local sidecar/emitter config и runtime diagnostics. |
| `provider-hub` | Provider artifact signals, hot reconciliation hints, provider limits и typed provider operations вне hook ingress. |
| `governance-manager` | Risk assessment, gate request/decision, policy-based approvals и audit-critical decision state. |
| `interaction-hub` | Доставка owner feedback, approvals, Human gate prompts и notifications для `PermissionRequest` без владения decision state. |
| `platform-mcp-server` | Отдельная MCP-поверхность tools; hook ingress только ссылается на MCP tool names в sanitized events. |
| `package-hub` | Package source, package installation refs, versions и manifest snapshots для skills, поставляемых через пакеты. |
| `access-manager` | Проверка actor, role, scope и policy для source binding и tool/capability use. |

## Апрув

- request_id: `owner-2026-05-22-codex-hook-ingress-docs`
- Решение: pending
- Комментарий: доменный пакет фиксирует docs-first границы #698 до реализации hook emitter, ingress service и контрактов.
