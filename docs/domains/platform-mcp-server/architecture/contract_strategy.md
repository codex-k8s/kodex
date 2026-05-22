---
doc_id: ARC-CK8S-PLATFORM-MCP-CONTRACT-STRATEGY-0001
type: design-doc
title: kodex — стратегия контрактов MCP и Codex hooks
status: active
owner_role: SA
created_at: 2026-05-15
updated_at: 2026-05-22
related_issues: [753, 698, 778, 322]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-15-mcp-hooks-contract-strategy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-15
---

# Стратегия контрактов MCP и Codex hooks

## Кратко

`platform-mcp-server` реализует только MCP-поверхность: `tools/list`, `tools/call`, схемы входа инструментов и безопасный результат вызова. Codex hooks идут в отдельный входной контур `codex-hook-ingress`, потому что Codex запускает hooks как command-обработчики, а не как MCP-вызовы или HTTP callbacks.

YAML-файл каталога инструментов не является каноническим контрактом. Для MCP каноникой становятся Go-регистрация инструментов через официальный MCP SDK, JSON Schema входов и snapshot-проверки `tools/list`. Для hook ingress каноникой становятся схемы нормализованных hook-событий и будущий транспортный контракт `codex-hook-ingress`.

## Основание

Первичные источники:

- OpenAI Codex hooks: <https://developers.openai.com/codex/hooks>
- OpenAI Codex configuration reference: <https://developers.openai.com/codex/config-reference>
- Model Context Protocol specification: <https://github.com/modelcontextprotocol/modelcontextprotocol>
- Model Context Protocol Go SDK: <https://github.com/modelcontextprotocol/go-sdk>

Из них следуют два разных механизма:

- MCP server раскрывает инструменты через протокол MCP. Клиент узнаёт инструменты через `tools/list`, вызывает через `tools/call`, а схема входа задаётся JSON Schema.
- Codex hooks настраиваются в `hooks.json`, `config.toml` или управляемых requirements. Codex запускает command hook в рабочей директории сессии, передаёт JSON на `stdin` и читает результат через `stdout`, `stderr` и exit code.

## Целевые границы

| Компонент | Владеет | Не владеет |
|---|---|---|
| `platform-mcp-server` | MCP transport, регистрация MCP tools, `tools/list`, `tools/call`, проверка источника MCP-клиента, маршрутизация вызова к сервису-владельцу. | Codex hook events, сырые hook payload, агентные запуски, provider write pipeline, бизнес-состояние сервисов-владельцев. |
| `codex-hook-ingress` | Приём нормализованных Codex hook events от hook emitter или локального sidecar, проверка источника slot/run/session, очистка данных, маршрутизация события владельцу. | MCP tools, `tools/list`, `tools/call`, бизнес-состояние `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager` или `interaction-hub`. |
| Hook emitter или sidecar | Локальный command hook для Codex, нормализация входа Codex, отправка события в `codex-hook-ingress`, локальный буфер и повтор при временной недоступности платформы. | Доменная политика, хранение секретов, принятие бизнес-решений. |

## MCP-контракты

Канонический путь для MCP:

1. Инструмент описывается Go-типами входа и выхода.
2. `platform-mcp-server` регистрирует инструмент через официальный MCP Go SDK.
3. JSON Schema входа формируется из Go-типов или задаётся явно в MCP tool definition.
4. `tools/list` становится машинно-читаемой поверхностью discovery для клиента.
5. Snapshot-тест фиксирует список инструментов, версии, описания и входные схемы.
6. Совместимость проверяется snapshot-тестами и тестами `tools/call` на безопасные ответы и ошибки.

MCP-инструмент не должен получать произвольный raw JSON без схемы, кроме явно ограниченной диагностики. Вызов маршрутизируется к сервису-владельцу по gRPC или другому внутреннему контракту владельца.

## Hook-контракты

Канонический путь для Codex hooks:

1. Codex запускает command hook из `hooks.json`, `config.toml` или managed requirements.
2. Hook command получает исходный Codex JSON на `stdin`.
3. Hook emitter или sidecar валидирует event name, очищает вход, добавляет run/session/slot/project context и отправляет нормализованное событие в `codex-hook-ingress`.
4. `codex-hook-ingress` проверяет источник, ограничения размера, redaction и route policy.
5. Событие маршрутизируется владельцу: `agent-manager`, `runtime-manager`, `provider-hub`, `governance-manager` или `interaction-hub`.

Поддерживаемый MVP-набор Codex hook events:

| Событие | Назначение |
|---|---|
| `SessionStart` | Старт или resume сессии. |
| `UserPromptSubmit` | Факт отправки пользовательского prompt. |
| `PreToolUse` | Намерение вызвать поддерживаемый tool. |
| `PermissionRequest` | Запрос разрешения Codex. |
| `PostToolUse` | Итог поддерживаемого tool. |
| `Stop` | Завершение хода и финальная контрольная точка. |

`PreCompact` и `PostCompact` не входят в текущий набор Codex hooks. Контрольные точки сжатия контекста и session snapshot проектируются как внутренние события `agent-manager`/`runtime-manager`.

## Почему не YAML как контракт

YAML-каталог инструментов не используется как стандарт MCP и создаёт риск расхождения:

- MCP уже имеет discovery через `tools/list`.
- Go SDK регистрирует инструменты в коде и может формировать JSON Schema.
- Hook events имеют собственную схему Codex и не являются MCP tools.
- Один YAML начал смешивать MCP tools, Codex hooks и внутренние события.

Поэтому YAML можно использовать только как временную пояснительную матрицу, если она не объявляется источником правды. В целевой реализации первичными становятся:

- Go tool definitions и JSON Schema для MCP;
- JSON Schema и validation examples для нормализованного hook ingress: `specs/jsonschema/codex-hook-ingress.v1/**`;
- OpenAPI или gRPC-контракт `codex-hook-ingress`, когда будет выбран транспорт;
- snapshot-тест `tools/list` для защиты от случайного изменения MCP-поверхности.

## Решение для реализации

- `platform-mcp-server` и `codex-hook-ingress` реализуются раздельно.
- Сервисный каркас `platform-mcp-server` создаётся с MCP SDK и без входного контура hooks.
- Hook emitter, локальный sidecar и приём нормализованных Codex hook events относятся к `codex-hook-ingress`.
- Реализация входного контура hooks не входит в MCP-сервер и не должна добавлять hook transport в `platform-mcp-server`.

## Апрув

- request_id: `owner-2026-05-15-mcp-hooks-contract-strategy`
- Решение: approved
- Комментарий: стратегия разделяет MCP-поверхность и входной контур Codex hooks; YAML не является каноническим контрактом.
