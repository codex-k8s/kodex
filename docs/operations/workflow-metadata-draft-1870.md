---
id: OPS-DOC-WORKFLOW-DRAFT-1870
title: Создание и чтение черновика процесса без этапов
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-25
---

# Черновик процесса без этапов

Issue [#1870](https://github.com/codex-k8s/kodex/issues/1870): форма создания
передаёт только имя, назначение и координатора. Сервер назначает проект,
идентификатор черновика, concurrency=1 и timeout=3600; отсутствие этапов
допустимо для чтения изменяемого черновика, но не для исполнения.

Путь: PWA `WorkflowsPage` → POST `/api/v1/projects/{projectRef}/workflows` →
API `CreateWorkflow` → CP `CreateWorkflow` → owner-транзакция `Execute` →
`applyResultActionPermissions` → actor-scoped `scanWorkflow` → ответ редактору.
Проверка обязана использовать caller `control-api-gateway`: иной caller может
пропустить чтение разрешённых действий после INSERT и скрыть исходный дефект.
Actor/project authority разрешаются прежними owner-предикатами; route/body refs
не предоставляют полномочия. Контракты API, Proto и машинная policy не меняются.

| Переход или чтение | Проверка и авторитетный исход |
| --- | --- |
| Metadata-only CREATE → DRAFT | Существующие owner/coordinator predicates; одна транзакция фиксирует workflow, idempotency receipt, audit и один WORKFLOW delta в outbox. Ответ содержит EDIT и недопущенный launch readiness. |
| CREATE replay | Тот же intent возвращает прежний результат без второго workflow/event; другой intent с тем же ключом отклоняется. |
| Get/List DRAFT | Actor-scoped eligibility и все прежние bounds полей/этапов сохраняются; допускается отсутствие этапов. |
| UPDATE из редактора | Прежняя строгая проверка полного draft; данное исправление не расширяет запись неполных конфигураций. |
| VALIDATE/PUBLISH/LAUNCH пустого draft | Закрытый отказ; нет опубликованной ревизии, run либо частичного command receipt/event. |
| Добавление этапа → VALIDATE → PUBLISH | Прежняя строгая структура и capability/dependency checks. Опубликованная ревизия остаётся immutable. |
| DRAFT → ARCHIVED | Пустой draft читается при postmutation readback; EDIT/LAUNCH отсутствуют. Одна прежняя транзакция события/receipt. |
| Archive replay после ARCHIVED | Прежний command owner resolver скрывает terminal resource до receipt: NOT_FOUND без нового receipt/audit/event. Это исправление не расширяет command eligibility. |
| Get после ARCHIVED | Прежняя actor-scoped query eligibility сохраняет чтение архивной записи; EDIT/LAUNCH отсутствуют, readiness заблокирован. Чтение не создаёт receipt/audit/event. |
| VALID/PUBLISHED draft либо published snapshot без этапов | Закрытый отказ чтения; разрешение для mutable draft сюда не переносится. |
| Повреждённый JSON/поля/этапы, чужой проект/координатор | Прежний закрытый отказ. Ошибка внутри owner-транзакции откатывает все изменения. |
| Retry/cancel/deadline/lease | Новые фоновые задачи и leases не создаются; существующий execution lifecycle не меняется. |

Прямой вызов `workflowDraftStructureInvalidField` без guard непустоты допустим
только при чтении DRAFT и ARCHIVED. `workflowVersionStructureInvalidField` сохраняет непустую структуру
для validation, publication, execution и immutable published snapshots.
`WORKFLOW_METADATA` помощника по-прежнему показывает только опубликованную
схему: эта правка не раскрывает draft instructions и не расширяет grants.

Оснастка: `TestWorkflowMetadataDraftReadBoundary` и
`TestWorkflowMetadataDraftComponent` в CP PostgreSQL adapter. Component
использует штатную изолированную мигрированную БД, точный API caller, payload
формы без draft, actor Get/List, replay/rollback cardinality, отрицательные
validation/publication/launch и положительный editor/publication/archive.
Внешние провайдеры и execution claims не вызываются.

Ручная проверка после отдельно разрешённой поставки: создать процесс только
с тремя полями формы, открыть его из списка, добавить этап и сохранить,
проверить и опубликовать. Отдельно убедиться, что пустой draft нельзя запустить
или опубликовать и что его можно архивировать. Эти действия не заменяют
проверку самостоятельного Human Gate или полного PDLC.

Миграций и изменений контрактов нет. Rollback — предыдущий CP image; сохранённые
пустые draft снова могут стать недоступными старому строгому reader, поэтому
перед откатом требуется учесть этот известный исход. Runtime/grants/credentials
не меняются; секреты не входят в fixtures или диагностические материалы.
