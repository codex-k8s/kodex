---
id: DOM-MC-006
title: Оркестрация среды выполнения
type: domain
status: approved
owner: architect
version: 1.2.0
updated: 2026-10-04
---

# Оркестрация среды выполнения

## Владение

Control-plane владеет Session, Turn, Run, RunNode, RunEdge, RunEvent,
RuntimeRevision, execution task, claim/lease, callback receipt, Human Gate и
terminal outcome. Runtime-controller materialize-ит Kubernetes resources и не
вычисляет lifecycle из prompt, role name или external thread.

## Обычный turn

Перед каждым turn/retry/continuation control-plane создаёт immutable
RuntimeRevision из exact Agent/Workflow/instruction/model/role image/capability/
grant/knowledge versions и digests. Claim выдаётся только для FIFO head и exact
attempt/fence.

Runtime-controller создаёт новый execution-scoped Pod из exact promoted role
image. Protected init проверяет signed immutable input и materialize-ит bounded
config. `agent-runner` запускает provider process, передаёт progress, выполняет
разрешённые MCP calls, собирает безопасный result и завершает attempt через
typed RPC. Pod и credential generation закрываются после terminal transition.

## Ограниченная настройка workspace

Typed `PREPARE_RUNTIME_ENVIRONMENT_REVISION` принимает необязательный
`policy.resources.workspaceLimits: {maxBytes, maxFiles}`. При наличии объекта
оба целых поля обязательны: от 1 до platform maxima 1073741824 байт и 10000
файлов. Отсутствие объекта сохраняет прежние квоты и resource digest; отдельный
top-level alias, root, protected path, hostPath и shared mutable PVC не принимаются.

| Этап | Авторитетная граница |
| --- | --- |
| Managed MCP → ProposeAssistantPlan | Exact lease/source SYSTEM или PROJECT; прежние owner/context/version checks |
| Подготовка и owner confirmation | Closed policy input → own ENV snapshot и OCC → прежняя canonical draft command |
| Публикация | Immutable environment resource JSON и resourcesDigest включают exact budget; прежние receipt/audit/events |
| Fresh warm/turn/retry revision | CP выводит quota policy из опубликованных ресурсов, не из caller snapshot |
| Controller → runner | Typed Proto/resource JSON → bound RunnerInput; consumer требует совпадения quota и environment budget |
| Исполнение и handoff | Прежний execution-scoped `/workspace`, защищённые input/knowledge/context/auth paths, quota canary и result publication; durable handoff через Project Files/Artifacts/VFS |

Публикация влияет только на новые immutable revisions. Уже назначенный turn
не переписывается; terminal/cancel/expiry сохраняют прежний lifecycle и cleanup.
Для read/rejoin используется штатное чтение environment version/собственной
конфигурации; новое событие или отдельный cache не вводятся.

## Always-hot системный помощник

System assistant использует отдельный warm runtime contract, а не фиктивный
ready label. Bootstrap создаёт desired state и system Session; reconciler
materialize-ит exact system role image/revision с resource limits и heartbeat.
Idle не является active Turn, а turns исполняются последовательно. После
process/Pod restart warm state восстанавливается до положительной assistant
readiness. Prompt/runtime revision меняется controlled forward transition.

Terminal warm Pod не переиспользуется: reconciler удаляет его и materialize-ит
новый экземпляр. Immutable ticket от прежнего controller instance либо другой
prompt/runtime revision также заменяется до создания Pod, поэтому restart не
может привязать помощника к устаревшей authority или revision.

Warm Pod не получает database, Kubernetes или secret-store authority.
Конфигурационные действия идут через session-scoped typed MCP tools и
полномочия проверенного пользователя.

## Execution graph

Node types: root process, agent execution, Human Gate и bounded external action.
Edge types: `DELEGATED_TO`, `CALLBACK_TO`, `RETRY_OF`, `CONTINUES`,
`WAITING_FOR`. Tool calls показываются в node timeline и не становятся каждой
вершиной основного графа.

Каждый root Run резервирует непрерывный sequence в той же транзакции, что
изменяет graph state. RunEvent неизменяем и bounded; duplicate ID+digest
безопасен, иной digest конфликтен. Frontend получает authoritative snapshot,
sequence и ordered deltas.

## Переходы полного графа

- launch создаёт Session/Turn/Run/root node/RuntimeRevision/task/audit/outbox;
- claim/start связывает exact workload, method, attempt, input digest и fence;
- delegate наследует server-owned root actor/policy и создаёт child graph;
- каждый child получает отдельную Session, а callback допускается один раз для
  terminal child и живого route;
- последний ожидаемый callback создаёт свежий coordinator continuation Turn в
  родительской Session;
- cancel закрывает active claims, leases, grants, gates и nodes одной
  owner-транзакцией;
- retry terminal attempt создаёт новый task/grant/revision и lineage edge;
- lease expiry даёт bounded retry либо terminal failure/dead-letter.

## Readiness

`/healthz` сообщает только liveness процесса. `/readyz` читает локальный
рассчитанный snapshot прямых зависимостей: собственный sidecar, PostgreSQL,
broker, local storage/cache и Kubernetes API для controller. Probe не звонит в
соседний business service. Межсервисный граф проверяет отдельный diagnostic
smoke; рабочий RPC возвращает typed `Unavailable`.

Kubernetes API snapshot допускает bounded LKG только для краткой transport
ошибки. Signature corruption, rollback, revision conflict, key/grace expiry
закрывают admission сразу. Одинаковый outage и recovery логируются один раз как
state transition, а не повторяющимся warning.

## Критерии приёмки

- два normal turns одного Session не выполняются параллельно;
- normal Agent всегда запускается в собственном role image Pod;
- assistant first request использует реально обслуживаемый warm runtime;
- callback, cancel, retry и gate resolution сохраняют согласованный graph;
- Mattermost outage не влияет на runtime claim и terminal result.
