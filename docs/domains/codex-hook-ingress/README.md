# codex-hook-ingress

## Назначение

`codex-hook-ingress` — сервисный пакет входного контура Codex hooks. Он нужен, чтобы не смешивать MCP-протокол и lifecycle-события Codex.

Codex hooks являются command-обработчиками Codex: hook emitter или локальный sidecar получает JSON от Codex на `stdin`, очищает его, добавляет платформенный контекст и отправляет нормализованное событие в `codex-hook-ingress`.

## Что входит

- Приём нормализованных Codex hook events от hook emitter или локального sidecar.
- Проверка actor/source/run/session/slot binding.
- Очистка входа, ограничение размера и запрет сырых секретов, больших stdout/stderr и полных session dumps.
- Маршрутизация событий к сервисам-владельцам: `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager`, `interaction-hub`.
- Короткая операционная лента для realtime UI и метрики срабатываний hooks.
- Передача только ссылок на выбранный capability context и skill refs, если они уже выбраны `agent-manager` и материализованы `runtime-manager`.
- Маршрутизация sanitized `PreToolUse`/`PostToolUse` в `agent-manager` для persistent activity timeline, когда owner-side contract подключён.

## Состояние реализации

Сервисный каркас расположен в `services/internal/codex-hook-ingress`.

Текущий срез реализует process, config, graceful shutdown, `/health/livez`, `/health/readyz`, `/metrics`, in-process logical boundary `SubmitHookEvent`, source binding placeholder, schema validation hook, sanitizer boundary, idempotency repository stub, route registry для dispatch безопасных частей событий через owner ports/stubs и bounded in-memory ops/realtime feed для operator diagnostics. Физический transport для `SubmitHookEvent` не выбран и не реализован; соседние домены получают только safe projections без raw payload и без бизнес-команд.

## Что не входит

- MCP tools, `tools/list`, `tools/call` и MCP transport — зона `platform-mcp-server`.
- `Run`, session, flow, ожидания flow, persistent tool/activity history и acceptance — зона `agent-manager`.
- Risk/gate request и decision state — зона `governance-manager`.
- Slot, workspace и platform jobs — зона `runtime-manager`.
- Provider write/read pipeline — зона `provider-hub`.
- Доставка уведомлений и диалоги — зона `interaction-hub`.
- Каталог skills, package manifest, installation truth и источники пакетов — зона `package-hub` и capability layer соседних сервисов.

## MVP hook events

| Событие | Назначение |
|---|---|
| `SessionStart` | Старт или resume сессии. |
| `UserPromptSubmit` | Факт отправки пользовательского prompt. |
| `PreToolUse` | Намерение вызвать поддерживаемый tool. |
| `PermissionRequest` | Запрос разрешения Codex. |
| `PostToolUse` | Итог поддерживаемого tool. |
| `Stop` | Завершение хода и финальная контрольная точка. |

`PreCompact` и `PostCompact` не входят в текущий платформенный MVP-набор Codex hooks. Контрольные точки сжатия контекста проектируются как внутренние события `agent-manager`/`runtime-manager`.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| Модель данных и состояния | `architecture/data_model.md` |
| API-обзор | `architecture/api_contract.md` |
| Контракт hook emitter/local sidecar | `architecture/emitter_sidecar_contract.md` |
| План поставки | `delivery/codex_hook_ingress_delivery.md` |
| JSON Schema CHI-1/CHI-2 | `../../../specs/jsonschema/codex-hook-ingress.v1/` |

## Связанные документы

| Документ | Путь |
|---|---|
| Требования `codex-hook-ingress` | `docs/domains/codex-hook-ingress/product/requirements.md` |
| Дизайн `codex-hook-ingress` | `docs/domains/codex-hook-ingress/architecture/design.md` |
| Модель данных и состояния `codex-hook-ingress` | `docs/domains/codex-hook-ingress/architecture/data_model.md` |
| API-обзор `codex-hook-ingress` | `docs/domains/codex-hook-ingress/architecture/api_contract.md` |
| Контракт hook emitter/local sidecar | `docs/domains/codex-hook-ingress/architecture/emitter_sidecar_contract.md` |
| Поставка `codex-hook-ingress` | `docs/domains/codex-hook-ingress/delivery/codex_hook_ingress_delivery.md` |
| Machine-readable schemas CHI-1/CHI-2 | `specs/jsonschema/codex-hook-ingress.v1/` |
| Стратегия контрактов MCP и Codex hooks | `docs/domains/platform-mcp-server/architecture/contract_strategy.md` |
| Codex hooks и skills | `docs/platform/architecture/codex_hooks_and_skills.md` |
| Карта Issue | `docs/delivery/issue-map/domains/codex-hook-ingress.md` |
