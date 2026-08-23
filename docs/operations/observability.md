---
id: OPS-MC-004
title: Наблюдаемость
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Наблюдаемость

## Сигналы

- OpenTelemetry traces и структурированные logs связывают request, Run, node,
  turn, attempt и external delivery через безопасные correlation IDs;
- Prometheus labels используют только закрытые множества и не содержат UUID,
  project names, prompt, provider payload или secret metadata;
- RunEvent и audit являются доменными источниками, а не заменяются logs;
- каждый alert содержит абсолютный HTTPS `runbook_url`.

Основные панели: owner API, PostgreSQL/outbox, NATS/inbox/cursor, run queues,
runtime Pod/claims, system assistant warm state, role image supply chain,
integration effects/grants, optional deliveries, artifacts и schedules.

## Health и readiness

- `/healthz` проверяет только жизнь текущего процесса;
- `/readyz` читает локальный уже рассчитанный snapshot и не вызывает сеть на
  каждую probe;
- readiness зависит только от процесса, sidecar и прямой инфраструктуры;
- service-to-service availability проверяет отдельный diagnostic/smoke path;
- деградация логируется один раз при переходе состояния, восстановление — один
  раз, без повторяющегося warning на каждом интервале.

JWKS/control-plane metadata допускают last-known-good максимум две минуты без
продления при повторных ошибках. Новые tokens ограничены остатком окна.
Integrity failure, rollback, conflicting revision, expired key или grace period
немедленно закрывают boundary.

## Конфиденциальность

Raw stdout/stderr, Codex JSONL, provider responses, prompts, file content,
credentials и произвольные MCP payload не экспортируются. Внешняя ошибка
нормализуется в stable message key; пользовательский текст локализуется в PWA
или доверенном interaction adapter по проверенной locale.
