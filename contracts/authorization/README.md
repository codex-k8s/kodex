---
id: CONTRACT-MC-004
title: Доверенный контекст внутренних RPC
type: contract
status: approved
owner: architect
version: 1.2.0
updated: 2026-07-29
---

# Доверенный контекст внутренних RPC

Документ фиксирует contract milestone `internal-rpc-authority` из GitHub Issue
#186 и является обязательной основой зависимого unit #187. Контракт описывает
полный security path, но не объявляет готовыми runtime binaries, PostgreSQL
migrations, OCI image или Kubernetes manifests полного unit.

## Источники и границы

Источники требований:

- Issue #179 — переход на полный unit и заморозка legacy;
- Epic #180 — порядок `internal-rpc-authority` → `control-plane`;
- Issue #186 — workload-local issuer/verifier, ES256, replay, rotation и
  fail-closed readiness;
- `PRD-MC-001`, `PRD-MC-005`, `ARCH-MC-001`, `ARCH-MC-002`,
  `ARCH-MC-004`;
- `CONTRACT-DOC-002`, `GUIDE-DOC-003`, `GO-DOC-001`, `GO-DOC-002`,
  `GO-DOC-003`, `GO-DOC-005`, `GO-DOC-006`.

`internal-rpc-authority` владеет:

- выпуском короткоживущего signed context;
- строгой проверкой JWS/JWKS и machine policy;
- lifecycle signing keys и signed snapshot;
- persistent replay reservation и verifier high-watermark;
- безопасным UDS protocol и его readiness.

Он не владеет пользователями, бизнесовыми ролями, организациями, проектами,
агрегатами или business permissions. Окончательную проверку ownership и
доменного состояния выполняет target domain service после общей transport
boundary.

Ни поле request, ни обычный client token, ни идентификатор из payload, ни mTLS
по отдельности не являются authority. Request к issuer переносит только
`operation_id`, signed authority proof и correlation ID. Actor, tenant,
project и provenance разрешает авторитетный owner/resolver и подписывает в
proof, который связан с exact caller workload/SPIFFE, operation, downstream
audience, freshness и revision. Issuer закрыто проверяет proof до выпуска
authorization context; синтаксически корректный tuple из request никогда не
подписывается как authority.

## Нормативные артефакты

| Артефакт | Назначение |
| --- | --- |
| `contracts/proto/internalrpcauthority/v1/authority.proto` | UDS issuer/verifier, preflight resolver, restore controller RPC и typed error detail |
| `contracts/authorization/v1/jws-protected-header.schema.json` | закрытый protected header |
| `contracts/authorization/v1/authority-proof.schema.json` | signed actor/tenant/project authority proof |
| `contracts/authorization/v1/authorization-context.schema.json` | canonical authorization claims |
| `contracts/authorization/v1/authority-snapshot.schema.json` | signed JWKS + machine policy snapshot |
| `contracts/authorization/v1/restore-fence-evidence.schema.json` | внешнее монотонное PITR evidence |
| `contracts/authorization/v1/restore-coordination.schema.json` | role credential, QUIESCING directive и one-time ACK claims |
| `contracts/authorization/v1/postgresql-readback-boundary.sql` | исполняемый exact PostgreSQL privilege/RLS/function contract |
| `contracts/authorization/v1/key-delivery-targets.schema.json` | exact `(workload,role)` key/trust/database-identity fan-out |
| `contracts/authorization/v1/authorization-error-matrix.json` | полная reason/code/stage/retryable/message matrix |
| `contracts/authorization/v1/bootstrap-deny-all-policy.json` | безопасное начальное состояние без business bindings |
| `contracts/authorization/v1/bootstrap-key-delivery-targets.json` | безопасное начальное состояние без key delivery targets |
| `contracts/authorization/v1/fixtures` | RFC 8785 golden и negative contract fixtures |
| `contracts/registry.yaml` | owner, source, generated path и consumers |
| `buf.yaml`, `buf.gen.yaml` | lint/build и воспроизводимый Go codegen |
| `libs/go/internalrpcauth/gen/internalrpcauthority/v1` | сгенерированный Go wire package |
| `deploy/k8s/base/internal-rpc-authority/capability-registry.yaml` | deploy ownership, identity, volumes, key delivery и readiness |

JSON Schema описывает semantic model. На wire header и payload сериализуются
по JSON Canonicalization Scheme RFC 8785 в UTF-8 без BOM. Verifier сначала
ограничивает размер и структуру, затем выполняет strict parse, сравнивает
исходные bytes с повторной canonical serialization и только после этого
использует значения.

## Сквозная карта сценариев

### Выпуск и downstream RPC

| Этап | Точный контракт |
| --- | --- |
| Инициатор | Service-specific client adapter после проверки внешней аутентификации либо чтения авторитетного domain/process/session state |
| Source requirement | #186, `ARCH-MC-002`, `GO-DOC-005`, `GUIDE-DOC-003` |
| Actor/tenant/project authority | Signed authority proof от exact `authority_proof_issuer`; каждый ID и provenance связан с immutable `reference`, положительной safe-integer `revision` и SHA-256 digest |
| Локальный caller | Application container, UID/GID `10001:10001`, workload и SPIFFE ID из pod identity; private pod `emptyDir` ограничивает область UDS |
| Issuer endpoint | `unix:///run/mattercodex/internal-rpc-authority/issuer.sock`; `/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext` |
| Issuer binding | `SO_PEERCRED` обязан совпасть с UID/GID policy; loaded workload SVID обязан совпасть с `caller_workload_id`/`caller_spiffe_id` operation binding |
| Authority proof | Issuer проверяет strict compact JWS, exact proof signer/trust generation, caller workload/SPIFFE, `operation_id`, proof audience, downstream audience, 15-second lifetime, proof revision/digest watermark и one-time proof JTI |
| Policy binding | Request выбирает только `operation_id`; issuer server-side выводит issuer, audience, target workload/SPIFFE, exact TLS server name/trust bundle, full RPC, permission, TTL и snapshot revisions, а authority копирует только из проверенного proof |
| ES256 JWS | Issuer создает новый UUID `jti`, canonical claims, compact JWS и signing key exact `(iss,kid)` со статусом `CURRENT` |
| Downstream transport | Client adapter открывает mTLS с exact server name/SPIFFE и доверенной CA; metadata содержит ровно одно `x-mattercodex-internal-authorization: Bearer <compact-jws>` |
| Target | Target gRPC interceptor получает фактический full method и проверенного mTLS peer из transport, а не из business request |
| Результат | Только после успешной локальной verifier проверки target adapter получает neutral `VerifiedAuthorizationContext` |
| Domain owner | Domain service заново разрешает resource внутри tenant/project boundary и проверяет business state/ownership |
| Consumers | Target transport adapter, domain service и единая observability boundary; raw token не передаётся дальше adapter |

Issuer не принимает actor/tenant/project/provenance fields, requested
audience, RPC, target workload, permission, `iat`, `nbf`, `exp`, snapshot
revision или `kid`. Их отсутствие в Proto является частью security contract.
Поле `authority_proof_compact_jws` переносит credential, но не становится
authority до полной криптографической проверки и one-time reservation.

### Авторитетное разрешение actor/tenant/project

Authority proof выпускает только versioned resolver доменного владельца.
Первый достижимый путь не зависит от уже выпущенного internal context:

```text
control-api-gateway
  -- mTLS exact gateway SPIFFE + OIDC bearer metadata -->
/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof
  -- server-side subject -> actor -> tenant -> project/ownership -->
signed authority proof
  -- local UDS SO_PEERCRED -->
/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext
  -- mTLS + internal context -->
/controlplane.v1.ProjectService/GetProject
```

`AuthorityProofResolverService` реализует `control-plane` как business owner;
#186 владеет wire schema, proof verification, key/trust delivery и machine
policy. Gateway не выбирает business identity. Resolver request содержит
только `operation_id`, неавторитетный `resource_reference`, idempotency key и
correlation ID. Actor, tenant, project, ownership, provenance, caller SPIFFE,
audience и permission в request отсутствуют. OIDC credential находится только
в metadata `authorization`; interceptor сначала проверяет exact gateway mTLS,
затем resolver проверяет signature/issuer/audience/time credential, получает
subject и разрешает subject→actor→tenant→resource внутри состояния
`control-plane`.

Для каждой operation binding snapshot задаёт один
`authority_proof_producer_id`. Producer profile фиксирует exact resolver
SPIFFE/full method/TLS server/trust bundle, тип и metadata application
credential, proof issuer/audience/trust, 15-second max age, authority sources,
allowed operation IDs, deadline 2000 ms и не более двух попыток только для
`Unavailable`/`DeadlineExceeded`. Повтор использует тот же idempotency key.
Receipt scoped по digest проверенного credential subject, caller workload,
operation и idempotency key; другой semantic request возвращает
`AlreadyExists/IDEMPOTENCY_CONFLICT`.
После двух одинаковых попыток `Unavailable`/`DeadlineExceeded` client adapter
возвращает bounded `Unavailable/AUTHORITY_PROOF_UNAVAILABLE`; другие коды не
повторяются.

