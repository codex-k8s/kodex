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

Deployment запускает workload-local `internal-rpc-authority-issuer` и
`internal-rpc-authority-verifier`. Issuer обслуживает только исходящие команды
control-plane, verifier — только входящий `IntegrationResultService`; оба
проверяют exact SPIFFE workload ID, snapshot/readback и собственную устойчивую
replay БД. Private keys не копируются в gateway container: result resolver
получает лишь публичный JWK server-owned grant через отдельный CSI volume.

Gateway-owned manifests включают ServiceAccount, Service, Deployment,
migration Job, PDB, CSI/Vault delivery, default-deny/exact-destination
NetworkPolicy, ServiceMonitor/PodMonitor, dashboard и alerts. Migration Job
использует отдельные migrator DSN/context credentials. Runtime readiness
проверяет тот же PostgreSQL, control-plane authority и
`integration-egress-proxy` path, что рабочий вызов. Unit материализует CNPG
`Cluster` с тремя экземплярами и точным `-rw` TLS endpoint, а также
двухрепличный Envoy egress proxy с client-mTLS, закрытым route registry и без
прямого внешнего egress. Базовый `provider-health` adapter обслуживается
локальным exact `/health` route; добавление реального provider upstream требует
отдельного утверждённого route и exact NetworkPolicy destination.

### Проверенные документы внешних библиотек

Через Context7 проверены актуальные документы:

- `/modelcontextprotocol/go-sdk/v1.2.0`: `StreamableHTTPHandler`, lifecycle
  сессий, tool registration и защита session binding;
- `/yaml/go-yaml`: `Decoder.KnownFields`, unique keys и строгая загрузка YAML;
- `/grpc/grpc-go`: server TLS credentials, unary interceptor, bounded message
  size и `Stop`/`GracefulStop` lifecycle.
- `/hashicorp/vault-secrets-operator`: workload-local `VaultAuth` через
  Kubernetes ServiceAccount, bounded token audience/TTL и связка
  `VaultStaticSecret.vaultAuthRef`.

Реализация сохраняет внешний MCP SDK за admission boundary, строго разбирает
YAML до семантического использования и поднимает mTLS gRPC listener до запуска
workers.

## Contract map исправлений review cycle 1

Эта карта задаёт целевую границу до изменения runtime-кода. Ни один ID из
request не является authority: transport peer подтверждается mTLS, actor и
lineage приходят из проверенного signed context либо разрешаются владельцем
состояния под блокировкой.

