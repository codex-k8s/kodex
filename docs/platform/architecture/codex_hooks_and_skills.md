---
doc_id: ALT-CK8S-CODEX-HOOKS-SKILLS-0001
type: alternatives
title: kodex — варианты использования Codex hooks и skills
status: proposed
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues:
  - 698
related_prs: []
approvals:
  required:
    - Owner
  status: pending
  request_id: owner-2026-05-14-codex-hooks-skills
---

# Варианты использования Codex hooks и skills

## TL;DR

Платформа должна заложить `Codex hooks` до MVP как управляемый канал связи slot-агента с платформой: жизненный цикл, запросы разрешений, сигналы о работе с провайдером, контрольные точки перед и после сжатия контекста, финальная контрольная точка хода. `Codex skills` лучше не делать обязательной частью MVP, но сразу зафиксировать модель владения, установки и будущего UI, чтобы не переделывать `agent-manager`, `runtime-manager`, `package-hub` и `provider-hub`.

Рекомендованный путь: до MVP реализовать минимальный слой hooks через `platform-mcp-server` и `agent-manager`, а skills спроектировать как будущую управляемую возможность поверх `package-hub`. Отдельный сервис для hooks/skills сейчас не нужен.

## Статус решения

Документ является вариантной проработкой. Он не меняет доменные документы, `proto`, `AsyncAPI` и код до выбора владельца.

## Исходные источники

- OpenAI Codex hooks: <https://developers.openai.com/codex/hooks>
- OpenAI Codex skills: <https://developers.openai.com/codex/skills>
- OpenAI Codex plugin structure: <https://developers.openai.com/codex/plugins/build#plugin-structure>
- Generated hook schemas в репозитории Codex: <https://github.com/openai/codex/tree/main/codex-rs/hooks/schema/generated>

## Цели

- Не потерять жизненный цикл slot-агента между локальной средой исполнения Codex и платформой.
- Дать платформе быстрый путь для `PermissionRequest`, policy gates, обратной связи владельца и жизненного цикла run/session.
- Не засорять БД сырыми логами, стенограммой сессии, входом/выходом инструмента и секретами.
- Не привязать provider-hub, package-hub и runtime-manager к частной реализации Codex так, чтобы потом нельзя было поддержать другую агентную среду исполнения.
- Заранее заложить управление skills через роли, flow/stage, рабочее пространство, права доступа и UI.

## Нецели

- Не реализовывать hooks, skills, MCP tools, `proto` или `AsyncAPI` в этом документе.
- Не выбирать окончательный формат манифеста для skills.
- Не переносить текущие руководящие пакеты документации в skills.
- Не делать provider write operations, bootstrap или adoption через hooks.

## Термины

| Термин | Смысл |
|---|---|
| `Codex hooks` | Жизненные события Codex, на которые можно повесить командные обработчики. |
| `Codex skills` | Пакеты повторяемых инструкций, ресурсов и опциональных скриптов, которые Codex подгружает по необходимости. |
| Hook emitter | Локальный обработчик hook-событий в рабочем пространстве slot, который нормализует событие и отправляет его в платформу. |
| Управляемая возможность | Skill, hook-политика, MCP tool, внешний пакет или встроенный платформенный сценарий агента. |

## Общая модель hooks

Hooks не должны становиться источником истины для доменных данных. Они являются входным сигналом от среды исполнения Codex, который проходит через тонкий слой нормализации и дальше маршрутизируется в сервис-владелец.

Целевая цепочка:

1. Среда исполнения Codex вызывает hook-обработчик внутри рабочего пространства slot.
2. Hook emitter нормализует событие: `run_id`, `session_id`, `slot_id`, `turn_id`, `hook_event_name`, категория инструмента, короткая безопасная сводка, correlation id.
3. Hook emitter отправляет событие в `platform-mcp-server` или локальный агентный sidecar, если прямой MCP-вызов недоступен.
4. `platform-mcp-server` проверяет источник, отбрасывает запрещённые поля и маршрутизирует событие в сервис-владелец.
5. Сервис-владелец фиксирует только доменное состояние, которое ему принадлежит.

### Разбор событий

