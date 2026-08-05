---
id: DOM-MC-010
title: Образы и цепочка поставки
type: domain
status: approved
owner: architect
version: 0.4.0
updated: 2026-08-05
---

# Образы и цепочка поставки

## Назначение

Владеет `RoleImageRecipe`, запросом сборки, неизменяемым дайджестом образа, кешем, SBOM, происхождением, проверкой уязвимостей и состоянием подписи.

## Рецепт

Рецепт содержит:

- закрепленную ссылку или дайджест базового образа;
- целевые платформы;
- типизированные пакеты ОС, языков и инструментов;
- возможности браузера и тестирования;
- необязательный проверенный администратором сценарий установки;
- сетевую политику и политику реестра на время сборки;
- метаданные для каталога инструментов в промпте.

Хеш вычисляется по канонической сериализации полной спецификации: base/source/
context/builder/frontend/platform/package/tool/toolchain/policy digests,
версиям и multiline installation block. Изменение любого байта installation
block меняет `spec_sha256`; этот hash входит в image labels и provenance binding,
поэтому новый рецепт не может переиспользовать старый manifest digest. Reuse
разрешён только для exact promoted artifact с актуальными admission receipt,
policy, signature и registry readback.

Installation block необязателен: пустая строка является единственным
каноническим значением отсутствия. Status projection не содержит этот block.
Полную редактируемую specification, включая пустой/multiline block и только
ссылки на secrets без значений, возвращает специализированный owner-scoped
`GetRoleImageRecipe` с обязательной exact version.

## Сборщик

Kaniko не используется в промышленной конфигурации, поскольку исходный проект
архивирован. Rootless BuildKit выполняет сборку с process sandbox в отдельном
workload, Kubernetes user namespace, `hostUsers: false`, `procMount: Unmasked`
и rootless AppArmor/seccomp primitives. Privileged, host escape,
`noProcessSandbox` и insecure fallback запрещены. Readiness выполняет тот же
Dockerfile `RUN`, что и рабочая сборка.

Сборщик не получает промышленные учетные данные среды выполнения. Токен реестра пакетов, если нужен, выдается как краткоживущий секрет с ограниченной областью и не попадает в слои образа или логи.

Канонический локальный контур разделяет staging push, staging admin,
promotion writer и node pull по Pod, ServiceAccount, mTLS/Vault identity,
NetworkPolicy и хранилищу. Pull монтирует promoted storage только read-only и
не имеет пути к внутренним endpoints. Отдельный deployable
`services/jobs/role-image-builder` получает server-owned claim. Его trusted
materializer по pull-only mTLS и basic identity читает context/package/tool
только из server-configured OCI repository: exact manifest содержит один слой
утверждённого media type, descriptor size/digest и потоковый payload digest
совпадают. Байты пишутся в private bounded `emptyDir`, тем же immutable
snapshot безопасно разбираются и удаляются после attempt. RWX PVC, ручной
producer и повторное чтение изменяемого inode после hash не входят в путь.
Пустой `buildSecretRefs` не создаёт обязательного Secret volume; непустые
immutable refs доступны только trusted fetch authority, а значения не
передаются BuildKit.

Builder обращается к BuildKit через client-only mTLS и публикует только в
staging. Installation block исполняется в удалённом worker без credential
files, secret mounts и builder Pod filesystem. После недоверенного `RUN`
защищённые `mattercodex-init` и `matter-codex-agent-runner` копируются из exact
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
Update, archive или delete рецепта в той же owner-транзакции закрывает
незавершённые build/artifact и отзывает их build, admission и promotion claims.
Только отдельный HMAC-signed fenced короткоживущий claim, который включает
оба receipt digest, выданный promotion workload после verdict, может быть
owner-side расходован в одноразовую authorization до registry copy;
истечение заменяет claim с повышением generation/fence. Admission receipt
до verdict публикуется отдельным staging OCI artifact с exact subject, а
promotion воспроизводит тот же payload с exact promoted subject. Authorization
связывает artifact/version/attempt/fence/generation/digests, имеет TTL не больше
Job deadline и durable idempotency receipt. Совместный image/receipt manifest
readback фиксируется owner-транзакцией по одноразовому token, а
pull видит только promoted admitted content. Admin DELETE не выдаётся сборщику
или pull. Rootless BuildKit
сохраняет process sandbox, работает без Kubernetes token, прикладных owner
secrets и persistent worker state; ослаблять mTLS или registry scopes запрещено.
Builder сверяет заявленный builder digest с exact BuildKit image, а toolchain
digest — с отрендеренным builder image. Package/tool blobs внутри context имеют
digest-named пути, повторно хешируются до BuildKit, устанавливаются offline, а
source context подключается к installation step read-only и не входит в layers.

Фазы сборки достижимы и закрыты: `MATERIALIZATION`, `CONTEXT_VALIDATION`,
`BASE_PULL`, `SOLVING`, `INSTALLATION`, `STAGING_PUSH`, `PROVENANCE`.
`ImageBuild` сохраняет только bounded `errorCode`, `diagnosticCode` и безопасный
summary до 256 байт. Raw BuildKit output, installation text, context paths и
credential values в status/log/audit/provenance не публикуются.

## Сквозная карта authority и lifecycle

| Шаг | Actor/authority | Exact contract и authoritative effect |
| --- | --- | --- |
| owner create/update/read | verified owner session → control-api-gateway | специализированные manage/get operations, server-owned tenant/owner/generation, version CAS и canonical hash в control-plane transaction |
| claim/materialize | role-image-builder SPIFFE + signed build claim | exact recipe/build/attempt/fence/immutable input; pull-only OCI mTLS materializer, private cleanup, bounded failure |
| solve/push | isolated BuildKit client/server mTLS | trusted base/runtime ABI и offline inputs; BuildKit единственный владелец staging push credential/egress |
| admit | image-admission SPIFFE + artifact claim | exact provenance/SBOM/policy/signature/runtime ABI; receipt и verdict owner-side |
| authorize/promote/complete | image-promotion SPIFFE + consumed claim/token | owner verification до side effect, exact destination digest/readback и durable replay protection |
| runtime revision | runtime-controller SPIFFE + protected read | current owner versions/evidence и exact promoted `repository@sha256` + ABI |
| Pod materialization | signed workload ticket + broker/webhook/VAP | два exact init и три exact containers; legacy repository, mutable ref и extras отклоняются |

Node pull — отдельная platform boundary: внешний exact DNS/SAN, trusted CA,
per-node client identity, forward-only pull credential generation и exact
rendered node CIDR. Pull registry требует mTLS+application auth; DaemonSet с
`imagePullPolicy: Always` проверяет реальный CRI path на каждом node. Push,
admin и promotion identities не принимаются.

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
- Изменение сценария, инструмента или основы меняет хеш.
- Неуспешная проверка блокирует использование и дает понятное состояние.
- Среда выполнения запускает дайджест, а не изменяемый тег.
- Перечень инструментов в промпте соответствует фактическому манифесту образа.
