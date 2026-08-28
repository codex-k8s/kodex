---
id: ARCH-MC-007
title: Runtime, сессии и запуски
type: architecture
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-28
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

Tool calls остаются в timeline/detail node и не засоряют основной graph.
Frontend получает готовые nodes/edges/state/nextActions от control-plane и не
выводит causality или terminal state локально.

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

RuntimeRevision создаётся заново перед каждым turn, retry и continuation после
авторитетного чтения текущих published versions. Она не содержит «latest»
ссылок: runtime-controller получает точные refs, versions и digests всех
перечисленных зависимостей. Provider account фиксируется в Session и остаётся
неизменным между turns; новая credential revision указывается явно и не
подменяет account affinity.

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

## Retention

Control-plane metadata, provider history manifest и Artifacts имеют явную
retention policy. Execution Pod и ephemeral workspace удаляются только после
terminal owner state и сохранённого bounded result/history. External channel
delete не инициирует core cleanup. Long-term external archive backend относится
к отдельному storage adapter и не обязателен для fresh web-only MVP.