Credential обычного клиента доказывает только subject. Resolver подписывает
proof лишь после положительного server-side membership/ownership resolution.
Даже trusted signer не может связать actor tenant A с tenant/project B:
проверка закрывается `PermissionDenied/AUTHORITY_SCOPE_MISMATCH` до подписи.
Неизвестный или скрытый ресурс имеет одинаковый `NotFound` domain outcome с
ровно одним `AuthorizationErrorDetail`: reason
`AUTHORITY_RESOURCE_NOT_FOUND`, stage `AUTHORITY_RESOLUTION`, retryable
`false` и сообщением `authority resource not found`. Missing и cross-tenant
hidden resource неразличимы по status/detail. First-call и negative contracts
находятся в `fixtures/authority-proof-first-call.json`,
`fixtures/authority-proof-negative.json` и
`fixtures/authority-resolution-negative.json`.

Proof имеет `typ=mattercodex-internal-rpc-authority-proof+jws`, проверяется
только ключом exact issuer/trust generation и содержит semantic model
`authority-proof.schema.json`:

- exact caller workload ID и SPIFFE ID;
- exact `operation_id`;
- downstream `authorization_context_audience`;
- actor kind и подписанные actor/tenant/project identities;
- для каждой identity — authority source, immutable reference, positive
  safe-integer revision и SHA-256 digest;
- positive proof revision, signer generation, canonical UUID JTI и
  `iat == nbf`, `exp == iat + 15`.

Issuer использует trusted database time и до подписи выполняет один закрытый
путь:

1. проверяет размер, compact shape, strict protected header, canonical payload
   и schema;
2. выбирает proof verification key только по operation binding и независимо
   доставленному proof trust bundle;
3. проверяет certificate/key generation и signature;
4. связывает proof с фактическими UDS UID/GID, загруженным workload SVID,
   `operation_id`, proof audience и downstream audience из той же binding;
5. проверяет project presence и каждый authority source по closed allowlist;
6. в одной PostgreSQL transaction сравнивает
   `(proof_revision, canonical_payload_digest)` с issuer-owned watermark и
   резервирует `(caller_workload_id, proof_jti)`;
7. только после commit без преобразования значений переносит authority в
   authorization claims и Proto response.

Lower revision, same-revision mutation, reused JTI, неизвестный signer,
просроченный proof, несовпавший workload/operation/audience и недоступное
proof trust/persistence состояние закрыто отклоняются. Произвольный
синтаксически корректный `CallerAuthority` без proof получает
`InvalidArgument/AUTHORITY_PROOF_REQUIRED`. Полный набор обязательных
негативных случаев находится в
`fixtures/authority-proof-negative.json`.

Bootstrap policy имеет одновременно пустые `authority_proof_producers` и
`operation_bindings`; поэтому ни resolver preflight, ни business RPC не
разрешён до атомарной публикации связанной policy. Каждый authority source
operation обязан принадлежать её единственному producer profile, а каждая
operation — его `allowed_operation_ids`; неиспользуемый producer либо source
отклоняет snapshot.

### Проверка и one-time reservation

| Этап | Точный контракт |
| --- | --- |
| Инициатор | Target gRPC interceptor после обязательной mTLS-проверки и извлечения exact full method |
| Локальный caller | Target application container UID/GID `10001:10001`; verifier сверяет `SO_PEERCRED` и local target workload identity |
| Verifier endpoint | `unix:///run/mattercodex/internal-rpc-authority/verifier.sock`; `/internalrpcauthority.v1.AuthorizationVerifierService/VerifyAuthorizationContext` |
| Вход | Compact JWS, `observed_full_method`, mTLS peer SPIFFE/certificate SHA-256 и correlation UUID; эти поля доверяются только в сочетании с UDS peer binding |
| Snapshot | Фактически обслуживаемый immutable snapshot содержит manifest signer generation, JWKS current/next/previous и exact operation policy |
| Проверка | Размер → compact shape → protected header → strict canonical claims → key `(iss,kid)` → ES256 signature → time/revision → exact audience/workloads/RPC/permission → mTLS peer |
| Reservation | В одной PostgreSQL transaction verifier проверяет restore fence/high-watermarks и вставляет уникальную `(target_workload_id,jti)` reservation с token digest |
| Multi-replica | Unique constraint и compare-and-swap дают ровно одного победителя; другая replica получает `REPLAY_DETECTED` |
| Ответ | Neutral claims возвращаются только после commit reservation |
| Ошибка | Любая ошибка до commit не разрешает domain handler; недоступность PostgreSQL закрывается `Unavailable` |

Reservation выполняется перед domain handler. Поэтому retry одного downstream
RPC всегда получает новый context/JTI. Бизнесовая command при unknown outcome
повторяется с тем же domain idempotency key, но с новым authorization context.

### Попытка только с mTLS или только с token

| Попытка | Исход |
| --- | --- |
| Есть mTLS, нет metadata | Target возвращает `Unauthenticated/AUTHORIZATION_CONTEXT_REQUIRED`; domain handler и verifier не вызываются |
| Есть metadata, нет проверенного mTLS | Target возвращает `Unauthenticated/MTLS_REQUIRED`; token не компенсирует transport |
| mTLS peer не равен `caller.spiffe_id` | `PermissionDenied/MTLS_PEER_MISMATCH`; reservation не создаётся |
| Верный token передан в другой RPC | `PermissionDenied/RPC_MISMATCH`; reservation не создаётся |
| Верный token передан другой replica target workload | Разрешено только при том же registered target workload/SPIFFE и общей persistent reservation; повтор JTI отклоняется |

### Ротация и exact readback

| Этап | Владелец и переход |
| --- | --- |
| Intent | Publisher под PostgreSQL lease CAS-фиксирует один immutable rotation intent с exact generation, digest и union обязательных `(workload,role)` targets из всех operation bindings и producer profiles |
| Подготовка | Publisher генерирует auth/proof key как `NEXT`; private parts и trust overlap пишет через Vault KV v2 CAS только в exact role paths зарегистрированных targets; wildcard/list/delete запрещены |
| Доставка | Caller `AUTHORIZATION_ISSUER` получает auth private key, manifest trust и proof trust; target `AUTHORIZATION_VERIFIER` получает только snapshot/manifest trust; `AUTHORITY_PROOF_RESOLVER` получает proof private key и trust. Target-only workload допустим и auth private key не получает |
| Cryptographic readback | Каждая обязательная `(workload,role)` identity проверяет только свой private→public/trust material; exact readback фиксируется отдельным workload/role-bound PostgreSQL login principal, publisher требует ровно одну строку на каждую требуемую роль |
| Публикация | Только после всех delivery readbacks publisher строит следующий signed snapshot с `source_revision + 1`, применимым key/policy/signer counter, predecessor и bounded history |
| Kubernetes CAS | Publisher может `get/update/patch` только заранее созданный Secret `internal-rpc-authority-snapshot`; `create/delete` запрещены |
| Readback publisher | После CAS publisher перечитывает exact Secret, проверяет compact JWS, signer certificate, canonical payload и digest; только затем финализирует revision в PostgreSQL |
| Readback consumers | Issuer, verifier и proof resolver независимо проверяют относящийся к роли served state; затем server-derived DB mapping фиксирует exact `(workload,role,workload_generation,revision,digest,key_generation,credential_generation)` readback |
| Readiness | Application UID вызывает оба реальных UDS `CheckReadiness`; readiness положительна только при совпадении фактически обслуживаемого digest, publisher-finalized revision и persistent store |
| Promotion | Не ранее 40 секунд после readback `NEXT` становится `CURRENT`, прежний current — `PREVIOUS`, а новый ключ создается как `NEXT` |
| Retirement | `PREVIOUS` удаляется отдельной revision не ранее ещё одного окна 40 секунд после подтвержденной promotion |
| Consumers | Issuers подписывают только exact `CURRENT`; verifiers принимают listed `CURRENT/NEXT/PREVIOUS` только для exact issuer; target workloads получают atomic snapshot |

Promotion выполняется одной `SERIALIZABLE` PostgreSQL transaction: publisher
блокирует pinned rotation intent `FOR UPDATE`, читает exact expected set обеих
consumer readback tables, проверяет revision/digest/generation каждого target
и атомарно обновляет intent, append-only snapshot history и
`CURRENT/NEXT/PREVIOUS`. Publisher не пишет readback rows и не исполняет
readback functions; неполный set откатывает всю transaction.

Окно 40 секунд равно TTL 30 секунд плюс двум допустимым clock-skew окнам по
5 секунд. Сокращать его runtime-настройкой запрещено.

