---
id: DOM-MC-010
title: Образы и цепочка поставки
type: domain
status: approved
owner: architect
version: 0.9.1
updated: 2026-10-05
---

# Образы и цепочка поставки

Документ закрепляет принятые решения `D1-A`-`D3-A`, `D4-B` и
`D5-A`-`D8-A` без совместимости с прежней моделью прототипа.

## Назначение

Владеет `RoleImage`, immutable `RoleImageRevision`, запросом сборки,
неизменяемым дайджестом образа, tool manifest, кешем, SBOM, происхождением,
проверкой уязвимостей, promotion и состоянием подписи.

## RoleImage и полный Dockerfile

`RoleImage` является mutable identity и контейнером истории. Каждая правка
создаёт новую immutable `RoleImageRevision`; опубликованная revision никогда не
перезаписывается. Revision содержит:

- полный пользовательский Dockerfile как exact UTF-8 source bytes;
- immutable refs и digests разрешённого build context;
- целевые платформы, builder/frontend/toolchain versions и policy digests;
- server-owned final-wrapper revision и runtime ABI digest;
- декларации инструментов с command/probe и безопасными metadata;
- сетевую и registry policy только для build path.

Пользовательский Dockerfile может полностью определять базовые образы, стадии,
пакеты, языки, браузеры и прикладное ПО, но не является окончательным
исполняемым Dockerfile. Платформа проверяет синтаксис и закрытые запреты,
выбирает объявленную terminal user stage и добавляет неизменяемый
platform-owned final wrapper. Wrapper наследует пользовательскую файловую
систему, затем из exact trusted base добавляет `kodex-init` и
`kodex-agent-runner`, назначает обязательные UID, entrypoint, runtime layout,
labels и ABI. Пользовательская инструкция после wrapper не исполняется.
Отсутствующая terminal stage, попытка подменить wrapper contract или
неоднозначный final target закрыто отклоняют revision до сборки.

`spec_sha256` вычисляется по каноническому envelope, который включает exact
байты пользовательского Dockerfile, context descriptor digests,
builder/frontend/platform/toolchain/policy versions, tool declarations и exact
final-wrapper/runtime ABI digests. Изменение любого байта или зависимости
создаёт другую revision и не может неявно переиспользовать прежний artifact.
Reuse разрешён только для exact promoted artifact с тем же полным envelope,
актуальными admission receipt, policy, signature и registry readback.

Полный Dockerfile возвращает только специализированная owner-scoped операция с
обязательной exact version и отдельным правом просмотра source. Status/list
projection не содержит Dockerfile. Build- и runtime-secrets в Dockerfile,
`ARG`, `ENV`, context или revision запрещены; recipe хранит только immutable
content refs без credentials.

## Immutable build и promotion lifecycle

`ImageBuild` принадлежит одной `RoleImageRevision` и фиксирует attempt, fence,
builder inputs, final-wrapper revision и policy snapshot. Retry создаёт новую
attempt, но не меняет source revision. Успешная сборка создаёт immutable
candidate digest в staging; scan, SBOM, provenance, signature и admission
относятся к этому exact digest. Promotion создаёт immutable `PromotedImage` с
`repository@sha256`, evidence digest, runtime ABI и tool manifest digest.

Теги являются только неавторитетными проекциями для человека. Runtime и
окружение используют исключительно promoted digest. Update/archive/delete
`RoleImage` не переписывают опубликованные revisions и artifacts; они запрещают
новые build/promotion claims согласно lifecycle и retention policy.

`RuntimeEnvironmentRevision` pin-ит exact `PromotedImage` digest. Несколько
окружений могут ссылаться на одну promoted revision. Новая image revision не
обновляет окружение автоматически: переход выполняется отдельной versioned
операцией с повторной проверкой tools, resources, network, scoped RBAC и Secret
references. Окружение задаёт runtime settings и не устанавливает ПО.

## Tool manifest

Каждая декларация инструмента содержит стабильные `name`, canonical command,
executable path, version probe, readiness probe и безопасное описание. Builder
выполняет probes после platform finalization в том же effective image и сохраняет
результаты в подписанном immutable tool manifest. Непрошедшая декларация
блокирует admission; автоматически обнаруженный executable без утверждённой
декларации не выдаёт агенту capability.

