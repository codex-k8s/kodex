---
id: ADR-MC-006
title: Обязательное S3-хранилище содержимого artifacts
type: decision
status: approved
owner: architect
version: 2.1.0
updated: 2026-08-31
---

# ADR-MC-006. Обязательное S3-хранилище содержимого artifacts

## Статус решения

Версия 2.0.0 суперсидирует решение 1.0.0 о хранении bounded content в
PostgreSQL и необязательном S3 после MVP.

## Решение

S3-compatible storage является обязательной зависимостью fresh web-only MVP.
Control-plane остаётся владельцем `Artifact`, lifecycle, tenant/project
eligibility, scan state, bindings и download grants. PostgreSQL хранит metadata
и точную квитанцию S3 (`object_key`, `object_version`, `object_etag`, digest,
size), а неизменяемое тело хранится только в выделенном bucket.

Ключ объекта назначает сервер из organization, Project, Artifact и SHA-256.
Browser, gateway, runtime Pod и optional adapter не получают endpoint,
credentials или произвольный storage locator. Они используют только
специализированные owner-checked streaming operations control-plane.

Запись завершается только после S3 upload и readback digest/size. PostgreSQL
transaction фиксирует Artifact metadata, S3 receipt, audit, idempotency и
обязательные события. При незавершённой transaction подготовленный объект
удаляется ограниченной cleanup-операцией. Download повторно сверяет receipt,
digest и size; неизвестное либо повреждённое состояние закрыто отклоняется.

## Профили

- Local hot-reload разворачивает SeaweedFS release 4.41 в `kodex-system` с
  digest-pinned image, PVC и S3 Service
  `seaweedfs-s3.kodex-system.svc.cluster.local:8333`. Bucket
  `kodex-artifacts` создаёт идемпотентная bootstrap Job до запуска
  control-plane.
- Local credentials генерирует `tools/dev/deploy-local.sh` в immutable Secret
  `kodex-external-s3` в `kodex-system` и отдельную credential-only проекцию с
  тем же именем в `kodex-runtime`; значения не входят в Git, render или log.
- Production не разворачивает встроенный object storage. Оператор заранее
  создаёт внешний S3 bucket, полный Secret `kodex-external-s3` с keys
  `endpoint`, `region`, `bucket`, `access-key`, `secret-key` в `kodex-system`
  и credential-only Secret с keys `access-key`, `secret-key` в
  `kodex-runtime`. Значения не входят в Git.
- Control-plane монтирует только два credential files и получает
  endpoint/region/bucket через `secretKeyRef`. Отсутствующий Secret, bucket или
  отрицательный `HeadBucket` оставляет startup/readiness в закрытом отказе.
- Session-archive controller передаёт проверенные endpoint/region/bucket worker
  Job, а worker монтирует только credential-only Secret в `kodex-runtime`.
  Обычный agent runtime Pod этот Secret не получает.
- Plain HTTP разрешён только local endpoint с полным DNS suffix
  `.kodex-system.svc.cluster.local`; production endpoint обязан использовать
  HTTPS с проверкой hostname и доверенной CA.

S3 connection является внутренней infrastructure dependency, а не
пользовательской `IntegrationDefinition`.

## Отдельные units

- Архивирование и восстановление Codex session JSONL не входят в это решение
  реализации и выполняются самостоятельным unit [#1002](https://github.com/codex-k8s/kodex/issues/1002).
- Backup policy, retention и restore drill PostgreSQL вместе с S3 objects не
  входят в это решение реализации и выполняются самостоятельным unit
  [#1003](https://github.com/codex-k8s/kodex/issues/1003).

До завершения #1002 активный session JSONL остаётся на session PVC. До
завершения #1003 нельзя заявлять полный поддерживаемый backup/restore платформы.

## Последствия

- PostgreSQL больше не хранит artifact body и его backup без S3 objects
  недостаточен для восстановления файлов.
- Fresh local профиль получает дополнительный StatefulSet, PVC, Secret,
  NetworkPolicy и bootstrap Job.
- Multipart/large-object upload и range download остаются вне текущего MVP;
  обязательность S3 сама по себе не расширяет bounded API limits.
- Optional Mattermost/result mirror остаётся неавторитетной доставленной копией
  или ограниченной ссылкой.