Crash recovery всегда продолжает тот же intent/generation/digest после Vault
metadata CAS, consumer cryptographic readbacks, Kubernetes snapshot CAS и
publisher readback. До однозначного завершения либо repair текущего intent
publisher не создает новую generation. Порядок
`prepare → deliver → cryptographic readback → publish → promote` одинаков для
auth key и manifest signer; для signer новый public certificate сначала
доставляется всем требуемым issuer/verifier/resolver ролям в overlap bundle, и
лишь затем новая snapshot revision подписывается новым private signer. Proof
signer проходит тот же `CURRENT/NEXT/PREVIOUS` overlap: resolver подтверждает
private→public, caller issuer — proof trust, и promotion запрещена без обеих
role-bound readback строк.

### Restart, пропущенные revisions и PITR

| Сценарий | Закрытое поведение |
| --- | --- |
| Issuer/verifier restart | `emptyDir` сохраняет только sockets; snapshot перечитывается, а watermark/replay state — из PostgreSQL до readiness |
| Пустой локальный snapshot | Не разрешает принять revision ниже persistent watermark |
| Пропуск до 32 revisions | Новый signed snapshot принимается, только если точная anchor `(revision,digest)` присутствует в упорядоченной signed history |
| Пропуск более 32 revisions | `SNAPSHOT_HISTORY_GAP`, readiness false; применяются архивные signed snapshots последовательно через `internal-rpc-authority-cli snapshot rejoin --bundle-file <path> --expected-target <workload-id> --expected-anchor <revision:digest>` |
| Same revision, другой payload | `SNAPSHOT_MUTATION`; snapshot и watermark не меняются |
| Более низкая revision | `SNAPSHOT_ROLLBACK`; snapshot и watermark не меняются |
| Crash до Kubernetes CAS | Новая revision не видима; прежний snapshot остаётся current |
| Crash после CAS до DB finalize | На restart publisher exact readback совпадающей revision/digest идемпотентно завершает finalize; другой digest блокирует readiness |
| Crash после verifier CAS до pointer swap | На restart pointer строится из persistent watermark и того же signed snapshot; более старый snapshot не обслуживается |
| PITR replay database | `internal-rpc-authority-restore-controller` сначала закрывает каждый issuance/reservation path и собирает ack всех текущих workload/role generations; только затем semantic-CAS публикует `PREPARED`. После restore публикует `COMPLETED`, recovery step фиксирует DB fence; все роли unready до exact external readback и 40-second safe window |
| PITR publisher database | Publisher не переиспользует revision; exact Kubernetes readback и persistent history должны доказать следующий номер/digest, иначе публикация блокируется |

Удалять или обнулять watermark/replay rows для recovery запрещено. Rejoin
применяет только последовательно проверенные signed snapshots. Restore fence
принадлежит recovery job, не caller и не application pod. Сам fence в
восстановленной PostgreSQL не является достаточным доказательством: текущий
signed external anchor `internal-rpc-authority-restore-evidence` хранится в
Kubernetes, принадлежит deployable
`internal-rpc-authority-restore-controller` из артефакта #186 и не входит в
PITR authority database.

Нормативный порядок restore закрыт:

```text
controller OPEN -> QUIESCING
-> every active (workload,role,generation) stops issuance/reservation,
   drains inflight and acks through exact mTLS identity
-> signed external PREPARED anchor
-> restore-operator Job verifies PREPARED readback and performs exact DB restore
-> signed external COMPLETED evidence
-> recovery step exact read + signature/predecessor/semantic-anchor checks
-> atomic database fence CAS
-> database-clock safe window 40 seconds
-> every workload/role external-anchor cryptographic readback
-> application readiness
```

`PREPARED` содержит revision и digest реестра ожидаемых
`(workload,role,generation)`, digest полного ack set и равные
`expected_ack_count`/`accepted_ack_count`. Missing ack, stale generation,
неостановленный рабочий issuance/reservation path или ошибка signer/controller
запрещают `PREPARED`. Пропуск шага, stale/missing evidence, mismatch external
`(restore_epoch,anchor_revision,digest)` с database fence, ошибка Job либо
неистекшее окно держат traffic закрытым. Startup issuer/verifier зависит от
завершенной recovery capability, а каждый переход readiness повторно сверяет
external anchor; restored database row не может самостоятельно открыть
traffic.

Kubernetes `resourceVersion` используется только как lost-update CAS и не
является semantic high-watermark. Forward-only anchor задают signed
`anchor_revision`, `restore_epoch` и exact predecessor revision/digest.
`ValidatingAdmissionPolicy`
`internal-rpc-authority-restore-anchor-forward-only` с `failurePolicy: Fail`
и exact binding разрешает только `old.anchor_revision + 1`, неизбывающий epoch
и predecessor, равный фактически обслуживаемому old digest; lower,
same-revision mutation, delete и запись не controller ServiceAccount
отклоняются. Одновременная потеря authority DB и внешнего Kubernetes anchor
не восстанавливается автоматически и оставляет traffic в quarantine до
отдельной disaster-recovery процедуры владельца.

Чтобы admission был исполняем без разбора JWS внутри CEL, controller атомарно
проецирует revision/epoch/evidence digest/predecessor в пять exact
`mattercodex.dev/restore-*` annotations того же Secret. Policy сравнивает
`oldObject`/`object` annotations. После API write controller и каждый consumer
перечитывают Secret, проверяют JWS и требуют полного равенства annotations
подписанным claims и SHA-256 compact JWS; annotation сама по себе authority не
является.

## UDS ownership и lifecycle

### Канонические endpoints

| Назначение | Path | Listener | Client | Owner | Mode |
| --- | --- | --- | --- | --- | --- |
| Issuer | `/run/mattercodex/internal-rpc-authority/issuer.sock` | issuer sidecar UID `29001` | application UID `10001` | `29001:29000` | `0660` |
| Verifier | `/run/mattercodex/internal-rpc-authority/verifier.sock` | verifier sidecar UID `29002` | application UID `10001` | `29002:29000` | `0660` |
| Parent | `/run/mattercodex/internal-rpc-authority` | socket init UID `29000` | все три процесса | `29000:29000` | `1770` |

Pod задает `fsGroup: 29000` и
`fsGroupChangePolicy: OnRootMismatch`. Application имеет
`runAsUser/runAsGroup: 10001/10001`; issuer — `29001/29001`; verifier —
`29002/29002`; init — `29000/29000`. UID/GID не переопределяются environment
overlay. Sticky bit parent не позволяет application удалить или заменить
socket, принадлежащий sidecar. Запись application в parent не является
authority: созданный им path имеет неверного owner и блокирует startup либо
restart до пересоздания pod.

Оба sockets находятся в pod-private `emptyDir`
`internal-rpc-authority-sockets`, `medium: Filesystem`, `sizeLimit: 8Mi`.
`hostPath`, PVC и общий между pod volume запрещены. `emptyDir` допустим только
для sockets; replay/high-watermark там не хранятся.

### Создание и stale socket

1. Init container через `openat2`/эквивалент с запретом symlink проверяет mount
   root, создает parent, задает exact owner/mode и завершает работу.
2. Sidecar выполняет `lstat` canonical path. Symlink, directory, regular file,
   device, неверные owner/mode или path вне проверенного parent блокируют
   startup.
3. Существующий socket удаляется только если owner UID/GID/mode совпадают,
   connect подтверждает отсутствие listener и повторный `lstat` показывает тот
   же device/inode. Иначе startup закрыто отклоняется.
4. Sidecar ставит `umask 0007`, bind на уникальный socket в том же parent,
   через `fchownat` задает exact owner и shared GID `29000`, через `fchmodat`
   задает mode `0660`, проверяет type/owner/mode, начинает listen и атомарно
   публикует canonical path через `renameat2(RENAME_NOREPLACE)`.
5. Application не считается ready, пока вызов обоих `CheckReadiness` от UID
   `10001` не прошел тот же `SO_PEERCRED`, snapshot и persistence path.

Path permissions не заменяют peer binding. Каждый RPC проверяет Linux
`SO_PEERCRED` и exact local workload identity из загруженной policy. PID не
используется как стабильная identity между container PID namespaces.

### Restart и shutdown

- При sidecar restart pod volume остается; stale socket проходит алгоритм
  выше.
- Перед shutdown application и sidecars переводят readiness в false.
- gRPC servers прекращают accept, ограниченно ждут in-flight RPC, закрывают
  listener.
- Canonical socket удаляется только при совпадении сохраненного device/inode;
  чужой replacement не удаляется.
- PostgreSQL закрывается после join verifier workers.
- Удаление pod уничтожает только socket `emptyDir`, но не security state.

## Proto и wire validation

Proto package:

```text
internalrpcauthority.v1
```

Сервисы и exact methods:

