---
id: ARCH-MC-007
title: Runtime, сессии и запуски
type: architecture
status: approved
owner: architect
version: 1.2.1
updated: 2026-10-04
---

# Runtime, сессии и запуски

## Session и Turn

Session — долговечная последовательная история Agent. Источники создания:
`CONTROL_CENTER`, `SYSTEM_ASSISTANT`, `SCHEDULE`, `INTEGRATION`,
`AGENT_DELEGATION` и optional `MATTERMOST`. External conversation binding не
обязателен.

Turns выполняются FIFO. Enqueue использует semantic idempotency и не допускает
двух active turns одной Session. Continuation создаёт свежую RuntimeRevision и
новый role Pod, но сохраняет provider-neutral session history.

## Run и execution graph

Root Run содержит source, initiator, target, input, state, result, usage,
incidents, graph revision и next event sequence. RunNode представляет root
process, agent execution, Human Gate или bounded external action. RunEdge имеет
семантику `DELEGATED_TO`, `CALLBACK_TO`, `RETRY_OF`, `CONTINUES`, `WAITING_FOR`.

При запуске immutable WorkflowVersion control-plane сразу добавляет в graph
snapshot все будущие workflow nodes со state/materialization state `PLANNED` и
server-owned dependency edges. Делегирование материализует существующий node,
связывает child Run/Turn и переводит его в `QUEUED`, не создавая второй node или
второе planning edge. Cancel закрывает также ещё не материализованные nodes.

Tool calls остаются в timeline/detail node и не засоряют основной graph. Каждый
terminal tool-call projection содержит только tool name, bounded safe
parameters, capability/grant ref, state, duration, safe result и audit ref.
Raw MCP request/response, prompt, provider payload, file body и secret material
не входят ни в RunEvent, ни в outbox envelope. Frontend получает готовые
nodes/edges/state/nextActions от control-plane и не выводит causality или
terminal state локально.

## RuntimeRevision

Перед каждым turn/retry/continuation control-plane atomically pin-ит:

- exact Agent/Workflow/instruction versions;
- runtime configuration ref/version/digest с model и runtime profile;
- provider policy ref/version/digest, выбранный provider account и exact
  credential revision UID/resourceVersion/content digest;
- published `config.toml` overlay ref/version/digest и canonical content;
- runtime environment ref/version/digest, Agent binding ref/version/digest,
  non-secret values и Secret descriptors без values;
- promoted role image digest/runtime ABI;
- capability и integration grant revisions;
- knowledge/artifact versions;
- resource, network и timeout policy;
- root actor/policy/route and immutable input digest.

Mutation любой зависимости влияет только на следующий RuntimeRevision и не
изменяет уже выполняемую attempt.

### Устойчивый срок Workflow и этапа

Workflow `timeoutSeconds` и срок каждого опубликованного этапа измеряются
wall-clock от первого допустимого claim, назначенного control-plane по часам
PostgreSQL. Очередь до первого claim не входит в срок. После старта ожидание
дочерних результатов, Human Gate, continuation и reclaim входят в срок; часы
не приостанавливаются и не сбрасываются. Новая owner-authorized retry создаёт
новый Run со своими часами, но не продлевает старый Run.

Immutable RuntimeRevision содержит `workflow-wall-clock-v1`: exact Run и
WorkflowVersion refs/digest, step key, configured seconds, first-start/deadline
и минимум сроков принадлежащей серверу цепочки родителей. RunnerInput ABI9 и
role runtime contract3 передают те же pins runner и controller. Оба независимо
ограничивают выполнение абсолютным deadline; прежний часовой controller
fallback применяется только к Ordinary Agent без Workflow-часов.

| Переход | Авторитетный результат |
| --- | --- |
| First claim | DB clock назначает неизменяемые root/step часы из pinned published WorkflowVersion; очередь исключена. |
| Renew, delegate, native effect, credential/file read | Exact lease/fence/generation и текущая authority обязательны; истёкший срок закрыто отклоняется. Invocation lease не переживает срок Workflow. |
| Continuation, reclaim | Свежая RuntimeRevision сохраняет прежние clocks; controller keeper отменяет и joins до публикации Pod при expired/denied lease. |
| Human Gate/ожидание без lease | Claim poll или свежая owner gate command фиксирует expiry, не выдаёт новый lease и не возобновляет исполнение. |
| Expiry, late complete | Одна owner transaction фиксирует `FAILED/RUNTIME_TIMEOUT`, закрывает весь Run graph, leases, grants, gates и pending effects; уже начатый WRITE сохраняется как `UNKNOWN_OUTCOME`, а не повторяется. |
| Валидный late complete до закрытия lease | Consumption usage и подтверждённый archive tuple сохраняются; success verdict и артефакты не публикуются. Невалидный tuple не принимается. |
| Owner cancel/delete | Существующая eligibility/OCC и полный terminal/purge graph сохраняются; immutable clocks не переписываются. |
| Retry | Только существующая owner command, новая lineage/attempt/RuntimeRevision; старый terminal verdict и clocks неизменны. |