Окружение выбирает только поднабор manifest и может уточнить пользовательское
описание и usage hint, но не command/path/probe. Runtime повторяет bounded
readiness probes после materialization. Только подтверждённый effective subset
попадает в `RuntimeRevision` и типизированную переменную prompt template.

## Фактический inventory общего toolchain

`recipe.Tools` — immutable OCI-входы, а `ImageArtifact.declaredTools` — их
декларативная проекция. Они никогда не являются доказательством наличия
исполняемого файла. `verifiedToolInventory` содержит отдельный signed read model;
`CURRENT_CONFIGURATION.image_tool_inventory` читает его из exact promoted
artifact текущего owner-scoped окружения. `tools` окружения остаётся выбранным
владельцем набором capabilities; обнаружение программы не выдаёт полномочий.
Создание и validation окружения разрешают command только из VERIFIED
intersection всех платформ signed inventory. Используется фактический basename
executable (`rg`, `tsc`), а не display name либо recipe input. Исторический
UNAVAILABLE artifact не разрешает добавлять tools; пустой поднабор не становится
доказательством наличия программ.

Server-owned BuildKit wrapper выполняет закрытый registry в отдельной стадии
`FROM kodex-final-rootfs`. До неё защищённый runner переносится из exact trusted
runtime-base: пользовательский runner не исполняется. Проверка использует
нативные loader, библиотеки и расположение программ того же финализированного
образа, а не ABI другого образа либо chroot. Exact `kodex-final-rootfs`
дополнительно подключается read-only в `/image`; trusted parent вычисляет SHA256
из этого дерева через `os.Root`. Абсолютные и выходящие за root symbolic links
не получают обхода защиты ради обнаружения инструмента.

Внешняя стадия выполняет probes с `--network=none`. Каждый инструмент запускается
отдельным ограниченным subprocess с UID/GID10001, без дополнительных групп,
capabilities и inherited environment, с закрытым registry exact path/arguments
и явным `GOROOT=/usr/local/go`. До исполнения обязательны `no_new_privs`, Landlock
ABI не ниже 3 и seccomp: Landlock запрещает изменение содержимого и структуры
файлов, включая `TRUNCATE`; единственное разрешение записи относится к проверенному
native `/dev/null`. Seccomp запрещает изменение прав, владельца, timestamps и
extended attributes, опасные namespace/mount/root operations и обходы через
`io_uring`.
Для проверенных stdout/stderr pipes разрешён только `ioctl(fd=1|2, FIONBIO)`:
он переключает nonblocking flag открытого pipe, но не разрешает изменение
файла, metadata либо device ioctl. Иные fd, requests и их high-bit aliases
закрыто отклоняются. Несовместимый ABI, identity либо отказ установки защиты закрыто
отклоняют probe; повтор без sandbox или с дополнительными привилегиями запрещён.
Это не утверждение о read-only всей стадии: trusted parent сохраняет bounded
manifest вне `/image`, а недоверенный child отдельно ограничен ядром. Побочные
эффекты чтения, включая atime, не считаются доказательством неизменности metadata.

Parent сохраняет только bounded version, `MISSING`, `PROBE_FAILED` либо
`VERIFIED`; raw stdout/stderr не сохраняется. Registry включает 38 обязательных
программ из QA §31 и 12 дополнительных, отличающихся признаком `required`.
`image-tool-inventory-validator` материализован в canonical tools/admission
образах и обязательном tool registry фаз. Он проверяет только bounded stdin
через общий закрытый decoder, не загружает credentials и не выполняет RPC;
claim/record остаются отдельной полномочной границей admission bridge.
Этот признак описывает self-development target, а не новую runtime capability.

Manifest связывает platform, spec, immutable build и runtime contract.
Admission извлекает только canonical final path из exact platform digest,
проверяет registry и immutable tuple, и отдельно подписывает outer binding
с image digest, platform digest, manifest SHA256 и provenance SHA256. Signed
inventory входит отдельным immutable layer в evidence v5; receipt v3 содержит
его SHA256. Signature binding v2 и receipt v3 дополнительно связывают exact
admission attempt/fence, полный report projection SHA256 и risk acceptance
SHA256. Promotion восстанавливает исходные подписанные байты, проверяет
digest/подпись/binding и не пересобирает evidence. CP сохраняет payload и digest
в той же транзакции с fenced admission receipt и существующим recipe event.
Отсутствие inventory у исторического artifact возвращается как `UNAVAILABLE`,
без decoder fallback к recipe, автодопуска или ручного заполнения.