```text
/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext
/internalrpcauthority.v1.AuthorizationIssuerService/CheckReadiness
/internalrpcauthority.v1.AuthorizationVerifierService/VerifyAuthorizationContext
/internalrpcauthority.v1.AuthorizationVerifierService/CheckReadiness
/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof
/internalrpcauthority.v1.AuthorityProofResolverService/CheckReadiness
/internalrpcauthority.v1.RestoreControllerService/PrepareRestore
/internalrpcauthority.v1.RestoreControllerService/GetRestoreDirective
/internalrpcauthority.v1.RestoreControllerService/AcknowledgeQuiescence
/internalrpcauthority.v1.RestoreControllerService/CompleteRestore
/internalrpcauthority.v1.RestoreControllerService/CheckReadiness
```

Source расположен в `contracts/proto/internalrpcauthority/v1`. Go package
генерируется в `libs/go/internalrpcauth/gen/internalrpcauthority/v1`. Field
numbers и names не переиспользуются; удаление резервирует оба.

### Ограничения request

| Значение | Ограничение |
| --- | --- |
| Decoded Proto message | не более 16 KiB |
| `operation_id` | 3–128 bytes, lowercase dotted/hyphenated identifier |
| Resolver `resource_reference` | 1–256 bytes, operation-specific lookup key; не authority |
| Idempotency key | canonical lowercase UUID, 36 bytes |
| ID/reference/JTI/correlation | canonical lowercase UUID, 36 bytes |
| SHA-256 digest | ровно 64 lowercase hex |
| SPIFFE ID | 12–512 bytes, absolute `spiffe://` |
| Full RPC | 5–256 bytes, canonical `/package.Service/Method` |
| Compact JWS | не более 8192 bytes |
| Decoded protected header | не более 512 bytes |
| Decoded claims | не более 6144 bytes, JSON depth не более 8 |
| ES256 signature | ровно 64 raw bytes, 86 unpadded base64url chars |

Unknown Proto fields, invalid wire type, duplicate singular fields, duplicate
map keys и повтор одного message field отклоняются до generated unmarshal.
Обычная last-one-wins семантика Protobuf для этой boundary запрещена. Enum
`UNSPECIFIED`, неизвестные enum numbers, невалидный timestamp и отсутствующий
required semantic field дают `InvalidArgument`.

UDS использует только binary Proto. ProtoJSON на UDS запрещен. CLI/import JSON
проходит те же правила unknown/duplicate/null до преобразования в Proto.
Resolver и restore controller также используют binary Proto поверх mTLS.
Application credential resolver находится только в bounded transport metadata
и не сериализуется в Proto. `ResolveAuthorityProofRequest` содержит ровно
`operation_id`, `resource_reference`, `idempotency_key`, `correlation_id`;
actor/tenant/project/ownership/SPIFFE/audience/permission/token/internal
context fields запрещены. Restore ack не принимает workload/role: controller
выводит их из проверенного mTLS peer и сопоставляет request generation с
активным registry.

### Нормативный caster Proto → proof/policy → JWS → verified Proto

Compiled descriptor `IssueAuthorizationContextRequest` обязан содержать ровно:

| Field | Number | Proto kind | Presence и источник |
| --- | ---: | --- | --- |
| `operation_id` | 1 | `string` | semantic required; выбирает exact operation binding |
| `correlation_id` | 3 | `string` | semantic required canonical UUID; используется только для detail/observability и не подписывается |
| `authority_proof_compact_jws` | 4 | `string` | semantic required; strict proof credential, не готовая authority |

Number `2` и name `authority` зарезервированы. Добавление в request полей
`actor`, `tenant`, `project`, `provenance`, `audience`, `full_method`,
`permission`, timestamps, snapshot revisions, `kid` или любых их aliases
запрещено. Contract test читает compiled descriptor, а mutation fixtures
добавляют каждый запрещенный field с допустимыми number/type и обязаны
получать failure.

Полная semantic mapping:

| Authorization claims | Источник до подписи | `VerifiedAuthorizationContext` |
| --- | --- | --- |
| `v` | constant `1` | `contract_version` uint32 `1` |
| `iss` | operation binding `issuer`; совпадает с loaded issuer SVID | `issuer` |
| `aud` | operation binding `audience`; совпадает с proof `authorization_context_audience` | `audience` |
| `sub` | operation binding `caller_spiffe_id`; совпадает с proof caller | `subject` |
| `caller.workload_id` / `caller.spiffe_id` | operation binding + UDS workload + proof exact match | `caller_workload_id` / `caller_spiffe_id` |
| `target.workload_id` / `target.spiffe_id` | operation binding | `target_workload_id` / `target_spiffe_id` |
| `rpc` | operation binding `full_method` | `full_method` |
| `operation_id` | request field 1, proof и operation binding exact match | `operation_id` |
| `authority.actor_kind` | verified proof only | `authority.actor_kind` по enum table ниже |
| `authority.actor/tenant/project.id` | verified proof only; project presence по binding | соответствующие `AuthorityIdentity.id` |
| `authority.*.provenance.source` | verified proof + binding allowlist | `AuthorityProvenance.source` по enum table ниже |
| `authority.*.provenance.reference` | verified proof only | `AuthorityProvenance.reference` |
| `authority.*.provenance.revision` | verified proof only, `1..9007199254740991` | `AuthorityProvenance.revision` uint64 без изменения |
| `authority.*.provenance.digest_sha256` | verified proof only | `AuthorityProvenance.digest_sha256` |
| `permission` | operation binding | `permission` |
| `jti` | issuer CSPRNG UUID; request/proof JTI не переиспользуется | `jti` |
| `iat` / `nbf` / `exp` | trusted database seconds; `nbf=iat`, `exp=iat+30` | `issued_at` / `not_before` / `expires_at`, `nanos=0` |
| `source_revision` / digest | accepted signed snapshot | `source_revision` / `source_digest_sha256` |
| `key_set_revision` | accepted signed snapshot | `key_set_revision` |
| `policy_revision` | accepted signed snapshot | `policy_revision` |
| `signer_generation` | accepted signed snapshot + manifest trust | `signer_generation` |

Enum mapping является exact и не использует `enum.String()` либо неявное
удаление prefix:

| Proto enum | Canonical JSON |
| --- | --- |
| `ACTOR_KIND_HUMAN` | `HUMAN` |
| `ACTOR_KIND_AGENT` | `AGENT` |
| `ACTOR_KIND_SERVICE` | `SERVICE` |
| `ACTOR_KIND_AUTOMATION` | `AUTOMATION` |
| `AUTHORITY_SOURCE_OIDC_SESSION` | `OIDC_SESSION` |
| `AUTHORITY_SOURCE_MATTERMOST_EVENT` | `MATTERMOST_EVENT` |
| `AUTHORITY_SOURCE_DOMAIN_STATE` | `DOMAIN_STATE` |
| `AUTHORITY_SOURCE_AGENT_SESSION` | `AGENT_SESSION` |
| `AUTHORITY_SOURCE_PROCESS_RUN` | `PROCESS_RUN` |
| `AUTHORITY_SOURCE_AUTOMATION_OCCURRENCE` | `AUTOMATION_OCCURRENCE` |

`UNSPECIFIED`, неизвестный enum number и любое canonical JSON enum вне таблицы
дают `InvalidArgument/MALFORMED_REQUEST` до подписи. Project message отсутствует
только при `project_required=false`; present empty message и partial nested
message отклоняются. Actor и tenant всегда present и полностью заполнены.

Любой Proto `uint64`, который должен попасть в canonical JSON, проверяется до
cast/signature и обязан быть не больше `9007199254740991`. Caller-derived
overflow дает `InvalidArgument/MALFORMED_REQUEST`; overflow в proof получает
`Unauthenticated/AUTHORITY_PROOF_INVALID`; overflow в accepted snapshot/server
state дает `FailedPrecondition/POLICY_REVISION_REJECTED`. Fractional,
exponential и negative JSON revisions запрещены schema/strict parser.
Невалидный timestamp, `seconds > 253402300799`, `nanos != 0` либо нарушение
exact TTL дает соответствующий `InvalidArgument/MALFORMED_REQUEST`,
`Unauthenticated/AUTHORITY_PROOF_INVALID` или `Unauthenticated/TOKEN_*` по
границе происхождения.

Response caster после successful verification выполняет round-trip
`canonical claims → VerifiedAuthorizationContext → binary Proto → binary
Proto decode` без потери presence, enum, time или revision. Descriptor,
round-trip, safe-integer и negative fixtures выполняются из module
`libs/go/internalrpcauth/contract`.

## ES256 compact JWS

### Protected header

Authorization context имеет ровно:

```json
{"alg":"ES256","crit":["mcxv"],"kid":"<id>","mcxv":1,"typ":"mattercodex-internal-rpc-auth+jws"}
```

Signed snapshot отличается только:

```json
{"alg":"ES256","crit":["mcxv"],"kid":"<id>","mcxv":1,"typ":"mattercodex-internal-rpc-snapshot+jws"}
```

