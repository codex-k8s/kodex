---
id: OPS-MAILBOX-1812
title: Продолжение почтовой операции после подтверждения
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-15
---

# Продолжение после подтверждения

Источник: Issue #1812, явное решение владельца быстро исправить и развернуть
зависшую подтверждённую отправку. Изменяется только control-plane, без миграций.

Путь: сотрудник → invoke integration → CP immutable invocation и mailbox gate →
owner HTTP решение → CP READY → integration-gateway claim → email-bridge exact
authorization → SMTP → durable receipt → CP completion/continuation.
Actor, organization, project, grant и input происходят из сохранённого owner
состояния; browser payload не назначает полномочия. Контракты не изменяются.

| Состояние | Допустимый результат |
| --- | --- |
| Generic NONE + mailbox gate OPEN | Claim и SMTP отсутствуют |
| Exact gate APPROVED, root WAITING_HUMAN, сотрудник RUNNING/SUCCEEDED | Одна исходная invocation доступна claim и exact authorization |
| Input изменён, чужой workload, grant отозван, terminal root | Закрытый отказ; approval не восстанавливает authority |
| Claim уже активен или durable receipt записан | Второй claim не создаёт повторный effect |

Исходный дефект: queue eligibility принимал одобренные gates только для generic
HUMAN_EACH_EFFECT, хотя email использует generic NONE и отдельный mailbox gate.
Вторая граница email authorization ошибочно требовала только RUNNING сотрудника
и отклоняла допустимое ожидание человека после завершения модельного хода.

Проверка: regression в email_authorization_component_test.go; фактический
disposable результат и exact SHA записываются в PR. Недоступный либо упавший
контур не считать PASS. Реальная отправка допускается только после отдельного
решения пользователя на точное письмо; неизвестный исход не повторять.

Выкатка: immutable OCI через tools/dev/build-local-control-plane.sh, exact import
в разрешённый локальный кластер и tools/release/scoped-release.mjs только для CP.
Сохраняются две реплики, sidecar и конфигурация. До apply создаётся release plan
с прежним exact image; readback проверяет обе реплики. Откат — тем же scoped
механизмом к прежнему image из плана, без отката БД. После SMTP сначала читать
квитанцию; rollback не отзывает уже отправленное письмо и не разрешает повтор.
