# Integration gateway

`integration-gateway` — внешняя граница выполнения MCP, API и безопасной
материализации CLI/env integrations. Runtime исполнения реализован в Issue
[#189](https://github.com/codex-k8s/matter-codex/issues/189), а целевой owner
lifecycle provider/Git — в Issue
[#236](https://github.com/codex-k8s/matter-codex/issues/236).

Gateway не читает PostgreSQL, Redis или S3 других компонентов. Опасные
учётные данные доступны только provider adapter через version-pinned Vault
boundary либо уже принятый файловый resolver runtime #189;
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
| secret values | Vault/Kubernetes Secret | только JIT-read внутри exact adapter; PostgreSQL хранит ref, immutable version и content digest metadata |

Gateway не создаёт вторую редактируемую копию desired integration metadata.
Изменение control-plane definition reference, endpoint, binding или revision
создаёт новый session snapshot/grant generation и закрывает прежние claims.

До one-shot cutover Issue #196 новый provider-management state не обслуживает
пользовательский runtime и не синхронизируется с legacy-состоянием
`bot-service`. Это отдельная целевая schema без dual-write и compatibility
facade. После cutover `integration-gateway` является единственным владельцем
authorization attempt, immutable credential generation, provider observation,
Git source binding и integration configuration; `control-plane` остаётся
единственным владельцем desired provider pool и `RuntimeRevision` pins.

## Owner-management contract Issue #236

### Матрица сценариев

| Сценарий | Инициатор и transport | Команда unit | Авторитетное состояние и readback | Внешний effect |
| --- | --- | --- | --- | --- |
| Provider catalog | verified owner OIDC либо exact internal application proof | `ListProviders`, `GetProvider` | закрытый server registry с immutable version/digest, auth modes и capabilities | отсутствует |
| Device authorization | owner context; provider и idempotency intent из bounded request | `StartProviderAuthorization`, `GetProviderAuthorization`, `RestartProviderAuthorization`, `CancelProviderAuthorization` | durable server-owned attempt; code выдаётся только до `code_expires_at`, token не сохраняется в business tables | Codex app-server `account/login/start`, notification/read/cancel; completion передаётся только secret writer |
| Connection lifecycle | owner context и locator после tenant resolution | `List/Get/Reauthorize/RevokeProviderConnection` | immutable credential generation, opaque secret ref/content digest, masked account metadata, monotonic revoke generation | provider readback, signed `ProviderEffectReadbackReceipt`, атомарная материализация `CredentialBinding` + `ProviderConnectionReference`, затем typed Get/List readback обоих |
| Provider pool | owner context; connection IDs являются locators | `Create/Update/Archive/DeleteProviderPool`, `Get/ListProviderPool` | desired policy/weights принадлежит control-plane; gateway сохраняет только fresh bounded observations и exact refs; effective digest связывает обе версии | specialized control-plane RPC и version-pinned readback |
| Integration definition/configuration | owner context | `List/GetIntegrationDefinition`, `ConfigureIntegration`, `TestIntegrationConnection` | immutable closed definition registry и immutable typed configuration revision; arbitrary kind/schema/YAML/secret defaults запрещены | test проходит тот же credential/TLS/egress adapter path и сохраняет bounded receipt |
| Capability assignment | owner context; caller предлагает подмножество | `ConfigureIntegration` | пересечение definition capabilities, provider capabilities и control-plane assignment; runtime получает только следующий server-created `RuntimeRevision` | control-plane readback, без локальной выдачи grant |
| Approval | опасное invocation #189 создаёт request; owner context принимает решение | `List/Get/DecideIntegrationApproval` | существующие invocation/approval rows, exact request hash, immutable redacted preview, OCC/version и один terminal winner | существующий continuation worker выполняет `SUSPEND`, decision, `BEGIN`, terminal |
| Git source | owner context создаёт только allowlisted intent | `Create/Update/ArchiveGitSourceBinding`, `ReconcileGitSourceBinding` | server-owned repository connection, repository/ref/path allowlist, opaque credential ref, immutable fetched commit/revision/digest | gateway fetch/readback → signed `GitReconciliationReceipt` → exact `ReconcileGit*` → version-pinned readback |
| Diagnostics | owner context либо readiness principal | `GetIntegrationDiagnostics` | только status/ref/digest/generation/timestamp и closed error category | рабочие PostgreSQL/control-plane/secret/provider/Git paths; payload/header/token/path не возвращаются |

Owner management публикуется только internal Proto для будущего mapping #237.
Существующий OpenAPI относится к runtime #189 и не расширяется browser
командами. Browser API в `control-api-gateway` и PWA здесь не материализуются.

### Матрица authority

| Граница | Источник полномочий | Запрещённый источник | Повторная проверка |
| --- | --- | --- | --- |
| Owner HTTP | verified OIDC transport context | actor/org/project/owner из body/path | resource сначала разрешается внутри organization/project, затем OCC/idempotency |
| Internal gRPC | exact mTLS SPIFFE peer + application credential + full-method permission + signed authority proof | один mTLS peer либо arbitrary bearer | handler повторно связывает actor/tenant/project с owner state |
| Agent MCP | фактический #189 process/session/thread/turn/attempt/runtime revision/grant tuple | session, turn, attempt, capability или connection из payload | перед dispatch проверяются current grant и provider revoke generation |
| Device worker | DB-owned attempt/intent/lease/fence | provider/account scope из job payload | terminal transition под row lock и PostgreSQL time; cancel/revoke имеет приоритет |
| Provider adapter | exact connection generation и secret ref | secret value из API/DB/audit | secret читается just-in-time; revoke generation проверяется сразу до effect |
| Git worker | DB-owned Git source binding и immutable fetch intent | repository/ref/path/spec/source digest из reconcile payload | allowlist, fetched commit and content digest проверяются до подписи receipt |
| Provider receipt | purpose `AI_PROVIDER_READBACK_RECEIPT`, exact target/intent/JTI | Git purpose/JTI либо caller proof | control-plane verifier владеет durable one-use watermark |
| Git receipt | purpose `GIT_RECONCILIATION_RECEIPT`, exact source/target/intent/JTI | provider purpose/JTI либо OIDC caller | control-plane verifier владеет durable one-use watermark |

Create-команды назначают owner/root lineage сервером. ID connection, pool,
definition, approval, invocation и Git source являются только locators после
server-side owner resolution. Каждый control-plane RPC зарегистрирован по
exact full method, а readiness проверяет тот же путь.

### Матрица lifecycle

| Aggregate | Допустимые переходы | One-winner и terminal правило | Rejoin/read path |
| --- | --- | --- | --- |
| Device attempt | `PENDING -> CODE_ISSUED -> AUTHORIZED|DENIED|EXPIRED|FAILED|CANCELLED` | terminal immutable; restart/new code/reauthorize создают новую attempt; row lock + generation + PostgreSQL time | owner Get exact attempt/version; code скрывается после TTL/terminal |
| Connection | `PENDING -> VALID|INVALID|REVOKED`, `VALID|INVALID -> REVOKED`; reauthorize создаёт новый generation | `REVOKED` не оживает; revoke закрывает pending attempts, leases, eligibility и открытые pre-dispatch claims | owner Get/List и control-plane provider reference readback по exact version/digest |
| Provider observation | fresh `VALID|INVALID` с monotonic version/generation; stale/unknown не eligible | observation не меняет control-plane desired pool | effective read связывает pool version/digest и observation version/digest |
| Pool | create active; update fresh revision; archive только без live pins; delete только archived без refs | membership разрешается в owner boundary; `least_used|weighted` deterministic/bounded/overflow-safe | control-plane Get/List exact version plus gateway effective projection |
| Definition/configuration | definition immutable; configure создаёт новую revision; old revision остаётся pin-able | closed kind/effect/capability registry; arbitrary config rejected | catalog/configuration Get exact version/digest |
| Approval | `PENDING -> APPROVED|REJECTED|EXPIRED|CANCELLED` | decision/expiry/cancel race под row lock; late/replay decision fail closed | существующие Get/List invocation/approval и control-plane continuation readback |
| Test/diagnostic intent | `PENDING -> SUCCEEDED|FAILED|EXPIRED|CANCELLED` | bounded TTL, один terminal receipt, no secret material | owner exact receipt/timestamp/category |
| Git source binding | `ACTIVE -> ACTIVE(new revision)|ARCHIVED`; reconcile intent `PENDING -> FETCHED -> APPLIED|FAILED|CANCELLED` | update создаёт immutable binding revision; fetched commit/content snapshot immutable; ambiguous apply не подписывается повторно | binding/reconcile exact revision/digest + returned control-plane version |

### Матрица effects и транзакций

| Команда | Одна owner transaction | Effect вне transaction | Recovery / ambiguity |
| --- | --- | --- | --- |
| Start/restart auth | attempt + idempotency receipt + audit + durable provider intent | app-server start/poll | до `account/login/start` фиксируется durable dispatch fence; `loginId` живёт только в памяти private adapter, поэтому crash закрывает attempt без повторного provider call, а новый код требует новую attempt |
| Authorization complete | immutable generation metadata + opaque pending secret ref + audit + provider-reference intent | secret writer создаёт immutable secret и выполняет exact Vault readback; отдельный durable effect публикует provider reference | candidate остаётся `PENDING`, а active pointer, binding и eligibility меняются одной transaction только после exact control-plane Get/List readback; raw token не входит в receipt/audit/event |
| Revoke | connection revoke generation + terminal attempts/leases/claims + все credential generations + receipt + audit + revoke intent | однократный provider logout, уничтожение всех immutable Vault versions и control-plane archive/readback | durable provider/secret/control-plane checkpoints; ambiguous logout не повторяется, но effect не terminal до подтверждения обязательных Vault destroy/readback и control-plane archive/readback |
| Pool/config mutation | local observation/config revision + receipt + audit + control-plane intent | specialized generated control-plane call/readback | exact command digest/JTI; stale readback закрывает intent без local eligibility |
| Test | bounded intent + receipt reservation + audit | same adapter/TLS/egress/credential path | timeout/protocol ambiguity даёт closed safe category; no repeat после dispatch marker |
| Approval transition | существующие approval/invocation/receipt/audit/continuation effect rows | existing continuation worker | replay returns exact receipt; late decision rejected; no parallel approval model |
| Git binding update | immutable binding revision + receipt + audit + fetch intent | exact allowlisted fetch/readback | fetched commit/digest checkpointed; caller spec absent |
| Git reconcile | fetched snapshot + target intent digest + receipt reservation + durable RPC intent | signer + exact generated `ReconcileGit*` call и typed mutation readback | JTI one-use; ambiguous RPC закрывается `UNKNOWN/FAILED` без повторной подписи и без fake success |

External provider, secret, Git и control-plane calls никогда не выполняются
внутри PostgreSQL transaction. State-changing command атомарно сохраняет
business state, idempotency receipt, audit и обязательный effect intent.
Отдельные AsyncAPI events не добавляются: каждый новый aggregate имеет exact
version-pinned PostgreSQL/Proto read path, а control-plane mutations возвращают
авторитетный typed readback.

Provider-reference и четыре Git producer path вычисляют
`command_intent_sha256` до подписи одним helper из `libs/go/controlplaneapi`.
Canonical bytes содержат только verified actor/organization/project,
workload, exact full method/target и typed business intent. JTI/receipt,
signature/proof, signer/policy revision, `AuthorityDigest`, idempotency key и
сам `command_intent_sha256` исключены. Control-plane повторно вычисляет тот же
hash после проверки authority; producer-local digest не принимается.

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
-> control-plane materializes version-pinned continuation turn; result остаётся
   доступен через session-scoped MCP invocation read, без отдельного result bearer
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
7. Durable `SUCCEED|FAIL` effect закрывает continuation в control-plane.
   Control-plane материализует server-owned continuation turn; structured
   result остаётся доступен через обычный session-scoped MCP invocation read.
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
payload keyset и используется только для исходного admission и первого
`SUSPEND`. После появления `continuation_id` business approval живёт до
server-owned `approval/invocation` deadline независимо от пятиминутного bearer:
каждый следующий переход использует новый узкий `INTEGRATION_CONTINUATION_GRANT`
с exact version/fence. Caller не может продлить этот срок.

После `SUCCEEDED|FAILED|UNKNOWN|REJECTED|CANCELLED|EXPIRED` control-plane
переводит continuation в `READY` и сам владеет version-pinned rejoin.
Integration-gateway сохраняет invocation/result receipt и обслуживает его
только существующим session-scoped MCP read; отдельные
`ResolveIntegrationResult`/`AcknowledgeIntegrationResult`, result bearer и
локальная копия owner ACK удалены. AsyncAPI event для этого сценария
намеренно отсутствует: control-plane materialization/readback является
авторитетной границей, а gateway не принимает caller-selected continuation.

### Internal RPC authority и deploy ownership

Deployment запускает workload-local `internal-rpc-authority-issuer` для
исходящих owner-команд control-plane и verifier для защищённой internal
readiness-границы. Оба проверяют exact SPIFFE workload ID, snapshot/readback и
собственную устойчивую replay БД. Private keys не копируются в gateway
container. Удалённые result resolve/ack операции не остаются в allow-set,
binding или verifier policy.

Gateway-owned manifests включают ServiceAccount, Service, Deployment,
migration Job, PDB, CSI/Vault delivery, default-deny/exact-destination
NetworkPolicy, ServiceMonitor/PodMonitor, dashboard и alerts. Migration Job
использует отдельные migrator DSN/context credentials. Runtime readiness
проверяет тот же PostgreSQL, control-plane authority и
`integration-egress-proxy` path, что рабочий invocation. Unit материализует CNPG
`Cluster` с тремя экземплярами и точным `-rw` TLS endpoint, а также
двухрепличный Envoy runtime egress proxy с client-mTLS, закрытым route registry и без
прямого внешнего egress. Envoy не имеет `direct_response`: `/readyz`,
`/validate` и `/health` проходят upstream TLS/SNI/CA к отдельному
`provider-health-adapter`; validation и рабочий GET проверяют один Vault-owned
credential. Серверный cert/key proxy выдаются одним `VaultPKISecret` request.

Общий `deploy/k8s/base/vault-ca-delivery` принадлежит инфраструктурному
capability и из единственного trust-manager source доставляет namespace-local
overlap bundles `integration-gateway-vault-ca`,
`integration-egress-proxy-ca` и `provider-health-adapter-ca` без копирования CA
values. `Bundle` остаются cluster-scoped без `metadata.namespace`, а exact
namespace задаётся только их target selector; сам `mattercodex-system`
принадлежит environment bootstrap. Gateway base владеет цепочкой
`VaultConnection -> VaultAuth ->
VaultStaticSecret|VaultPKISecret -> generated Secret -> workload`. Общий
`tools/deploy/kubernetes-api-egress.sh` по `INFRA-DOC-001` является единственным
code-first владельцем фактических Service/default/kubernetes и ready IPv4
EndpointSlice destinations; manifests не содержат фиктивный apiserver selector.

Management provider authorization и Git fetch не создают второй proxy. Их
`HTTPS_PROXY` указывает точно на platform deployable
`http://egress-gateway.mattercodex-system.svc.cluster.local:8080`, а
`NO_PROXY=localhost,127.0.0.1,::1,.svc,.svc.cluster.local` оставляет
PostgreSQL, Vault и внутренние RPC внутри cluster boundary. Consumer
`NetworkPolicy` разрешает только Pod с labels
`app.kubernetes.io/name=egress-gateway` и
`app.kubernetes.io/component=platform-egress` в namespace
`mattercodex-system` на TCP/8080; прямой внешний TCP/443 и management port
9090 отсутствуют. Readiness использует compatibility `GET /readyz` на том же
8080. Exact FQDN registry принадлежит platform egress policy, не копируется в
этот deployable.

### Проверенные документы внешних библиотек

В исходной реализации через Context7 проверены актуальные документы:

- `/modelcontextprotocol/go-sdk/v1.2.0`: `StreamableHTTPHandler`, lifecycle
  сессий, tool registration и защита session binding;
- `/yaml/go-yaml`: `Decoder.KnownFields`, unique keys и строгая загрузка YAML;
- `/grpc/grpc-go`: server TLS credentials, unary interceptor, bounded message
  size и `Stop`/`GracefulStop` lifecycle.
- `/hashicorp/vault-secrets-operator`: workload-local `VaultAuth` через
  Kubernetes ServiceAccount, bounded token audience/TTL и связка
  `VaultStaticSecret.vaultAuthRef`.

Для текущего fix-пакета повторный Context7 lookup OpenAI Codex вернул
`Monthly quota exceeded`. Shape `account/rateLimits/read` (`usedPercent`,
`windowDurationMins`, `resetsAt`) и device authorization lifecycle сверены с
официальным `openai/codex` app-server README; локальный лимит не выдумывается,
а percentage observation хранится в точной шкале `0..100` и при отсутствии
fresh window закрыто исключается из eligibility.

В review cycle 2 квота Context7 была исчерпана. Поэтому дополнительно проверены
официальные HashiCorp VSO API (`VaultConnection.caCertSecretRef`,
`VaultPKISecret`, destination transformation/rollout restart), Vault CSI
`vaultCACertPath`/exact SNI, cert-manager trust-manager `Bundle`, CloudNativePG
operator→instance-manager TCP/8000 и Envoy upstream TLS/active health check.

Реализация сохраняет внешний MCP SDK за admission boundary, строго разбирает
YAML до семантического использования и поднимает mTLS gRPC listener до запуска
workers.

## Contract map исправлений review cycles 1–2

Эта карта задаёт целевую границу до изменения runtime-кода. Ни один ID из
request не является authority: transport peer подтверждается mTLS, actor и
lineage приходят из проверенного signed context либо разрешаются владельцем
состояния под блокировкой.

| Finding / сценарий | Actor и источник authority | Transport / команда | Авторитетный owner и transaction | Version, idempotency и result | Terminal / revoke / readiness |
| --- | --- | --- | --- | --- | --- |
| 1. Первичный suspend | `integration-gateway`: короткий `AGENT_SESSION_GRANT` служит только application credential, а workload-local issuer выпускает свежий exact-method authority proof на каждую попытку | `SuspendForIntegrationApproval` | control-plane повторно разрешает source session/turn/attempt/input/runtime revision, блокирует полный runtime graph и создаёт continuation | invocation/request/bindings pinned; receipt сохраняется с transition; handoff имеет 32 попытки и terminal dead-letter | ответ содержит свежий server-owned continuation grant; истёкший source bearer не вызывается повторно и не ограничивает business approval после handoff |
| 1. Решение, execution и terminal | `INTEGRATION_CONTINUATION_GRANT`, подписанный control-plane и связанный с continuation/version/fence/source lineage/разрешёнными следующими методами | только специализированные `Approve|Reject|Cancel|Expire|Begin|Complete|Fail` | control-plane повторно разрешает continuation и owner graph до receipt | каждый успешный переход увеличивает version/fence и выдаёт новый узкий grant; старый grant становится replay-only для того же receipt | READY/REJOINED, cancel, expiry и retry отзывают прежний grant; readiness проверяет signer/trust и отсутствие stranded delivery |
| 2. Continuation result | exact session-scoped MCP actor после server-owned materialization control-plane | существующий MCP invocation read; отдельного result RPC/bearer нет | gateway владеет immutable invocation/result receipt, control-plane — continuation/rejoin | session, invocation и grant generation разрешаются сервером; caller не выбирает continuation | stale/чужая session закрыто отклоняется; internal readiness не выдаёт result authority |
| 3. Входной HTTPS | закрытый registry peer SPIFFE: `agent-runner|control-api-gateway` для MCP и утверждённые owner API peers | TLS 1.3 + `RequireAndVerifyClientCert` + bearer/OIDC | gateway transport admission до handler | certificate chain и exact URI проверяются независимо от bearer | client CA/SPIFFE mismatch закрывает запрос; startup до workers загружает TLS profile и pre-bind-ит рабочий listener |
| 4. Provider outcome | gateway execution worker после durable dispatch marker | exact mTLS egress-proxy adapter operation | gateway attempt/result transaction | `FAILED` только для доказанного adapter `NO_EFFECT`; 5xx, protocol/schema mismatch, timeout и ambiguous response → `UNKNOWN` | UNKNOWN не повторяет provider effect и даёт terminal FAIL с safe digest; метрика учитывает состоявшийся dispatch |
| 5. PostgreSQL credential rotation | migration controller identity; intent не является источником current state | forward-only CLI reconcile/readback | PostgreSQL verifier-owned lifecycle state + principal rows в одной transaction | durable high-watermark; только `NEXT -> CURRENT -> PREVIOUS -> RETIRED`, promotion после exact NEXT LOGIN/readback | stale/skip/backward intent отклоняется; retire закрывает login/membership/backends; readiness сверяет served generation |
| 6. Immutable execution pins | source invocation, созданная из control-plane snapshot | локальный claim/dispatch | gateway versioned connection snapshot + pinned definition/grant/credential tuple | join по exact connection ID/revision/generation и immutable payload digest; current eligibility проверяется отдельно | mismatch до dispatch атомарно terminalizes graph без provider call |
| 7. Authority change | server-derived newer connection/grant state | reconciliation transaction | gateway блокирует connection/grant, open invocation, approval, attempt и continuation effect | один winner сохраняет audit и exact `CANCEL|EXPIRE|FAIL` effect; lease/fence сбрасывается монотонно | work scopes/claims закрываются; effect остаётся claimable до CP READY, crash recovery идемпотентен |
| 8. Environment render | repository-owned definition source внутри base load root | обычный `kubectl kustomize` | integration-gateway base и два overlays | один канонический source, без unsafe load restrictor и копии | staging/production render обязаны собираться до review |
| 9. Data/provider deployables | отдельные service accounts и workload identities | PostgreSQL Service/workload TLS; egress-proxy mTLS ingress/upstream + закрытый exact route registry | каждый component имеет собственные manifests/config/secret names/readiness/failure policy | pinned image, exact SNI/CA/destination; provider-health adapter проверяет credential и не имеет egress | gateway readiness закрыта до Envoy active upstream health; overlays включают полный ownership |
| 10. Startup | composition root | pre-bound public/technical/internal listeners | gateway владеет listener lifecycle и worker group | bind всех sockets завершается до readiness и polling | partial bind закрывает уже созданные listeners; workers join до DB/client/telemetry shutdown |
| 11. Rollback | repository cleanup base передан из `main` через composition root | bounded independent rollback context | pgx transaction connection | rollback error объединяется с исходной ошибкой; successful commit не откатывается | отменённый request context не используется; cleanup timeout закрыт и наблюдаем |
| 12. Tool collision | server-owned startup loader | strict YAML parser + staged catalog | gateway materializes definitions только после проверки полного набора | exposed name уникален между всеми versions/definitions и не может иметь namespace `mattercodex-*` | любой collision закрывает startup до session/tool registry |
| 14. Signer rotation | server-owned signer + verifier-delivered public keyset | ES256 `kid`/generation | verifier PostgreSQL fence | revision/high-watermark/served digest только вперёд; PREVIOUS имеет bounded overlap | NEXT/RETIRED/unknown/rollback закрыто отвергаются; readiness проходит durable readback |
| 15. Vault/API trust path | VSO/trust-manager/CNPG owners, не application caller | exact TLS SNI/CA и generated exact NetworkPolicy | CA source, VaultConnection status и discovered Kubernetes API endpoints | CA overlap bundle и Service/EndpointSlice readback | отсутствие CA, Vault Secret, ready endpoint либо server validation блокирует rollout/apply |

Полный граф continuation закрыт:

| Состояние | Допустимая authority | Следующие команды | Отзыв прежнего состояния |
| --- | --- | --- | --- |
| invocation создана, CP ещё не suspended | свежий workload-local exact-method proof, построенный из ещё действующего source application credential, только для `SUSPEND` | `SUSPEND` | expiry/32 неудачные попытки атомарно terminalize локальный graph; истёкший bearer не используется |
| `PENDING / NOT_STARTED / SUSPENDED` | свежий grant exact version/fence с `APPROVE|REJECT|CANCEL|EXPIRE` | одна terminal decision либо approve | успешный переход делает предыдущий grant stale; reject/cancel/expire → READY |
| `APPROVED / NOT_STARTED / SUSPENDED` | свежий grant с `BEGIN|CANCEL|EXPIRE` | один winner | begin закрывает decision grant; cancel/expiry закрывают attempt и scopes |
| `APPROVED / EXECUTING / SUSPENDED` | свежий grant с `COMPLETE|FAIL` | ровно один terminal | provider dispatch marker не откатывается; ambiguity → FAIL/UNKNOWN без repeat |
| `READY` | server-owned continuation turn | session-scoped MCP invocation read и control-plane owner rejoin | transition grants отозваны; caller-selected result authority отсутствует |
| `REJOINED` | нет mutation grant | идемпотентный readback ACK receipt | cleanup разрешается только owner-side после полного graph check |

## Lifecycle и authority matrix

| Aggregate / transition | Owner lock и authority | Idempotency / fence | Audit | Event или read path | Revocation |
| --- | --- | --- | --- | --- | --- |
| Definition create | startup loader; immutable name+version | canonical package digest; same version different digest conflict | loaded/rejected | list/get definition | supersede только новой version |
| Connection resolve | exact control-plane session snapshot and definition version | server-derived snapshot ID over runtime/integration/credential pins | resolved/changed | session discovery/read | new revision creates isolated PENDING snapshot |
| Connection validate | exact snapshot read; OIDC operator or bound MCP session | generation-checked OCC update after bounded probe | bounded validation code | owner API либо MCP validation result | expired/revoked snapshot rejects probe |
| Grant create | server derives project/role/session/runtime scope | unique capability tuple + generation | granted | session tool discovery | revoke/expiry closes new invocation |
| Grant revoke/expiry | control-plane state and exact local transport generation/TTL | CP version/fence + PostgreSQL clock | rejected/expired | grant/session/continuation read | expiry закрывает initial handoff/new invocation; после SUSPEND durable continuation grant, а не source bearer, владеет переходом |
| Transport initialize | verified bearer + control-plane exact context | token digest/JTI reservation | initialized | MCP session/readiness | expiry/revoke closes transport |
| Transport close/expire | same credential binding or PostgreSQL clock | single terminal update | closed/expired | MCP 404/read | releases quota leases only |
| Invocation create + suspend | locked transport session + connection + exact authority/grant tuple | semantic key + exact canonical request hash; one local transaction | invoked/pending | MCP response/owner GetInvocation + CP continuation | revoked grant fails closed |
| Approval pending / `WAITING_OWNER` | invocation/hash lock + CP server-owned continuation | one approval and one SUSPEND receipt per hash | pending | CP GetContinuation exact version | expiry/rotation blocks execution |
| approve / `CHANGES_REQUESTED` | approval+invocation `FOR UPDATE`; verified OIDC actor | decision receipt + CP version/fence | exact outcome | get approval/invocation + CP read | reject maps to READY without execution; changed input requires new invocation |
| reject/expire/cancel | exact row locks; verified actor либо PostgreSQL clock | single-winner local transition + specialized CP command | terminal outcome | CP READY + protected rejoin | all paths close claims/grants |
| Execution begin | approved invocation + current connection/grant generation | attempt fence, then CP BEGIN version/fence | executing | CP SUSPENDED until result | provider blocked before CP confirmation |
| Provider dispatch | exact unfinished attempt and `provider_dispatched_at IS NULL` после local decrypt/credential resolution | one-winner dispatch marker + provider key | external effect/result | get invocation | pre-dispatch failure terminal `FAILED/NO_EFFECT`; crash/ambiguity после marker → `UNKNOWN` без repeat |
| transport retry | exact semantic key and request hash | возвращает тот же invocation/receipt; второго provider attempt нет | replayed receipt | invocation/read | иной hash конфликтует, terminal не открывается повторно |
| succeed/fail/unknown | exact active attempt token/fence | immutable result/error digest and delivery version/fence | terminal outcome | session-scoped MCP invocation read | terminal closes claims; чужая session отклоняется |
| continuation succeed/fail | exact attempt/result + CP version/fence | deterministic terminal command receipt | terminal outcome | CP READY и server-owned materialization/readback | stale version/fence rejected |

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
control-plane materialization/readback. Gateway хранит durable command effect,
а для локальных invocation/result авторитетным остаётся PostgreSQL-backed
OpenAPI/MCP read path. Отдельного gateway result ACK и result bearer нет.

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
12. Прочитать terminal invocation из continuation turn через session-scoped
    MCP; чужая session и stale grant generation должны закрыто отклоняться.
13. Через internal gRPC с exact owner proof прочитать provider catalog, начать
    device authorization и проверить, что `verification_url`/`user_code`
    исчезают после `code_expires_at`; в логах и PostgreSQL raw code/token нет.
14. Завершить device authorization и проверить immutable Vault version/content
    digest, затем exact provider-reference Get/List readback. При остановке Pod
    после provider completion повторная provider authorization не запускается.
15. Выполнить reauthorize и убедиться, что появилась новая immutable generation,
    а pinned runtime #189 продолжает ссылаться на прежнюю. Затем revoke должен
    закрыть pending attempt, выполнить `account/logout`, уничтожить exact Vault
    version и исключить connection из новых pool snapshots.
16. Создать `least_used` и `weighted` pools из принадлежащих project
    connections; чужой locator и stale observation должны быть отклонены, а
    control-plane readback обязан совпасть по version/projection digest.
17. Настроить closed integration definition, выполнить test и проверить только
    safe category/timestamp/receipt. Header, payload, path и credential value в
    ответе, audit, логе и метриках отсутствуют.
18. Создать Git binding из allowlisted repository/ref/path, выполнить reconcile
    и сверить immutable commit/source revision/digest, semantic intent и typed
    readback одного из четырёх `ReconcileGit*`. Изменённый request с прежним JTI
    и stale receipt должны быть отклонены.

## Rollback

Rollback приложения выполняется возвратом предыдущего immutable image digest.
Схема только forward: применённые migrations не редактируются и не откатываются
`goose down`. Перед возвратом приложения новые claims останавливаются;
незавершённые invocations остаются в PostgreSQL и не выполняются вручную.