ASCII member names расположены в RFC 8785 order
`alg,crit,kid,mcxv,typ`; это порядок canonical UTF-8 bytes, а не порядок
доверия. Разрешены только пять указанных members. `alg` ровно `ES256`; `typ`
обязан совпасть с видом payload; `kid` 3–64 bytes и выбирает ключ только по
паре `(iss,kid)` для context, exact proof/evidence signer либо по exact
manifest signer generation для snapshot; `crit` ровно `["mcxv"]`; `mcxv`
ровно `1`.

Для authorization context `kid` является issuer-local ID из exact
`(iss,kid)`. Для snapshot `kid` имеет вид
`manifest-signer-g<signer_generation>`, где generation записана десятичным
числом без ведущих нулей, и обязана совпасть с payload до применения snapshot.

Unprotected header, `b64=false`, padding base64url, empty segment, больше или
меньше трех compact segments, unknown/duplicate/null member и non-canonical
JSON отклоняются до signature use. Algorithm inference по типу ключа
запрещен.

`fixtures/protected-header-golden.json` фиксирует exact UTF-8 и unpadded
base64url bytes для authorization context и snapshot. Обе прежние
перестановки `alg,typ,kid,crit,mcxv` являются обязательными negative fixtures
и отклоняются до signature use даже при тех же semantic values.

Криптография реализуется поддерживаемой `github.com/go-jose/go-jose/v4`
версии 4.1.4. Project wrapper задает strict parsing/header/key policy, но не
реализует ECDSA/JOSE primitives самостоятельно.

### Claims

Полный состав задает
`contracts/authorization/v1/authorization-context.schema.json`.

Инварианты:

- `v == 1`;
- `iss` — issuer SPIFFE ID текущего caller workload;
- `sub == caller.spiffe_id`;
- `aud == "urn:mattercodex:internal-rpc:" + target.workload_id`;
- caller/target workload и SPIFFE совпадают с exact operation binding;
- `rpc`, `operation_id` и `permission` образуют одну binding;
- actor/tenant обязательны; project присутствует только когда policy требует
  его, а отсутствие при `project_required=true` отклоняется;
- каждый provenance source входит в allowlist binding, а positive revision и
  digest относятся к immutable snapshot, подписанному authority proof issuer;
- `jti` выпускается CSPRNG-backed UUID и никогда не принимается от caller;
- `replay_mode == ONE_TIME`;
- revision/digest claims относятся к exact signed snapshot, обслуживаемому
  issuer при выпуске.

Issuer задает:

```text
iat = floor(current UTC time to seconds)
nbf = iat
exp = iat + 30 seconds
```

Verifier использует trusted database time и допускает clock skew не более
5 секунд в обе стороны. Он дополнительно требует `exp - iat == 30`,
`nbf == iat`, `iat <= now + 5`, `nbf <= now + 5` и `exp > now - 5`.
Overlong/shortened TTL, дробные JSON numbers и число вне safe integer range
отклоняются. Clock-skew не продлевает publisher rotation window менее 40
секунд.

Unknown, duplicate и `null` fields запрещены на любой глубине. Строки не
обрезаются и не нормализуются после parse. Идентификаторы и digests принимаются
только в canonical lexical form.

## Signed JWKS и machine policy snapshot

Snapshot является compact ES256 JWS. Payload schema:
`contracts/authorization/v1/authority-snapshot.schema.json`.

### JWKS

Для каждого issuer snapshot содержит:

- exact issuer SPIFFE ID;
- stable workload ID;
- ровно один `CURRENT`;
- ровно один `NEXT`;
- не более одного `PREVIOUS`.

Каждый JWK содержит только:

```text
kty=EC
crv=P-256
use=sig
key_ops=["verify"]
alg=ES256
kid=<unique issuer-local id>
x=<43-char unpadded base64url>
y=<43-char unpadded base64url>
```

`d`, x509 chain, arbitrary extensions и unknown members запрещены. Координаты
декодируются ровно в 32 bytes, обязаны задавать точку P-256 не на бесконечности.
`kid` не переиспользуется ни в одной более поздней revision того же issuer.

### Machine policy

Каждая operation binding содержит ровно:

- stable `operation_id`;
- caller workload ID и SPIFFE ID;
- issuer SPIFFE ID;
- target workload ID и SPIFFE ID;
- exact audience;
- exact full RPC;
- exact downstream TLS server name и trust bundle ID;
- одну machine permission;
- ровно один `authority_proof_producer_id`, который разрешает эту operation;
- закрытый allowlist authority sources;
- признак обязательности project;
- exact local caller/target UID, primary GID и shared fsGroup.

Producer profile содержит exact resolver workload/SPIFFE/full method/TLS
server/trust, application credential, proof issuer/audience/trust/max age,
authority sources, allowed operation IDs, timeout/retry и server-resolved
fields. Wildcard workload, SPIFFE, proof issuer, TLS server name, trust
bundle, audience, RPC, permission или delivery path запрещен. Client adapter
проверяет TLS с exact SNI/hostname, trust bundle и target SPIFFE. Issuer
проверяет proof по profile, на который ссылается binding. Duplicate
`operation_id`, неоднозначный full RPC, отсутствующий/лишний producer либо
несовпадение operation/source с producer отклоняют snapshot целиком.

Validator строит union ролей: caller каждой binding требует ровно одну
`AUTHORIZATION_ISSUER`, target — ровно одну `AUTHORIZATION_VERIFIER`, owner
каждого producer — ровно одну `AUTHORITY_PROOF_RESOLVER`. Одна
`(workload,role)` строка может покрыть несколько operations, но duplicate,
missing или extra/opposite role запрещены. Поэтому target-only workload
валиден без issuer и private auth key; отсутствие verifier/readback либо
появление auth private key у verifier закрывает snapshot.

Machine permission обозначает право конкретного workload вызвать точный RPC.
Она не является business role и не разрешает aggregate ownership. Actor kind
также не является ролью.

Trust graph закрыт следующими ребрами:

| Caller | Target | Обязательное доказательство |
| --- | --- | --- |
| Gateway | Authority proof resolver | exact mTLS gateway SPIFFE + проверенный application credential; resolver server-side выводит actor/tenant/project/ownership |
| Application | Local issuer | private UDS, socket owner/mode, `SO_PEERCRED`, exact caller workload binding |
| Authoritative owner/resolver | Issuer | signed authority proof, exact proof issuer/trust generation, caller workload, operation, audiences, 15-second freshness, revision watermark и one-time proof JTI |
| Issuer | Signed snapshot | independently delivered manifest trust, signature/certificate validity+generation, canonical payload, predecessor/high-watermark, exact cryptographic readback |
| Issuer client adapter | Downstream target | exact TLS SNI/hostname, trust bundle, target SPIFFE и один compact JWS |
| Downstream target interceptor | Local verifier | private UDS, `SO_PEERCRED`, фактический full method и проверенный mTLS peer |
| Verifier | Signed snapshot | independently delivered manifest trust overlap, signature/certificate generation, persistent target watermark и served digest |
| Issuer/verifier/resolver | Readback store | отдельный login principal exact `(workload,role,generation)`, TLS hostname/CA, server-derived mapping и одна transaction |
| Verifier | Replay store | workload/role-bound PostgreSQL login, TLS hostname/CA и одна transaction reservation |
| Target adapter | Domain owner | только neutral verified context; owner заново проверяет tenant/project/resource state |

Других доверительных ребер нет. В частности, issuer не доверяет target claims
caller, verifier не доверяет full method из JWS без фактического transport
method, а domain owner не доверяет permission как доказательству ownership.

`bootstrap-deny-all-policy.json` имеет пустые `authority_proof_producers` и
`operation_bindings`, а также `default_decision=DENY`.
`bootstrap-key-delivery-targets.json` имеет пустой `targets`. Это рабочее
безопасное начальное состояние, а не fallback. Unit #187 добавляет exact
producer/bindings одновременно со своим versioned Proto; до этого ни
preflight, ни business RPC к control-plane не разрешены.

## Persistent replay и high-watermarks

Источник истины — PostgreSQL `internal-rpc-authority-data`. Runtime roles,
таблицы и ownership перечислены в capability registry.

### Authority proof watermark и reservation

Issuer использует отдельную минимальную роль
`internal_rpc_authority_issuer`. В одной transaction он:

1. читает trusted database time, active external restore fence match и proof
   watermark для `(caller_workload_id,operation_id,authority_proof_issuer)`;
2. сравнивает safe-integer `proof_revision` и SHA-256 canonical proof payload;
3. отклоняет lower revision и same-revision different-digest;
4. вставляет unique reservation `(caller_workload_id,proof_jti)` с proof
   digest и `expires_at`;
5. CAS-продвигает proof watermark;
6. commit;
7. только после commit выпускает authorization context.