| Hook | Платформенный смысл | Основные получатели | Класс события | Что хранить |
|---|---|---|---|---|
| `SessionStart` | Старт или resume Codex-сессии внутри slot. | `agent-manager`, `runtime-manager` | Управление, аудит | В БД: связь session/run/slot, источник старта, время, версия модели, workspace ref. Не хранить стенограмму сессии. |
| `UserPromptSubmit` | Новый prompt, который может быть проверен на секреты, политику и контекст задачи. | `agent-manager`, `interaction-hub` | Управление, аудит | В БД: факт prompt submit, hash, короткая сводка, решение политики. Полный prompt хранить только в контуре диалога, если это пользовательская переписка. |
| `PreToolUse` | Предварительная проверка инструмента: shell, `apply_patch`, MCP tool. | `platform-mcp-server`, `agent-manager`, при необходимости `runtime-manager` | Управление, диагностика | В БД: только deny/ask/risk decision и безопасная сводка. Массовые allow-события держать как короткие события или метрики. |
| `PermissionRequest` | Запрос разрешения Codex на действие с повышенным риском. | `agent-manager`, `interaction-hub`, `platform-mcp-server` | Управление, аудит | В БД: request id, decision id, субъект, действие, риск, gate ref, sanitized reason, решение и время. |
| `PostToolUse` | Результат инструмента после выполнения; полезен для provider signals и диагностики. | `provider-hub`, `runtime-manager`, `agent-manager` | Диагностика, аудит для рискованных действий | В БД: только важные итоги, exit status, bounded error, provider artifact signal. Полный stdout/stderr не хранить. |
| `PreCompact` | Контрольная точка перед сжатием контекста. | `agent-manager`, `runtime-manager` | Управление, диагностика | В БД: метаданные snapshot, trigger `manual/auto`, object ref, hash. Полное состояние сессии — в объектное хранилище. |
| `PostCompact` | Контрольная точка после сжатия контекста. | `agent-manager`, `runtime-manager` | Управление, диагностика | В БД: новые метаданные snapshot, краткая сводка сжатия, token metrics. Полную стенограмму сессии не хранить. |
| `Stop` | Завершение хода агента; возможность зафиксировать итог и pending actions. | `agent-manager`, `runtime-manager`, `provider-hub`, `interaction-hub` | Управление, аудит | В БД: контрольная точка run, итоговый status, pending gates, provider signals, короткая сводка. |

### Маршрутизация по доменам

| Получатель | Что получает | Что не получает |
|---|---|---|
| `agent-manager` | Жизненный цикл run/session, policy gate refs, request/decision, контрольные точки сжатия контекста, stop summary. | Сырые tool outputs, секреты, полные стенограммы сессий. |
| `runtime-manager` | Slot/session binding, диагностика рабочего пространства, snapshot object refs, короткий хвост ошибок среды исполнения. | Provider payload и решения бизнес-политик. |
| `platform-mcp-server` | Нормализованные hook calls для проверки источника, минимальной policy pre-check и маршрутизации. | Долгое хранение состояния и доменную бизнес-логику. |
| `interaction-hub` | Permission request, запрос обратной связи владельца, human gate prompt, notification intent. | Технические tool logs и provider payload. |
| `provider-hub` | Сигналы об изменённых provider artifacts, rate-limit hints, reconciliation hot cursor. | Сырые токены, значения секретов, полный stdout `gh`. |

### Политика хранения

- В Postgres хранить только состояние владельца: request/decision, контрольную точку жизненного цикла, метаданные snapshot, operation refs, provider signal, bounded error.
- Полную стенограмму сессии, session JSON/JSONL, большие tool outputs и raw logs хранить вне Postgres с retention и ссылкой из сервиса-владельца.
- Высокочастотные allow-события `PreToolUse` и успешные `PostToolUse` без доменного эффекта не писать в БД построчно.
- Для аудита хранить who/what/when/decision/correlation, но не raw input/output.
- Для диагностики хранить короткий bounded tail, hash и object ref.

### Защита от секретов и шума

- Hook emitter обязан удалять значения env, токены, authorization headers, строки, похожие на секреты, большие stdout/stderr и бинарные данные.
- Нельзя отправлять `tool_input` и `tool_response` целиком в платформу по умолчанию.
- Для shell-команд хранить только нормализованную категорию, command hash, bounded sanitized preview и exit status.
- Для provider-операций хранить provider, repository, artifact type, artifact id/number, command id и correlation id.
- Для prompt хранить полный текст только там, где это является пользовательским диалогом и имеет отдельную retention-политику.

### Permission requests, обратная связь владельца и policy gates

`PermissionRequest` должен маппиться не на локальный yes/no без следа, а на доменный gate:

1. Hook emitter отправляет запрос в `platform-mcp-server`.
2. `platform-mcp-server` определяет actor, run, role, stage, project, repository, tool category.
3. `agent-manager` создаёт или находит pending gate.
4. `interaction-hub` доставляет запрос обратной связи владельца в UI или внешний адаптер.
5. После решения `agent-manager` фиксирует decision и возвращает allow/deny/ask в hook handler.

