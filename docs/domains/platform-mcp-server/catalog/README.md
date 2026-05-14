---
doc_id: CAT-CK8S-PLATFORM-MCP-TOOLS-0001
type: catalog
title: kodex — каталог инструментов platform-mcp-server
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-05-14
related_issues: [753]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-platform-mcp-catalog"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-14
---

# Каталог инструментов platform-mcp-server

## TL;DR

Каталог `platform-mcp-server` живёт внутри доменного пакета сервиса, потому что описывает его внешнюю инструментальную поверхность, входной контур Codex hooks и внутренние синтетические события. Это не общий каталог пакетов, ролей или руководящей документации.

## Файлы

| Файл | Назначение |
|---|---|
| `tool_catalog.v1.yaml` | Машинно-читаемый каталог инструментов, событий, envelope, ошибок и правил версионирования. |
| `fixtures/valid_mcp_envelope.json` | Валидный общий MCP-envelope. |
| `fixtures/invalid_missing_source.json` | Невалидный envelope без source binding. |
| `fixtures/permission_request.json` | Пример `PermissionRequest` hook event. |
| `fixtures/post_tool_use_provider_signal.json` | Пример `PostToolUse` provider artifact signal. |
| `fixtures/project_runtime_package_read.json` | Пример безопасного read-инструмента. |
| `fixtures/policy_redaction_denied.json` | Пример отказа из-за policy/redaction. |
| `fixtures/internal_session_compact_checkpoint.json` | Пример внутреннего синтетического события для compact/session snapshot. |

## Правила

- Codex hooks в каталоге — только реальные события Codex: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `Stop`.
- `internal_session_events` — платформенные синтетические события для будущего lifecycle/snapshot/compact контура. Они не являются Codex hooks.
- Каталог разделяет три формы envelope: `mcp_call`, `hook_event` и `internal_event`; тестовые примеры должны соответствовать одной из этих форм.
- Любой инструмент маршрутизируется к сервису-владельцу и не делает `platform-mcp-server` владельцем бизнес-состояния.
- Каталог не заменяет будущие `proto`, `AsyncAPI` или Go-реализацию MCP transport.
