---
id: OPS-DOC-2145-RESOURCE
title: Ресурсный HTTP consumer управляемого Kubernetes
type: operations-guide
status: approved
owner: architect
version: 1.0.4
updated: 2026-11-15
---

# Ресурсный HTTP consumer Kubernetes

Источник — [#2145](https://github.com/codex-k8s/kodex/issues/2145),
`GUIDE-DOC-003`, `GUIDE-DOC-006`, `GO-DOC-006`. Это часть общего047/108,
не самостоятельная поставка. Ресурсный adapter не выдаёт owner permit,
не читает kubeconfig клиента и не подменяет выбранную identity операторской.
Наличие зарегистрированных60 операций не подтверждает их доступность выбранному
пользователю. Этот consumer реализует27 записей с `Execution=RESOURCE`;
stream/composite и защищённый Secret upload принадлежат другим согласованным
consumer того же unit. Четыре безадресных чтения ниже используют тот же owner
ledger; классификация PLAN/OFFLINE в реестре не расширяет их полномочия.

## Безадресные чтения метаданных

Закрытый `KubernetesMetadataRead` допускает только `kubernetes.version`,
`kubernetes.identity`, `kubernetes.help`, `kubernetes.config.metadata`.
Инициатор, Connection, target, private identity и grant выводятся из обычного
owner snapshot. Требуется точный READ/NONE grant с namespace `@cluster`;
ввод не содержит адреса ресурса, discovery, UID или schema observation.
Пустой child observation хранится как JSON object `{}` в047, а не как
выдуманный ресурс либо `null`. Это не общий допуск произвольных PLAN операций.

| Переход                       | Проверка и результат                                                                                                                                                                                                                  |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Resolve → Prepare             | Private purpose identity, exact target/cluster UID и текущий SelfSubjectReview. Для version дополнительно SSAR точного `GET /version`; business GET ещё не выполняется                                                                |
| PrepareChildren               | Owner закрепляет исходный canonical input, digest, grant/version и один child без resource observation; proposal пуст. Нет отдельного события, авторитетен readback того же plan                                                      |
| Begin → Execute               | Обычный одноразовый owner effect; повторная cluster/identity/RBAC проверка, bounded native request. Version возвращает закрытый набор полей; identity исключает Extra; config возвращает только targetRef/revision/clusterUID/managed |
| Help                          | Данные закрытого реестра и input schema. `effectiveReadinessChecked=false`: зарегистрированная команда не объявляется доступной пользователю либо реализованной по наличию справки                                                    |
| Complete → result             | Positive Close/join, owner receipt и свежая проверка selected identity; исторический version result требует текущего GET /version RBAC без повторного business GET                                                                    |
| Cancel/revoke/expiry/recovery | Общие owner lease/fence/deadline и независимый cleanup context сохраняются. Терминальный child читается через owner reconciliation без нового исполнения; отзыв закрывает выдачу bytes. Отдельного metadata события нет               |

Dynamic discovery/api-resources, explain и access review не входят в этот список:
их итоговый план должен сохранять точные non-resource paths и повторно проверять
их перед выдачей истории. Доступ к `/api`/`/apis` сам по себе этого не доказывает.
Проверены официальные [Kubernetes API](https://kubernetes.io/docs/concepts/overview/kubernetes-api/)
и [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/)
через Context7 `/websites/kubernetes_io`; документация не заменяет квалификацию
закреплённого client-go0.36.3 и фактического target.

## Карта полномочий и жизненного цикла

| Переход                      | Источник и проверка                                                                                                                                             | Результат и авторитетный read path                                                                                                                                        |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Инициатор → owner            | Root actor/delegation, exact Connection/target/identity revision, namespace grant и immutable input назначает либо разрешает CP; payload не является authority  | Private Resolve snapshot, общий owner ledger; отдельного события у нижнего adapter нет                                                                                    |
| Resolve → Prepare            | Shared `DecodeOperationInput`/`EncodeOperationInput`, actual cluster UID, выбранная identity SelfSubjectReview/SSAR, APIResourceList и exact immutable OpenAPI3 | `ResourceObservation` с schema/object digest; Prepare выполняет только read/review, не mutation/dry-run                                                                   |
| Secret Prepare → Bind        | Публичный input содержит только exact `secretMaterialRef`; upper owner разрешает private purpose/operation/patchType/target/namespace/name/hash/expiry          | Нижний Bind сразу получает exclusive ownership, проверяет checksum и полный proposed object; до Bind Observation не ready                                                 |
| Prepare → effect             | CP связывает child proposal с owner grants/version и свежим EACH/SCOPED решением; dynamic floor не повышается автоматически                                     | Только затем верхний consumer вызывает Execute с deadline; нижний adapter не может сам создать/renew permit                                                               |
| Execute → request            | Fresh cluster/subject/SSAR, policy evidence и schema index hash; native request содержит exact UID/RV для existing object                                       | Один fresh HTTP/1 request, без redirects/ambient proxy/GetBody/retry; deadline не расширяется                                                                             |
| Response → Complete          | Actual ACK identity/GVK проверяются; bounded safe JSON и digest поступают upper owner                                                                           | Fresh result credit/Complete до выдачи каждого READ byte, включая partial/error output; adapter не публикует событие самостоятельно                                       |
| Cancel/revoke/expiry/UNKNOWN | Верхний owner закрывает lease/grants/credits и присоединяет consumer; lower Close cancel/join before clearing owned buffers                                     | Transport ambiguity после dispatch — UNKNOWN, не доказательство остановки remote effect. Lower preparation не переисполняется; immutable owner history остаётся read path |

Закреплённый `ChildCandidate` возвращается владельцем в каждом resource child
через `ManagedKubernetesChildEffect.observation`. В ответе сохраняются исходные
state, адрес с UID/RV, object/schema digests, optional namespace UID и Pod witness.
Брокер проверяет вложенные поля и совпадение адреса с каноническим вводом;
пропущенное наблюдение не восстанавливается из текущего состояния Kubernetes.
Исключение — зарегистрированные безадресные PLAN/OFFLINE команды: у них нет
ни resource, ни observation, ни node transport. Неизвестная команда этим
исключением не пользуется.

При возврате после Human Gate новое private preparation сравнивается с полным
исходным наблюдением **до Begin**. Изменение object/schema digest, state,
UID/RV или двух известных разных namespace UID требует нового допуска;
старый план не заменяется свежим наблюдением под прежним approval.
Исходный optional namespace witness остаётся в плане, даже если повторное
наблюдение его не содержит. Это не вводит обязательный GET namespace
и не обещает атомарную защиту от пересоздания namespace.

CREATE — collection proposal без выдуманного ABSENT и без обязательного GET
именованного объекта или namespace. `metadata.name`/`generateName` связывает
immutable body; один поздний POST, конфликт409 не вызывает upsert. APPLY
отдельно требует authenticated current GET; authenticated NotFound может дать
ABSENT только без прежнего UID/RV. ABSENT APPLY выполняет один POST. PRESENT
APPLY сохраняет native SSA/fieldManager/force, local structured-merge проверяет
полный результат с managedFields; UID/RV остаются в request. Стратегическая
patch использует exact registered OpenAPI model только для известных built-in
типов; неизвестная CRD strategy закрыто отклоняется без MERGE fallback.

Typed round-trip использует общий `EncodeOperationInput`, а не обычный
`json.Marshal(OperationInput)`: `omitempty` удаляет обязательные empty group/
namespace у core/cluster/all-namespace resource shapes. Encoder восстанавливает
только эти schema-required keys и проверяет UTF-8 до lossy replacement;
остальные обязательные поля не выдумываются. Shared parser остаётся единственным.

Обоснование wire fence проверено по официальному исходному Kubernetes1.36.3:
[patcher.applyPatch](https://github.com/kubernetes/kubernetes/blob/v1.36.3/staging/src/k8s.io/apiserver/pkg/endpoints/handlers/patch.go#L538-L563)
отклоняет supplied UID при отсутствии текущего объекта;
[DefaultUpdatedObjectInfo.Preconditions](https://github.com/kubernetes/kubernetes/blob/v1.36.3/staging/src/k8s.io/apiserver/pkg/registry/rest/update.go#L168-L188)
переносит UID PUT в storage precondition;
[Store.Update](https://github.com/kubernetes/kubernetes/blob/v1.36.3/staging/src/k8s.io/apiserver/pkg/registry/generic/registry/store.go#L577-L688)
проверяет storage precondition и resourceVersion. Это не новый универсальный
HTTP `If-Match` и не доказательство поведения произвольного HTTP API.
Context7 `/websites/kubernetes_io` дополнительно проверен для SSA,
ObjectMeta и Preconditions; описан именно выбранный Kubernetes wire protocol.

Именованный READ может вернуть более новый RV того же UID/name/namespace,
если caller не выбрал explicit RV pin. READ виртуального read-only API без UID/RV
имеет отдельный `UNVERSIONED_READ`, никогда не PRESENT с выдуманными pins;
owner допускает такой child только для READ/NONE. Mutation/Pod stream не
наследуют это исключение. DELETE Status подтверждает принятие удаления,
не исчезновение объекта; post-RV не выдумывается, ожидание — отдельный consumer.
Sensitive core Secret/Pod/ServiceAccount, auth/RBAC/certificate objects и
subresources не используют unversioned exception; missing pins там закрываются
до выдачи и не становятся generic projection fallback.

## Выдаваемые данные и Secret

### Точечные команды изменения ресурса

`set image/env/resources/serviceaccount` работают с известным расположением
Pod spec; `set selector` — только с Service; `set subject` добавляет subjects
RoleBinding/ClusterRoleBinding, сохраняя прежние subjects и roleRef.
`rollout pause/resume` применяются к Deployment, `cordon/uncordon/taint` — к Node.
Все11 команд вычисляют один merge patch из наблюдённого объекта, затем проходят
общий путь проверки полной схемы, текущего и предлагаемого объекта, UID/RV,
свежих прав и однократного dispatch. Это не запуск kubectl с произвольными flags.
Patch собирается из закрытого набора полей команды без generic JSON diff:
целые числа в заменяемых массивах контейнеров сохраняются точно, включая значения
больше2^53. Существующий native patch adapter выполняет реконструкцию; отдельного
интерпретатора patch нет. Wire regression проверяет обычный и init-контейнер.

Контейнеры выбираются по точным именам либо единственному `*`; отсутствующее имя
или смешивание wildcard с именами отклоняется. Порядок существующих контейнеров,
переменных и taints сохраняется. Новые env names сортируются; существующие env
values заменяются на месте. Requests/limits объединяются по явно указанным
ключам через штатный Kubernetes Quantity parser. Изменение существующего taint
требует `overwrite`; удаление отсутствующего taint отклоняется. Добавление уже
присутствующего RBAC subject не создаёт дубликат, включая отсутствующий пустой
apiGroup у ServiceAccount. Проверки безопасности serviceAccount и RBAC сохраняются;
Human Gate сам по себе не разрешает запрещённое повышение привилегий.

Семантика сверена с исходниками
[kubectl v0.35.5 set](https://github.com/kubernetes/kubectl/tree/v0.35.5/pkg/cmd/set)
и Context7 `/kubernetes/kubernetes`. Публичный `make test-managed-kubernetes-resource`
включает фактический TLS request для каждой команды, проверку условного PATCH,
отзыва прав, смены схемы, конфликт409 и неопределённый результат500 без повторного
dispatch. Это синтетический API; приёмка установленного пути через сотрудника,
owner ledger и Human Gate проверяется отдельно. Составные PLAN/OFFLINE команды
этими тестами не подтверждаются.

### Проекция ответа

Mutation ACK содержит только GVK и name/namespace/UID/RV; write grant не
получает full-object echo как скрытое READ permission. READ возвращает JSON
не более64KiB без усечения. List сохраняет continuation и фильтрует каждый
protected namespace; полная namespace qualification inventory не использует
label selector и отклоняет unknown tail. Secret всегда проходит существующий
`ProjectSecret`: только bounded metadata/type/immutable/keyNames, без values,
annotations/managedFields. Arbitrary error body, URL, material и transport
diagnostics не попадают в Result/errors/logs.

Private CREATE/REPLACE/APPLY/EDIT material — узкий JSON `{data?,stringData?}`,
без metadata/type/target selectors. Private PATCH — полный native JSON/MERGE/
STRATEGIC документ; public body при reference строго пустой `[]`/`{}`.
Совмещение двух patch документов запрещено. Нижний adapter проверяет full
reconstruction, decoded size≤1MiB, key/base64/UTF-8, UID/RV и policy. Copy/move
из private data/stringData в public metadata/type/другие поля запрещены;
native data→data сохраняется. Ошибочный Bind отравляет preparation и очищает
переданный owned buffer; replacement binding не разрешается. Upper protected
upload/material resolver ещё должен связать exact ref/operation/destination
владелецким ledger; один opaque ref не подтверждает эту связь.

## Operator-qualified storage и controllers

`ResourcePolicy` — bounded inline metadata существующего operator target revision
и canonical digest. Caller не может прислать allow/profile/credential selector.
Broker ParseCredential клонирует policy и сверяет checksum; fresh owner Resolve/
Begin/renew независимо сохраняют target revision, retirement и eligibility.

`CSI_APP_DATA_V1` действительно разрешает ordinary PVC mount после свежих GET
выбранной identity: exact PVC UID → bound PV claimRef UID/name/namespace →
StorageClass UID/provisioner → CSIDriver UID/full spec hash. Сверяются whole
PV volume source/mount options, volume mode и readOnly. Все CSI Secret refs,
tokenRequests, hostPath/mixed/substituted sources отклоняются даже при matching
operator hash. Не выводим provisioner из annotation/managedFields. Filesystem
и Block mount/device режимы различаются. Missing profile/read permission даёт
`KUBERNETES_TARGET_STORAGE_POLICY_REQUIRED`, без operator credential fallback.

Inline CSI/ephemeral volumes и автоматическое создание PVC через StatefulSet
volumeClaimTemplates не получают такой профиль автоматически. Они остаются
явно unqualified, пока нет соответствующего server-owned provider consumer.
Это не подтверждение полного охвата storage выбранного target.

Custom GVR допускается только exact GVR/kind/OpenAPI digest и finite reviewed
controller consumer/extractor. Broker свежо соединяет exact Deployment или
StatefulSet UID/full template с полными namespace ReplicaSet/Pod inventory,
controlling owner UIDs, stable counters/revisions, actual container image IDs и
ready/running/init-terminal состояниями. Desired object/counter/label selector
сам по себе не является serving proof. Extra old/debug/foreign-SA/unready Pod,
init/ephemeral/template drift либо unknown tail дают
`KUBERNETES_CONTROLLER_RUNTIME_UNAVAILABLE`. Неподдержанный профиль даёт
`KUBERNETES_CONTROLLER_POLICY_REQUIRED`; схема сама не доказывает безопасность контроллера.
`POD_TEMPLATE_V1` квалифицирует только namespaced GVR с тем же namespace
результирующих Pod; template metadata не может скрыто выбрать чужой namespace.
Cluster-scoped CRD не получает implicit namespace fan-out из `@cluster`.
Operator qualification утверждает именно этот finite consumer, а не разрешение
произвольного поведения controller image; неизвестные иные semantics закрыты.
Admission-added Pod spec, не совпадающий с qualified template за вычетом finite
scheduling/StatefulSet name fields, не игнорируется.

Built-in Pod/workload policy проверяет полный current и reconstructed proposed
object, включая Scale parent того же UID/RV. ServiceAccount/token/Secret mounts
и env, host namespaces/ports/paths, опасные capabilities/privilege и неизвестные
volume sources закрыты независимо EACH. `automountServiceAccountToken=false`
явно обязателен. Optional namespace UID остаётся witness: name-scope ordinary
RBAC не требует GET namespace; известная наблюдённая смена UID закрывает новый
admission у owner, отсутствие наблюдения не доказывает ни замену, ни atomic fence.

## Квалификация

Публичная bounded точка — `make test-managed-kubernetes-resource` (Go1.26.10,
race,180s outer/120s package budget). Fixtures используют disposable TLS CA,
selected token/certificate и production HTTP transport без живого Kubernetes.
Они проверяют16 actual resource wire paths, pre-effect denial/freshness,
UNKNOWN ACK, private Secret Bind/redaction, native patch/SSA/status/scale,
fresh ordinary PVC chain и controller serving join. Другие suites и финальный
source SHA фиксируются в общем047 qualification manifest. Этот документ не
заявляет установленный Kubernetes, full owner execution,048/049 readiness либо
live business effects; их проверяет полный согласованный unit.

Каждый lower Result.ReasonCode берётся из общего closed owner registry; локальные
aliases не пересылаются в Complete. Перед SUCCEEDED фактическая safe projection
проходит тот же `ValidateEffectResult` с точным SHA256 и depth16, что owner.
Более глубокий provider source может быть прочитан для private validation, но
несовместимый выход даёт READ FAILED либо write UNKNOWN с
`KUBERNETES_RESPONSE_INVALID`, без raw bytes. Верхний consumer формирует пустой
metadata receipt `{}` для такого отказа, не подставляет private source в result.
