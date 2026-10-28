---
id: OPS-DOC-WORKFLOW-GATE-1872
title: Точные решения Gate опубликованного этапа
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-25
---

# Gate результата этапа

Issue [#1872](https://github.com/codex-k8s/kodex/issues/1872). Источник решений —
`GateDecisions` точного опубликованного этапа, а не общий список четырёх решений,
новый draft либо текущий указатель публикации. Это исправление control-plane;
оно не внедряет остановленный server-DAG unit #1836 или result-delivery #1848.

Producer: форма/типизированный план → Workflow draft → validation/publish →
immutable `workflow_versions`. Launch закрепляет `runs.workflow_version_id`,
создаёт каждый planned step с его `human_gate_after`; делегирование материализует
тот же node и сохраняет флаг. Claim выдаёт immutable RuntimeRevision и exact
lease/fence/generation. CompleteExecution проверяет lease, root и связь
revision/run/node/agent/turn/attempt с опубликованным этапом, выбирает точный
список и передаёт его в owner Gate INSERT. Gate/node/edge, usage, receipt,
audit и ordered events фиксируются прежней owner-транзакцией.

Consumer: GetOwnerGate/RunGraph и typed RPC → API → Control Center. Server
`resolveGate` проверяет actor/project authority, OCC и сохранённый
`allowed_decisions` до переходов. UI ограничивает действия этим списком;
payload decision и ref не являются источником полномочий.

Отдельной опубликованной aggregate-policy для `workflow.coordinator.initial`
нет. Прежний producer включал его Gate, если любой этап self-only workflow
требовал согласования. Он дублировал Gate этапа без exact step origin.
Координатор теперь не наследует этот флаг: каждый обязательный step Gate
сохраняется на собственном materialized node, включая этап самого координатора
в отдельной дочерней сессии. Это соответствует утверждённой
[матрице собственного этапа](assistant-prototype-debug-1763.md#матрица-жизненного-цикла-собственного-этапа-координатора).
Невыполненные planned steps не превращаются в согласованный результат.

| Сценарий | Источник и переход |
| --- | --- |
| Успешный gated child, включая self-step | Точная pinned revision + key + executing agent; Gate сохраняет список без расширения, сортировки или нового ограничения дубликатов. |
| Draft edit/новая публикация после launch | Старый root продолжает использовать прежний immutable version ID; новый список влияет только на новый launch. |
| Координатор без выполнения обязательного этапа | Нет aggregate Gate; прежний incomplete guard закрывает root как FAILED/RUNTIME_WORKFLOW_INCOMPLETE и planned nodes как CANCELLED. |
| Ошибочный completion/не-gated step | Новый Gate не создаётся. Неизвестное либо повреждённое происхождение gated step закрыто отклоняется с rollback owner-транзакции. |
| Исключённое решение/чужой actor/project/stale version | Отказ до mutation; нет новых gate/node/turn/receipt/audit/event/внешнего эффекта. |
| Разрешённые APPROVE/REJECT/CANCEL | Прежние gate/node/root transitions; one-winner OCC и actor boundary сохраняются. |
| Разрешённый REQUEST_CHANGES и exact replay | Прежний новый turn/attempt исходного этапа; повтор возвращает receipt, не создавая второй turn/event. Свежая RuntimeRevision сохраняет exact step origin. |
| Cancel/timeout/terminal root | Прежняя owner-транзакция закрывает граф и leases/gates; список решений не восстанавливает terminal работу. Поздний ResolveOwnerGate относится к отдельному #1873. |
| Retry/reclaim | Новый grant/revision проходит прежние lease/deadline проверки и читает pinned published version новой attempt; старый lease не создаёт Gate. |
| Agent/assistant/integration/delivery Gate | Обычные agent/assistant nodes не получают step flag. Отдельные integration/delivery producers сохраняют APPROVE/REJECT/CANCEL и свои policies. |

Миграций и переписывания исторических Gate/RuntimeRevision нет. При поставке
нужно дождаться terminal старых workflow executions: существующий coordinator
с ранее назначенным derived flag нельзя выдавать за новый exact step origin.
Уже открытые исторические Gate читаются и разрешаются по прежнему сохранённому
списку; завершённый воспроизводящий Gate не изменяется.

Проверки: bounded unit policy/SQL binding и новый
`TestWorkflowGateDecisionsComponent` со штатными реальными claims. Старые
остановленные PG suites и duplicate CompleteExecution не запускаются. После
отдельного допуска владельца ручная проверка: опубликовать этап с
APPROVE/REJECT/CANCEL, выполнить его, проверить отсутствие REQUEST_CHANGES в
API/UI и отказ такой команды; затем проверить self-step без лишнего Gate.
Local unit/component не являются staging, browser или полным PDLC PASS.

Rollback — предыдущий CP image без отката БД; он вернёт известное расширение
решений у новых Gate. Секреты, интеграционные grants и внешний эффект не
изменяются; глобальные runtime policies и существующие данные не расширяются.
