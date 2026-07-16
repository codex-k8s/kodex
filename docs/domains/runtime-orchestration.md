---
id: DOM-MC-006
title: Runtime Orchestration
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Runtime Orchestration

## Назначение

Владеет sessions, turns, runtime revisions, durable queue, leases, capacity decisions и reconciliation desired agent runtime.

## Состояния

Session: `idle`, `queued`, `running`, `waiting`, `blocked`, `expired`, `failed`.

Turn: `queued`, `claiming`, `running`, `succeeded`, `failed`, `stopped`, `retry_wait`, `blocked`.

Переходы выполняются compare-and-set/transaction, а не присваиванием из transport handler.

## Commands

- enqueue turn;
- claim/heartbeat/complete/fail/stop turn;
- ensure/release session runtime;
- refresh runtime revision;
- retry transient failure;
- archive/restore session;
- evict idle runtime.

## Transient failures

Capacity/provider/network ошибки классифицируются typed policy. Retry сохраняет session/PVC и использует bounded backoff. Usage/auth/validation failures не маскируются как capacity retry.

## Kubernetes adapter

Adapter создает resources из typed specs. Business code не формирует YAML/shell. Runtime controller reconcile-ит deterministic Pod/PVC/Secret/ServiceAccount state и записывает status condition.

## Acceptance

- Один session выполняет максимум один running turn.
- Queued сообщения выполняются в порядке sequence.
- Bot-service/controller restart не теряет queue.
- Stale running state repair-ится по lease/runner evidence.
- Недостаток capacity не переводит turn в permanent failure.
- Runtime revision change применяет свежие env/config/integrations к следующему turn.
- Один idle pod максимум на session, default TTL четыре часа.