| Переход | Authority и binding | Effect / consumer |
| --- | --- | --- |
| Build → probe | Серверный builder claim, exact runtime-base/frontend/toolchain, RO finalized rootfs | Canonical manifest; tool failures не становятся capabilities |
| Scan → sign | Exact index/platform digest и BuildKit provenance; все объявленные OCI tools VERIFIED | Signed inventory binding, evidence v5 / admission workload |
| RecordAdmission | Existing exact workload permission, claim/fence/version/expiry, payload SHA и immutable tuple | Payload + digest + idempotency receipt + existing recipe event атомарно |
| Promotion | Existing одноразовая owner authorization, receipt и evidence manifest digests | Исходные signed layers; exact image/evidence readback |
| Own CURRENT_CONFIGURATION | Existing own SYSTEM/PROJECT lease и canonical current environment/image eligibility | Fresh typed inventory отдельно от immutable execution snapshot; новых событий нет |

Реальный inventory проверяется canonical build/admission; unit и synthetic
recovery fixtures не доказывают наличие toolchain в развёрнутом образе.

## Технический terminal admission

Технический отказ `FAILED` хранится отдельно от verdict `ACCEPTED|REJECTED`:
не создаёт scanner/signature/evidence receipt и не разрешает promotion. Owner
DTO `RoleImageRecipeDetail.admissionFailure` содержит только artifact/version,
recipe/generation, build/attempt, полный scope и закрытый errorCode. Чтение
SYSTEM/ORGANIZATION и PROJECT разрешает только failure текущего рецепта и
последней сборки; старый failure не перекрывает новую attempt или generation.

| Инициатор / переход | Authority и OCC | Транзакция и consumer |
| --- | --- | --- |
| `PENDING → CLAIMED` | image-admission mTLS + fresh application proof; owner выбирает только ACTIVE exact current recipe/latest COMPLETED build | Claim/fence/generation/token и receipt; expired claim не переиспользуется |
| `CLAIMED → ACCEPTED|REJECTED` | Существующий `RecordImageAdmission`, exact token/fence/version/expiry и подписанные digests | Существующие verdict/evidence и recipe event; eligibility без ослабления |
| `CLAIMED → FAILED` | `FailImageAdmission`, exact image-admission method/proof и полный сохранённый immutable tuple; DBclock ещё до expiry | Worker codes только `ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND`, `ADMISSION_EVIDENCE_EXCEEDS_BOUND`, `ADMISSION_WORKER_FAILED`; revoke claim + audit + idempotency receipt + один `ROLE_IMAGE_RECIPE_CHANGED` атомарно |
| Expired `CLAIMED → FAILED` | Dedicated `ExpireImageAdmissionClaim`: fresh maintenance proof того же workload, exact old fence/version/authority-generation/tuple как OCC; без token и caller reason; expiry назначает DBclock | Только `ADMISSION_LEASE_EXPIRED`; тот же атомарный terminal envelope. Existing Claim command также закрывает не более 32 expired claims своей организации, с durable outcome receipt |
| Terminal receipt rejoin | Fresh proof, owner-resolved artifact/organization, неизменный command intent; transport correlation/credential rotation не меняют intent | Повтор возвращает прежний receipt без второго события; expiry rejoin после Claim hook сверяет приватный pin прежнего claim generation, не выдавая его как authority |
| Abrupt FAILED admit / callback outage | Controller разрешает свежий exact managed Job UID/run и сохраняет PVC cursor до удаления FAILED Job | Новая admit Job получает только `ADMIT_PREDECESSOR_FAILED`; bridge повторяет тот же сохранённый claim и closed failure intent. Jobs/PVC остаются до успешного durable owner callback |
| Recovery / cleanup | Cursor содержит только predecessor UID и next UTC; backoff 10s–10m, максимум 24h от API-assigned PVC creationTimestamp | Controller restart восстанавливает cursor; только callback Job success закрывает workspace. Исчерпание бюджета сохраняет workspace и закрыто возвращает technical degradation, без нового claim |