Если решение не пришло за timeout, действие должно завершиться безопасной ошибкой или перейти в ожидание, но не продолжаться молча.

## Общая модель skills

Skills не являются заменой руководящих пакетов документации. Руководящий пакет отвечает на вопрос “какие правила и знания использовать”, а skill отвечает на вопрос “какой повторяемый workflow выполнить и какие локальные ресурсы/скрипты для него доступны”.

| Тип | Назначение | Где живёт | Кто управляет |
|---|---|---|---|
| Встроенные платформенные skills | Повторяемые платформенные сценарии: ревью, релизный чек, пакетная проверка, работа с provider artifacts. | Платформенный пакет или системный слой Codex runtime. | Платформа. |
| Пользовательские skills | Сценарии организации, команды или проекта. | Репозиторий организации, project docs repo или package source. | Организация или проект. |
| Skills из пакетов/магазина | Переиспользуемые skills, поставляемые через каталог пакетов. | Package source repository. | Автор пакета, package-hub проверяет манифест и установку. |

### Связь skills с ролями, flow и workspace

- Роль агента может иметь список разрешённых, обязательных и запрещённых skills.
- Stage может добавлять skills, нужные только для конкретного этапа.
- Flow может фиксировать версии skills через package installation refs.
- Рабочее пространство должно получать только те skills, которые разрешены для run, роли, stage, проекта и организации.
- Права доступа skill не должны расширять права роли. Skill может требовать MCP tools или локальные скрипты, но политика должна разрешить их отдельно.
- Версия skill должна попадать в метаданные run, чтобы результат был воспроизводимым.

### Отличие от руководящих пакетов документации

| Руководящий пакет | Skill |
|---|---|
| Содержит правила, стандарты, шаблоны и справочные материалы. | Содержит повторяемый workflow, инструкции запуска, опциональные scripts/references/assets. |
| Не должен выполнять код как часть своего назначения. | Может включать скрипты, поэтому требует явной policy и sandbox. |
| Подключается как знания для агента. | Подключается как управляемая возможность, которую Codex может выбрать явно или неявно. |
| Может быть обязательным baseline для проекта или домена. | Должен быть включён для роли/stage/workspace явно или через пакетную установку. |

### Будущий UI

UI должен показывать:

- каталог доступных skills и источники: встроенный, пользовательский, пакетный;
- версию, digest, автора, организацию, область установки;
- где skill включён: организация, проект, flow, stage, role;
- какие MCP tools, scripts и внешние доступы требуются;
- режим invocation: явный, неявный, запрещённый;
- audit установки, обновления, включения и отключения;
- предупреждения о skills, которые не попали в стартовый список Codex из-за бюджета контекста.

## Варианты архитектуры

### Вариант 1. Минимальный слой hooks, skills после MVP

Суть: до MVP реализовать только нормализованный канал hook-событий из slot в `platform-mcp-server` и `agent-manager`. Skills пока не становятся доменной сущностью; их можно использовать вручную в рабочем пространстве или в системной настройке среды исполнения Codex.

Плюсы:

- Минимальный объём до MVP.
- Быстро закрывает связь slot-агента с платформой.
- Не создаёт новый сервис и не расширяет package-hub раньше времени.
- Хорошо ложится на текущую модель `agent-manager` + `runtime-manager` + `platform-mcp-server`.

Минусы:

- Нет полноценного UI управления skills.
- Пользовательские и пакетные skills придётся формализовать позже.
- Возможна временная разница между тем, что Codex видит локально, и тем, что платформа считает разрешённым.

Риски:

- Если hook emitter сделать слишком “толстым”, он начнёт дублировать доменную логику.
- Если писать все hook-события в БД, быстро появится шум и рост хранилища.

MVP-объём:

- Hook envelope.
- Очистка чувствительных данных.
- `PermissionRequest` через `agent-manager` и `interaction-hub`.
- `PostToolUse` provider artifact signal.
- `PreCompact`/`PostCompact` snapshot metadata.
- `Stop` run checkpoint.

Влияние на домены:

- `agent-manager`: жизненный цикл, gates, контрольные точки.
- `runtime-manager`: slot/session diagnostics и object refs.
- `provider-hub`: hot cursor по provider artifacts.
- `interaction-hub`: обратная связь владельца и notifications.
- `package-hub`: без изменений до следующего этапа.

### Вариант 2. Пакетная модель skills, hooks как часть политики agent-manager

