---
id: ARCH-MC-007
title: Runtime и сессии агентов
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Runtime и сессии агентов

## Session scope

Session может быть создана из:

- Mattermost thread;
- default agent room context;
- ScheduledRun;
- ProcessRun child;
- administrative/manual run.

ConversationBinding является optional: headless schedule не обязан создавать Mattermost post.

## Очередь turn

- Turns внутри session выполняются FIFO.
- Несколько сообщений пользователя создают несколько turns и не теряются.
- Delegation requests к занятому agent/session сохраняются durable.
- Совместимые queued delegations могут объединяться, но prompt сохраняет каждого initiator и исходную инструкцию.
- Turn имеет idempotency key и не выполняется параллельно двумя runners.

## RuntimeRevision

Перед каждым turn вычисляется canonical manifest и hash из:

- agent и role revisions;
- runtime profile и provider config;
- immutable role image digest;
- provider account/auth revision;
- integration grants и connection revisions;
- env/secret binding versions;
- InstructionSet version;
- workspace/room overrides;
- resource class и Kubernetes access policy.

Если effective hash совпадает, idle pod можно переиспользовать. Если изменились env, auth, image, mounts или permissions, pod пересоздается перед claim следующего turn. Изменения не прерывают running turn.

`config.toml` и provider-specific runtime files генерируются перед каждым `exec/resume`, даже если pod не пересоздается.

## AI account affinity

- Account выбирается при создании session вручную, из fixed binding или account pool.
- Scheduler учитывает auth status, разрешения, observed limits и freshness observation.
- Account ID записывается в session до первого provider call.
- Resume использует только этот account.
- Reauthorization того же logical account повышает auth revision и вызывает runtime refresh.
- Недоступный account требует reauthorization либо новой session с explicit context handoff.

## Session persistence

Session metadata хранится в PostgreSQL. Session archive хранится в S3-compatible storage с checksum и version. PVC используется как рабочий cache, а не единственная долговечная копия.

После каждого turn runner:

1. завершает provider process;
2. собирает session archive;
3. загружает immutable archive version;
4. атомарно завершает Turn и обновляет session archive reference;
5. публикует result/outbox events.

## Pod lifecycle

- Максимум один session pod на session.
- Default warm TTL — 4 часа после последней активности.
- Running/queued turn продлевает lease.
- Idle pod можно удалить по LRU при capacity pressure.
- Удаление pod не удаляет PVC/session archive/turn queue.
- Runtime controller восстанавливает pod по desired state.
- Dead pod и stale lease автоматически repair-ятся без duplicate turn.

## Capacity

Admission учитывает allocatable capacity, requests, namespace quota, pending pods и resource class. При нехватке resources run остается queued с понятным reason. Перед отказом controller может удалить старейший idle warm pod, не затрагивая active sessions.

## Process supervision

Runner использует `tini` либо эквивалентное reaping/forwarding поведение, корректно обрабатывает SIGTERM, дает provider процессу grace period и отправляет structured stop result.

## Status delivery

На turn существует одно стартовое сообщение с stop action. После получения provider limits оно обновляется, а не дублируется. Progress публикуется отдельными notrigger updates. Финальный ответ не перезаписывает стартовое сообщение.