Controller получает только namespace-scoped PVC `update` дополнительно к
существующим capabilities. Workspace VAP допускает UPDATE ровно двух recovery
annotations, запрещает изменения spec, labels, ownerReferences, finalizers и
всех иных annotations; CREATE не принимает готовый recovery cursor. Другой actor
не может добавить, удалить или изменить recovery annotations. Cursor/Job UID
является Kubernetes recovery locator, но не источником control-plane authority.
Forward-only upgrade сначала обновляет migration, CP/gateway, bridge/controller
бинарии и обе exact authority policies. ConfigMap-only обновление скрипта не
материализует callback RPC. Новый штатный Claim сохраняет `authorityGeneration`.
Старый workspace без этого server pin не принимает legacy default или inference:
перед обновлением требуется exact managed Jobs/PVC preflight. Если workspace
пуст, bounded expiry hook первого свежего Claim закрывает старые expired DB claims.
Удаление Kubernetes ресурсов само по себе не является owner terminal proof.
Grant issuer/profile остаётся прежним; registry, локальный CP service allowlist
и generated policy содержат два exact специализированных метода, недоступных
builder, promotion, gateway и controller workloads. Availability read path
не выполняет state transitions. Stale/reclaimed tuples закрыто отклоняются.

Cancel/update/archive/delete сохраняют существующий owner terminal graph;
дополнительных admission retry/delete или поддельного durable verdict нет.
Событие технического terminal имеет cardinality один на закрытый artifact,
origin — owner command либо bounded owner Claim expiry hook; consumers —
существующий platform event/rejoin path и owner recipe detail readback.

## Безопасный отчёт и явное принятие риска

