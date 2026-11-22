---
id: OPS-DOC-2145-CONSUMER
title: Контракт package и typed consumer управляемого Kubernetes
type: operations-guide
status: approved
owner: architect
version: 1.0.0
updated: 2026-11-15
---

# Контракт package и typed consumer Kubernetes

Источники: [#2145](https://github.com/codex-k8s/kodex/issues/2145),
`GUIDE-DOC-003`, `GUIDE-DOC-006`, `GO-DOC-006`. Это контракт вклада в полный
unit047/108, а не самостоятельная поставка или подтверждение установленного
target. Наличие `READY` у adapter descriptor означает зарегистрированный
исполняемый интерфейс; readiness выбранного target принадлежит owner/broker и
не следует из package, HTTP200 либо сохранения setup metadata.

`kubernetes-managed`1.0.0 содержит60 операций из `KubernetesOperations()`.
Точный набор совпадает с `managedkubernetes.OperationInputOperations()` и
квалифицируется тестом, а не копией числа в worker. Штатный health — только
`kubernetes.identity`. Profile21 `KUBERNETES_SCOPED_RESOURCE_V1` требует этот
inputless prerequisite; generic observation enum не расширяется. Actual subject
digest и current access checks получает broker с выбранной identity. Health не
подтверждает чтение всех ресурсов или право записи.

| Граница                                      | Источник authority и поведение                                                                                                                                                                                                                      |
| -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| USER → setup                                 | Проверенный actor и owner-local CLI; PWA получает только safe metadata и одноразовый admission, не kubeconfig                                                                                                                                       |
| Connection                                   | `target_ref` разрешает owner по operator registry. Optional `service_subject_ref` — locator активного SERVICE только для явного PROJECT_SHARED; personal policy с непустым service selector отклоняется owner                                       |
| Grant → specialized owner command            | Текущие ACL и actor из transport; Connection OCC и idempotency. `kubernetesNamespace` — точный proposed namespace, `@cluster` либо отдельный READ `@all-namespaces`; final scope/digest назначает owner                                             |
| Обновление/отзыв grant                       | `kubernetesGrantRef` указывает существующую точную строку того же Connection/capability/recipient/namespace; create оставляет ref пустым. Caller ref не заменяет owner lookup                                                                       |
| RuntimeRevision → invocation claim           | Текущий root initiator/delegation, exact grants, package/config/identity и input snapshot разрешает CP; namespace из payload не выдаёт чужие child grants                                                                                           |
| Dedicated lane → `Adapter.ExecuteKubernetes` | Request metadata сверяются с package, input/scope digest и typed profile21. Binding передаётся отдельным параметром из owner claim; input не может выбирать actor, target, credential или binding                                                   |
| Adapter → broker                             | Один `ManagedKubernetesService.ExecuteManagedKubernetesOperation` с binding; внутренний mTLS endpoint `secret-broker.kodex-system.svc:8443`, canonical TLS SNI из deploy. Нет generic credential reader, provider HTTP proxy либо обычного `Result` |
| Broker → CP owner → Kubernetes               | `ResolveManagedKubernetesOperation`, fresh identity/discovery/SSAR, `PrepareManagedKubernetesChildren`, `BeginManagedKubernetesEffect` и exact current-object CAS. Host-escalating и credential-owning objects запрещены независимо EACH            |
| Completed result                             | Safe JSON до64KiB, exact digest и закрытый outcome. Dedicated lane получает свежий owner credit до выдачи READ bytes, включая partial/error data; adapter не создаёт success receipt                                                                |
| Stream                                       | Descriptor с exact invocation/generation, сроком, channel/frame/total bounds; отдельные owner Begin/Complete frame checks, renew10s→30s, cancel/join. Descriptor не является terminal result                                                        |

`ActiveStream.TerminalEvidence` после join возвращает копию фактической
native-квитанции, отправленной при закрытии потока. Она сохраняет origin,
handshake digest и наблюдённый exit/EOF либо UNKNOWN; локальный caller не
собирает её заново из одного статуса. Потеря owner ACK не уничтожает эти факты,
но readback не доказывает принятие квитанции владельцем и не выдаёт output credit.
Текущий REVOKED/CANCELED остаётся отдельным авторитетным состоянием. Верхний
consumer должен сверить сохранённый owner receipt и выполнить Complete effect
до business completion; наличие этого readback само по себе не подключает
stream RPC и не означает готовность всего пути.

В composition брокера `ResourceOperations` использует тот же private identity
store и owner client, что setup/health. Весь unary interceptor/handler path
входит в общий `RequestLifecycle` до обработки; release выполняется после
синхронного cleanup, включая native Close и bounded owner completion.
HTTP setup использует `RequestGate` и заранее резервирует listener до readiness.

| Состояние локального компонента | Остановка и зависимости |
| --- | --- |
| Ошибка startup до Serve | Закрыть собственный listener; запросов ещё нет, зависимости можно освободить |
| Отмена работающего приложения | Закрыть admission, отменить request contexts, выполнить bounded graceful/forced stop внутри worker до `WorkerGroup.Wait` |
| Неожиданный выход Serve | Закрыть admission, выполнить cleanup и вернуть ошибку; выход не означает здоровую остановку |
| Shutdown deadline при живом handler | Вернуть ошибку, сохранить owner/provider/verifier/telemetry; `Close`/`Stop` и Serve return не подменяют join |
| Фактическое завершение всех handlers и транспорта | Разрешить закрытие общих зависимостей; remote outcome остаётся отдельным фактом |

HTTP hijack и долгоживущие native streams не покрываются одним unary/HTTP
request join: их отдельные workers обязан присоединить соответствующий
consumer. Общий hook не объявляет stream RPC готовым. Неполная owner readiness
по-прежнему закрывает запуск профиля.

Семантика остановки проверена через Context7 `/golang/go` и исходники точного
Go1.26.10: `net/http.Server.Shutdown` после deadline не ждёт оставшиеся handlers,
`context.AfterFunc` stop не подтверждает завершение callback. TLS lifecycle
проверки включены в `make test-managed-kubernetes-identity-setup`; они не
являются live-проверкой учётных данных пользователя.

Input проверяет один общий parser/schema. Top-level и options имеют точные
camelCase keys; duplicate/case aliases, explicit null scalars, неизвестные
поля, malformed UTF-8, глубина более16 и input более512KiB закрыто отклоняются.
Full resource body, partial merge/strategic patch, JSON Patch array и
DeleteOptions не смешиваются. CREATE не требует ещё не существующий UID;
existing effects связываются с UID/RV. APPLY без таких witnesses не доказывает
отсутствие: owner обязан разрешить ABSENT/current object. Полную GVR/kind/schema
проверку и preconditions patch/delete выполняет broker по pinned discovery;
внешние `$ref` не загружаются.

READ использует NONE; обычный WRITE — SCOPED с разрешённым более строгим EACH.
Destructive, sensitive, interactive и зарегистрированные privilege operations
требуют EACH. Дополнительно общая `ValidateInvocationApprovalPolicy` закрыто
отклоняет SCOPED для cluster-scoped nonREAD и auth/RBAC/certificate groups.
Owner повторяет это правило на наблюдаемых координатах каждого child после
reconstruction; input body не классифицирует полномочия самостоятельно.

`@cluster` не означает все namespaces. Только `list.all-namespaces` и
`watch.all-namespaces` принимают `@all-namespaces`; ordinary list/watch сохраняют
точную область. Composite drain требует Node child grant `@cluster` и
отдельные exact namespace grants каждого Pod child из server RuntimeRevision.
Отсутствие любого child grant блокирует план; каждый destructive child сохраняет
свой EACH Gate. Защищённые namespaces и Secret results фильтрует broker.

| Переход                       | Terminal/rejoin и событие                                                                                                                                              |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Вызов/read                    | Adapter не изменяет owner state и не публикует событие. Authoritative read — текущие CP operation/effect/frame records                                                 |
| Lost ACK/error после dispatch | Нет автоматического повторного Execute. Для mutation сохраняется UNKNOWN; raw gRPC/provider diagnostic не возвращается                                                 |
| Invalid successful reply      | Closed response refusal; mutation остаётся UNKNOWN. Не создаётся success completion либо новый effect key                                                              |
| Cancel/revoke/expiry/terminal | Owner атомарно закрывает права/leases соответствующего graph; dedicated lane отменяет transport и ждёт join. Старый descriptor не даёт новый credit                    |
| Retry/restart                 | Rejoin только прежних owner records и exact binding; новый attempt требует нового owner admission. Не повторять неизвестный внешний эффект                             |
| Grant change/revoke           | Specialized owner command, Connection version и exact grant row. Прежние immutable receipts не переписываются. Отзыв не отправляет paths как новое SCOPED согласование |

Системная граница: общий package validator может переиспользоваться token и
private broker adapters, но наличие `Credential` в package не означает наличие
generic Secret descriptor. Ordinary consumer сохраняет прежний token guard;
Kubernetes требует BROKER_IDENTITY/KUBERNETES_TARGET и `Request.Credential=nil`.
Исключение TLS8443 ограничено единственным внутренним broker tuple, не открывает
произвольный HTTPS port, host либо external proxy route.

Локальная hermetic проверка: `make test-managed-kubernetes-consumer` с pinned
Go1.26.10. Она проверяет shared parser, все shipped/managed package contracts и
typed gateway port, не создаёт PG/Pod и не вызывает кластер. Broker fixture
проверяет один RPC и неизменённый binding; это не proof actual SSAR/GVR effects.
UI unit/browser проверяют явную область, выбор ref из Connection snapshot,
сброс при смене selection и неизменность nonKUBE payload; synthetic DOM не
заменяет owner PostgreSQL/native consumer qualification.

PostgreSQL-проверки владельца по #2145 запускают Bootstrap текущего
исходного кода. Их временная база получает схему 049 через
`TestPersonalEmailOwnerDevelopmentSeed`, поскольку Bootstrap
сверяет также SSH и почтовые определения. Исторический
`TestManagedKubernetesOwnerRegressionSeed` по-прежнему применяет
точную схему 047. Проверки полного каталога миграции и живой установки
выполняются отдельно.

`make test-kubernetes-grant-contract` проверяет scoped exact-JSON admission двух
grant HTTP commands до первого owner RPC и safe KUBE scope projection обоих
assistant candidate paths. Общий primitive соответствует ранее проверенному
plain DTO codec #2168, дополненному строгим UTF-8 admission; body byte limit,
explicit boolean intent и nullable/unknown-field границы сохраняются. Остальные
HTTP endpoints не переводятся на новый parser. Raw `@cluster` в переводах
vue-i18n передаётся через named parameters, чтобы не стать linked-message
syntax; настоящий SSR render и browser проверяют видимость поля.

Ручная проверка полного unit после его поставки: выбрать собственную identity,
завершить owner-local setup, отдельно выполнить identity test; в разрешениях
указать exact namespace и получателя. Проверить отказ для `*`, неизвестного
target, чужого grant ref и @all-namespaces у mutation. Смена namespace не должна
перезаписывать соседнее разрешение; отзыв — менять только выбранную строку.
После revoke/expiry прежний stream не выдаёт новые bytes. Внешний эффект
проверяется только в явно разрешённой владельцем области и по выбранному Gate.

Для итогового full047 обязательны owner multi-scope persistence/candidates,
assistant-plan blueprint с теми же typed namespace/ref, dedicated claim/health/
effect/stream consumers, policy108 и installer closure. Они сводятся в один PR;
этот документ не разрешает публиковать частичный unit или обходить отсутствие
любого consumer. Schema047/048/049 pins не заменяются выдуманным predecessor.

Проверены Context7 `/websites/kubernetes_io`: официальные
[API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/),
[kubectl patch](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_patch/)
и [виды patch](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/).
Для jsonschema6 проверены Context7 `/santhosh-tekuri/jsonschema/v6.0.3` и
исходники точного module cache. Compiler использует только owned schemas с
отключённым внешним loader. Секреты и live account данные в проверках отсутствуют.
