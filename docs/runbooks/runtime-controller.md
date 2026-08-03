---
id: RUN-MC-008
title: Диагностика и восстановление runtime-controller
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-03
---

# Диагностика и восстановление runtime-controller

## Предварительная проверка

Зафиксировать Git SHA и digest образов. Не выводить lease token, application
grant, DSN, S3 keys, NATS credentials, TLS private key или содержимое PVC.
Получить exact render командой из `SVC-MC-005`; адреса Kubernetes API всегда
разрешать заново из Service и готовых EndpointSlice.

```bash
kubectl -n mattercodex-system get deploy,pod,job,pvc,configmap,networkpolicy \
  -l app.kubernetes.io/name=runtime-controller
kubectl -n mattercodex-system get events \
  --field-selector involvedObject.kind=Pod --sort-by=.lastTimestamp
```

## Controller не готов

Проверить по именам, без значений: workload TLS, application grant, PostgreSQL,
NATS, OTLP/Sentry, local issuer socket и Kubernetes RBAC. Готовность должна
проверять тот же protected `CheckReadiness`, что рабочие RPC, durable inbox и
точный stream/durable JetStream contract. Не ослаблять mTLS, bearer,
authorization context или TLS SNI.

## Reconcile или capacity остановились

Сверить `RuntimeExecution.version/fence/grant_generation` с journal и Pod
annotations. Stale tuple не исправлять ручным patch: revoke/terminal выполняет
`control-plane`. При `capacity=deferred` проверить ResourceQuota, allocatable и
Memory/Disk/PID pressure. `CLUSTER_ADMIN` Pod не допускается к автоматическому
LRU eviction.

## Archive/restore/cleanup остановились

Проверить состояние Job и факт exact S3 `versionId`, `ChecksumSHA256`, размер и
metadata provenance. Нельзя удалять PVC по наличию объекта или checksum без
успешного независимого restore proof. Cleanup authorization должна быть
`ACTIVE`, относиться к тем же execution/revision/input/attempt/generation и
потребляться сразу после guarded delete. Истёкшую authorization переиздаёт
только `runtime-cleanup-authorizer`.

Четыре часа простоя разрешают удалить только warm Pod. PVC остаётся до
семисуточной отсрочки; давление на диск этот срок и proof gate не сокращает.

Backfill не требует ручного создания Job: controller заново сверяет каждый
сохранённый terminal journal и создаёт отсутствующий archive/restore Job.
Ручной вызов S3/RPC либо изменение journal запрещены. PVC прежнего runtime без
server-owned execution/journal только инвентаризируется и не усыновляется по
labels. При недоступном archive/restore PVC сохраняется fail-closed.

## Rollback

Откат образа выполняется на предыдущий подписанный digest после отдельного
владельческого шлюза. Миграция forward-only: rollback схемы делается новой
компенсирующей migration. До восстановления совместимого consumer остановить
controller, не удаляя PVC, durable inbox, journal или JetStream consumer.
