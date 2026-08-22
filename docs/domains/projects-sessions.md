---
id: DOM-MC-003
title: Проекты, сессии и внешние диалоги
type: domain
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-22
---

# Проекты, сессии и внешние диалоги

## Назначение

`Project` — единственный универсальный бизнес-контейнер. Он содержит members,
Agents, Workflows, Integration grants, Knowledge bindings, Schedules, Runs,
Artifacts, settings и audit. Workspace/Room не являются конкурирующими core
терминами и не используются как обязательные агрегаты.

## Проект

Проект имеет stable opaque ref, name, slug, description, lifecycle, version и
organization ownership. Он не обязан иметь repository, external chat, CRM,
storage или Kubernetes namespace. Archive запрещает новые launch/configuration
operations, но сохраняет history и audit.

## Сессия

`Session` принадлежит Project и Agent, хранит последовательную provider-neutral
историю и FIFO turns. Она может быть создана direct launch, Workflow,
system-assistant, Schedule, Integration event, Agent delegation или optional
interaction adapter. Продолжение использует session ref из авторитетного
readback и никогда не требует внешнего thread ID.

## ExternalConversationBinding

Binding связывает Session с необязательной поверхностью: Mattermost thread,
email conversation или иным adapter. Он хранит provider kind, masked display
metadata, revision и delivery policy. External IDs не выдаются как authority и
не входят в core lifecycle.

Удаление, outage или disable внешнего binding:

- не удаляет Project, Session, Run или Artifact;
- прекращает новые inbound/delivery операции данной capability;
- создаёт отдельный retryable delivery state и audit;
- не меняет успешный core Run на `FAILED`.

## Mattermost capabilities

Mattermost definition содержит независимые `INBOUND_MESSAGES`, `NOTIFICATIONS`,
`RESULT_MIRROR`, `HUMAN_GATE_DECISIONS`. Любая комбинация допустима. Team,
channel, post, thread и bot identity находятся только в adapter metadata.

## События

`project.created`, `project.updated`, `project.archived`, `session.created`,
`session.turn_queued`, `external_binding.changed`, `delivery.attempt_changed`.

## Критерии приёмки

- web-only Project создаётся и исполняет Run при нуле внешних connections;
- в UI нет ручного ввода UUID или external IDs;
- optional inbound receipt идемпотентно создаёт не более одного Turn;
- удаление внешнего канала не запускает cleanup core Session;
- adapter outage не участвует в core Pod readiness.
