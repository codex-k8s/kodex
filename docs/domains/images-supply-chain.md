---
id: DOM-MC-010
title: Образы и цепочка поставки
type: domain
status: approved
owner: architect
version: 0.6.0
updated: 2026-08-29
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

## Сборщик

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
- прохождения политики уязвимостей;
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
