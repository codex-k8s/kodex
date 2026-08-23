---
id: RUN-MC-007
title: Диагностика control-plane
type: runbook
status: approved
owner: sre
version: 2.0.1
updated: 2026-08-23
---

# Диагностика control-plane

## Локальные probes

`/healthz` подтверждает жизнь процесса. `/readyz` читает локальный snapshot
PostgreSQL, NATS publisher/outbox и workload-local authority. Gateway, runtime,
integration и interaction services не вызываются из probe.

Если сосед недоступен, его рабочий RPC получает `Unavailable`; control-plane не
объявляется неготовым из-за отсутствия optional или downstream consumer.

## Fresh schema

Единственная migration:
`services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql`.

Migration Job вызывает `control-plane-cli up` и читает только
`CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE`. `control-plane-cli status` выполняет
readback. Старые migration numbers, legacy source DSN, backfill/cutover и schema
down не используются.

## Bootstrap

Проверить через owner API/audit, не прямым изменением SQL:

- одну Organization и initial owner claim contract;
- один Agent со stable key `system-assistant`;
- protected core prompt и owner supplement;
- built-in capabilities, integration definitions и runtime defaults;
- desired/observed warm assistant revision;
- идемпотентный повтор bootstrap без duplicate.

## Run/event path

Для incident зафиксировать безопасные opaque refs и correlation ID. Проверить:

1. state/attempt/version из authoritative Run detail;
2. graph snapshot sequence;
3. ordered `RunEvent` без gap или conflicting duplicate;
4. outbox publication receipt и NATS subject;
5. runtime claim/fence и terminal/cancel readback;
6. audit/idempotency receipt без raw payload.

Human Gate разрешается специализированной командой с OCC. Повтор exact intent
возвращает receipt, stale version — conflict/winner readback и не создаёт вторую
continuation.

## Запрещено

- исправлять lifecycle прямым SQL;
- принимать actor/project/root lineage из request payload;
- вручную переоткрывать terminal Run или published version;
- удалять outbox/event row для разблокировки;
- выводить prompt, artifact content, provider response или secret material.
