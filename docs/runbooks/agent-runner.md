---
id: RUNBOOK-MC-AGENT-RUNNER
title: Диагностика agent-runner
type: runbook
status: approved
owner: sre
version: 2.0.0
updated: 2026-08-23
---

# Диагностика agent-runner

Runner работает внутри promoted role image конкретной attempt. Он не является
отдельным Mattermost bot и не обслуживает следующую обычную attempt после
terminal state.

## Startup

Проверить:

1. input schema `mattercodex.agent-runner-input.v4` и bounded file mode/size;
2. exact execution/revision/turn/attempt/fence;
3. trusted runtime ABI digest;
4. runtime-controller callback TLS/SPIFFE/ticket;
5. materialized instructions/artifacts с digest verification;
6. exact provider binding и generated MCP config;
7. provider process UID без Kubernetes token/authority material.

Runner не использует shell orchestration. Provider/CLI запускаются прямым
`exec.CommandContext` с typed arguments и явным environment.

## MCP

Каждый required server проходит тот же рабочий initialize path. Недоступный MCP
возвращает safe typed failure/`Unavailable`; fallback на universal proxy,
непроверенный endpoint или direct secret запрещён. Human Gate завершает текущую
attempt в долговечное ожидание, а после решения запускается новая attempt.

## Terminal handoff

Runner останавливает child processes, закрывает progress, формирует bounded
result/artifact manifest и подписывает handoff. Raw stdout/stderr, JSONL,
provider response, prompt, arbitrary tool payload и secret values не входят в
handoff, logs или events. Runtime-controller удаляет Pod только после
authoritative owner readback.

## Warm assistant

Warm mode ждёт server-owned FIFO turns через callback. Idle не является Turn.
Stale revision/ticket отклоняются; controller пересоздаёт Pod. System assistant
не получает прямой DB/Kubernetes/secret storage access и выполняет configuration
actions только через typed platform MCP commands с полномочиями пользователя.

## Локальная проверка

```bash
cd services/jobs/agent-runner
GOWORK=off go test ./...
```