Решение владельца от 2026-10-05 (#1797) допускает узкое исключение из
vulnerability-порога только по явному решению человека `OWNER|ADMIN` организации.
Исключение не отключает policy и не исправляет образ: оно навсегда связывает
один неизменный образ, исходный отчёт и конкретное решение. Ассистент, signed
PROJECT actor, worker и service identity не могут принять риск или назначить
человеческого actor. Locator проекта в endpoint не является authority.

Новый admission сохраняет полный исходный Grype report и безопасную typed
projection `kodex.dev/image-vulnerability-report/v1`. Projection включает все
`matches` и `ignoredMatches`, включая `UNKNOWN`, `LOW`, `MEDIUM` и сведения без
исправления. Ignored findings явно помечаются, считаются отдельно и не становятся
blocking; остальные blocking/no-fix признаки вычисляются неизменной baseline
policy. Неизвестный severity не скрывается и не превращается в доказательство
безопасности. LOW/MEDIUM сами по себе baseline не блокируют.

Одна строка группирует только точное совпадение packageName, installedVersion,
ecosystem, advisoryId, severity, fixState, отсортированных fixedVersions,
blocking и ignored. `occurrences` сохраняет число исходных совпадений;
`matchCount` и каждый severity count равны сумме occurrences, а
`uniqueAdvisoryCount` считает уникальные advisoryId. Ref строки — детерминированный
SHA256 канонического tuple. Ограничения — 10 000 сгруппированных строк и 4MiB
projection; превышение или неполнота являются техническим отказом, не усечённым
`READY`. Исходный report сохраняет прежний evidence budget.

Публичный read path выдаёт только package/version/ecosystem, severity, advisory,
fixState/fixedVersions, occurrences и вычисленные сервером признаки. Для CVE,
GHSA и GO сервер формирует соответственно exact ссылки
`https://nvd.nist.gov/vuln/detail/{CVE}`,
`https://github.com/advisories/{GHSA}` и `https://pkg.go.dev/vuln/{GO}`.
OTHER отображается без ссылки. Raw report, locations, произвольные URL,
credentials и registry addresses не выдаются. Страница ограничена 100 строками;
cursor связывает exact report/projection digests и все фильтры. Projection
связывает scope/organization/project, recipe version/generation, build
version/attempt, artifact/image digest, SBOM/report и policy revision/digest.
Receipt/admission revision и текущая artifact version добавляются owner read
path отдельно, чтобы исключить циклическое хеширование projection и receipt.

| Сценарий / endpoint | Actor и owner-команда | Version, effect и consumer |
| --- | --- | --- |
| GET `organization/role-image-recipes/{recipeRef}/artifacts/{artifactRef}/vulnerability-report` либо PROJECT path | Fresh USER, активный OWNER/ADMIN организации; gateway → `GetOrganizationImageVulnerabilityReport` либо `GetImageVulnerabilityReport` → CP owner eligibility | Exact current recipe/latest build/artifact; безопасная report page, без изменения состояния и события |
| POST тех же artifact paths `/risk-decision`, `REJECT_RISK` | Только тот же human admin; gateway → `DecideOrganizationImageAdmissionRisk` либо `DecideImageAdmissionRisk` | Owner разрешает ресурс до OCC/replay; обязательные reason, If-Match, idempotency и все report/evidence/image/policy pins; immutable decision/audit/receipt и один recipe event атомарно, исходный REJECTED сохраняется |
| POST `/risk-decision`, `ACCEPT_RISK` | Та же specialized owner-команда; actor/time/decisionRef назначает CP | Append-only decision и прежняя admission history, новая PENDING attempt/fence; revoke старого claim, audit/idempotency/один recipe event атомарно; текущая projection не является ACCEPTED |
| PENDING → CLAIMED → ACCEPTED либо FAILED | Existing image-admission workload, fresh grant и exact attempt/fence/expiry; Claim → Record/Fail/Expire | Worker восстанавливает exact исходные report/SBOM/evidence, проверяет image/provenance/runtime ABI/tools/policy; подписывает исходные bytes, decision binding и новый receipt; CP атомарно завершает новую attempt, consumers — promotion и owner rejoin |
| Update/archive/новая build | Existing recipe owner graph | Exact old decision не разрешает иной tuple; stale claim закрыто отклоняется, старые report/decision/receipt остаются историей |

Операции registry — `platform.query.organization.role-images.vulnerability-report.get`,
`platform.query.role-images.vulnerability-report.get`,
`platform.command.organization.role-images.risk.decide` и
`platform.command.role-images.risk.decide`. Они разрешены только OIDC gateway
с `USER_CREDENTIAL_REQUIRED`, без PROJECT authority. CP дополнительно проверяет
свежую роль OWNER/ADMIN и `organization.manage`; transport permission не
заменяет owner rule. `If-Match` совпадает с expected artifact version в body;
OCC включает также recipe/build/admission/report/projection/evidence/policy pins.
Reason обязателен, ограничен 2048 UTF-8 bytes и не содержит control characters.

`kodex.dev/image-risk-acceptance/v1` связывает immutable decisionRef/version,
human actor/time/reason, scope, exact recipe/build/image/report/projection/policy
и исходные admission revision/receipt/evidence manifest digests. Это не waiver
на проект, период или будущие сборки. Attempt/fence не входят в постоянное решение:
их назначает новый owner claim. Автоматическое пересканирование с другим отчётом
не может подменить утверждённый report. Scanner/database/parser/integrity,
provenance, runtime ABI/tools и signature errors никогда не override.

Прежний immutable REJECTED receipt и его evidence не изменяются и не объявляются
задним числом подписанными. Новый admission имеет отдельный attempt-qualified
evidence tag/manifest, подписанный decision binding и новый подписанный receipt;
signer остаётся отдельной identity, private key не передаётся admission worker.
Текущий evidence v5 имеет ровно 26 ordered descriptors с exact title/media type,
size и digest. В него входят исходные SBOM и Grype bytes, inventory/provenance,
полная typed `vulnerability-report.json`, `risk-acceptance.json`, receipt v3
и их detached signatures; signature binding использует v2. Для normal admission
оба risk layers присутствуют пустыми, а risk SHA256 в receipt/binding пустой.
Каждый SBOM и исходный vulnerability report сохраняется четырьмя фиксированными
частями: непоследняя непустая часть ровно 16MiB, короткая непустая часть допускает
только пустые завершающие части. Raw layer limit остаётся 16MiB, общий — 64MiB.
Recovery проверяет закрытый порядок/descriptors, восстанавливает exact исходные
bytes и только затем сверяет logical hashes, полный JSON и signatures.
Promotion проверяет все исходные bytes и новые signatures/tuple, а окружение
получает только новый exact promoted digest после штатного publish/pin.
FAILED/expired attempt не переиспользует прежний token; если отдельная owner
retry-команда не материализована, доступен только штатный rebuild, без queue reset.

Forward-only cutover не выполняет backfill старых reports и не читает evidence
registry от имени CP. Artifact без новой полной projection имеет `UNAVAILABLE`
и nextAction `REBUILD_FOR_REPORT`, без ACCEPT_RISK/REJECT_RISK. Владелец делает
новый typed update/build после обновления migration, policy, CP/gateway,
worker/signer/promotion и итогового render до возобновления controller.
Legacy decoder, NULL fallback, ручное заполнение DB и изменение старых migrations
запрещены. Проекция READY и разрешённые nextActions назначаются owner, не UI.
Новый writer/reader принимает только evidence v5, receipt v3 и signature binding
v2; исторические immutable evidence v3/v4 и receipt v2 не переписываются и не
декодируются запасной веткой ради нового допуска или risk decision.

Append-only history запрещает UPDATE/DELETE отчётов, решений и admission attempts
в ACTIVE, ARCHIVED и trash lifecycle. Единственное retention-исключение —
существующий authorized permanent Project purge: owner в exact защищённом purge
context атомарно очищает только history своего project в общем terminal graph.
Organization history не затрагивается. Caller-set GUC, отключение triggers,
ручной SQL и частичный history-delete не заменяют этот specialized owner path.

## Сборщик и граница исполнения

Kaniko не используется в промышленной конфигурации, поскольку исходный проект
архивирован. BuildKit выполняет сборку с process sandbox от namespace-root в
отдельном workload и обязательном Kubernetes Pod user namespace с
`hostUsers: false`. Контейнер использует `privileged: true` только внутри
remapped user namespace; host-root или host user namespace из этого не
следуют. Профили rootless `newuidmap`, `noProcessSandbox`, host escape и
insecure fallback запрещены. Readiness выполняет тот же Dockerfile `RUN`, что
и рабочая сборка.

Сборщик не получает промышленные учетные данные среды выполнения. Токен реестра пакетов, если нужен, выдается как краткоживущий секрет с ограниченной областью и не попадает в слои образа или логи.

Канонический локальный контур разделяет staging push, staging admin,
promotion writer и node pull по Pod, ServiceAccount, mTLS/Kubernetes Secret identity,
NetworkPolicy и хранилищу. Pull монтирует promoted storage только read-only и
не имеет пути к внутренним endpoints. Отдельный deployable
`services/jobs/role-image-builder` получает server-owned claim. Его trusted
materializer по pull-only mTLS и basic identity читает context/package/tool
только из server-configured OCI repository: exact manifest содержит один слой
утверждённого media type, descriptor size/digest и потоковый payload digest
совпадают. Байты пишутся в private bounded `emptyDir`, тем же immutable
snapshot безопасно разбираются и удаляются после attempt. RWX PVC, ручной
producer и повторное чтение изменяемого inode после hash не входят в путь.
Role image revision не принимает build credentials. Context/package/tool blobs
заранее публикует владелец в закрытый immutable input repository, а trusted
materializer использует только собственную pull-only authority этого
repository. Пользовательские `RUN` не получают credentials через spec, mount,
environment или build context.

Builder обращается к BuildKit через client-only mTLS и публикует только в
staging. Пользовательский Dockerfile исполняется в удалённом worker без
credential files, secret mounts и builder Pod filesystem. После недоверенных `RUN`
защищённые `kodex-init` и `kodex-agent-runner` копируются из exact
trusted base. Output фиксирует exact `USER`, entrypoint/commands, runtime ABI
revision/digest и labels. Отдельный admission owner связывает exact
source/build/image digest с BuildKit provenance, SBOM digest, версией и
результатом vulnerability policy, проверенной signature identity и
OCI admission receipt, чей content и manifest digests фиксируются owner-side.
Staging registry принимает запись только по отдельной BuildKit push mTLS role
и exact Pod network boundary; builder Pod не имеет этой role или egress.
Readiness BuildKit исполняет защищённый `RUN` и реальный push в выделенный
readiness repository, поэтому декларативный worker без рабочего exporter path
не получает readiness.
Update, archive или delete `RoleImage` в той же owner-транзакции закрывает
незавершённые build/artifact и отзывает их build, admission и promotion claims.
Только отдельный HMAC-signed fenced короткоживущий claim, который включает
оба receipt digest, выданный promotion workload после verdict, может быть
owner-side расходован в одноразовую authorization до registry copy;
истечение заменяет claim с повышением generation/fence. До verdict admission
owner публикует bounded evidence bundle (provenance, SBOM, vulnerability
evidence, detached signatures и receipt) как immutable OCI artifact в
выделенный evidence repository. Единственный авторитетный OCI manifest содержит
закрытый набор отдельных layers с точными media type, title, size и digest:
подписанные payload сохраняются как исходные байты без JSON reserialization.
Exact OCI manifest digest фиксируется owner-side; свежая promotion Job по этому
digest восстанавливает каждый layer, сверяет descriptor и подпись над теми же
байтами и только после authorization копирует тот же manifest в закрытый
promoted evidence repository. Authorization
связывает artifact/version/attempt/fence/generation/digests, имеет TTL не больше
Job deadline и durable idempotency receipt. Совместный image/evidence manifest
readback фиксируется owner-транзакцией по одноразовому token, а
pull видит только promoted admitted content. Admin DELETE не выдаётся сборщику
или pull. Userns BuildKit
сохраняет process sandbox, работает без Kubernetes token, прикладных owner
secrets и persistent worker state; ослаблять mTLS или registry scopes запрещено.
Builder сверяет заявленный builder digest с exact BuildKit image, а toolchain
digest — с отрендеренным builder image. Context/tool blobs имеют
digest-named пути, повторно хешируются до BuildKit, устанавливаются offline, а
source context подключается к user stage read-only и не входит в layers.

Фазы сборки достижимы и закрыты: `MATERIALIZATION`, `CONTEXT_VALIDATION`,
`BASE_PULL`, `USER_DOCKERFILE_SOLVE`, `TRUSTED_RUNTIME_FINALIZATION`,
`STAGING_PUSH`, `PROVENANCE`. Финализация означает только server-owned перенос
защищённых runtime-компонентов после пользовательских стадий и не считается
возвратом в общую фазу `SOLVING`.
`ImageBuild` сохраняет только bounded `errorCode`, `diagnosticCode` и безопасный
summary до 256 байт. Raw BuildKit output, Dockerfile text, context paths и
credential values в status/log/audit/provenance не публикуются.

Авторитетный build spec связывает только immutable `contextRef`, user Dockerfile,
tool source refs и их digest. Credential reference не входит в source Proto/OpenAPI,
canonical hash, owner readback или builder claim; private external source
переносится в input repository до создания recipe через owner-side boundary.

BuildKit frontend/base pull использует отдельные `pki-public` CA/SNI и
pull-only Docker config; тот же путь выполняют readiness и production
`buildctl`. Staging write проходит через отдельный trust root и server-side
authorizer, допускающий только CN BuildKit, методы OCI push и два закрытых
repository. Scan/sign/admit/promote читают staging через отдельный read-only
endpoint. Отдельный evidence authorizer принимает OCI write только от exact
`image-admission` mTLS/application identity, только для закрытого evidence
repository и без DELETE/admin; signer и promotion имеют соответственно key-only
и read/target-copy полномочия. Job workspace не является recovery source:
promotion восстанавливает все доказательства из durable OCI manifest digest;
rollback или retry не зависит от прежнего `emptyDir` и не повторяет сериализацию
подписанных данных.

Ожидающие admission и promotion автоматически запускает
`image-admission-controller`. Его единственные полномочия — exact-чтение
immutable typed policy parameters и их runtime `ConfigMap`-проекции, а также
ограниченные операции над собственными Job/PVC. Controller не имеет
control-plane, registry, signing или installation Secret identity фаз. Kubernetes
`ValidatingAdmissionPolicy` проверяет caller ServiceAccount и точный phase
contract: закреплённые образы, команды, env, тома, ServiceAccount и отсутствие
host authority. Поэтому компрометация controller не позволяет использовать его
право `create jobs` для запуска произвольного Pod под scanner, signer,
admission либо promotion identity. Состояние Job/PVC служит только устойчивым
reconcile cursor; owner lifecycle остаётся в `control-plane`.

Node pull на single-node k3s получает отдельный pull-only credential из
owner-controlled installation material. Code-first installer атомарно создаёт
`/etc/rancher/k3s/registries.yaml`, включает только exact HTTPS registry host,
перезапускает k3s и проверяет фактическую конфигурацию и готовность API. Общий
push/admin credential, anonymous fallback, plaintext registry и ручная
незакреплённая настройка host запрещены. Для multi-node/existing-Kubernetes
оператор обязан применить эквивалентный node runtime contract на каждом node.

## Сквозная карта authority и lifecycle

| Шаг | Actor/authority | Exact contract и authoritative effect |
| --- | --- | --- |
| owner create/update/read | verified owner session → control-api-gateway | специализированные manage/get operations, server-owned tenant/owner/generation, version CAS, full Dockerfile access permission и canonical hash в control-plane transaction |
| claim/materialize | role-image-builder SPIFFE + signed build claim | exact recipe/build/attempt/fence/immutable input; pull-only OCI mTLS materializer, private cleanup, bounded failure |
| solve/push | isolated BuildKit client/server mTLS | full user Dockerfile, immutable final wrapper, trusted base/runtime ABI и offline inputs; BuildKit единственный владелец staging push credential/egress |
| orchestrate | image-admission-controller Kubernetes identity + immutable policy + VAP | создаёт только точную последовательность phase Job/PVC; не получает credential фаз и не владеет artifact lifecycle |
| admit | image-admission SPIFFE + artifact claim | exact provenance/SBOM/policy/signature/runtime ABI; receipt и verdict owner-side |
| authorize/promote/complete | image-promotion SPIFFE + consumed claim/token | owner verification до side effect, exact destination digest/readback и durable replay protection |
| environment pin | verified owner session → control-plane | immutable environment revision pin-ит exact promoted `repository@sha256`, tools/resources/network/scoped RBAC и versioned Secret refs |
| runtime revision | runtime-controller SPIFFE + protected read | перед каждым turn/retry/continuation получает current owner versions/evidence, exact environment revision, promoted digest, ABI и effective tool/policy digests |
| Pod materialization | signed workload ticket + broker/webhook/VAP | два exact init и три exact containers; неутверждённый repository, mutable ref и extras отклоняются |

Node pull — отдельная platform boundary: внешний exact DNS/SAN, trusted CA,
per-node client identity, forward-only pull credential generation и exact
rendered node CIDR. Pull registry требует mTLS+application auth; DaemonSet с
`imagePullPolicy: Always` проверяет реальный CRI path на каждом node. Push,
admin и promotion identities не принимаются.

## Secret и RuntimeRevision boundary

Image supply chain не принимает runtime Secret values и не является secret
store. Runtime secrets создаёт и ротирует отдельный `secret-broker` с
минимальным namespace-scoped Kubernetes доступом. Image/environment records
содержат только versioned descriptors; PostgreSQL хранит metadata и безопасный
`display_hint`, но не plaintext или обратимо зашифрованную копию.

Повторное раскрытие D4-B требует отдельного permission, свежей OIDC
re-authentication и одноразового короткоживущего `no-store` ответа напрямую от
`secret-broker`. Hint содержит суммарно не более 15 процентов и максимум 12
символов; короткие, binary и structured значения не раскрывают фрагменты.

Перед каждым turn, retry и continuation создаётся новая immutable
`RuntimeRevision`, которая связывает exact image digest, environment revision,
Secret grants/versions, effective tools, resources, volumes, network, scoped
RBAC и instruction-template digest. Ранее созданная revision не обновляется и
не используется как shortcut после изменения любой из этих зависимостей.

Instruction template исполняется ограниченным Go `text/template` с
типизированными namespaces, allowlisted функциями, validate/preview и `range`
по effective tools. Secret values в template catalog и prompt не передаются.

Прежний recipe, installation-block-only API, mutable image reference,
автоматическое следование окружения за tag и fallback на прежний runtime
contract не поддерживаются. Dual-read/dual-write и миграционная ветвь для
прототипа не создаются.

## Допуск к публикации

Образ доступен агентам после:

- успешной сборки;
- формирования SBOM;
- прохождения versioned политики уязвимостей: полный отчёт сохраняется, а
  `High`/`Critical` с доступной исправленной версией закрыто блокируют допуск,
  кроме нового подписанного admission по точному человеческому ACCEPT_RISK,
  описанному выше;
- фиксации происхождения;
- проверки подписи;
- публикации в разрешенный OCI-реестр.

## Критерии приемки

- Одинаковый рецепт переиспользует дайджест.
- Изменение Dockerfile, context, инструмента, wrapper или ABI меняет хеш.
- Пользовательский Dockerfile не может удалить или подменить final wrapper.
- Неуспешная проверка блокирует использование и дает понятное состояние.
- Среда выполнения запускает дайджест, а не изменяемый тег.
- Окружение pin-ит exact promoted digest и обновляется только явно.
- Перечень инструментов в prompt соответствует подписанному manifest и
  повторной readiness-проверке materialized container.
- Новый turn получает свежую `RuntimeRevision` с exact environment, Secret и
  policy digests.
