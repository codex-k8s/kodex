---
id: CONTRACT-MC-004
title: Доверенный контекст внутренних RPC
type: contract
status: approved
owner: architect
version: 1.0.0
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
по отдельности не являются authority. Request к issuer переносит утверждения
локального workload с доказуемым provenance. Возможность этого workload
утверждать конкретный provenance для exact operation задаётся signed machine
policy.

## Нормативные артефакты

| Артефакт | Назначение |
| --- | --- |
| `contracts/proto/internalrpcauthority/v1/authority.proto` | UDS issuer/verifier RPC и typed error detail |
| `contracts/authorization/v1/jws-protected-header.schema.json` | закрытый protected header |
| `contracts/authorization/v1/authorization-context.schema.json` | canonical authorization claims |
| `contracts/authorization/v1/authority-snapshot.schema.json` | signed JWKS + machine policy snapshot |
| `contracts/authorization/v1/bootstrap-deny-all-policy.json` | безопасное начальное состояние без business bindings |
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
| Actor/tenant/project authority | `CallerAuthority`; каждый ID связан с `AuthoritySource`, immutable `reference`, положительной `revision` и SHA-256 digest |
| Локальный caller | Application container, UID/GID `10001:10001`, workload и SPIFFE ID из pod identity; private pod `emptyDir` ограничивает область UDS |
| Issuer endpoint | `unix:///run/mattercodex/internal-rpc-authority/issuer.sock`; `/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext` |
| Issuer binding | `SO_PEERCRED` обязан совпасть с UID/GID policy; loaded workload SVID обязан совпасть с `caller_workload_id`/`caller_spiffe_id` operation binding |
| Policy binding | Request выбирает только `operation_id`; issuer server-side выводит issuer, audience, target workload/SPIFFE, exact TLS server name/trust bundle, full RPC, permission, TTL и revisions |
| ES256 JWS | Issuer создает новый UUID `jti`, canonical claims, compact JWS и signing key exact `(iss,kid)` со статусом `CURRENT` |
| Downstream transport | Client adapter открывает mTLS с exact server name/SPIFFE и доверенной CA; metadata содержит ровно одно `x-mattercodex-internal-authorization: Bearer <compact-jws>` |
| Target | Target gRPC interceptor получает фактический full method и проверенного mTLS peer из transport, а не из business request |
| Результат | Только после успешной локальной verifier проверки target adapter получает neutral `VerifiedAuthorizationContext` |
| Domain owner | Domain service заново разрешает resource внутри tenant/project boundary и проверяет business state/ownership |
| Consumers | Target transport adapter, domain service и единая observability boundary; raw token не передаётся дальше adapter |

Issuer не принимает requested audience, RPC, target workload, permission,
`iat`, `nbf`, `exp`, revision или `kid`. Их отсутствие в Proto является частью
security contract.

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
| Подготовка | Publisher создает новый auth key как `NEXT`; private part доставляется только соответствующему issuer через Vault-bound mount |
| Публикация | Publisher строит следующий signed snapshot с `source_revision + 1`, `key_set_revision + 1`, predecessor и bounded history |
| Kubernetes CAS | Publisher может `get/update/patch` только заранее созданный Secret `internal-rpc-authority-snapshot`; `create/delete` запрещены |
| Readback publisher | После CAS publisher перечитывает exact Secret, проверяет compact JWS, signer certificate, canonical payload и digest; только затем финализирует revision в PostgreSQL |
| Readback verifier | Verifier проверяет snapshot, CAS-продвигает target-owned watermark, атомарно меняет обслуживаемый pointer и записывает exact `(revision,digest)` readback |
| Readiness | Application UID вызывает оба реальных UDS `CheckReadiness`; readiness положительна только при совпадении фактически обслуживаемого digest, publisher-finalized revision и persistent store |
| Promotion | Не ранее 40 секунд после readback `NEXT` становится `CURRENT`, прежний current — `PREVIOUS`, а новый ключ создается как `NEXT` |
| Retirement | `PREVIOUS` удаляется отдельной revision не ранее ещё одного окна 40 секунд после подтвержденной promotion |
| Consumers | Issuers подписывают только exact `CURRENT`; verifiers принимают listed `CURRENT/NEXT/PREVIOUS` только для exact issuer; target workloads получают atomic snapshot |

Окно 40 секунд равно TTL 30 секунд плюс двум допустимым clock-skew окнам по
5 секунд. Сокращать его runtime-настройкой запрещено.

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
| PITR replay database | Recovery job создает новый restore fence и держит issuer/verifier unready 40 секунд; старые context истекают до открытия traffic |
| PITR publisher database | Publisher не переиспользует revision; exact Kubernetes readback и persistent history должны доказать следующий номер/digest, иначе публикация блокируется |

