---
id: OPS-MC-003
title: SLO и управление ресурсами
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# SLO и управление ресурсами

## Начальные цели

- availability owner API и realtime control surface: 99,9% в месяц;
- принятый Turn не теряется при одиночном restart stateless Pod;
- один `(scheduleRef, scheduledFor)` материализует не более одного core Run;
- callback и Human Gate continuation дают один доменный эффект;
- RPO PostgreSQL/WAL: до 15 минут, RTO поддерживаемого restore: до 4 часов;
- первый запрос системному помощнику использует фактически ready warm runtime,
  а не обычный cold materialization path.

Время ответа model provider и optional integration измеряется отдельно. Их
ошибка классифицируется и показывается пользователю, но недоступность Mattermost
не снижает core availability и не меняет успешный Run.

## Role runtime

Каждая роль выбирает server-owned resource profile и exact promoted OCI image.
Обычный turn получает новый execution-scoped Pod. Очередь учитывает project и
organization concurrency, quotas, node pressure и provider policy; нехватка
capacity оставляет Run в `QUEUED` до bounded timeout.

System assistant использует отдельный long-lived warm Pod с явными requests,
limits, heartbeat и controlled RuntimeRevision. Idle warm state не является
активным Turn и не снимает лимиты с фактического выполнения.

## Storage и backpressure

- PVC хранит только долговечную session workspace/history;
- terminal Pod удаляется после owner-confirmed handoff, PVC — только по retention;
- progress events coalesced и bounded; raw stdout и files не идут через NATS/WS;
- event consumer обнаруживает gap, выполняет catch-up или authoritative snapshot;
- при критическом pressure новые executions остаются в очереди, а данные без
  подтверждённого archive/retention никогда не удаляются автоматически.
