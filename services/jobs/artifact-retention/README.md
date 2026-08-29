---
id: JOB-MC-006
title: Artifact retention
type: service
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-29
---

# Artifact retention

`artifact-retention` необратимо удаляет exact object version через 30 дней
после soft-delete. Ограниченная выборка использует `SKIP LOCKED`, lease,
монотонный generation и owner fence, поэтому несколько реплик безопасно делят
очередь. После удаления объекта одна PostgreSQL-транзакция удаляет bindings,
download grants и content receipt, сохраняет минимальный `PURGED` tombstone и
audit от отдельного service subject.

Падение после S3 delete безопасно: после истечения lease следующая попытка
получает новый generation, принимает `NotFound` exact version как уже
выполненное удаление и завершает tombstone. Удаление последней версии вместо
зафиксированной `object_version` запрещено.

Готовность требует доступности PostgreSQL и bucket. Параметры batch, lease,
operation timeout и poll interval ограничены конфигурацией. Deployment и
least-privilege database identity находятся в `deploy/k8s/base`.
