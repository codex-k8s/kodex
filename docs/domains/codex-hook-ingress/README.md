# codex-hook-ingress

## Назначение

`codex-hook-ingress` — сервисный пакет входного контура Codex hooks. Он нужен, чтобы не смешивать MCP-протокол и lifecycle-события Codex.

Codex hooks являются command-обработчиками Codex: hook emitter или локальный sidecar получает JSON от Codex на `stdin`, очищает его, добавляет платформенный контекст и отправляет нормализованное событие в `codex-hook-ingress`.

## Что входит

- Приём нормализованных Codex hook events от hook emitter или локального sidecar.
- Проверка actor/source/run/session/slot binding.
- Очистка входа, ограничение размера и запрет сырых секретов, больших stdout/stderr и полных session dumps.
- Маршрутизация событий к сервисам-владельцам: `agent-manager`, `runtime-manager`, `provider-hub`, `interaction-hub`.
- Короткая операционная лента для realtime UI и метрики срабатываний hooks.

## Что не входит

- MCP tools, `tools/list`, `tools/call` и MCP transport — зона `platform-mcp-server`.
- `Run`, session, flow, gate и acceptance — зона `agent-manager`.
- Slot, workspace и platform jobs — зона `runtime-manager`.
- Provider write/read pipeline — зона `provider-hub`.
- Доставка уведомлений и диалоги — зона `interaction-hub`.

## MVP hook events

| Событие | Назначение |
|---|---|
| `SessionStart` | Старт или resume сессии. |
| `UserPromptSubmit` | Факт отправки пользовательского prompt. |
| `PreToolUse` | Намерение вызвать поддерживаемый tool. |
| `PermissionRequest` | Запрос разрешения Codex. |
| `PostToolUse` | Итог поддерживаемого tool. |
| `Stop` | Завершение хода и финальная контрольная точка. |

`PreCompact` и `PostCompact` не входят в текущий набор Codex hooks. Контрольные точки сжатия контекста проектируются как внутренние события `agent-manager`/`runtime-manager`.

## Связанные документы

| Документ | Путь |
|---|---|
| Стратегия контрактов MCP и Codex hooks | `docs/domains/platform-mcp-server/architecture/contract_strategy.md` |
| Codex hooks и skills | `docs/platform/architecture/codex_hooks_and_skills.md` |
| Карта Issue | `docs/delivery/issue-map/domains/codex-hook-ingress.md` |
