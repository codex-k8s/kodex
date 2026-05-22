# platform-mcp-server

## Назначение

`platform-mcp-server` описывает платформенную MCP-поверхность для быстрого `agent-manager`, агентов в слотах и будущих управляемых интеграций.

Это не домен-владелец бизнес-состояния. Пакет размещён в `docs/domains/**`, потому что у сервиса есть самостоятельная граница, контракты, поставка и эксплуатация, но сквозная архитектура продолжает считать его пограничным компонентом.

## Что входит

- MCP-поверхность инструментов платформы.
- Нормализация MCP-вызовов инструментов.
- Проверка источника вызова: actor, source, run, session и slot.
- Минимальная проверка политики доступа к инструменту.
- Маршрутизация к сервисам-владельцам по внутренним контрактам.
- Безопасные ответы без секретов, сырых данных вызова и больших логов.
- Ограниченная диагностика состояния платформы.
- Идемпотентность, correlation id, rate limits и backpressure на границе MCP.

## Что не входит

- `Run`, session, flow, stage, role, prompt и состояние ожидания flow — зона `agent-manager`.
- Risk classification, review gates, policy-based approvals, gate decision и release decision — зона `governance-manager`.
- Slot, workspace, platform job, cleanup и prewarm — зона `runtime-manager`.
- Серверы, Kubernetes-кластеры, health и placement — зона `fleet-manager`.
- `Issue`, `PR/MR`, комментарии, связи, операции GitHub/GitLab и сверка — зона `provider-hub`.
- Проекты, репозитории, `services.yaml` и workspace policy — зона `project-catalog`.
- Пакеты, manifest, установки и каталоги — зона `package-hub`.
- Диалоги, уведомления, внешние каналы, доставка запросов владельцу и callbacks — зона `interaction-hub`.
- Codex hook events, hook emitter и локальный sidecar — зона `codex-hook-ingress`.
- Пользовательская HTTP-поверхность, `staff-gateway`, `user-gateway` и `integration-gateway`.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| API-обзор | `architecture/api_contract.md` |
| Стратегия контрактов MCP и Codex hooks | `architecture/contract_strategy.md` |
| План поставки | `delivery/platform_mcp_server_delivery.md` |

## Карта Issue

- Карта сервисного пакета: `docs/delivery/issue-map/domains/platform-mcp-server.md`.
