---
id: DOM-MC-008
title: Файлы, результаты и знания
type: domain
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-23
---

# Файлы, результаты и знания

## Artifact

`Artifact` принадлежит Organization и Project; version хранит source,
provenance, media type, size, digest, scan status, retention и binding к Session,
Run, node, input или result. Filename является недоверенным display metadata и
не используется как storage key.

Источники: Control Center upload, Agent result, Integration result, Knowledge
source и optional interaction attachment. Ни один источник не требует
Mattermost post/thread.

## Жизненный цикл

Fresh baseline выполняет bounded inspection синхронно до фиксации upload и
сохраняет один из итоговых scan states: `CLEAN`, `QUARANTINED`, `FAILED`.
Transport-состояние ongoing upload отображает клиент, но оно не является
долговечным доменным переходом. Поля `PENDING` и `SCANNING` зарезервированы в
схеме для будущего отдельного scanner adapter и не выдаются как успешная
готовность без фактического consumer. Runtime получает только `CLEAN` version
через bounded immutable materialization. Upload повторно сверяет declared
size/digest с фактически прочитанным content.

Download использует короткоживущий one-time grant, связанный с User,
organization, Project, artifact version и purpose. Browser не получает storage
credential или внутренний locator. Preview поддерживает только allowlisted safe
media и никогда не исполняет активный контент.

## Knowledge

Knowledge source связывает immutable Artifact versions либо typed external
source с Agent/Project. Индекс и embeddings являются перестраиваемой проекцией и
не расширяют eligibility исходного документа. Проекция хранит source version,
content/model provenance и tenant/project scope. Ошибка projection не блокирует
авторитетное чтение доступного файла.

## Realtime и delivery

`artifact.available` содержит только safe metadata и ref; файл читается
отдельным HTTP API, не через WebSocket. Optional result mirror создаёт отдельный
DeliveryAttempt. Его outage не меняет Artifact availability и core Run outcome.

## Критерии приёмки

- пользователь загружает input и скачивает generated result в web-only режиме;
- Unicode/повторяющиеся имена не перезаписывают content;
- foreign Project и expired/replayed grant закрыто отклоняются;
- artifact со state, отличным от `CLEAN`, не materialize-ится в role Pod;
- raw file bytes, provider payload и secret не попадают в events, logs или audit.