Role может `SELECT/INSERT/UPDATE` только proof watermark/reservation,
snapshot/readback и restore-fence paths из capability registry; `DELETE`,
watermark reset, replay reset и DDL запрещены. Proof reservation сохраняется
минимум до `proof exp + 10 minutes`. Сбой после commit до подписи потребляет
proof безопасно: caller получает новый proof, а прежний JTI не используется
повторно.

### Replay reservation

В одной transaction verifier:

1. читает database time и active restore fence;
2. блокирует target watermark;
3. проверяет source/key-set/policy/signer revisions и digests;
4. вставляет reservation с unique key `(target_workload_id,jti)`;
5. сохраняет SHA-256 digest exact compact JWS и `expires_at`;
6. commit;
7. только после commit возвращает success.

Существующий JTI всегда означает replay, даже если token digest совпадает.
Другой digest дополнительно создает security incident, но наружу возвращается
тот же bounded reason. Reservation сохраняется минимум до
`expires_at + 10 minutes`; cleanup использует database time и bounded batches.

### Watermark

Watermark принадлежит target verifier и имеет CAS tuple:

```text
target_workload_id
source_revision
source_digest_sha256
key_set_revision
policy_revision
signer_generation
updated_at
```

Переход разрешен только вперед и только по signed predecessor/history.
Same-revision same-digest идемпотентен; same-revision different-digest и lower
revision запрещены. Caller, issuer request и application process не имеют
write access к таблице.

Несколько replicas используют row lock/CAS в PostgreSQL transaction. In-memory
cache может хранить только immutable accepted snapshot pointer и не заменяет
reservation/watermark.

### Workload/role-bound readback

Shared capability roles `internal_rpc_authority_issuer`,
`internal_rpc_authority_verifier` и
`internal_rpc_authority_proof_resolver` имеют `NOLOGIN` и не могут
использоваться как session identity. Publisher для каждой активной
`(workload_id,role,workload_generation,credential_generation)` создаёт
отдельный PostgreSQL login principal из exact delivery registry. Vault
database secret доставляет его только соответствующему sidecar/resolver;
current и next principals кратко перекрываются, а previous отзывается лишь
после promotion и readback.

Таблица `authority_workload_database_identities` имеет unique active keys по
`session_user` и `(workload_id,role,workload_generation)`. Каждая readback row
имеет FK на эту identity и unique
`(workload_id,role,workload_generation,source_revision,digest_sha256)`.
`ENABLE ROW LEVEL SECURITY` и `FORCE ROW LEVEL SECURITY` применяют
default-deny policy по `session_user`.

Клиент не получает `INSERT/UPDATE` таблиц
`authority_key_delivery_readbacks` и `authority_snapshot_readbacks`. Он
вызывает только одну из двух exact `SECURITY DEFINER` functions:

- `internal_rpc_authority.record_authority_key_delivery_readback(bigint,text,bigint,bigint,text) RETURNS bigint`;
- `internal_rpc_authority.record_authority_snapshot_readback(bigint,text,bigint,bigint,bigint,text) RETURNS bigint`.

Параметров `workload_id` и `role` у функций нет. Обе имеют fixed
`search_path=pg_catalog,internal_rpc_authority,pg_temp`, `EXECUTE` для `PUBLIC`
отозван, server-side разрешают `session_user`, блокируют exact registry row,
проверяют active credential/workload generation и соответствующий роли
cryptographic proof, затем атомарно пишут и перечитывают строку своей таблицы.
Readiness проверяет тот же transaction/readback path. Ни одна readback table
не принимает прямой caller-controlled workload или role.

Schema, обе таблицы и обе функции принадлежат отдельному
`internal_rpc_authority_readback_owner` с
`NOLOGIN/NOSUPERUSER/NOBYPASSRLS/NOCREATEDB/NOCREATEROLE/NOINHERIT`; runtime
principals не имеют membership этой роли. Обе таблицы используют `ENABLE` и
`FORCE ROW LEVEL SECURITY`; `CREATE` в schema доступен только owner.
`REVOKE/GRANT EXECUTE` всегда указывают schema и полный список типов, unsafe
overload запрещён. Publisher имеет только `SELECT` обеих readback tables, но
не `INSERT/UPDATE` и не `EXECUTE`. Исполняемый catalog/RLS/privilege contract
зафиксирован в `postgresql-readback-boundary.sql`.

Principal одного target не пишет за другой target или opposite role; retired
generation и shared group principal закрыто отклоняются. Негативный
integration contract находится в
`fixtures/readback-authority-negative.json`. Эти identity и readbacks входят
в тот же rotation intent, поэтому promotion невозможна при missing,
ambiguous либо cross-role row.

### PITR fence

До любого restore двухрепличный
`internal-rpc-authority-restore-controller` из digest-only artifact #186
получает ручной code-first запрос только от
`internal-rpc-authority-restore-operator` по mTLS с projected ServiceAccount
token exact audience. Controller переводит состояние в `QUIESCING` и
CAS-записывает versioned `internal-rpc-authority-restore-coordination` вне
восстанавливаемой БД. Каждый sidecar/resolver выполняет versioned poll
`GetRestoreDirective`, получая exact `restore_id`, epoch и signed QUIESCING
directive до `PREPARED`.

ES256 role credential server-side связывает
`(workload,role,workload_generation,credential_generation)`, workload SPIFFE,
controller audience и thumbprint отдельного ACK key. Поэтому verifier и
resolver одного `control-plane` с одинаковым SPIFFE получают разные
application identities. После stop accepting и drain до `inflight_count=0`
роль подписывает ACK bound key. Controller проверяет credential, actual mTLS
SPIFFE, current generation, directive/restore/revision и атомарно резервирует
directive/ACK JTI во внешнем coordination state. Только полный distinct ack
set разрешает signed `PREPARED`. Restore operator перечитывает exact served
evidence, выполняет TLS restore exact PostgreSQL cluster, вызывает
`CompleteRestore`, перечитывает `COMPLETED` и запускает recovery step того же
Job. Реальный `internal-rpc-authority-recovery-job` step:

1. читает exact Secret через resourceNames-limited `get`;
2. проверяет JCS, signature, controller certificate generation, predecessor,
   external high-watermark, database cluster/restore ID и `COMPLETED`;
3. ролью `internal_rpc_authority_recovery` атомарно CAS-записывает
   `(restore_epoch,anchor_revision,evidence_digest)` и
   `blocked_until = database_clock + 40 seconds`;
4. не имеет права менять watermark или replay/proof reservations.

Пока fence закрыт либо external/database epoch+digest не совпадают:

- issuer не выпускает context;
- verifier не принимает context;
- application readiness false;
- cleanup replay rows не выполняется.

Это исключает повтор context, reservation которого исчезла из-за PITR.
Отсутствующий/stale restore evidence, missing/stale ack, ошибка controller,
signer/admission/readback либо recovery step, восстановленная
старая fence row или неистекшее safe window являются операционным блокером;
автоматический watermark reset запрещен. Внешний anchor не хранится в
authority PostgreSQL и потому не откатывается вместе с PITR.

Controller использует отдельные ServiceAccount, bounded config, exact
Kubernetes Secret/Lease RBAC и egress только к exact Kubernetes API/Vault.
Restore operator не имеет Kubernetes write RBAC; его PostgreSQL restore
credential доставляется Job через Vault и ограничен exact cluster. Controller
signer и trust ротируются `CURRENT/NEXT/PREVIOUS`, проходят
private→public/served-evidence readback, а readiness требует фактически
наблюдаемую admission policy/binding и semantic anchor. Негативный contract
lower/same anchor, missing ack, stale generation, signature/controller failure
и restore без `PREPARED` находится в `fixtures/restore-negative.json`.

## Forward-only rotation protocol

Любое изменение signed payload повышает `source_revision`. Изменение keys
повышает `key_set_revision`; bindings — `policy_revision`; manifest signer
certificate/key — `signer_generation`. Незатронутые counters остаются прежними,
но никогда не уменьшаются.

Payload содержит immediate predecessor и до 32 предыдущих `(revision,digest)`
в строгом возрастающем порядке без повторов. Последний history entry равен
predecessor. Для revision 1 history пуст, а bootstrap predecessor имеет
revision `0` и digest из 64 нулей; он принимается только при пустом persistent
store во время первичного bootstrap, а не после restore.

Snapshot digest — lowercase SHA-256 canonical payload bytes, не compact JWS и
не Kubernetes resourceVersion.

`published_at` и `valid_from` совпадают, `valid_until = published_at + 86400`
секунд. Publisher выпускает следующую revision до истечения этого окна.
Verifier отклоняет snapshot с другой длительностью, будущим `published_at`
дальше clock skew или уже истекшим `valid_until`.

### Auth key rotation

1. Publisher фиксирует immutable rotation intent и генерирует новый key pair.
2. Через Vault KV v2 CAS пишет private key только в exact path каждого
   зарегистрированного `AUTHORIZATION_ISSUER`; wildcard/list/delete запрещены.
