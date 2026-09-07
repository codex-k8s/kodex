---
id: OPS-DOC-1174
title: Копирование и архивирование управляемых конфигураций
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Копирование и архивирование CFG

Исходные требования CFG-01/02 сохраняют поставляемые RoleImage и
IntegrationDefinition неизменными. Копирование создаёт отдельный UI-owned set
и DRAFT, который проходит обычные validate/publish. Архивирование закрывает
новые назначения и публикации, сохраняя историю и уже связанные revisions.

## Полномочия и контракт

Инициатор — пользователь Control Center с проверенной OIDC authority.
Gateway передаёт специализированный command RPC; actor и organization
разрешает Control Plane. MutationContext содержит idempotency key и точную
версию выбранного источника либо архивируемого set. Полномочия над источником
и назначением проверяются до OCC и чтения receipt; новая организация из payload
не принимается. `next_actions` вычисляется владельцем и не заменяет повторную
авторизацию команды.

| Сценарий | RPC и ресурс authority | Owner predicate и результат |
| --- | --- | --- |
| RoleImage copy | `CopyRoleImageConfiguration`, recipeRef либо configurationRef | Чтение исходного source; для managed source также project.manage. В проекте назначения project.manage и image.source.view/manage. Новые set/ref/revision назначает сервер |
| IntegrationDefinition copy | `CopyIntegrationDefinitionConfiguration`, shipped.key либо configurationRef | organization.manage. SHIPPED проверяется по catalog version, definitionVersion, digest и загруженному пакету |
| RoleImage archive | `ArchiveRoleImageConfiguration`, configurationRef | project.manage и image.build, только UI, не SHIPPED. Existing recipe ARCHIVE атомарно закрывает builds/promotions по прежнему lifecycle |
| IntegrationDefinition archive | `ArchiveIntegrationDefinitionConfiguration`, configurationRef | organization.manage, только UI. Никакого удаления bound revision или connection |

`copy_provenance` неизменна: origin, sourceRef, sourceRevision, sourceVersion,
sourceDigest назначаются сервером. Для recipe sourceRevision — generation,
для managed set — ref опубликованной revision, для SHIPPED integration —
definitionVersion. Изменение имени копии не меняет исходный объект.

Bootstrap RoleImage определяется существующей owner-связью
`platform-owned:default-role-image`/`system-base`, а не `managed_by=GIT`.
Его копия сопоставляет точные base image reference/digest с доступной средой
каталога без добавочных пакетов/инструментов. Неоднозначность закрыто
отклоняется. Исходная provenance сохраняется; новая revision использует
действующий ключ среды и проходит обычную проверку supply-chain input.

## Переходы и сохранение исполнения

Copy фиксирует новый DRAFT, audit и receipt в одной owner-транзакции.
Validate/publish/discard сохраняют существующий specialized lifecycle.
Для самого copy и IntegrationDefinition archive отдельного domain event нет:
авторитетный read path — list/history/effective binding; receipt replay
обновляет текущие archived/version и доступные действия, сохраняя исходную
immutable revision.

RoleImage archive дополнительно публикует `ROLE_IMAGE_RECIPE_CHANGED` в той же
транзакции; прежние отмены build/promotion и их события не подменяются.
Специализированные RoleImage ARCHIVE/RESTORE остаются доступны и синхронно
меняют managed archived. REQUEST_BUILD не обходит архив. У
IntegrationDefinition нового restore endpoint нет.

Git configure/refresh/write-back, managed publish/rebind и новые drafts не
могут возобновить архив. GIT/SHIPPED source не архивируется UI-командой.
Исторические revisions и действующие integration bindings продолжают
разрешаться по прежним точным pins; archive не отзывает их полномочия и не
имитирует cancellation. Каталог новых managed selections исключает архив.

## Проверка и откат

`make test-control-plane-postgres` проверяет настоящие SHIPPED→UI copy,
validate/publish, UI/GIT lineage, stale version и foreign tenant, immutable
provenance, archived selection/history/binding, source denial и совместимость
RoleImage ARCHIVE/RESTORE. Go unit проверяет точное сопоставление каталога и
закрытую доступность действий. Proto и authority policy проверяются через
канонические codegen targets; оба environment profiles должны рендериться.

Ручная проверка после доставки HTTP/PWA: скопировать поставляемую конфигурацию,
изменить draft, validate/publish, архивировать UI-копию, открыть историю и
убедиться, что исходник и ранее bound execution не изменились. UI/live QA
является отдельным результатом, локальные проверки её не подтверждают.

Откат приложения требует согласованных Proto/client/policy версий. Миграцию
с archived/provenance не откатывают при работающих новых consumers: это
потеряло бы состояние архива и происхождение копий. Секреты не добавляются
в content, audit, события или диагностические журналы.
