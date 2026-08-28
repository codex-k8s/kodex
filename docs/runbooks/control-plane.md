---
id: RUN-MC-007
title: Диагностика control-plane
type: runbook
status: approved
owner: sre
version: 2.1.0
updated: 2026-08-28
---

# Диагностика control-plane

## Локальные probes

`/healthz` подтверждает жизнь процесса. `/readyz` читает локальный snapshot
PostgreSQL, NATS publisher/outbox и workload-local authority. Gateway, runtime,
integration и interaction services не вызываются из probe.

Если сосед недоступен, его рабочий RPC получает `Unavailable`; control-plane не
объявляется неготовым из-за отсутствия optional или downstream consumer.

## Fresh schema

Fresh установка применяет baseline
`services/internal/control-plane/cmd/cli/migrations/20260822000100_web_first_baseline.sql`
и следующие versioned forward-only migrations по номеру. Контекстные планы и
Run activity добавляет
`services/internal/control-plane/cmd/cli/migrations/20260828099700_contextual_plans_and_run_activity.sql`.

Migration Job вызывает `control-plane-cli up` и читает только
`CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE`. `control-plane-cli status` выполняет
readback. Legacy source DSN, ручной backfill/cutover и schema down в рабочей
среде не используются.

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

## System assistant title и plans

Для жалобы на неверный title или план проверить через owner API и audit:

1. `titleSource` и `titleRevision`; `AGENT_PROPOSED` не должен заменять
   `USER_EDITED`;
2. contextual descriptor и server-resolved entity version/allowed operations;
3. plan `version`, `revision`, `validatedRevision`, state и content digest;
4. immutable revision readback до edit и после него;
5. apply/reject receipt, operation audit refs и отсутствие частичных ресурсов
   при `STALE`/`CONFLICT`;
6. idempotency receipt для exact retry и отдельный отказ при intent mismatch.

Не переводить plan state и не исправлять title прямым SQL. Для воспроизведения
schema и plan lifecycle использовать disposable проверку:

```bash
make test-control-plane-postgres
```

## Safe Run activity

В graph snapshot до первого delegate должны присутствовать `PLANNED` workflow
nodes. После materialization тот же node ref получает `QUEUED`; duplicate node
или duplicate `DELEGATED_TO` edge является дефектом.

Для `TOOL_CALL_RECORDED` проверить actor, message kind, tool, safe parameters,
capability/grant ref, state, duration, safe result и audit ref. Отсутствующий
audit ref, неизвестный capability/grant или raw payload требует закрытого
отказа и расследования; вручную дописывать event/outbox запрещено.

## Запрещено

- исправлять lifecycle прямым SQL;
- принимать actor/project/root lineage из request payload;
- вручную переоткрывать terminal Run или published version;
- удалять outbox/event row для разблокировки;
- выводить prompt, artifact content, provider response или secret material.
