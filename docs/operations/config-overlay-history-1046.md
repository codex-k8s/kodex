---
id: OPS-CONFIG-OVERLAY-HISTORY-1046
title: История опубликованных ConfigOverlay
type: operational-guide
status: approved
owner: control-plane
version: 1.0.0
updated: 2026-09-05
---

# История опубликованных ConfigOverlay

Источник: Issue #1046, Epic #1018, MVP-UI-21. Существующий RollbackConfigOverlay
принимает exact published_overlay_ref. История предоставляет защищённый способ
выбрать эту ссылку без произвольного пользовательского ввода.

| Сценарий | Публичный endpoint → RPC | Полномочия и owner | Ответ / потребитель |
| --- | --- | --- | --- |
| История | GET agents/{agentRef}/config-overlay/revisions → ListConfigOverlayRevisions | Verified actor, PostgreSQL, agent.view; exact system assistant — organization.manage | revisions/page/total, выбор прежней публикации |
| Preview | GET agents/{agentRef}/config-overlay/revisions/{revisionRef} → GetConfigOverlayRevision | Тот же agent и tenant boundary, revision принадлежит ему | immutable metadata и ограниченный TOML |
| Rollback | Существующий RollbackConfigOverlay | agent.manage либо organization.manage системного помощника; Agent OCC и idempotency | Новая опубликованная revision и authoritative runtime view |

Read RPC metadata: resource_ref=agent_ref REQUIRED; resource_version,
attempt, idempotency и project metadata FORBIDDEN. Второй ref одиночного GET
входит в exact unary request digest, не назначает authority.

История включает только PUBLISHED/SUPERSEDED с published_at. Публикация
назначает immutable ref/revision/content/digest; SUPERSEDED отражает положение
в истории, не переписывает содержимое. Непубликованные черновики не выдаются.
Preview ограничен существующим TOML budget 64 KiB и принадлежит тому же
защищённому читателю, что safe effective configuration.

List использует RepeatableRead, count до pagination, revision DESC/ref DESC.
Страница ограничена 20 ревизиями, чтобы bounded TOML preview оставался
в пределах транспортного бюджета.
Query — буквальная подстрока ref, максимум 200 символов. Cursor связывает
tenant, actor, agent и нормализованный query; изменение scope даёт
InvalidArgument. Total ограничен точным публичным integer range.
Неправильные refs закрыто отклоняются, чужая revision не возвращается.

Чтения не меняют состояние, не создают события и не используют OCC/idempotency.
После rollback UI перечитывает runtime view и историю. Rollback остаётся
новой публикацией: сервер повторно проверяет текущие catalog pins и effort
compatibility, поэтому историческая запись сама по себе не обещает успешный
rollback при изменившихся зависимостях.

Contract checkpoint не означает готовность owner RPC, profile63 или HTTP SDK.
Исполняемый owner и публичный consumer подтверждаются отдельным exact SHA
ledger в соответствующих полных unit PR.
