---
id: OPS-MC-008
title: Хранение и очистка runtime-ресурсов
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Хранение и очистка runtime-ресурсов

## Владелец lifecycle

Control-plane владеет Session, Turn, Run graph, attempts, leases, artifacts и
retention policy. Runtime-controller удаляет Kubernetes workload только после
authoritative terminal/cancel readback и успешного signed handoff. Наличие или
удаление внешнего Mattermost thread не влияет на core lifecycle.

## Обычный turn

Каждый обычный turn выполняется новым Pod exact promoted role image. После
terminal transition:

1. runner завершает child processes и формирует bounded terminal manifest;
2. control-plane проверяет attempt/fence/digests и атомарно фиксирует terminal
   state, artifacts, events, audit и закрытие grants/leases;
3. runtime-controller подтверждает terminal readback и удаляет Pod;
4. session PVC сохраняется до отдельной retention decision.

Stale Pod не получает новые credentials и не переиспользуется для следующего
turn. Retry создаёт новую attempt, RuntimeRevision, grant и Pod.

## System assistant

Warm Pod системного помощника является отдельным runtime class. Он сохраняется
между idle periods, но не хранит secret values и не считается активным Turn.
При revision change Pod заменяется контролируемо; после restart reconciler
восстанавливает desired warm runtime до положительной assistant readiness.

## Durable state

- RunEvent, audit, callback/gate receipts и published instruction/workflow
  versions хранятся согласно organization policy и не удаляются с Pod;
- artifact body удаляется только после retention expiry, отсутствия legal hold
  и подтверждённого удаления всех bindings/download grants;
- PVC удаляется только после archive/digest readback и отсутствия active,
  queued, waiting или retryable attempts;
- optional delivery attempts имеют собственный срок хранения и не удерживают
  core Run/PVC после terminal state.

Очиститель использует bounded batch, owner-side claim/fence и аудит. Ручное
массовое удаление SQL или Kubernetes resources не является штатной процедурой.