Удалять или обнулять watermark/replay rows для recovery запрещено. Rejoin
применяет только последовательно проверенные signed snapshots. Restore fence
принадлежит recovery job, не caller и не application pod.

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
```

Source расположен в `contracts/proto/internalrpcauthority/v1`. Go package
генерируется в `libs/go/internalrpcauth/gen/internalrpcauthority/v1`. Field
numbers и names не переиспользуются; удаление резервирует оба.

### Ограничения request

| Значение | Ограничение |
| --- | --- |
| Decoded Proto message | не более 16 KiB |
| `operation_id` | 3–128 bytes, lowercase dotted/hyphenated identifier |
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

## ES256 compact JWS

### Protected header

Authorization context имеет ровно:

```json
{"alg":"ES256","typ":"mattercodex-internal-rpc-auth+jws","kid":"<id>","crit":["mcxv"],"mcxv":1}
```

Signed snapshot отличается только:

```json
{"alg":"ES256","typ":"mattercodex-internal-rpc-snapshot+jws","kid":"<id>","crit":["mcxv"],"mcxv":1}
```

Порядок JSON выше является результатом RFC 8785, а не порядком доверия.
Разрешены только пять указанных members. `alg` ровно `ES256`; `typ` обязан
совпасть с видом payload; `kid` 3–64 bytes и выбирает ключ только по паре
`(iss,kid)` для context либо по exact manifest signer generation для snapshot;
`crit` ровно `["mcxv"]`; `mcxv` ровно `1`.

Для authorization context `kid` является issuer-local ID из exact
`(iss,kid)`. Для snapshot `kid` имеет вид
`manifest-signer-g<signer_generation>`, где generation записана десятичным
числом без ведущих нулей, и обязана совпасть с payload до применения snapshot.

Unprotected header, `b64=false`, padding base64url, empty segment, больше или
меньше трех compact segments, unknown/duplicate/null member и non-canonical
JSON отклоняются до signature use. Algorithm inference по типу ключа
запрещен.

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
  digest относятся к immutable snapshot локального authoritative adapter;
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
- закрытый allowlist authority sources;
- признак обязательности project;
- exact local caller/target UID, primary GID и shared fsGroup.

Wildcard workload, SPIFFE, TLS server name, trust bundle, audience, RPC или
permission запрещен. Client adapter проверяет TLS с exact SNI/hostname,
указанным binding, exact trust bundle и target SPIFFE. Duplicate
`operation_id`, duplicate full binding и один full RPC с неоднозначными
permissions отклоняют snapshot целиком.

Machine permission обозначает право конкретного workload вызвать точный RPC.
Она не является business role и не разрешает aggregate ownership. Actor kind
также не является ролью.

Trust graph закрыт следующими ребрами:

| Caller | Target | Обязательное доказательство |
| --- | --- | --- |
| Application | Local issuer | private UDS, socket owner/mode, `SO_PEERCRED`, exact caller workload binding |
| Issuer | Signed snapshot | manifest signer certificate generation, predecessor chain, exact cryptographic readback |
| Issuer client adapter | Downstream target | exact TLS SNI/hostname, trust bundle, target SPIFFE и один compact JWS |
| Downstream target interceptor | Local verifier | private UDS, `SO_PEERCRED`, фактический full method и проверенный mTLS peer |
| Verifier | Signed snapshot | manifest signer trust overlap, persistent target watermark и served digest |
| Verifier | Replay store | отдельная PostgreSQL runtime role, TLS hostname/CA и одна transaction reservation |
| Target adapter | Domain owner | только neutral verified context; owner заново проверяет tenant/project/resource state |

Других доверительных ребер нет. В частности, issuer не доверяет target claims
caller, verifier не доверяет full method из JWS без фактического transport
method, а domain owner не доверяет permission как доказательству ownership.

`bootstrap-deny-all-policy.json` имеет пустой `operation_bindings` и
`default_decision=DENY`. Это рабочее безопасное начальное состояние, а не
пример или fallback. Unit #187 добавляет exact bindings одновременно со своим
versioned Proto; до этого ни один business RPC к control-plane не разрешен.

## Persistent replay и high-watermarks

Источник истины — PostgreSQL `internal-rpc-authority-data`. Runtime roles,
таблицы и ownership перечислены в capability registry.

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

### PITR fence

После любого restore authority database recovery job записывает новый
server-owned fence и `blocked_until = database_clock + 40 seconds`. Пока fence
закрыт:

- issuer не выпускает context;
- verifier не принимает context;
- application readiness false;
- cleanup replay rows не выполняется.

Это исключает повтор context, reservation которого исчезла из-за PITR.
Отсутствующий restore evidence после заявленного restore является
операционным блокером; автоматический watermark reset запрещен.

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

1. Publisher фиксирует новый key pair и доставляет private key только issuer.
2. Публикует public key как `NEXT`, выполняет CAS и exact readback.
3. Verifier принимает snapshot, продвигает watermark и обслуживает его
   атомарно; issuer подтверждает соответствие mounted private key.
4. Не ранее 40 секунд publisher повышает revision и меняет
   `NEXT -> CURRENT`, `CURRENT -> PREVIOUS`, добавляя новый `NEXT`.
5. Issuer подписывает новым current только после readback этого snapshot.
6. Не ранее еще 40 секунд previous удаляется отдельной revision.

Crash на любом шаге повторяет тот же intended revision/digest. Создавать новый
revision до readback неопределенного предыдущего CAS запрещено.

### Manifest signer rotation

Manifest signer certificate имеет отдельный generation. Ротация:

1. Новый signer public certificate добавляется в verifier trust bundle, старый
   остается.
2. Каждый verifier readback подтверждает bundle overlap.
3. Publisher подписывает следующую revision новым signer и повышает
   `signer_generation`.
4. После accepted snapshot и overlap window старый certificate удаляется
   отдельным изменением trust bundle.

Signer certificate обязан быть валиден в `published_at` и в момент новой
публикации. Истекший signer не может выпускать recovery snapshot. Уже принятый
snapshot обслуживается только до собственного `valid_until`; после этого
issuer/verifier readiness false.

## Success, errors и negative paths

Каждый non-OK UDS status содержит ровно один
`internalrpcauthority.v1.AuthorizationErrorDetail`. Detail
содержит только reason, stage, retryable и canonical correlation UUID. Status
message является фиксированной английской строкой по reason и не содержит
token, claims, IDs, `kid`, key coordinates, certificate digest, SQL/provider
diagnostics или PII.

| Условие | gRPC code | Reason | Retryable |
| --- | --- | --- | --- |
| Malformed/oversized/unknown/duplicate Proto | `InvalidArgument` | `MALFORMED_REQUEST` | false |
| UDS peer UID/GID/workload mismatch | `Unauthenticated` | `UDS_PEER_REJECTED` | false |
| Symlink/non-socket/owner/mode mismatch при startup | process not ready | `UDS_ENDPOINT_INVALID` | false |
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
| Непредвиденный дефект | `Internal` | `INTERNAL` | false |

Target transport до локального verifier использует тот же detail для
`MTLS_REQUIRED`. Неизвестный status/detail либо несогласованная
`reason/code/retryable` комбинация преобразуется в `Internal/INTERNAL` и
считается нарушением контракта.

Parser warning, partial decode, recovered JSON/Proto model и unknown enum
никогда не используются семантически. Ошибка синтаксиса закрывает запрос.

## Deploy ownership и readiness

Capability registry фиксирует:

- один digest-only OCI artifact с init/issuer/verifier/publisher/CLI binaries;
- issuer/verifier как sidecars consuming workload;
- publisher как отдельный двухрепличный Deployment с PostgreSQL lease;
- PostgreSQL source of truth и отдельные runtime/publisher/migrator roles;
- Vault CSI delivery private keys без secret values;
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
- publisher финализировал exact readback revision/digest;
- issuer и verifier обслуживают тот же source/policy/key-set/signer tuple;
- verifier persistent watermark, replay reservation и restore fence доступны;
- application UID прошел UDS peer binding на обоих endpoints;
- downstream client adapter способен собрать mTLS и signed context для
  зарегистрированной operation.

Отдельный health socket, bypass method и «файл существует» не заменяют этот
путь.

## Codegen и проверки

Используются:

- Buf CLI `1.71.0`;
- remote plugin `buf.build/protocolbuffers/go:v1.36.11`;
- remote plugin `buf.build/grpc/go:v1.6.2`.

Команды:

```bash
buf lint
buf build
buf generate
make check-proto-codegen
make test-contract-authority
```

`check-proto-codegen` генерирует код в отдельный temporary output и требует
нулевой diff с committed files. Contract tests проверяют registry ownership,
exact methods/paths, schema closure, deny-all bootstrap, one-to-one operation
bindings, UDS identities/modes, rotation cardinality и error matrix.

## Ручная проверка contract milestone

1. Открыть Proto и убедиться, что request issuer не содержит audience, RPC,
   permission, TTL, revision или `kid`.
2. Сопоставить четыре full methods с UDS paths и capability registry.
3. Проверить JSON Schemas: `additionalProperties=false`, ES256/typ/crit/mcxv,
   exact claims, JWK P-256 и max history 32.
4. Проверить bootstrap policy: empty bindings и default deny.
5. Проверить, что replay/watermark принадлежат PostgreSQL, а `emptyDir` —
   только sockets.
6. Выполнить Buf lint/build/codegen diff и targeted contract tests.
7. Убедиться, что PR не содержит secret values, private keys, DSN, tokens или
   production credentials.

## Проверенная внешняя документация

Context7 повторно вызван для gRPC-Go, go-jose и pgx. Все три resolve-запроса
29 июля 2026 года вернули `Monthly quota exceeded`; документация через
Context7 недоступна.

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
  `https://buf.build/docs/configuration/v2/buf-gen-yaml/` — pinned remote
  plugins и reproducible output;
- RFC 7515, RFC 7517, RFC 7518 — compact JWS, JWK/JWKS и ES256/P-256;
- RFC 8725 — algorithm verification, audience/issuer validation и explicit
  typing;
- RFC 8785 — canonical JSON serialization.
