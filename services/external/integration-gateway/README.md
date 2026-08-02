# Integration gateway

`integration-gateway` — внешняя граница выполнения MCP, API и безопасной
материализации CLI/env integrations. Компонент реализуется в Issue
[#189](https://github.com/codex-k8s/matter-codex/issues/189).

Gateway не читает PostgreSQL, Redis или S3 других компонентов. Опасные
учётные данные доступны только provider adapter через файловый secret resolver;
они не возвращаются агенту, не попадают в `RuntimeRevision`, TOML, preview,
аудит, логи, ошибки или метрики.

## Владение данными

| Поле или состояние | Авторитетный владелец | Представление в gateway |
| --- | --- | --- |
| `IntegrationSpec` reference/version, разрешённые capabilities, ссылки на credential bindings, endpoint metadata | `control-plane` | закреплённый session snapshot с ID, версиями и digest; не редактируется API gateway |
| integration IDs роли и неизменяемая `RuntimeRevision` | `control-plane` | точная прочитанная версия session admission |
| YAML package, JSON Schema tools, risk, permission, approval и idempotency policy | `integration-gateway` | immutable `IntegrationDefinition(name, version, digest)` |
| разрешённое execution-состояние connection | `integration-gateway` | snapshot refs/digests/status, выведенный из exact control-plane context |
| project/role/session grants | `control-plane` | неизменяемая локальная проекция exact grant generation только для пересечения RuntimeRevision, IntegrationSpec и definition |
| invocation, approval, preview/hash, execution/result receipts | `integration-gateway` | PostgreSQL, одна owner transaction на каждый переход |
| session/turn continuation | `control-plane` | durable effect с зашифрованным application grant и version/fence; business state не копируется |
| transport session, quota, replay и request receipts | `integration-gateway` | PostgreSQL; SDK session не является `AgentSession` |
| secret values | Vault/Kubernetes Secret | только файл внутри gateway; PostgreSQL хранит ref, revision и digest metadata |

Gateway не создаёт вторую редактируемую копию desired integration metadata.
Изменение control-plane definition reference, endpoint, binding или revision
создаёт новый session snapshot/grant generation и закрывает прежние claims.

## Сквозные сценарии

### Discovery и вызов MCP

```text
agent-runner application grant (Bearer, exact process/session/thread/turn/attempt)
-> TLS + bounded HTTP admission
-> control-plane ResolveIntegrationSession через exact internal RPC profile
-> locked Session/Turn/RuntimeRevision/Role/Integration/CredentialBinding versions и digests
-> gateway definition digest + resolved connection revision
-> control-plane-owned grant generation и server-derived local grant IDs
-> durable MCP transport session + replay reservation + quota
-> ListTools содержит только пересечение разрешённых tools
-> CallTool validates exact input schema and size
-> risk/permission/grant check
-> canonical execution request + encrypted immutable arguments
-> closed redaction schema -> immutable preview
-> SHA-256 exact canonical request
-> invocation + approval + SUSPEND continuation effect одной PostgreSQL transaction
-> specialized control-plane SUSPEND/APPROVE/REJECT/EXPIRE/CANCEL
-> BEGIN подтверждается control-plane до provider effect
-> one-winner provider claim/fence
-> provider idempotency key derived from request hash
-> encrypted structured result + bounded receipt + audit
-> specialized control-plane COMPLETE/FAIL
-> version-pinned GetIntegrationContinuation/AcknowledgeIntegrationContinuation в agent-runner
```

Неизвестные definition/version/tool/risk/permission/state, чужая session или
binding и изменившийся request hash закрыто отклоняются. Опасный вызов без
binding, grant и exact approval не достигает provider adapter. Для одного
server-owned session/turn/attempt допускается только один open invocation:
конкурирующий второй вызов не может создать ещё одну continuation.

Для safe direct CLI/env mode отдельный MCP tool
`mattercodex-list-safe-delivery` возвращает только разрешённые имена,
описания и opaque references из session-scoped discovery. Descriptor разрешён
только для `READ` + `NEVER` tool без credential headers. Gateway не выдаёт
значения environment variables, аргументы CLI или credential values и не
материализует их в agent Pod/TOML/prompt.

### Согласование

1. Gateway сохраняет invocation, зашифрованные canonical arguments,
   неизменяемый redacted preview, request hash, `ApprovalRequest`, audit и
   idempotency receipt одной транзакцией.
2. MCP немедленно возвращает structured `pending`; HTTP request и Pod не
   удерживаются.
3. Decision principal выводится только из проверенного OIDC transport context.
   Payload содержит лишь opaque approval ID, decision и idempotency key.
4. Решение блокирует stored approval и invocation. Click, expiry, cancel и
   execute имеют одного победителя по PostgreSQL clock/OCC/fence.
5. `APPROVED` применимо только к сохранённому request hash. Любое изменение
   tool, версии, connection/grant generation или arguments создаёт новый
   invocation и approval.
6. До ответа MCP gateway одной транзакцией сохраняет encrypted application
   grant и `SUSPEND` effect. Continuation worker применяет специализированную
   команду с детерминированным idempotency key; ответ обязан совпасть по
   invocation, request digest, version, fence и закрытому набору состояний.

### Выполнение и неизвестный исход

1. Worker после startup barrier атомарно claim-ит только intent, для которого
   control-plane уже подтвердил `APPROVED/NOT_STARTED/SUSPENDED`.
2. Первая фаза фиксирует attempt, generation, fence и неизменяемый provider
   idempotency key, затем durable effect переводит continuation через `BEGIN`.
3. Provider call допускается только после подтверждённого
   `APPROVED/EXECUTING/SUSPENDED`; перед отправкой атомарно ставится
   `provider_dispatched_at`, поэтому crash после dispatch не повторяет effect.
4. Provider call выполняется вне PostgreSQL transaction с bounded deadline.
5. Transport retry с тем же semantic key возвращает сохранённый invocation и
   не создаёт новый provider call. После возможной отправки outcome становится
   `UNKNOWN`; автоматический второй attempt не создаётся даже для provider
   idempotency contract до отдельной bounded repair policy.
6. Terminal result фиксирует encrypted structured payload, safe code/digest,
   execution receipt и audit. Сырой provider body/headers/path не сохраняется.
7. Durable `SUCCEED|FAIL` effect закрывает continuation в control-plane. При
   terminal state `READY` agent-runner читает version-pinned result reference и
   подтверждает точную версию через защищённый read/rejoin path.
8. При graceful shutdown terminal запись использует отдельный bounded context
   от переданного `main` базового контекста. После аварийного restart
   PostgreSQL lifecycle переводит осиротевший `EXECUTING` старше одной минуты
   в terminal `UNKNOWN`; внешний effect повторно не запускается.

### Connection validation

Validation разрешается только для exact resolved connection revision. Adapter
получает credential values из файлов непосредственно перед bounded probe.
Наружу возвращаются только закрытые `OK|CREDENTIAL_UNAVAILABLE|UNAUTHORIZED|
FORBIDDEN|ENDPOINT_UNAVAILABLE|TIMEOUT|PROTOCOL_ERROR`, status, revision и
timestamps. URL path, DSN, token, headers и provider payload не возвращаются.
MCP tool `mattercodex-validate-connection` принимает только server-owned
`integration_id` из текущей RuntimeRevision, внутри разрешает opaque connection
snapshot ID и не позволяет проверить чужую binding. После успешной проверки
следующий `tools/list` включает разрешённое пересечение provider tools.

### MCP transport lifecycle

| Переход | Проверка | Результат |
| --- | --- | --- |
| initialize | valid session grant, exact control-plane snapshot, body/deadline/concurrency limits | SDK session ID связывается с durable transport row и grant generation |
| request | stored token digest/session tuple, non-expired state, replay key, quota and request lease | один request receipt; чужая binding — `403`, неверная credential — `401` |
| close | exact session credential and open row | только MCP transport становится `CLOSED` |
| idle expiry | PostgreSQL clock и last activity | `EXPIRED`, open request leases закрываются |
| request to closed/expired ID | credential остаётся валидной и принадлежит той же binding | MCP `404`, клиент инициализирует новый transport |

Очистка MCP transport никогда не закрывает `AgentSession`, Codex session,
turn, Pod, PVC или workspace.

### Credential rotation и отзыв

Каждый gateway connection ID выводится из exact RuntimeRevision, integration,
credential versions/digests и grant generation. Новая revision создаёт новый
изолированный snapshot с отдельным validation status и не изменяет connection
другой session. Перед provider dispatch control-plane повторно проверяет все
pins в `BEGIN`; отозванный snapshot закрыто отклоняется. Старый execution claim
не может воскресить отозванный grant/credential generation. Gateway не хранит
credential value или его обратимый digest.

### Authoritative continuation и rejoin

Gateway не вызывает generic `EnqueueTurn` и не принимает session, turn,
runtime revision или owner из payload. `ResolveIntegrationSession` выводит их
из проверенного application grant и состояния control-plane. Все локальные
session, grant, canonical request и invocation сохраняют точные process,
session/thread versions, turn, attempt, input digest, RuntimeRevision ID,
version/digest, runtime manifest digest, role/version и grant generation.

В транзакции создания invocation появляется durable continuation effect.
Worker использует только специализированные RPC control-plane и сохраняет
полученные `continuation_id/version/fence`. `SUSPEND`, решение, `BEGIN` и
terminal переходы имеют отдельные детерминированные idempotency keys и lease
fence. Caller не выбирает authority tuple. Application grant зашифрован
payload keyset; TTL transport session, grant, approval и invocation ограничен
его сроком с safety window. Истечение либо отзыв закрывают выполнение.

После `SUCCEEDED|FAILED|UNKNOWN|REJECTED|CANCELLED|EXPIRED` control-plane
переводит continuation в `READY`. Agent-runner использует защищённые
`GetIntegrationContinuation` и `AcknowledgeIntegrationContinuation` с exact
version, session, turn и runtime revision binding. Consumer adapter и durable
inbox agent-runner принадлежат отдельному owner unit #192 и не копируются в
gateway; AsyncAPI event для этого сценария намеренно отсутствует, потому что
авторитетным является version-pinned read/rejoin path.

### Internal RPC authority и deploy ownership

Deployment запускает workload-local `internal-rpc-authority-issuer` sidecar.
Закрытый профиль `integration-gateway` разрешает только issuer mode, exact
SPIFFE workload ID, отдельный Vault auth role, отдельные key/readback/restore
paths и capability `integration-gateway.CONTROL_PLANE_CLIENT`. Private key не
копируется в gateway container и не совпадает с identity другого gateway.
Publisher target, restore coordination, readback/restore NetworkPolicy и
forward-only PostgreSQL principals материализованы общей реализацией authority.

Gateway-owned manifests включают ServiceAccount, Service, Deployment,
migration Job, PDB, CSI/Vault delivery, default-deny/exact-destination
NetworkPolicy, ServiceMonitor/PodMonitor, dashboard и alerts. Migration Job
использует отдельные migrator DSN/context credentials. Runtime readiness
проверяет тот же PostgreSQL, control-plane authority и
`integration-egress-proxy` path, что рабочий вызов. Сам egress proxy является
отдельным инфраструктурным deployable: gateway имеет только exact mTLS client,
readiness и NetworkPolicy destination и не получает прямой provider egress.

## Lifecycle и authority matrix

| Aggregate / transition | Owner lock и authority | Idempotency / fence | Audit | Event или read path | Revocation |
| --- | --- | --- | --- | --- | --- |
| Definition create | startup loader; immutable name+version | canonical package digest; same version different digest conflict | loaded/rejected | list/get definition | supersede только новой version |
| Connection resolve | exact control-plane session snapshot and definition version | server-derived snapshot ID over runtime/integration/credential pins | resolved/changed | session discovery/read | new revision creates isolated PENDING snapshot |
| Connection validate | exact snapshot read; OIDC operator or bound MCP session | generation-checked OCC update after bounded probe | bounded validation code | owner API либо MCP validation result | expired/revoked snapshot rejects probe |
| Grant create | server derives project/role/session/runtime scope | unique capability tuple + generation | granted | session tool discovery | revoke/expiry closes new invocation |
| Grant revoke/expiry | control-plane state and exact local generation/TTL | CP version/fence + PostgreSQL clock | rejected/expired | grant/session/continuation read | `BEGIN` blocks stale pins; local new claims stop |
| Transport initialize | verified bearer + control-plane exact context | token digest/JTI reservation | initialized | MCP session/readiness | expiry/revoke closes transport |
| Transport close/expire | same credential binding or PostgreSQL clock | single terminal update | closed/expired | MCP 404/read | releases quota leases only |
| Invocation create + suspend | locked transport session + connection + exact authority/grant tuple | semantic key + exact canonical request hash; one local transaction | invoked/pending | MCP response/owner GetInvocation + CP continuation | revoked grant fails closed |
| Approval pending / `WAITING_OWNER` | invocation/hash lock + CP server-owned continuation | one approval and one SUSPEND receipt per hash | pending | CP GetContinuation exact version | expiry/rotation blocks execution |
| approve / `CHANGES_REQUESTED` | approval+invocation `FOR UPDATE`; verified OIDC actor | decision receipt + CP version/fence | exact outcome | get approval/invocation + CP read | reject maps to READY without execution; changed input requires new invocation |
| reject/expire/cancel | exact row locks; verified actor либо PostgreSQL clock | single-winner local transition + specialized CP command | terminal outcome | CP READY + protected rejoin | all paths close claims/grants |
| Execution begin | approved invocation + current connection/grant generation | attempt fence, then CP BEGIN version/fence | executing | CP SUSPENDED until result | provider blocked before CP confirmation |
| Provider dispatch | exact unfinished attempt and `provider_dispatched_at IS NULL` | one-winner dispatch marker + provider key | external effect/result | get invocation | crash after dispatch never repeats effect |
| transport retry | exact semantic key and request hash | возвращает тот же invocation/receipt; второго provider attempt нет | replayed receipt | invocation/read | иной hash конфликтует, terminal не открывается повторно |
| succeed/fail/unknown | exact active attempt token/fence | immutable result digest and receipt | terminal outcome | get invocation/result | terminal closes claims |
| continuation succeed/fail | exact attempt/result + CP version/fence | deterministic terminal command receipt | terminal outcome | CP READY, agent-runner Get/Ack read/rejoin | stale version/fence rejected |

PostgreSQL time is authoritative for expiry. Every unknown state/version/risk,
tool, permission or transition returns a closed failure before an external
effect.

Фоновые workers не выбирают произвольный tenant. Owner-only triggers атомарно
поддерживают минимальные `execution_work_scopes` и `lifecycle_work_scopes` без
business payload; runtime имеет только `EXECUTE` на selector. После получения
реально ожидающего scope каждая операция снова устанавливает одноразовый
подписанный transaction context и проходит `FORCE RLS`.

## API и состояние ошибок

- MCP endpoint: `/mcp/v1` через официальный `github.com/modelcontextprotocol/go-sdk`.
- Owner/operations OpenAPI: `contracts/openapi/integration-gateway/v1/openapi.yaml`.
- Cancel доступен только специализированной командой для project-authorized
  owner либо exact MCP transport session и только до `EXECUTING`.
- Health/metrics: `/livez`, `/readyz`, `/metrics` без business bypass.
- HTTP errors use the closed mapping in `contracts/errors/v1/rpc-http-mapping.yaml`.
- Diagnostics expose only bounded counters/status codes and timestamps.

## События

Continuation не публикует domain event: контракт #224 задаёт авторитетный
version-pinned RPC read/rejoin. Gateway хранит durable command effect, а
control-plane и agent-runner атомарно владеют state и inbox/Ack соответственно.
Для локальных invocation/result авторитетным остаётся PostgreSQL-backed
OpenAPI/MCP read path.

## Ручная проверка

1. Загрузить immutable definition package без secret values и проверить
   одинаковый digest при повторе; тот же version с иным content отклоняется.
2. Инициализировать MCP с неверной credential (`401`), чужой binding (`403`) и
   истёкшей session (`404` на известный ID).
3. Вызвать `mattercodex-validate-connection` для integration текущей revision,
   затем убедиться, что `tools/list` показывает только точные разрешённые tools.
4. Вызвать dangerous tool без grant и без approval; provider не вызывается.
5. Создать approval, изменить arguments и убедиться, что прежнее решение не
   подходит к новому request hash.
6. Одновременно подтвердить/истечь approval и одновременно запустить два
   workers; сохраняется один decision и один provider attempt.
7. Повторить semantic request после unknown outcome с тем же key и убедиться,
   что gateway возвращает тот же receipt без второго provider attempt.
8. До доставки `SUSPEND` отменить pending invocation из чужой MCP session
   (`403`), затем из исходной; после `SUSPEND` выполнить owner OIDC cancel.
   Approval и invocation должны завершиться `CANCELLED` одной транзакцией.
9. Перезапустить gateway между SUSPEND/decision/BEGIN/result и проверить, что
   effect возобновляется с тем же idempotency key/fence, а provider dispatch не
   повторяется после выставленного `provider_dispatched_at`.
10. Выполнить validation с неверным secret file и проверить только bounded code
   без path/value/provider response.
11. Проверить `/readyz` при отказе PostgreSQL, control-plane authority issuer и
    `integration-egress-proxy`; каждый alert должен вести на абсолютный HTTPS
    runbook.
12. Прочитать terminal continuation из agent-runner по exact version и
    подтвердить Ack; чужие session/turn/runtime revision и stale version должны
    закрыто отклоняться.

## Rollback

Rollback приложения выполняется возвратом предыдущего immutable image digest.
Схема только forward: применённые migrations не редактируются и не откатываются
`goose down`. Перед возвратом приложения новые claims останавливаются;
незавершённые invocations остаются в PostgreSQL и не выполняются вручную.