| Finding / сценарий | Actor и источник authority | Transport / команда | Авторитетный owner и transaction | Version, idempotency и result | Terminal / revoke / readiness |
| --- | --- | --- | --- | --- | --- |
| 1. Первичный suspend | `integration-gateway` с исходным `AGENT_SESSION_GRANT`, exact source session/turn/attempt/input/runtime revision | `SuspendForIntegrationApproval` | control-plane блокирует полный runtime graph и создаёт continuation | invocation/request/bindings pinned; receipt сохраняется с transition | ответ содержит свежий server-owned continuation grant; исходный bearer больше не используется |
| 1. Решение, execution и terminal | `INTEGRATION_CONTINUATION_GRANT`, подписанный control-plane и связанный с continuation/version/fence/source lineage/разрешёнными следующими методами | только специализированные `Approve|Reject|Cancel|Expire|Begin|Complete|Fail` | control-plane повторно разрешает continuation и owner graph до receipt | каждый успешный переход увеличивает version/fence и выдаёт новый узкий grant; старый grant становится replay-only для того же receipt | READY/REJOINED, cancel, expiry и retry отзывают прежний grant; readiness проверяет signer/trust и отсутствие stranded delivery |
| 2. Result read | будущий `agent-runner` с exact internal-RPC signed context плюс control-plane-owned result access grant для нового continuation turn | `IntegrationResultService.ResolveIntegrationResult` | integration-gateway владеет invocation/attempt/result и durable delivery row | request pin-ит invocation, attempt, result digest, continuation version/fence; response несёт immutable structured JSON и те же pins | чужой/stale turn/runtime revision/grant закрыто отклоняется; endpoint readiness проходит тот же mTLS/verifier/read path |
| 2. Result ack | тот же continuation actor и result access grant | `AcknowledgeIntegrationResult` | gateway одной transaction сохраняет receipt и ACK version | stable idempotency key + expected delivery version/fence/digest | повтор возвращает тот же ACK; terminal/revoke закрывает access grant, но не удаляет result |
| 3. Входной HTTPS | закрытый registry peer SPIFFE: `agent-runner|control-api-gateway` для MCP и утверждённые owner API peers | TLS 1.3 + `RequireAndVerifyClientCert` + bearer/OIDC | gateway transport admission до handler | certificate chain и exact URI проверяются независимо от bearer | client CA/SPIFFE mismatch закрывает запрос; startup до workers загружает TLS profile и pre-bind-ит рабочий listener, а защищённый result readiness проходит отдельный mTLS/verifier/PostgreSQL path |
| 4. Provider outcome | gateway execution worker после durable dispatch marker | exact mTLS egress-proxy adapter operation | gateway attempt/result transaction | `FAILED` только для доказанного adapter `NO_EFFECT`; 5xx, protocol/schema mismatch, timeout и ambiguous response → `UNKNOWN` | UNKNOWN не повторяет provider effect и даёт terminal FAIL с safe digest; метрика учитывает состоявшийся dispatch |
| 5. PostgreSQL credential rotation | migration controller identity; intent не является источником current state | forward-only CLI reconcile/readback | PostgreSQL verifier-owned lifecycle state + principal rows в одной transaction | durable high-watermark; только `NEXT -> CURRENT -> PREVIOUS -> RETIRED`, promotion после exact NEXT LOGIN/readback | stale/skip/backward intent отклоняется; retire закрывает login/membership/backends; readiness сверяет served generation |
| 6. Immutable execution pins | source invocation, созданная из control-plane snapshot | локальный claim/dispatch | gateway versioned connection snapshot + pinned definition/grant/credential tuple | join по exact connection ID/revision/generation и immutable payload digest; current eligibility проверяется отдельно | mismatch до dispatch атомарно terminalizes graph без provider call |
| 7. Authority change | server-derived newer connection/grant state | reconciliation transaction | gateway блокирует connection/grant, open invocation, approval, attempt и continuation effect | один winner сохраняет audit и exact `CANCEL|EXPIRE|FAIL` effect; lease/fence сбрасывается монотонно | work scopes/claims закрываются; effect остаётся claimable до CP READY, crash recovery идемпотентен |
| 8. Environment render | repository-owned definition source внутри base load root | обычный `kubectl kustomize` | integration-gateway base и два overlays | один канонический source, без unsafe load restrictor и копии | staging/production render обязаны собираться до review |
| 9. Data/provider deployables | отдельные service accounts и workload identities | PostgreSQL Service/workload TLS; egress-proxy mTLS ingress + закрытый exact route registry | каждый component имеет собственные manifests/config/secret names/readiness/failure policy | pinned image, exact SNI/CA/destination и code-first apply/readback metadata; базовый adapter не имеет внешнего egress | gateway readiness закрыта до обоих paths; overlays включают их ownership, rollback сохраняет data; реальный provider upstream добавляется только отдельным approved route |
| 10. Startup | composition root | pre-bound public/technical/internal listeners | gateway владеет listener lifecycle и worker group | bind всех sockets завершается до readiness и polling | partial bind закрывает уже созданные listeners; workers join до DB/client/telemetry shutdown |
| 11. Rollback | repository cleanup base передан из `main` через composition root | bounded independent rollback context | pgx transaction connection | rollback error объединяется с исходной ошибкой; successful commit не откатывается | отменённый request context не используется; cleanup timeout закрыт и наблюдаем |

Полный граф continuation закрыт:

| Состояние | Допустимая authority | Следующие команды | Отзыв прежнего состояния |
| --- | --- | --- | --- |
| invocation создана, CP ещё не suspended | исходный source grant только для `SUSPEND` | `SUSPEND` | локальный cancel/expiry сохраняет desired terminal и сначала завершает SUSPEND |
| `PENDING / NOT_STARTED / SUSPENDED` | свежий grant exact version/fence с `APPROVE|REJECT|CANCEL|EXPIRE` | одна terminal decision либо approve | успешный переход делает предыдущий grant stale; reject/cancel/expire → READY |
| `APPROVED / NOT_STARTED / SUSPENDED` | свежий grant с `BEGIN|CANCEL|EXPIRE` | один winner | begin закрывает decision grant; cancel/expiry закрывают attempt и scopes |
| `APPROVED / EXECUTING / SUSPENDED` | свежий grant с `COMPLETE|FAIL` | ровно один terminal | provider dispatch marker не откатывается; ambiguity → FAIL/UNKNOWN без repeat |
| `READY` | result access grant нового continuation turn | result read и ACK | transition grants отозваны; stale result grants отклоняются по CP/gateway version/fence |
| `REJOINED` | нет mutation grant | идемпотентный readback ACK receipt | cleanup разрешается только owner-side после полного graph check |

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
