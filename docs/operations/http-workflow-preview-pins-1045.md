---
id: HTTP-WORKFLOW-PREVIEW-1045
title: Точные Workflow revisions для редактора и preview
type: verification
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Workflow draft и preview

Refs #1045, #1022, #1031; MVP-UI-34/35/36. Existing owner RPC и authority
не меняются. Actor приходит из authenticated session; owner проверяет
Workflow eligibility. GET/list и ответ UpdateWorkflowDraft проходят один
mapper. Update сохраняет существующие If-Match/idempotency semantics.

Основной Workflow по-прежнему показывает Published, иначе Draft.
`revisionRef` относится именно к этим steps/inputFields, а не вычисляется
из числового revision. `publishedRevisionRef` и `draftRevisionRef` отражают
наличие соответствующих owner snapshots. Отсутствующая revision не получает
синтетический ref.

Отдельный `draft` содержит точные ref/version/revision/state и поля редактора
из CP DraftVersion. Это сохраняет изменения после save/refetch, когда рядом
существует прежний PublishedVersion. Редактор использует draft при его
наличии; preview получает этот же ref и stage ref. Для опубликованного
просмотра используются основной body и revisionRef. Смешивать draft ref и
published steps запрещено. Состояние draft передаёт existing CP caster;
новая локальная lifecycle-интерпретация отсутствует.

Malformed owner ref, state или revision number закрыто отклоняются до
ответа. Тесты покрывают Draft/Published/оба/отсутствие, сохранение и повторный
GET с разными body/pins, а также неверные owner refs. Нормализация не
раскрывает исходные private envelope fields.

HTTP результаты проверяются локально с generated RPC fake. Реальные
save/refetch/preview в PWA и live acceptance относятся к #1022/#1031 и
этими unit-проверками не заменяются.