Terminal событие и version-pinned graph readback принадлежат control-plane.
Controller ограниченно cancel/join останавливает Pod, runner независимо
останавливает provider и сохраняет подтверждённые usage/archive pins. При
недоступном control-plane физическая отмена не ждёт его восстановления;
устойчивый terminal commit выполняется после восстановления owner-path.

Cutover forward-only: сначала owner-idle/quiesce, затем новая migration,
CP/controller/runner/schema/policy3 вместе. Историческим Run first-start не
выдумывается. Старые admitted образы не проходят новый exact contract;
владелец штатными ROLE_IMAGE и image-only ENV командами восстанавливает own
SYSTEM/PROJECT helper, затем помощники обновляют остальные ENV. Fresh OCC и
baseline preservation обязательны; новых maintenance permissions нет.

RuntimeRevision создаётся заново перед каждым turn, retry и continuation после
авторитетного чтения текущих published versions. Она не содержит «latest»
ссылок: runtime-controller получает точные refs, versions и digests всех
перечисленных зависимостей. Provider account фиксируется в Session и остаётся
неизменным между turns; новая credential revision указывается явно и не
подменяет account affinity.

## Provider Responses transport

Runtime proxy допускает provider WebSocket только с прежним server-owned
ProviderAccess, exact HTTPS origins/paths `api.openai.com:443/v1/responses`
и `chatgpt.com:443/backend-api/codex/responses`, методом GET и проверенным
RFC6455 handshake. Generic WebAccess не разрешает Upgrade. TLS/SNI/CA,
публичный DNS snapshot, destination policy и resource/lifecycle bounds не
заменяются предложенным extension.

Закреплённый Codex 0.160.0 предлагает
`permessage-deflate; client_max_window_bits`. Gateway принимает только
закрытый bounded token-only negotiation профиль
[GUIDE-DOC-003](../guides/distributed-security.md): один extension header
≤256 байт, один `permessage-deflate`, ≤4 уникальных known parameters,
window bits 9..15 и response, связанный с фактическим offer. Unknown/duplicate
extension или parameter, malformed value, unsolicited response и subprotocol
закрыто отклоняются до opaque stream. Отсутствующий response extension
допускает uncompressed stream. Gateway сохраняет headers и compressed frames
побайтно, не распаковывает payload и не делает их authority. Отказ negotiation
диагностируется только закрытой категорией, без headers/body/provider values.

## Delegation и callback

Coordinator использует типизированный MCP tool с target ref из server catalog.
Control-plane самостоятельно создаёт child Run/node/edge, наследует root actor и
policy и выдаёт opaque delegation ref. Workflow Run закрепляет immutable
WorkflowVersion; каталог координатора содержит только ещё не выполненные шаги
этой версии. Каждый child Run получает отдельную server-created Session, поэтому
его Turns не нарушают FIFO родительской Session и могут исполняться параллельно.
Child terminal result создаёт один FIFO callback Turn родительской Session;
explicit/fallback paths разделяют одну callback receipt. После последнего
ожидаемого callback control-plane создаёт coordinator continuation с новой
RuntimeRevision, а не просит frontend восстановить процесс.

## Human Gate

Gate сохраняется независимо от Pod. Active attempt может быть снята, пока Run
ожидает человека. Resolution в web либо optional adapter имеет one-winner OCC;
успех создаёт ровно один continuation с новой RuntimeRevision.

## Artifacts и история provider

Bounded inputs/results сохраняются через Artifact boundary и связываются с exact
Session/Turn/Run/node/attempt. Provider rollout/history capture имеет digest и
provenance и не доверяет caller-provided path. Runtime Pod не получает database
или broad storage credential.

## Cancel и retry

Cancel root Run одной owner-транзакцией закрывает queued/active turns, claims,
leases, grants, open Gates и non-terminal nodes и публикует ordered events.
Retry допустимой terminal attempt создаёт новую attempt, RuntimeRevision и
`RETRY_OF` edge; прежние result/errors остаются доступны.

## Realtime

Каждый graph change резервирует sequence и сохраняет RunEvent + outbox envelope
одной транзакцией. NATS JetStream доставляет at least once. Gateway отправляет
browser current snapshot, sequence и ordered deltas; reconnect использует
`afterSequence`, catch-up и fallback snapshot. Duplicate игнорируется, gap не
заполняется phantom state.

RunEvent имеет server-resolved actor (`USER`, `AGENT`, `SYSTEM_ASSISTANT`,
`PLATFORM`, `INTEGRATION`) и закрытый message kind. Presentation metadata Run
хранит server-owned title source и bounded activity summary. Runtime tool может
предложить title/activity, но не получает право менять lifecycle или выполнять
внешний effect. #997 фиксирует только generic typed projection; реализация
конкретного integration effect принадлежит отдельному adapter unit.

## Retention

Control-plane metadata, provider history manifest и Artifacts имеют явную
retention policy. Execution Pod и ephemeral workspace удаляются только после
terminal owner state и сохранённого bounded result/history. External channel
delete не инициирует core cleanup. До реализации session-archive unit #1002
session PVC нельзя удалить, если дальнейшее продолжение требует Codex JSONL.
S3 archive/restore и guarded удаление такого PVC относятся только к #1002.
