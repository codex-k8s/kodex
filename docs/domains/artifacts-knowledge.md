---
id: DOM-MC-008
title: Artifacts & Knowledge
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Artifacts & Knowledge

## Назначение

Владеет файлами, версиями, deliveries, retention, instruction/knowledge objects и безопасной materialization в agent workspace.

## Входные источники

- Mattermost attachment;
- Control Center upload;
- integration result, например email attachment;
- agent output;
- Git/instruction import;
- backup/restore.

## Storage lifecycle

States: `uploading`, `scanning`, `available`, `quarantined`, `delivery_pending`, `deleted`, `retained`.

Object становится доступен runtime только после завершения обязательных checks. ArtifactVersion immutable и адресуется внутренним ID, а не user filename.

## KnowledgeSpace

KnowledgeSpace группирует versioned documents/artifacts и search/index metadata. Исходные документы остаются источником истины; embeddings/search index являются перестраиваемой проекцией.

## Retention

Policy учитывает organization, artifact kind, legal hold, linked active process/session и audit requirements. Удаление metadata и object выполняется согласованно и повторяемо.

## Acceptance

- PDF/image/text доступны агенту по safe paths.
- Unicode/duplicate filenames не перезаписываются.
- Agent публикует Markdown/CSV/PDF/PNG от собственной identity.
- Publish outside outbox запрещен.
- Mattermost delivery retry не создает duplicate post.
- Oversized artifact получает scoped link.
- Cross-organization access отклоняется.
- Backup restore проверяет object checksum.
