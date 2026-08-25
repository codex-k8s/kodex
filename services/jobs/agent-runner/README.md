---
id: SVC-MC-017
title: agent-runner
type: service
status: approved
owner: developer
version: 2.0.0
updated: 2026-08-23
---

# agent-runner

`agent-runner` — защищённый runtime ABI внутри каждого promoted role image. Это
не legacy service и не общий long-lived bot. Для обычного turn отдельный runner
стартует внутри нового execution-scoped Pod этой роли; системный помощник имеет
отдельный warm режим.

## Граница ответственности

Runner:

- читает и валидирует immutable `kodex.agent-runner-input.v4`;
- подтверждает exact turn/attempt/fence через runtime-controller callback;
- материализует bounded instructions, files, provider binding и MCP config;
- запускает provider runtime прямым `exec.CommandContext` без shell workflow;
- передаёт coalesced safe progress;
- завершает child processes и формирует signed bounded terminal handoff.

Runner не создаёт Project/Agent/Run, не вычисляет graph lifecycle, не принимает
actor/owner/lineage из prompt, не читает PostgreSQL и не обращается к Kubernetes
API. У него нет Mattermost token, registry writer/admin или managed integration
credentials. External channel delivery выполняет optional interaction adapter
после core terminal transaction.

## Role image

Role image содержит собственное окружение роли: OS packages, языки, CLI,
браузеры, OCR/office tools либо другое ПО. После недоверенного installation
step supply chain добавляет trusted `kodex-init` и
`kodex-agent-runner`, фиксирует runtime ABI и допускает exact digest.

Таким образом, образ юридического сотрудника может содержать PDF/OCR, образ
аналитика — Python/R, а образ разработчика — compiler/Git tools. Выбор display
role name не выдаёт инструмент; authority определяется RuntimeRevision,
capabilities и grants.

## MCP

MCP остаётся runtime-протоколом. Каждый server/tool имеет стабильную
типизированную schema, timeout, required flag и allowlist. Config генерируется
из server-owned RuntimeRevision; secret values в TOML не записываются.

- platform MCP: `delegate_agent`, `invoke_integration` по exact grants и
  `propose_configuration_plan` только для системного помощника;
- integration MCP: специализированные adapters `integration-gateway`;
- generic external API proxy и произвольная command authority запрещены.

Terminal completion, child callback и публикация bounded artifacts идут через
защищённый runner callback contract. Продолжение Session создаёт владелец
состояния через специализированный owner API, а Human Gate материализуется из
опубликованной WorkflowVersion; эти действия не объявляются отдельными MCP
tools.

Долговечное ожидание Human Gate не удерживает Pod. Control-plane фиксирует
`WAITING_HUMAN`, закрывает текущую attempt и после решения создаёт новую
RuntimeRevision/Pod. MCP timeout не используется как многодневный wait.

## Warm mode

Warm runner системного помощника стартует provider session заранее и получает
server-owned turns последовательно через защищённый callback. Idle loop не
считается Turn. При revision mismatch, stale ticket или callback loss runtime
закрыто отклоняет работу; controller восстанавливает desired Pod.

## Безопасность вывода

Raw provider JSONL, stdout/stderr, arbitrary tool payload, prompts и secret
values не публикуются в logs, NATS или WebSocket. Runtime возвращает stable
safe code/message key и bounded status. Пользовательский текст локализуется по
проверенной locale из YAML i18n, а runtime diagnostics остаются на английском.

## Локальная проверка

```bash
cd services/jobs/agent-runner
GOWORK=off go test ./...
```

Schema: `contracts/runtime-controller/v4/agent-runner-input.schema.json`.
Supply chain: `docs/domains/images-supply-chain.md`.