3. Каждый issuer проверяет mounted private key против intended `NEXT` public
   JWK и фиксирует cryptographic readback; publisher требует полный
   per-workload/role fan-out.
4. Publisher публикует public key как `NEXT`, выполняет Kubernetes CAS и exact
   cryptographic readback.
5. Все требуемые issuer/verifier/resolver роли независимо проверяют относящийся
   к ним snapshot/trust, продвигают собственные watermarks и подтверждают
   served tuple.
6. Не ранее 40 секунд publisher повышает revision и меняет
   `NEXT -> CURRENT`, `CURRENT -> PREVIOUS`, добавляя новый `NEXT`.
7. Issuer подписывает новым current только после readback этого snapshot.
8. Не ранее еще 40 секунд previous удаляется отдельной revision.

Crash на любом шаге повторяет тот же intended revision/digest. Создавать новый
revision до readback неопределенного предыдущего CAS запрещено.

### Manifest signer rotation

Manifest signer certificate имеет отдельный generation. Ротация:

1. Publisher через exact Vault targets и CAS добавляет новый signer public
   certificate в независимые manifest trust bundles всех требуемых ролей;
   старый остается.
2. Каждая issuer/verifier/resolver role криптографически подтверждает overlap,
   certificate validity/generation и записывает readback.
3. Publisher подписывает следующую revision новым signer и повышает
   `signer_generation`.
4. Каждая зарегистрированная role проверяет новую signature, canonical payload,
   predecessor/high-watermark и served readback.
5. После accepted snapshot и overlap window старый certificate удаляется
   отдельным CAS-изменением всех exact trust targets.

Signer certificate обязан быть валиден в `published_at` и в момент новой
публикации. Истекший signer не может выпускать recovery snapshot. Уже принятый
snapshot обслуживается только до собственного `valid_until`; после этого
issuer/verifier readiness false.

## Success, errors и negative paths

Каждый non-OK authority UDS либо mTLS RPC status содержит ровно один
`internalrpcauthority.v1.AuthorizationErrorDetail`. Detail
содержит только reason, stage, retryable и canonical correlation UUID. Status
message является фиксированной английской строкой по reason и не содержит
token, claims, IDs, `kid`, key coordinates, certificate digest, SQL/provider
diagnostics или PII.

| Условие | gRPC code | Reason | Retryable |
| --- | --- | --- | --- |
| Malformed/oversized/unknown/duplicate Proto | `InvalidArgument` | `MALFORMED_REQUEST` | false |
| UDS peer UID/GID/workload mismatch | `Unauthenticated` | `UDS_PEER_REJECTED` | false |
| Symlink/non-socket/owner/mode mismatch при startup/readiness | `Unavailable` | `UDS_ENDPOINT_INVALID` | false |
| Authority proof отсутствует | `InvalidArgument` | `AUTHORITY_PROOF_REQUIRED` | false |
| Authority proof malformed/unknown signer/bad signature | `Unauthenticated` | `AUTHORITY_PROOF_INVALID` | false |
| Proof caller/workload/operation/audience mismatch | `PermissionDenied` | `AUTHORITY_PROOF_BINDING_MISMATCH` | false |
| Proof expired/not-yet-valid/wrong 15-second TTL | `Unauthenticated` | `AUTHORITY_PROOF_EXPIRED` | false |
| Proof rollback/same-revision mutation | `FailedPrecondition` | `AUTHORITY_PROOF_REVISION_REJECTED` | false |
| Proof JTI уже зарезервирован | `Unauthenticated` | `AUTHORITY_PROOF_REPLAY_DETECTED` | false |
| Proof trust/watermark/reservation path недоступен | `Unavailable` | `AUTHORITY_PROOF_UNAVAILABLE` | true |
| Resolver application credential недействителен | `Unauthenticated` | `APPLICATION_CREDENTIAL_INVALID` | false |
| Resolver resource отсутствует либо скрыт другим tenant | `NotFound` | `AUTHORITY_RESOURCE_NOT_FOUND` | false |
| Actor tenant A запрошен для tenant/project B | `PermissionDenied` | `AUTHORITY_SCOPE_MISMATCH` | false |
| Idempotency key повторён с другим semantic request | `AlreadyExists` | `IDEMPOTENCY_CONFLICT` | false |
| Unknown operation или provenance source | `PermissionDenied` | `OPERATION_NOT_ALLOWED` / `AUTHORITY_PROVENANCE_REJECTED` | false |
| Не три JWS segments, bad base64url, duplicate/null/unknown JSON | `Unauthenticated` | `MALFORMED_JWS` | false |
| Wrong `alg/typ/kid/crit/mcxv` | `Unauthenticated` | `INVALID_PROTECTED_HEADER` | false |
| Unknown `(iss,kid)` или bad ES256 signature | `Unauthenticated` | `UNKNOWN_KEY` / `INVALID_SIGNATURE` | false |
| Wrong issuer/audience/caller/target | `PermissionDenied` | соответствующий `*_MISMATCH` | false |
| Wrong full RPC/operation/permission | `PermissionDenied` | `RPC_MISMATCH` / `PERMISSION_MISMATCH` | false |
| Expired/not-yet-valid/overlong TTL | `Unauthenticated` | `TOKEN_*` | false |
| JTI уже зарезервирован | `Unauthenticated` | `REPLAY_DETECTED` | false |
| Нет проверенного mTLS | `Unauthenticated` | `MTLS_REQUIRED` | false |
| При mTLS отсутствует authorization metadata | `Unauthenticated` | `AUTHORIZATION_CONTEXT_REQUIRED` | false |
| Metadata повторена либо имеет неверную схему | `Unauthenticated` | `MALFORMED_REQUEST` | false |
| mTLS SPIFFE не равен token caller | `PermissionDenied` | `MTLS_PEER_MISMATCH` | false |
| Unknown/stale policy revision | `FailedPrecondition` | `POLICY_REVISION_REJECTED` | false |
| Snapshot lower/same-mutation/history gap | `FailedPrecondition` | `SNAPSHOT_ROLLBACK` / `SNAPSHOT_MUTATION` / `SNAPSHOT_HISTORY_GAP` | false |
| PostgreSQL/replay store недоступен | `Unavailable` | `PERSISTENCE_UNAVAILABLE` | true |
| Snapshot file/trust/readback временно недоступен | `Unavailable` | `SNAPSHOT_UNAVAILABLE` / `READBACK_MISMATCH` | true |
| Missing/stale workload generation ack или restore без `PREPARED` | `FailedPrecondition` | `RESTORE_BARRIER_INCOMPLETE` | false |
| Lower/same-mutation/predecessor/signature anchor | `FailedPrecondition` | `RESTORE_ANCHOR_REJECTED` | false |
| Controller/admission/served anchor временно недоступен | `Unavailable` | `RESTORE_CONTROLLER_UNAVAILABLE` | true |
| Role credential либо workload/role/generation/SPIFFE binding неверен | `Unauthenticated` | `RESTORE_ROLE_CREDENTIAL_REJECTED` | false |
| Current QUIESCING directive отсутствует или не совпадает | `FailedPrecondition` | `RESTORE_DIRECTIVE_REJECTED` | false |
| Directive/ACK JTI уже принят | `Unauthenticated` | `RESTORE_ACK_REPLAY_DETECTED` | false |
| Coordination state или exact network path недоступен | `Unavailable` | `RESTORE_COORDINATION_UNAVAILABLE` | true |
| Непредвиденный дефект | `Internal` | `INTERNAL` | false |

Target transport до локального verifier использует тот же detail для
`MTLS_REQUIRED`. Неизвестный status/detail либо несогласованная
`reason/code/retryable` комбинация преобразуется в `Internal/INTERNAL` и
считается нарушением контракта.

Parser warning, partial decode, recovered JSON/Proto model и unknown enum
никогда не используются семантически. Ошибка синтаксиса закрывает запрос.

Machine-readable `authorization-error-matrix.json` является источником истины
для каждой ненулевой `AuthorizationErrorReason` и фиксирует ровно одну
комбинацию `grpc_code`, `stage`, `retryable` и fixed English `message`.
Descriptor test требует one-to-one coverage enum↔matrix; неизвестная,
дублированная либо неполная строка закрывает contract test. Таблица выше —
сводка, а не альтернативный источник.

## Deploy ownership и readiness

Capability registry фиксирует:

- один digest-only OCI artifact с init/issuer/verifier/publisher,
  restore-controller/restore-operator/recovery и CLI binaries;
- issuer/verifier как sidecars consuming workload и recovery binary в том же
  artifact contract;
- publisher как отдельный двухрепличный Deployment с PostgreSQL lease;
- двухрепличный restore controller с exact Proto, ServiceAccount, signer,
  admission readback, minimal Secret/Lease RBAC и default-closed policy;
- ручной code-first restore-operator Job с exact mTLS interface, bounded
  PostgreSQL restore credential и recovery step того же Job;
