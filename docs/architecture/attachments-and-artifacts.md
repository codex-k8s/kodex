---
id: ARCH-MC-008
title: Вложения и artifacts
type: architecture
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Вложения и artifacts

## Источник истины

S3-compatible storage хранит canonical object. Mattermost содержит пользовательскую копию или delivery reference. PostgreSQL хранит metadata, bindings, status и retention.

## Inbound pipeline

1. Interaction Gateway получает post и `file_ids`.
2. Metadata проверяется до скачивания.
3. Content скачивается stream-ом с ограничением размера.
4. Вычисляется SHA-256, определяется фактический media type.
5. Выполняются policy и malware checks.
6. Object сохраняется по непрогнозируемому storage key.
7. Создаются ArtifactVersion и MessageArtifactBinding.
8. Runtime materializer размещает файл read-only в session inbox.
9. Turn prompt получает manifest, но не полный content.

## Workspace paths

```text
/workspace/.matter-codex/
  inbox/<turn-id>/manifest.json
  inbox/<turn-id>/<safe-name>
  outbox/<turn-id>/
  state/
```

Original filename хранится отдельно. Safe name не допускает absolute path, `..`, control characters и symlink traversal. Архивы автоматически не распаковываются.

## Prompt manifest

Для каждого файла передаются:

- original name;
- local path;
- media type;
- size;
- checksum;
- source type/post;
- краткое пользовательское описание, если задано.

При resume в новый prompt входят только новые attachments; ранее materialized artifacts остаются доступны по session manifest.

## Outbound pipeline

Agent пишет файл в `MATTERCODEX_OUTPUT_DIR` и вызывает локальный MCP/runtime tool `publish_artifact`.

Tool:

1. canonicalize path и проверяет, что он внутри outbox текущего turn;
2. запрещает symlink и special file;
3. проверяет size/media/scan policy;
4. загружает canonical object в S3;
5. создает ArtifactVersion;
6. передает delivery command Interaction Gateway;
7. Gateway загружает файл в Mattermost от bot identity агента и отвечает в исходный thread;
8. post получает internal notrigger property.

Если Mattermost limit меньше файла, Gateway публикует scoped expiring download link и metadata. Artifact не теряется при временной ошибке доставки.

## Изоляция и безопасность

- Agent не получает Mattermost token и S3 master credential.
- Runtime tool не публикует `/etc`, secret mounts и файлы другой session.
- Storage keys разделены organization/workspace/session prefixes и проверяются authorization layer.
- Sensitive filenames маскируются в logs/audit при необходимости.
- Scan state: `pending`, `clean`, `quarantined`, `failed`.
- Quarantined artifact не materialize-ится и не доставляется.
- Retention и legal hold задаются policy, а не временем жизни pod/PVC.

## Backup consistency

Restore считается успешным, если восстановлены PostgreSQL metadata и соответствующие S3 object versions. Backup run сохраняет marker/correlation для проверки согласованности.