Суть: skills становятся package kind или управляемой возможностью внутри package-hub. Hooks остаются слоем жизненного цикла slot-runtime, но их политика и привязка задаются через `agent-manager`.

Плюсы:

- Skills получают версионирование, установку, каталог, права и будущий UI через уже нужный package контур.
- Можно включать skills на уровне организации, проекта, flow, stage и role.
- Хорошо подходит для платных и бесплатных пакетов из магазина.

Минусы:

- Package-hub получает дополнительную модель управляемых возможностей и должен не смешать guidance packages со skills.
- Runtime-manager должен уметь материализовать skills в рабочее пространство.
- Agent-manager должен фиксировать версии skills в метаданных run.

Риски:

- Появится соблазн выполнять scripts из skills без достаточной policy.
- Если skill package смешать с plugin package, будет неясно, что запускается в Kubernetes, а что просто кладётся в workspace Codex.

MVP-объём:

- До MVP можно только заложить документальные ограничения.
- Реализация package kind или управляемой возможности для skills лучше после MVP или на границе MVP, если UI и package-hub уже готовы.

Влияние на домены:

- `package-hub`: манифест, install, version, digest, skill capability.
- `agent-manager`: привязка skill refs к role/stage/run.
- `runtime-manager`: materialization в рабочее пространство.
- `platform-mcp-server`: проверка разрешённых tools для skill.

### Вариант 3. Отдельный слой управляемых возможностей

Суть: создать отдельный слой управляемых возможностей, который владеет hooks, skills, MCP tools, runtime tool policy и UI-каталогом возможностей агента.

Плюсы:

- Самая чистая модель для нескольких агентных сред исполнения, не только Codex.
- Удобно строить сложные enterprise policy и UI.
- Меньше давления на package-hub и agent-manager.

Минусы:

- Новый домен и сервис до доказанной необходимости.
- Высокий объём проектирования и реализации.
- Может заблокировать MVP.

Риски:

- Дублирование политики между access-manager, agent-manager и слоем управляемых возможностей.
- Слишком ранняя абстракция без рабочих сценариев.

MVP-объём:

- Не рекомендуется для MVP.
- Можно оставить как направление после MVP, если пакетная модель станет тесной.

Влияние на домены:

- Потребуется новая граница сервиса.
- `agent-manager` и `runtime-manager` станут потребителями слоя управляемых возможностей.
- `package-hub` останется поставщиком пакетов, но не владельцем привязок управляемых возможностей.

## Рекомендация

До MVP выбрать вариант 1 как обязательный минимум и не реализовывать полноценное управление skills. При этом в документации и будущих контрактах оставить задел под вариант 2.

Практическая линия:

1. До MVP: hook emitter в рабочем пространстве slot, нормализованный event envelope, `PermissionRequest`, compact checkpoints, `Stop`, provider artifact signals.
2. До MVP: skills не становятся отдельной сущностью БД; допускаются только встроенные или вручную поставляемые skills как часть контролируемого образа среды исполнения.
3. На границе MVP: выбрать, делать ли skills как package kind или как управляемую возможность внутри манифеста пакета.
4. После MVP: UI управления skills, установка из магазина, пользовательские skills, политика по role/stage/workspace.
5. Отдельный слой управляемых возможностей рассматривать только после появления двух и более сред исполнения или сложной enterprise policy, которую нельзя выразить через `agent-manager` + `package-hub`.

## Какие документы обновить после выбора

- `docs/domains/agent-orchestration/product/requirements.md`
- `docs/domains/agent-orchestration/architecture/design.md`
- `docs/domains/agent-orchestration/architecture/data_model.md`
- `docs/domains/agent-orchestration/architecture/api_contract.md`
- `docs/domains/runtime-and-fleet/architecture/design.md`
- `docs/domains/runtime-and-fleet/architecture/data_model.md`
- `docs/domains/package-platform/product/requirements.md`
- `docs/domains/package-platform/architecture/design.md`
- `docs/domains/provider-native-work-items/architecture/design.md`
- `docs/platform/architecture/mcp_and_interaction_model.md`
- `docs/platform/architecture/service_boundaries.md`
- UI-документацию Mission Control после появления раздела управления skills.

## Открытые вопросы владельцу

1. Подтвердить, что hooks входят в MVP как канал связи slot-агента с платформой.
2. Подтвердить, что skills не входят в MVP как полноценная платформа управления.
3. Выбрать будущую модель skills: package kind, управляемая возможность внутри манифеста пакета или отдельный слой управляемых возможностей.
4. Решить, нужна ли платформа для миграции существующих локальных Codex skills в управляемые package sources.