- внешний монотонный restore evidence anchor, который не восстанавливается
  вместе с PostgreSQL;
- PostgreSQL source of truth, `NOLOGIN` capability groups и отдельные login
  principals на каждую `(workload,role,generation)`;
- publisher ownership генерации и Vault KV v2 CAS-write auth private keys и
  manifest/proof trust overlap по exact per-workload/role target registry;
- Vault CSI delivery private keys/trust без secret values и cryptographic
  role-bound issuer/verifier/resolver readback;
- exact pre-created Kubernetes Secret и resourceNames-limited RBAC publisher;
- UDS-only ingress sidecars;
- exact PostgreSQL/Kubernetes API destinations без wildcard egress;
- application-owned readiness через оба реальных UDS;
- bounded shutdown и restore fence.

Publisher не получает `create/delete` Secret. Verifier не получает Kubernetes
write. Application не получает signing private key или direct replay table
write.

Readiness положительна только если:

- issuer mounted key соответствует current public JWK exact issuer;
- issuer независимо получил manifest trust и проверил snapshot signature,
  certificate validity/generation, canonical payload, predecessor,
  high-watermark и exact readback;
- issuer проверил authority proof trust, proof watermark/reservation path и
  exact proof operation binding;
- proof resolver проверил application credential/domain read path,
  private→public proof signer и served policy readback;
- publisher финализировал exact readback revision/digest;
- issuer, verifier и proof resolver обслуживают согласованный
  source/policy/key-set/signer tuple;
- verifier persistent watermark, replay reservation и restore fence доступны;
- external restore anchor имеет `COMPLETED`, совпадает по epoch/digest с
  database fence и safe window истекло;
- application UID прошел UDS peer binding на обоих endpoints;
- downstream client adapter способен собрать mTLS и signed context для
  зарегистрированной operation.

Отдельный health socket, bypass method и «файл существует» не заменяют этот
путь.

## Codegen и проверки

Используются:

- Buf CLI `1.71.0`;
- remote plugin `buf.build/protocolbuffers/go:v1.36.11`, BSR `revision: 1`;
- remote plugin `buf.build/grpc/go:v1.6.2`, BSR `revision: 1`.

Команды:

```bash
buf lint
buf build
buf generate
make check-proto-codegen
make test-contract-authority
```

`check-proto-codegen` генерирует код в отдельный temporary output и требует
нулевой diff с committed files. Toolchain contract отдельно проверяет Buf
version и exact remote plugin version+revision. Contract tests проверяют
compiled Proto descriptors, exact fields/numbers/types/reserved authority,
binary round-trip/presence/time/safe-integer mapping, mutation regressions
запрещенных caller fields, RFC 8785 UTF-8/base64url golden, proof negative
fixtures, registry ownership, schema closure, deny-all bootstrap, producer
coverage, union caller/target/resolver roles, one-to-one workload/role
delivery/readback, UDS identities/modes, rotation fan-out, executable restore
controller/semantic anchor/quarantine ordering, DB principal isolation и
полную enum↔error matrix.

## Ручная проверка contract milestone

1. Открыть compiled Proto descriptor и убедиться, что issuer request содержит
   только fields `1 operation_id`, `3 correlation_id`,
   `4 authority_proof_compact_jws`, а number/name `2/authority` reserved.
2. Сопоставить issuer/verifier UDS, resolver preflight и restore controller
   full methods с capability registry.
3. Проверить JSON Schemas: `additionalProperties=false`, ES256/typ/crit/mcxv,
   exact claims, JWK P-256 и max history 32.
4. Проверить bootstrap policy: empty producers/bindings, empty delivery
   targets и default deny.
5. Проверить, что proof/replay/watermark принадлежат PostgreSQL, внешний PITR
   anchor находится вне restored DB, а `emptyDir` — только sockets.
6. По `authority-proof-first-call.json` пройти preflight без internal context,
   local issuance и первый `control-api-gateway → control-plane` RPC; затем
   подтвердить, что trusted-signer cross-tenant fixture отклоняется до подписи.
7. По delivery/restore/readback negative fixtures проверить missing target,
   opposite role/private key, lower/same anchor, missing/stale/replay ACK,
   exact NetworkPolicy и cross-target/publisher DB write. Positive
   same-workload two-role fixture обязан дать два distinct ACK до `PREPARED`.
8. Выполнить Buf lint/build/codegen diff и targeted contract tests.
9. Убедиться, что PR не содержит secret values, private keys, DSN, tokens или
   production credentials.

## Проверенная внешняя документация

Context7 повторно вызван для gRPC-Go, go-jose и pgx. Все три resolve-запроса
29 июля 2026 года вернули `Monthly quota exceeded`; документация через
Context7 недоступна. В fix-cycle раунда 1 отдельно повторен resolve Buf CLI с
тем же результатом `Monthly quota exceeded`. В fix-cycle раунда 2 отдельно
повторены resolve PostgreSQL, Kubernetes, HashiCorp Vault, Buf CLI и Protocol
Buffers; каждый запрос вернул тот же `Monthly quota exceeded`.
В fix-cycle раунда 3 повторены resolve PostgreSQL, Kubernetes и объединённый
gRPC/Protocol Buffers; все три вернули `Monthly quota exceeded`.

Проверены только официальные первичные источники:

- gRPC-Go `v1.81.0`: `https://github.com/grpc/grpc-go/tree/v1.81.0` —
  generated clients/servers, status/details, transport credentials и Unix
  target;
- go-jose `v4.1.4`: `https://github.com/go-jose/go-jose/tree/v4.1.4` —
  current stable v4, compact JWS, case-sensitive JOSE JSON и ES256;
- pgx `v5.10.0`: `https://github.com/jackc/pgx/tree/v5.10.0` —
  pgxpool, transactions и commit/rollback behavior;
- Protobuf Go generated guide:
  `https://protobuf.dev/reference/go/go-generated/` — `go_package` и generated
  API;
- Protobuf language guide:
  `https://protobuf.dev/programming-guides/proto3/` — field numbers,
  presence, enums и compatibility;
- Buf generation/config:
  `https://buf.build/docs/reference/cli/buf/generate/` и
  `https://buf.build/docs/configuration/v2/buf-gen-yaml/` — отдельные upstream
  plugin version и BSR revision, pinned remote plugins и reproducible output;
- Go Protobuf reflection:
  `https://pkg.go.dev/google.golang.org/protobuf` — generated descriptors,
  `protoreflect` и `protodesc` для structural contract tests;
- Vault KV v2:
  `https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2` — exact
  data paths, `create/read/update`, version metadata и CAS;
- Vault database secrets:
  `https://developer.hashicorp.com/vault/docs/secrets/databases` — уникальные
  auditable credentials, static-role one-to-one username и rotation;
- Vault Kubernetes/CSI:
  `https://developer.hashicorp.com/vault/docs/auth`,
  `https://developer.hashicorp.com/vault/docs/deploy/kubernetes` и
  `https://developer.hashicorp.com/vault/docs/deploy/kubernetes/csi/configurations`
  — workload authentication и file delivery;
- PostgreSQL row security:
  `https://www.postgresql.org/docs/current/ddl-rowsecurity.html` — default
  deny, policy по database role, `ENABLE`/`FORCE ROW LEVEL SECURITY`;
- PostgreSQL `CREATE FUNCTION` и session identity:
  `https://www.postgresql.org/docs/current/sql-createfunction.html` и
  `https://www.postgresql.org/docs/current/functions-info.html` —
  `SECURITY DEFINER`, безопасный `search_path`, `session_user`/`current_user`;
- PostgreSQL privileges:
  `https://www.postgresql.org/docs/current/ddl-priv.html` и
  `https://www.postgresql.org/docs/current/sql-revoke.html` — schema/function
  privileges и exact overloaded signature;
- gRPC status codes:
  `https://grpc.io/docs/guides/status-codes/` — canonical `NOT_FOUND`;
- Kubernetes `NetworkPolicy`:
  `https://kubernetes.io/docs/concepts/services-networking/network-policies/`
  — source egress и destination ingress обязательны одновременно;
- Kubernetes API/Secret:
  `https://kubernetes.io/docs/reference/using-api/api-concepts/` и
  `https://kubernetes.io/docs/concepts/configuration/secret/` —
  resourceVersion lost-update protection и eventually consistent projection,
  из-за которой file delivery требует cryptographic runtime readback;
- Kubernetes `ValidatingAdmissionPolicy` и RBAC:
  `https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/`
  и `https://kubernetes.io/docs/reference/access-authn-authz/rbac/` —
  fail-closed admission transition checks и минимальные exact permissions;
- RFC 7515, RFC 7517, RFC 7518 — compact JWS, JWK/JWKS и ES256/P-256;
- RFC 8725 — algorithm verification, audience/issuer validation и explicit
  typing;
- RFC 8785 — canonical JSON serialization.
