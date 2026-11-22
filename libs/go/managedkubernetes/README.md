# Общие значения управляемого Kubernetes

Модуль задаёт одинаковую проверку resource locator и claim binding для владельца
состояния и broker #2145. Он не содержит repository, SQL, RPC adapter, credentials,
сетевых запросов либо фонового lifecycle. Command/risk registry остаётся в
`integrationpackage`; private credential parser и transport — в secret-broker.

`DecodeOperationInput(operation, raw)` — общий закрытый input boundary owner,
package, gateway и broker. Он возвращает `OperationInput` и упорядоченный JSON
без изменения значений чисел. `OperationInputSchema(operation)` возвращает ту же
registered schema; `OperationInputOperations()` позволяет проверить равенство
наборов с command registry. Неизвестная команда не получает fallback. Общий
`internalrpcauth.DecodeStrictJSON` проверяет duplicate keys, trailing data и
глубину16; перед ним проверяются UTF-8 и размер512KiB. Owned schemas закрывают
top-level/options keys, включая case aliases и explicit null scalars, и не
загружают внешние `$ref`. `options` — JSON object, проверенный точной typed
per-operation schema; произвольные flags, URL, target и identity запрещены.

Полный resource body нужен для create/replace/apply/edit/status.update/diff и
auth.reconcile. У него проверяются G/V и metadata witnesses; root fields
ConfigMap, RBAC, CRD не заменяются универсальным `spec/status` whitelist.
Broker обязан проверить pinned discovery GVR/kind и structural/OpenAPI schema
без внешней загрузки refs. JSON Patch — отдельный bounded array с RFC6901 paths;
MERGE/STRATEGIC — partial object, где null имеет смысл удаления. DeleteOptions
сохраняет собственный GVK `v1/DeleteOptions`. Для patch/delete broker привязывает
UID/RV preconditions к trusted current read; input сам не подтверждает CAS.
CREATE не требует несуществующий UID/NamespaceUID. APPLY без UID/RV допускает
только последующую owner-проверку отсутствия либо свежие current object pins,
не назначает право создания по одному отсутствию полей.

`ValidateGrantNamespace` и `OperationInput.ValidateGrantScope` принимают точный
DNS namespace либо явный `@cluster`. Только зарегистрированные
`list.all-namespaces/watch.all-namespaces` принимают `@all-namespaces` и пустой
input namespace. Discovery определяет настоящий scope GVR: `@cluster` не
превращает namespaced collection в all-namespaces. Node/CSR и non-resource
команды требуют `@cluster`. Составной drain отдельно требует Node grant и
точные grants каждого Pod child из server RuntimeRevision; этот parser не
выдаёт дочерние полномочия и не снимает EACH Gate каждого destructive child.

Публичная hermetic проверка shared input: из корня `make
test-managed-kubernetes-input`. Она не создаёт Pod/PG, не читает kubeconfig и
не обращается к кластеру. Проверены Context7 `/websites/kubernetes_io`,
[API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/) и
[kubectl patch](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_patch/):
JSON Patch array отличается от merge/strategic partial object; strategic merge
не поддерживается для CRD и должен закрыто отклоняться broker discovery handler.

`IdentityLocator` описывает только отдельный приватный purpose Secret broker:
identity/target/revision, namespace/name, UID/RV и digest. Это не общий runtime
Secret descriptor. Имя выводится из identity/revision и не даёт полномочий;
подставлять namespace, UID или digest при неполном locator запрещено. Поля
исключены из обычного JSON и диагностического форматирования. Только отдельный
broker-only RPC передаёт их явно; PWA и gateway получают safe identity ref/revision.

`ResourceAddress.Validate` проверяет точный GVR, явную namespace scope, один
URL segment для имени и форму UID/RV. Raw proxy, token и interactive subresources
отклоняются: для них нужны отдельные зарегистрированные transports. Принятый
locator не доказывает ownership, permission, актуальность UID или доступность API.

`ResourceObservation` различает PRESENT с UID/RV, ABSENT с именем без UID/RV
и COLLECTION с явной namespace/GVR scope. Unknown не превращается в PRESENT;
проверка формы не делает denied/failed GET доказательством отсутствия. Owner
должен назначить допустимую operation по исходному grant и проверенному discovery.

`DiscoveryAddress` задаёт отдельные registered `/api`, `/apis`, version и OpenAPI
reads без фиктивного GVR либо raw URL. OpenAPI hash закрепляется owner; полученный
`serverRelativeURL` не исполняется. Broker использует точный non-resource SSAR
path с той же выбранной identity; query hash не превращается в RBAC permission.
Проверены Context7 `/websites/kubernetes_io`, официальные разделы Kubernetes API
(discovery/OpenAPI v3). Наличие этих типов не означает готовность handler либо
выбранного target.

`Binding.Validate` требует ровно один invocation/test ref и полный bounded
lease/fence/generation/input-digest набор. Значения не нормализуются. Fence скрыт
при обычном и Go-форматировании, исключён из default JSON; защищённый transport
передаёт его явно. Проверка формы не заменяет current owner graph и trusted actor.

Неверный или неизвестный input закрыто отклоняется без исходных значений в ошибке.
Тесты используют только disposable строки и не подтверждают live RBAC либо account.

`BuildStreamEffectResult` и `ValidateStreamEffectResult` задают общий canonical
result для broker и owner: только `stream.localState`, `remoteOutcome` и
наблюдённый `exitCode`, когда он существует. Исходный `TerminalReceipt` сначала
проходит protocol validation; owner берёт его из своего stored graph, не из
caller payload. Сравниваются exact bytes/digest/outcome/reason без второго
permissive decoder. Remote SUCCEEDED с local LOST не означает полную доставку;
stdout, origin refs и private metadata в projection не входят. Helpers не
выдают output credit. Для PREPARED/ACTIVE без terminal receipt UNKNOWN+`{}`
обрабатывается отдельно владельцем, а не имитирует terminal projection.


`CopyProfile` разделяет upload source pins и server-assigned download artifact.
`DecodeCopyProfile` и `NativeCommand` задают закрытые FILE/TAR parameters и
fixed argv; они не проверяют право на artifact, tar content или разрешение на
Pod exec. Owner выполняет source/archive validation и publication, broker
сохраняет original cp origin, current grants и native budgets. Матрица:
[managed Kubernetes](../../../docs/operations/managed-kubernetes-2145.md).

`NormalizeCopyUpload`/`NormalizeCopyDownload` проверяют source SHA и complete TAR
перед native upload либо captured publication. FILE upload оборачивает raw bytes
ровно destination basename; FILE download извлекает только один regular member
ожидаемого имени. TAR сохраняет regular/directory members с конечными лимитами
4096 entries, depth64 и approved wire bytes, включая headers/padding/EOF.
Traversal, aliases/duplicates, links, device/FIFO/sparse entries, special modes и
unknown PAX extensions закрыты. Owner не распаковывает архив в host filesystem:
нормализация удаляет ownership/timestamps/extended metadata, сохраняет только
безопасные path/mode/data и возвращает owned bytes plus exact digest/revision1.

Эти helpers не подтверждают artifact ACL, текущую scan/retention eligibility,
owner grant, native result либо публикацию. Producer связывает original immutable
ArtifactRevision/SHA с normalization revision и exact normalized digest до
открытия upload; captured download отдельно проходит existing artifact scanning,
provenance и current owner eligibility. Receiver-side filesystem isolation от
preexisting remote symlinks не заявляется: действие ограничено уже разрешённым
Pod/container exec scope.
