---
id: UNIT-INTERACTION-GATEWAY-001
title: Необязательный interaction gateway Kodex
type: unit-readme
status: approved
owner: platform
version: 1.0.0
updated: 2026-08-23
---

# interaction-gateway

`interaction-gateway` — необязательный interaction adapter. Он обслуживает
четыре независимо выдаваемые Mattermost capability:

- `mattermost.inbound`;
- `mattermost.notifications`;
- `mattermost.result_mirror`;
- `mattermost.gate_decisions`.

Метаданные подключений, grants, delivery attempts, inbound receipts, Runs и
Human Gates принадлежат `control-plane`. Идентификаторы Mattermost используются
только как внешние локаторы и audit metadata, но не как источник полномочий.
Gateway не читает PostgreSQL и не меняет core lifecycle самостоятельно.

Исходящие доставки claim-ятся с fenced lease и завершаются отдельно от Run:
ошибка Mattermost не меняет успешный core Run на `FAILED`. Входящее сообщение
маршрутизируется только через единственный активный server-owned grant.
Решение Human Gate использует тот же one-winner/OCC contract, что и Control
Center, поэтому повтор с другой поверхности получает stale readback.

Неопределённая отправка фиксируется как `UNKNOWN_OUTCOME` без автоматического
повтора. Входящий gate reply подтверждается чтением post/root и точной связкой
gate/run/version; внешнего user identifier недостаточно без server-owned
привязки к субъекту Kodex. Детали жизненного цикла и оставшаяся область полного
unit описаны в [контракте #1030](../../../docs/operations/interaction-gateway-1030.md).

Пользовательский текст локализуется по locale подключения из embedded YAML.
Credential material читается только из точного server-mounted файла и не
попадает в API, логи или audit. Весь внешний трафик идёт через egress gateway к
hostname из deployment allowlist с TLS 1.3.

`/healthz` отражает жизнь собственного процесса. `/readyz` читает локальный
снимок authority sidecar и не вызывает `control-plane` или Mattermost. Сбои
рабочего межсервисного и внешнего пути наблюдаются как отдельные переходы
degraded/recovered без влияния на Kubernetes readiness.
