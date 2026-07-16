---
id: ADR-MC-006
title: S3 как canonical artifact storage
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-006. S3 как canonical artifact storage

## Решение

Attachments, agent outputs, InstructionSets и session archives хранятся в S3-compatible object storage. Mattermost содержит delivery copy/reference, PostgreSQL — metadata и lifecycle state.

Входные файлы проходят hashing/policy/scan до materialization. Агент публикует выходные файлы только через `publish_artifact` из разрешенного outbox.

## Последствия

- Pod/PVC и Mattermost retention не уничтожают canonical result.
- Требуются tenant isolation, retention, versioning и backup consistency.
- Oversized Mattermost file можно доставить expiring scoped link.
