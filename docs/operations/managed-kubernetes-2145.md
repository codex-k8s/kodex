---
id: OPS-DOC-MANAGED-KUBERNETES-2145
title: Границы полного управляемого Kubernetes unit
type: operations
status: approved
owner: developer
version: 1.0.12
updated: 2026-11-17
---

# Управляемый Kubernetes

Локальная подготовка credential выполняется штатным CLI:

```bash
python3 tools/dev/managed-kubernetes-identity.py inspect --kubeconfig <private-file>
chmod 600 <admission-file>
python3 tools/dev/managed-kubernetes-identity.py upload --admission-file <admission-file> --kubeconfig <private-file>
```

`inspect` выдаёт только `contentSHA256` и `contentBytes`. Эти metadata используются
в PWA/API Begin; kubeconfig туда не загружается. Только первый Begin возвращает
private admission file с одноразовым permit и operator-owned TLS endpoint.
CLI требует owned regular files без symlink/hardlink и group/other permissions,
проверяет CA digest, SNI и exact address pins, игнорирует proxy environment и
передаёт материал одним POST прямо broker. После HTTP write он не делает
redirect, reconnect либо retry. Неопределённый ответ требует адресного owner
readback; новое material Create не разрешается.

Если owner уже сохранил явный UNKNOWN исходной попытки и permit ещё действителен,
отдельная команда `observe --admission-file <admission-file>` запрашивает новый
owner admission только на чтение исходного purpose object. Lease expiry либо
отсутствующий Complete не дают такого допуска. COMPLETED означает привязку
metadata; target identity/RBAC ещё проверяются отдельной командой Test.

Публичная локальная проверка этого setup-среза:
`make test-managed-kubernetes-identity-setup` с pinned Go1.26.10, private Go cache
и временными файлами. Контур включает настоящий CLI→TLS broker handler,
domain lifecycle, SDK purpose transport и metadata RPC/API. Owner/storage ports
в CLI→broker fixture одноразовые; это не проверка установленной integration,
положительного KUBE owner eligibility либо live cluster. Полный047/108 требует
отдельной квалификации invocation/streams, installer и policy consumers.

Профиль `make test-managed-kubernetes-invocation-postgres` проверяет в disposable
PostgreSQL фактический запуск/RR, создание intent и отдельный Claim/Renew. Начальный
grant создаётся публичной owner-командой; broker effects,
streams и подписи policy108 этот профиль не подтверждает. Обычный worker не
получает Kubernetes lease и не может завершить такой вызов через generic completion.
Истечение READ lease сохраняет UNKNOWN_OUTCOME и исходное поколение, без нового
execution credit.
Очередь хранит только позицию discovery `(created_at, ref)` на организацию: source
lock → cursor lock → Connection/binding → invocation. За один claim проверяется
не более32 строк в пределах существующего10-секундного claim; большой старый
префикс продвигается последовательными commits. Позиция переживает process replacement и сбрасывается в начало
при EOF. Новый input со временем перед cursor будет проверен после wrap. Cursor
не является eligibility, retry либо authority; каждая строка проходит canonical
owner predicate заново. Существующие queue/claim readers и `GO-DOC-006` проверены:
готового owner fair-discovery cursor для этого домена нет; SQL и состояние остаются
в control-plane, отдельный runtime engine не вводится.

Непригодный исходный источник у ещё не начатой READY/generation0 строки даёт FAILED
с фиксированным INTEGRATION_CREDENTIAL_UNAVAILABLE без lease/effect credit. Связанный
continuation использует существующий owner scheduler; переход публикует существующий
integration action event в той же транзакции. Открытый Gate либо уже начатый effect
этот переход не закрывает под видом unstarted. Временный personal preflight отказ
не превращается в terminal и не удерживает fair discovery position.

Повторный успешный health-test той же immutable identity/source/subject не меняет
`health_attempt_id` исходного invocation. Старый immutable fact остаётся связан с
исходным intent, а актуальный owner binding и subject/source проверяются заново.
Другая identity, generation, subject либо registry/target revision не подменяет pin.
Claim/Renew не создают отдельного domain event; авторитетный
read path — exact original-binding GetManagedKubernetesInvocation без result bytes. Наличие исходного intent и
health fact не является разрешением Kubernetes API: перед каждым эффектом
требуются свежие owner grant и проверка RBAC выбранной identity.

Источник требований — [#2145](https://github.com/codex-k8s/kodex/issues/2145),
родительский [#2033](https://github.com/codex-k8s/kodex/issues/2033),
`docs/architecture/integration-map.md` и согласованные владельцем ограничения.
Целевой кластер этой разработки — только `k3d-kodex`. Продуктовый контракт
переиспользуемый; фиксированное имя локального стенда не является полномочием.
Teleport в этот unit не входит.

**Состояние реализации: NOT_READY.** Наличие command registry, Proto, safeview,
парсера, purpose storage и локальных wire-тестов не означает установленную
интеграцию. Ни один live Kubernetes/RBAC/exec сценарий этой разработкой пока не
квалифицирован. Рабочая authority registration 108, owner persistence 047,
подключение исполнителей, installer и полный пользовательский путь должны быть
завершены до допуска unit. Резерв predecessor — schema046/policy107; SSH следует
отдельно как schema048/policy109.

## Полномочия и передача данных

| Сценарий                  | Инициатор и authority                                                                                  | Путь                                                                                                  | Авторитетное состояние и результат                                                                                                                               |
| ------------------------- | ------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Настройка личной identity | Проверенный USER и текущая видимость Connection с INITIATOR_REQUIRED                                   | Owner CLI → authenticated API без kubeconfig → CP setup permit → прямой broker TLS upload             | CP назначает subject, target, binding generation и revision; broker сохраняет отдельный immutable purpose Secret; safeview содержит только identity ref/revision |
| Явная общая identity      | Оператор Connection и явно выбранный SERVICE subject                                                   | Тот же отдельный setup protocol с PROJECT_SHARED                                                      | Fresh service ownership и RBAC проверяются отдельно; личный отказ не переключает на общую identity                                                               |
| Вызов сотрудника          | Root actor существующего run/session/turn, опубликованный exact package, активный version-pinned grant | Managed MCP → integration-gateway claim → broker → CP Resolve                                         | CP заново проверяет полный owner graph; gateway не назначает actor, namespace, target endpoint либо private credential locator                                   |
| План нескольких объектов  | Тот же originating claim и конечный selector                                                           | Scoped discovery → CP PrepareChildren → существующие child invocation/Human Gate → BeginEffect        | Owner назначает child refs, операции, UID/RV, body/schema digests и cardinality; discovery response не назначает полномочия                                      |
| Одиночное действие        | Свежий claim и одобренный exact child                                                                  | CP BeginEffect → selected identity SSAR → exact API request → safe result → CP Complete               | ATTEMPTING фиксируется до эффекта; unknown ACK не разрешает повтор; результат и effect receipt принадлежат CP                                                    |
| Длительный поток          | Исходный invocation остаётся RUNNING до terminal                                                       | Единый Runtime Claim и purpose renew → broker transport → owner frame credit → gateway frame consumer | Stream ref связан с invocation, subject, target, Pod UID, fence, generation и immutable limits; выдача stream ref не завершает внешний invocation                |

Kubeconfig, bearer token и client private key остаются только у broker. Они не
попадают в role Pod, runtime revision, prompt, workspace, PWA, gateway либо argv.
Локальный owner CLI читает указанный владельцем файл и передаёт материал прямо
broker через TLS. В аргументах допускается путь локального файла; его содержимое
и setup permit не передаются через argv и не выводятся.

Owner-local setup не требует выдавать CLI платформенную mTLS identity. API
выдаёт короткоживущий одноразовый permit для одного owner/Connection/target/
revision/content digest. Отдельный bounded TLS listener broker проверяет permit
через внутренний CP RPC с обычными mTLS, bearer и exact permission. Существующий
broker gRPC listener ради этого не ослабляется. Upload не использует cookie,
CORS, redirect, proxy environment, caller URL или выбор произвольного Secret.
Проверка kubeconfig запрещает `exec`, auth plugins, внешние пути, impersonation,
TLS bypass, несколько contexts и неявный service identity fallback.

Generic TOKEN и BROKER_IDENTITY — строгий union. Общая BI revision содержит
только Ref/Revision/Purpose/TargetRef/CreatedAt и owner binding. Приватные
Kubernetes material pins принадлежат отдельной purpose таблице. SSH использует
собственную таблицу и resolver в048; mailbox принадлежит отдельному owner, не
расширяет KUBE/SSH purpose enum и не становится generic PAT. Старые receipts без
kind остаются UNKNOWN metadata; их bytes и история не переписываются.

## Покрытие семейств команд

Реестр `libs/go/integrationpackage/kubernetes_operations.go` классифицирует
известные операции. Это каталог семантики, не признак доступного handler.
Каждая строка требует конечного owner resource scope и свежего RBAC выбранной
identity. Продуктовый UI обязан отдельно показывать готовность adapter,
настройки target, identity test и конкретного действия; UNKNOWN не является PASS.

| Семейство kubectl                                                               | Зарегистрированная семантика                                       | Минимальная граница                                                                                                                         |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- |
| get, describe, events, top                                                      | get/list/status.get; describe/top разворачиваются в конечные reads | READ/NONE, exact namespace/GVR/subresource; каждое дополнительное чтение имеет собственную проверку                                         |
| api-resources, api-versions, explain, version, cluster-info, auth               | discovery, explain, version, cluster-info, identity, access        | Закрытые non-resource addresses либо exact resource SSAR; OpenAPI hash закреплён owner; serverRelativeURL не исполняется                    |
| watch, logs, wait, rollout status/history                                       | watch/logs streams; конечные wait/rollout plans                    | Bounded duration/bytes, свежая lease, cancel/join; transport EOF не доказывает условие wait либо успех remote process                       |
| create, run, expose, autoscale                                                  | create и конечные owner plans                                      | Scoped mutation; exact approved body/schema; запрет escalation и защищённых targets                                                         |
| apply, replace, edit, patch, status update/patch                                | Отдельные effect operations                                        | Для существующего объекта UID/RV CAS; create отсутствующего объекта задаётся явно; apply/replace EACH, unknown result не переигрывается     |
| label, annotate, scale, set image/env/resources/selector/serviceaccount/subject | Отдельные setters и owner child plans                              | Scoped resource, exact body и свежие зависимости; чувствительные setters EACH; настройки transport/credentials не являются допустимым patch |
| rollout restart/pause/resume/undo                                               | Конечный owner plan                                                | Scoped resource; undo EACH; история ревизий не предоставляет authority                                                                      |
| delete, drain                                                                   | Конечные destructive children                                      | EACH для каждого эффекта, UID/RV preconditions, eviction/PDB семантика без скрытого force bypass                                            |
| cordon, uncordon, taint, certificate approve/deny, auth reconcile               | Отдельные чувствительные plans                                     | EACH; зарегистрированная безопасная область и отсутствие повышения полномочий; неподдержанная либо запрещённая область явно unavailable     |
| exec, attach, debug                                                             | Отдельный interactive gate и transport                             | EACH, protected-Pod policy, exact Pod/container, независимые ordinary-Pod и nodes/proxy RBAC проверки той же identity                       |
| cp upload/download, port-forward                                                | Отдельные transfer/tunnel gates                                    | EACH, bounded transport и cancel/join; безопасный file topology, без произвольного tar/shell/proxy обхода                                   |
| kustomize, help, config metadata                                                | Bounded offline formatting либо safe metadata                      | Без kubeconfig, auth material, чтения host paths либо удалённых произвольных URLs                                                           |

Произвольные `--raw`, `proxy`, credential/config export, context switch,
`--as*`, произвольные client authentication/TLS/transport flags, plugins и
auth exec не являются выполненным command parity. Они закрыто отклоняются;
нельзя скрывать такой отказ за другим выбранным handler. Поддержка JSONPath,
describe, watch или output formatting не расширяет проекцию Secret: только
metadata и имена keys, без `data`/`stringData` values и raw upstream body.
Запись Secret требует отдельной чувствительной операции и EACH gate; обычный
scoped create/patch не выдаёт такое право. Материал поступает из owner-bound
private draft broker, а public input содержит только его locator и безопасные
metadata. Это не путь для смены собственных broker/platform credentials.

Namespace scope использует обычную Kubernetes RBAC семантику: доверенный cluster
и точное имя namespace. Owner сохраняет NamespaceUID как witness; обнаруженная
смена закрывает дальнейшие вызовы и требует свежего допуска. Для существующего
объекта применяются UID/RV preconditions там, где API их поддерживает; streams
используют точный PodUID. Это не атомарный NamespaceUID fence для `CREATE`:
Kubernetes API не принимает такую precondition, а чтение namespace до и после
запроса её не создаёт. Приёмка отдельно проверяет обнаружение замены и отсутствие
автоматического переноса прежнего grant на новую обнаруженную инкарнацию.
Новый VAP либо отдельный namespace lifecycle protocol не требуется.

### Создание Pod через `run`

`kubernetes.run` использует закрытые параметры `podName`, `image`, `command`,
`args`, `environment`, `restartPolicy`, `port`, `labels` и один native POST
коллекции `core/v1/pods`. Семантика сопоставлена с исходниками
[kubectl v0.35.5](https://github.com/kubernetes/kubectl/blob/v0.35.5/pkg/cmd/run/run.go)
и [генератора Pod](https://github.com/kubernetes/kubectl/blob/v0.35.5/pkg/generate/versioned/run.go),
а также Context7 `/kubernetes/kubectl`. Версия target/client-go проверяется
отдельно; эта ссылка не доказывает совместимость любого кластера.

Порядок command/args сохраняется; только ключи environment сортируются.
Без явного выбора используются `restartPolicy=Always` и label `run=<podName>`.
Kodex явно задаёт `automountServiceAccountToken=false`; обязательные object policy,
registered schema, protected namespace и fresh RBAC проверки остаются общими.
Attach, expose и удаление после завершения — отдельные эффекты, не неявные
последствия создания Pod. Kubeconfig и broker credentials в Pod не попадают.

Путь: actor/root и Connection определяет owner → immutable input/target и
точный namespace grant → discovery/schema и SSAR `create` selected identity →
collection observation без предварительного GET Pod → один owner child и
`HUMAN_SCOPED` либо более строгий `HUMAN_EACH_EFFECT` → свежий Begin → один POST →
проверка exact intended name, namespace, GVK, UID/RV ответа → metadata receipt
и existing owner completion. Gate показывает intended name, не выдумывает
существующий UID/RV; scoped consent связан с exact Pod name и GVR/namespace.
Новый event type или отдельный lifecycle для `run` не вводится.

При явном конфликте create завершается FAILED; потерянный ответ либо несовпавший
ACK остаётся UNKNOWN без повторного POST. Cancel/revoke/expiry используют
существующий owner lifecycle child/invocation; разрешение на создание не даёт
права прочитать полный ответ Pod. Native TLS и owner pure/projection проверки
не заменяют установленный путь сотрудник → gateway → broker → Kubernetes.

## Привилегированный транспорт

API target содержит отдельные `serverOrigin`, `addressIPs`, `tlsServerName`, CA hash
и `kubeconfigSourceOrigins`. Последнее поле — закрытый набор exact HTTPS origins
исходного kubeconfig, например конкретный host-loopback порт локального k3d.
Явный source alias `0.0.0.0`/`[::]` также допустим как metadata экспорта;
unspecified IP не допускается в reachable address pins и не используется для dial.
Aliases не являются transport destinations. Они не принимают userinfo/path/query,
не расширяются аргументами Connection и закреплены вместе с ClusterUID и target
revision в operator history. Изменение aliases при прежней target revision
отклоняется. CLI сохраняет исходный kubeconfig приватно; не переписывает его
ради скрытого fallback и не передаёт material frontend/gateway.

После parsing setup и preflight каждого действия проверяют через reachable TLS route действительную CA/SNI
и UID namespace `kube-system`, закреплённый как ClusterUID. Это read той же
selected identity; отсутствие `get namespaces/kube-system` даёт конкретный
preflight отказ без повышения RBAC. Alias другого кластера не проходит такую
проверку только потому, что синтаксически присутствует в списке. Смена UID
закрывает setup; доказательство относится к свежему read, не атомарному fence
для будущих effects. Полный setup consumer обязан выполнить эту проверку до
единственного materialization WRITE и отдельно квалифицировать owner commit.

Обычный API адрес и kubelet адрес выводятся из operator-owned target registry.
Допустим только зарегистрированный UID-aware transport с exact NodeUID,
PodUID, container, протоколом и serving-key pin. Это operator-attested binding;
read-before/read-after не объявляются atomic NodeUID precondition.

Owner хранит retired key/generation постоянно. Перенос serving key на другой
NodeUID и незаметная смена протокола запрещены. Ротация явно закрывает прежние
grants/streams. При отсутствии pin, поддержки протокола либо RBAC UI показывает
unavailable/preflight reason. Privileged transport проходит отдельное review
вместе с фактическим consumer.

Protected namespace names сохраняются монотонно по ClusterUID, включая историю
всех target refs. Новый alias либо удаление прежнего target не снимает защиту
того же кластера. Смена NamespaceUID не освобождает защищённое имя.

Для personal identity нужны оба разрешения: обычный `pods/exec|attach|portforward`
и фактический метод `nodes/proxy`. Не допускается получать второе право от
глобальной service identity. Credential-owning и system-sensitive Pods исключены;
exec, debug, copy либо tunnel не могут раскрыть broker kubeconfig или платформенные
credentials. Создание и изменение ресурсов также не должны обходить этот запрет.

## Lifecycle и восстановление

Ошибка SQL, driver Scan либо контекста не является доказательством отсутствия
subject, потери permission, несовместимости purpose fact или изменения версии.
До проверки `Active` разбирается ошибка subject и отдельного OIDC group fallback.
Только доказанные `NotFound`/`Forbidden` скрываются как отказ доступа;
`Unavailable`/отмена/истечение бюджета имеют приоритет даже в составной ошибке.
Такая ошибка откатывает Complete/attach/quarantine целиком: setup, binding
version/generation, material receipts и cancellation graph не меняются. Нулевая
кардинальность успешного CAS остаётся `Conflict`; ошибка выполнения CAS сохраняет
инфраструктурный класс и существующий serialization retry marker.

Отдельного события для откатившегося перехода нет: авторитетный setup status и
текущий binding read возвращают прежнее состояние. Доставка того же receipt после
восстановления БД не разрешает повторный внешний WRITE. Профиль
`make test-managed-kubernetes-health-postgres` включает successful-SELECT Scan
failure без PostgreSQL transaction abort и настоящие SQL faults на attach/quarantine;
снимок всех owner rows/receipts доказывает сохранение state/version до повторной
доставки receipt. Семантика `QueryRow.Scan` и `ErrNoRows` сверена через Context7
`/jackc/pgx` по официальным API pgx/v5.

| Переход                         | Owner transaction и эффект                                                                                                     | Событие либо read/rejoin path                                                                   |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| Setup begin                     | Назначает одноразовый permit, current owner/target/version и новый setup ref                                                   | Отдельного domain event нет; owner-addressed setup status без credential material               |
| Setup claim                     | До Secret create фиксирует точную попытку и закрывает повторное write credit                                                   | Owner setup receipt; повтор допускает только read-only reconciliation                           |
| Setup commit                    | Привязывает exact purpose UID/RV/hash, увеличивает binding generation, сбрасывает прежнюю проверку и закрывает прежние grants  | Metadata-only owner status; поздний ACK не восстанавливает отозванную authority                 |
| Claim/renew                     | Проверяет workload, originating invocation, root actor, session/turn/attempt, fence, input/package/config/identity generations | Существующий invocation owner path плюс K8s-specific lease metadata                             |
| Prepare children                | Создаёт immutable конечный набор и существующие специализированные Human Gates                                                 | Существующие gate events; exact plan version/digest read path                                   |
| Begin effect/open stream        | Фиксирует ATTEMPTING/PREPARED до внешнего действия; первый handshake не повторяется                                            | Exact effect/stream owner receipt; неопределённый ACK остаётся UNKNOWN                          |
| Frame begin/complete            | Резервирует bounded sequence/digest/bytes под свежей authority; повтор input не исполняется                                    | Owner frame ACK metadata; локальный transport ACK не доказывает remote consumption              |
| Complete                        | Записывает safe result и terminal graph атомарно                                                                               | Существующий invocation completion/read path; stream ref сам по себе не terminal result         |
| Cancel/revoke/delete dependency | Атомарно закрывает invocation children, claims, stream/frame grants                                                            | Существующий owner transition плюс exact status; broker отменяет и join-ит transport            |
| Lease expiry/replacement        | Закрывает authority без автоматического повторного remote write/exec                                                           | UNKNOWN/LOST receipt и owner reconciliation; Pod replacement не означает удалённый process exit |
| Retry                           | Новая явно разрешённая attempt с новыми input snapshot, generation и grant                                                     | Существующий owner retry path; uncertain mutation/stdin не воспроизводится                      |

Broker `ResolveKubernetesOperation` вызывает защищённый CP read с исходным
gateway binding; не подставляет собственный workload, identity или target.
Адаптер сохраняет один canonical input, private identity locator, policy hash,
protected namespaces и optional node pin. Неизвестные nested wire fields,
несовпадение resource с canonical input, ослабленная policy, истёкший lease и
неполный plan закрыто отклоняются без retry. Mutable RPC buffers копируются.
Повторный Resolve после renew может менять lease expiry и добавлять назначенный
plan, сохраняя immutable snapshot digest. Этот read сам по себе не даёт права
external effect и не заменяет отдельный допуск конкретного ACTIVE stream.
Native transport отдельно проверяет выбранные credentials, CA, адреса и текущие
RBAC; наличие синтаксически корректного node pin не означает готовность маршрута.

`ResolveKubernetesStream` передаёт отдельный `stream_ref` в тот же protected RPC.
Owner проверяет exact stream→effect→child→plan, исходный binding и snapshot,
`ACTIVE`/`ATTEMPTING`, hard deadline и retirement node pin. Срок ответа ограничен
текущим outer lease и сроком самого stream. Отказ не повторяется как обычный
Resolve; живой parent не разрешает закрытый либо чужой stream. Этот read не
создаёт frame credit и не открывает transport.

Broker `BeginKubernetesEffect` передаёт исходный binding и назначенные CP
plan/child pins один раз. Только точный `KUBERNETES_APPROVAL_REQUIRED` из
`kodex.control-plane` с совпадающими plan/child возвращает Gate без effect credit.
Неопределённый ответ не повторяется. Первый stream grant закрепляет hard deadline,
generation и каналы; opening admission ограничен текущим lease. При `reconcile_only` metadata могут
быть просрочены, но новый transport grant не выдаётся. `CompleteKubernetesEffect`
передаёт exact safe JSON и SHA256 через общий `ValidateEffectResult`; этот
валидатор не заменяет очистку данных native adapter. Поздний `CANCELLED` остаётся
результатом владельца, даже если внешний API ранее подтвердил действие.
Эти RPC adapters проверяются отдельно; их наличие не доказывает подключение
всего `Execute/Exchange/Cancel` consumer либо установленную приёмку.

`PrepareKubernetesChildren` отправляет точный digest конечного набора наблюдений
и сохраняет назначенный владельцем порядок child, включая одинаковые координаты
зависимых стадий. Неизвестные поля, повтор child ref, более256 children,
несовпадение canonical input/hash/resource, расширение namespace scope либо
изменение уже закреплённого плана закрыто отклоняются. Broker не назначает
operations из observations: это работа CP с текущими grants. Успешный Prepare
не является Begin и не даёт разрешения на внешний запрос.
Для завершённых children истёкший node pin остаётся historical metadata с
исходным expiry; interval/hash/target продолжают проверяться. Незавершённые
children, fresh Begin и native I/O требуют актуального срока.

Broker `ActiveStream` владеет отдельным reader и watchdog после startup barrier.
Один unary Exchange не владеет сроком native transport: отмена ожидающего
poll не завершает весь stream. При остановке broker прерывает transport,
дожидается reader и всех принятых Exchange/writer/receipt calls, очищает private
output и лишь затем отправляет typed terminal receipt в CP. Ожидание owner
terminal выполняется вне Exchange lane, иначе закрытие ожидало бы само себя.

| Локальный переход                     | Поведение broker                                                                               | Authoritative read и ограничение                                                               |
| ------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Начало                                | Проверка exact binding, handshake и grant; свежая owner/identity authority; копия channel sets | Owner Open уже завершён; локальный constructor не открывает target                             |
| Idle/renew                            | Свежая проверка не реже пяти секунд и до окончания прежнего credit                             | Новый credit не перекрывает истёкшую lease boundary и не продлевает hard deadline              |
| Потерянный unary output reply         | Повтор только private retained buffer с новым frame admission                                  | OUTPUT completion только после точного client ACK                                              |
| Partial native read + error           | Сохранить разрешённый prefix, дождаться client ACK, закрыть с UNKNOWN                          | EOF/error не является remote success                                                           |
| Отмена/истечение/отзыв                | Прервать I/O, дождаться reader и writer, уничтожить retained bytes                             | Final owner state имеет приоритет; local close не доказывает remote process stop               |
| Remote exit до output ACK, затем stop | Сохранить отдельно подтверждённый exit, локально LOST                                          | Неполная доставка не становится COMPLETED; успешный remote exit не выдумывается и не стирается |
| Потеря broker process                 | Отсутствующий private buffer нельзя восстановить повторным exec/read                           | LOST через owner rejoin, никаких нового handshake и повторного stdin                           |

Доменные race-проверки `TestActiveStream*` входят в существующий
`make test-managed-kubernetes-identity-setup`. Они доказывают ограниченный
локальный lifetime и порядок join, но не заменяют owner PostgreSQL,
регистрацию production consumer, настоящую streaming API либо cluster E2E.

`ObservePodStream` готовит предварительное наблюдение для exec/attach/port-forward:
читает exact Pod через выбранную identity, discovery и schema, проверяет полный
spec по policy самой stream-операции. Внешняя команда и GET Node не выполняются.
Pod UID/RV, body/schema digests, nodeName и выбранный container входят в proposal.
Отсутствующий NodeUID допустим только в `ValidateProposal`; окончательный
`Validate` по-прежнему требует owner-bound UID из единственного действующего
operator registry transport. Исходный proposal digest не заменяется digest
обогащённой копии; окончательное observation должно входить в plan digest.
Явный container проверяется точно; иначе используется корректная
`kubectl.kubernetes.io/default-container`, затем первый regular container.
Это соответствует проверенному [исходнику kubectl v1.35.5](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/kubectl/pkg/cmd/util/podcmd/podcmd.go)
и документации Context7 `/kubernetes/kubernetes`; каталог latest не используется
как доказательство совместимости закреплённой версии. Окончательная привязка
узла и запуск по-прежнему требуют owner admission и свежей проверки перед dial.

`CurrentStreamAccess` связывает watchdog/Exchange с exact `stream_ref` read,
исходными binding/origin и immutable plan. Проверка разрешает продление текущей
lease, но отвергает замену identity UID/RV/content/revision, subject, target route,
policy и canonical input до обращения к target. Приватные identity pins хешируются
явно: их публичные JSON tags намеренно скрывают значения и не подходят для этой
проверки. Новый credit ограничен меньшим из owner lease и срока выбранного
credential. Неуспешный read закрывает local stream с `LOST`, не выдумывая причину
отзыва; окончательный owner state имеет приоритет. Native verifier `BoundPodStreamAccess` заново читает exact private identity и
через выбранные TLS credentials проверяет cluster, SelfSubjectReview, точные
Pod get/exec/attach/port-forward и node proxy permissions. Свежий Pod должен
сохранять UID, placement и выбранный container и проходить qualified resource
policy; mutable status RV не закрепляется на весь lifetime. Current operator
node pin обязан совпадать целиком; credit ограничен пятью секундами, owner/node
expiry и известными сроками CA/client certificates. Opaque token expiry не
угадывается: проверяется действующая identity в API. Verifier не открывает
новый transport и не отправляет effects. Real TLS token/certificate tests
проверяют текущий RBAC/revoke, Pod replacement, защищённый spec, изменение
registry/locator и corrupted material. Production Execute/Exchange consumer
ещё требует подключения и сквозной квалификации. Для DEBUG_APPEND→DEBUG_ATTACH
нужна отдельная проверка owner-admitted ephemeral container: общий Pod policy
пока закрыто отклоняет Pods с ephemeral containers.

`BeginEffect.accepted_result` выдаётся только для reconciliation с новым коротким
output admission. Broker проверяет outcome/reason, digest, размер и JSON, копирует
ровно эти bytes и не превращает readback в новый effect. Для READ перед выдачей
broker заново загружает exact private identity, сверяет cluster/subject и
проверяет SSAR конкретного GVR/name/namespace. Это не resource GET, discovery
либо повтор effect. Credit ограничен owner admission, сроком известных
certificates и пятью секундами; revoke/неизвестный auth закрывает cached READ.
Квитанция WRITE содержит только metadata effect и не разрешает read-through-write.
Исторический receipt без
fresh credit остаётся только metadata. `CompleteEffect` различает
`submission_digest` (подтверждение отправленного evidence) и `result_digest`
(действительно сохранённые bytes). Discarded late READ получает `CANCELLED`,
`applied=false`, hash `{}` и отсутствие `authorized_until`; такой ACK не разрешает
выдать исходный результат. Applied receipt требует точного submitted outcome/hash
и свежего output credit не более текущего30-секундного lease.

ResourceExecutor связывает native preparation с точными canonical input, target
metadata, subject и observed resource; перед Begin перечитывает исходный owner
graph и plan. Один child проходит Begin → native request → positive Close →
Complete. Отмена клиента сохраняет независимый bounded Complete, но запрещает
выдачу bytes. Gate и неопределённый Begin не запускают запрос. Recovery принятого
результата допускает отсутствие native preparation: нет повторных discovery,
чтения target или effect. Это не завершает composite plan: CREATE_SEED/SSA и
DEBUG_APPEND/ATTACH остаются отдельными owner phases. Локальная TLS проверка
покрывает запись, отзыв RBAC перед dispatch и потерянный Complete ACK; она не
заменяет подключение верхнего RPC consumer, PostgreSQL owner и cluster E2E.
`Resolve.reconcile_only` отмечает наличие хотя бы одного effect во всём plan;
он не запрещает следующий child. Dispatch конкретного child определяется только
его свежим `Begin.reconcile_only` и точной подготовкой native request.

Верхний direct RESOURCE consumer получает только original invocation binding:
Resolve → private preparation → Prepare observations → exact child execution.
Existing plan читается до private material; ATTEMPTING/terminal child не вызывает
повторной подготовки ресурса. Typed RPC проверяет server admission, закрытый
binding и fresh output credit; Human Gate возвращается как exact
`KUBERNETES_APPROVAL_REQUIRED` с plan/child/gate refs. Expanded plan не объявляется
завершённым после первого child. App lifecycle/composition и полный composite
consumer квалифицируются отдельно до включения общего readiness.

`StreamGrant.expiresAt` — неизменяемый общий предел stream, ограниченный
input timeout, capability/runtime deadline и сроком registered node pin. Он не
равен начальному30-секундному outer lease. Первый handshake имеет отдельный
короткий `BeginEffect.expiresAt`; broker сравнивает stream grant с отдельным
`EffectOpening.HardStreamDeadline`, а handshake ограничивает меньшим из lease,
hard deadline и credential TTL. Native WebSocket dial использует отдельный
`admissionUntil`, а socket/watcher — broker lifecycle context с `expiresAt`.
Завершение dial context не отменяет принятый stream; просроченный либо медленный
handshake закрывается без повторного открытия. TLS/WebSocket тест отдельно
передаёт bytes после истечения первого admission. Каждый `BeginFrame` заново ограничивается
current outer lease и общим stream deadline. Renew не меняет stream generation
и общий срок. Broker дополнительно ограничивает локальный transport сроком
credential и свежей проверкой selected identity; idle watchdog выполняет ту же
проверку owner/authentication даже при отсутствии данных.

`PodStreamPreparer` связывает private identity loader, exact final child observation
и native WebSocket-v5 exec/attach. До Begin он повторяет discovery/Pod read и
сверяет UID/RV, body/schema digests, container и operator-bound node UID. Перед
единственным dial заново читает private credentials и проверяет current RBAC,
cluster, protected Pod policy и placement. Grant channels, input digest,
effect/stream generation и hard deadline сверяются до handshake. Preparation
не хранит credentials и не допускает второй Open; Close отменяет и присоединяет
pending Open, а успешно переданный transport закрывает его новый владелец.
Короткий unary context отделён от worker lifecycle. Для закреплённого
`gorilla/websocket` `e064f32e3674` проверен исходник `client.go`: context cancellation
после TCP connect не прерывает сама по себе `http.ReadResponse`. Admission callback
закрывает exact connection и присоединяется; после handoff callback снят.
Документация Context7 `/gorilla/websocket` проверена вместе с этим exact source.
TLS-тесты подтверждают передачу после окончания unary attempt, отсутствие
повторного Open, revoke и join зависшего upgrade. Это локальный native factory;
owner Open/Complete, RPC Exchange/Cancel и cluster E2E остаются отдельной обязательной
квалификацией. Наличие port-forward observation не означает готовый port-forward
transport: этот consumer ещё не подключён.

`StreamOperations` соединяет исходный owner binding, immutable child plan,
короткий Begin, единственный native handshake, owner Open, frame lifecycle и
Close/Complete. Потерянный Open ACK передаётся как optional typed observed
handshake в Close; подмена origin/protocol/digest отвергается до RPC. Pending Gate
не открывает transport. Historical Begin не загружает private identity и не
воссоздаёт утраченный локальный buffer. Полное восстановление такого случая
требует отдельного owner metadata read; наличие registry не заменяет этот путь.

`Operations` выбирает execution family по fresh owner snapshot. gRPC Execute
возвращает ровно один safe result либо exact stream descriptor; Exchange требует
admitted method, original invocation binding, bounded frame и точный digest.
Неизвестные enum/payload поля и private diagnostics закрыто отклоняются. Короткий
RPC не владеет native lifetime; общий worker group отменяет streams и ожидает
reader, writer и completion workers до закрытия зависимостей. Ошибка native
Close сохраняет unjoined guard до конца процесса, не создаёт положительный join
и блокирует новые stream admissions. Локальная ёмкость —16 streams; owner grants
и общий bytes/deadline budget этим не расширяются.

Cancel RPC принимает original binding и stream ref, но сам locator не даёт права
остановки. Domain требует отдельную owner closure receipt с exact invocation,
stream, generation, binding digest и snapshot digest. При отсутствии owner
cleanup read, чужих metadata либо незакрытом execution он не закрывает native
transport. Ошибка связи с owner не подменяется разрешением отмены. После закрытия
возможен поздний Complete ACK `Applied=false/CANCELLED`: только exact submission
и canonical discarded result без output credit подтверждают принятую сверку.
Это не возрождает business invocation и не доказывает remote process exit.
Owner cleanup RPC и native PostgreSQL этого пути квалифицируются отдельно.

Подключение Execute/Exchange к composition не подтверждает готовый полный
Kubernetes unit: Cancel с authoritative closed-owner proof, потеря process-local
buffer, остальные native transports, PostgreSQL и installed E2E проходят свои
обязательные проверки. Локальные tests synthetic owner/native boundaries не
объявляются кластерной приёмкой.

INPUT `ATTEMPTING` не повторяется: неизвестный ACK остаётся `UNKNOWN`.
Для OUTPUT broker удерживает ровно один собственный buffer и exact
stream/direction/sequence/channel/digest/bytes tuple. Перед каждой доставкой
того же buffer owner повторяет свежую проверку и выдаёт `SEND`, не увеличивая
logical sequence/bytes ещё раз. Общий предел `MaximumOutputDeliveries=4`;
фактически переданные повторные bytes также входят в локальный bounded budget.
Для OUTPUT `BeginFrame.native_authorized_until` назначает только broker из
свежего selected-identity допуска. Owner сохраняет срок как минимум native witness,
current lease, hard deadline и DB clock +5 секунд. Перед записью и возвратом срок
усекается до микросекунд: wire deadline точно равен сохранённому. Каждый `SEND`
возвращает текущий durable `delivery_ordinal=1..4`; INPUT и ответы без допуска
имеют ordinal0. Отсутствующий либо истёкший native witness не создаёт credit.
Будущий private stage обязан сопоставлять ordinal и этот неизменяемый срок;
метаданные исторического stage не продлевают допуск и не разрешают ACK.
`Complete OUTPUT` разрешён только после exact client ACK. Потеря ответа Complete
допускает повтор metadata receipt, но не дополнительную доставку данных. После
потери buffer нет reconstruction, повторного target read либо remote execution:
локальный результат `LOST`, remote outcome остаётся независимо доказанным либо
`UNKNOWN`. Закрытый/отозванный source никогда не получает replay credit.

Handshake/terminal receipts имеют общие типизированные helpers. Owner заново
вычисляет digest и сопоставляет original binding/snapshot/child/effect/stream/
target и registered transport, не принимая один произвольный checksum за proof.
Remote exit и clean HTTP EOF различаются; port-forward join не доказывает
завершённый remote effect. Отдельного события о позднем transport evidence нет:
exact owner stream/effect receipt — авторитетный read/rejoin path.

Если remote process не вернул явный корректный exit status, закрытие socket либо
локальная отмена не превращаются в успех. Native v5 adapter ограничивает TLS/HTTP
upgrade bytes, сообщения, payload и срок; EOF, duplicate/unknown status fields,
неизвестные channels и превышение budget закрывают поток. Официальные client-go
и Gorilla contracts сверены через Context7 и exact pinned source; это проверка
протокола, а не live RBAC, privilege admission или установленного пути.

Закрытие setup dependency выполняется одной owner transaction: она отменяет
все активные upload attempts и setup grants, сохраняя observations. Deferred
constraint не допускает terminal setup с активной attempt. Изменение версии
Connection закрывает эти grants также через существующий lifecycle trigger;
смена configuration/package/policy очищает оба вида verified source, включая BI.
Сверены Context7 `/websites/postgresql/18` и
[PostgreSQL18: data-modifying WITH](https://www.postgresql.org/docs/18/queries-with.html):
модифицирующий CTE исполняется полностью, даже если основной запрос не читает
его `RETURNING`. Это не превращает закрытие grant в доказательство отсутствия
неопределённого внешнего CREATE.

Для materialization закрытие setup grant по cancel/expiry также не доказывает
отсутствие Secret. После unknown Create даже GET404 не является positive join
для ещё возможного server-side commit. Исходный immutable intent и UNKNOWN
material receipt сохраняются; поздний ACK может записать orphan evidence, но не
восстановить binding/grant. До первого known UID допустим только отдельный
Observe исходного intent; после binding нельзя принимать replacement UID/RV
таким способом. Cleanup требует exact UID/RV и свежей проверки неиспользования,
а новый lease не разрешает повторный Create либо забывание прежнего intent.

Истёкшая attempt без owner completion не становится разрешением OBSERVE.
Следующая attempt требует durable classified outcome каждой прежней generation.
Это сохраняет закрытый отказ, если broker успел получить неверный успешный ACK,
но завершился до записи terminal rejection владельцем. Неотчитавшийся исход
остаётся UNREPORTED и требует отдельной сверки оператором; автоматическая
«проверка ещё раз» либо новый Create того же intent недопустимы.

Полный неверный successful ACK сохраняется как `ACK_REJECTED` с bounded hash,
признаком усечения и optional UID witness. Raw body, credential material и usable
Secret locator в такой квитанции отсутствуют. Она не разрешает `Observe` либо
исправление прежнего ACK. Если отрицательный ACK пришёл после retained `KNOWN`,
owner сохраняет конфликт, не переписывая completed history. Ещё текущая source
этой RI отзывается атомарно; subsequently selected другая RI не затрагивается.
Повтор квитанции не продвигает новое поколение. Все новые test/effect/frame reads
обязаны отвергать RI с таким конфликтом.

Setup-переходы не публикуют отдельное событие: авторитетные read paths —
`GetKubernetesIdentitySetup` и чтение текущей personal/Connection metadata.
`COMPLETED` с причиной `SETUP_ACK_CONFLICT` описывает прежний metadata commit,
а не текущую пригодность identity. Cancel/expiry закрывают grants и сохраняют
material history; они не доказывают отсутствие объекта или завершение remote
Create. Внешний API, PWA и broker workflow должны явно обрабатывать эти причины.

Перед допуском полного unit обязательны 047 normal/canonical/table-only schema
proofs, upgrade/failure/cancel/lost-ACK cleanup, retained workspace preservation,
authority108 registration/readiness, итоговый environment render и реальный
API/PWA/installer consumer. Локальные fake TLS/Kubernetes и disposable PG
проверки, CI, live SSO, live cluster и owner acceptance учитываются отдельно.

Для bounded upload listener проверены Context7 `/websites/pkg_go_dev_net_http`
и [официальный net/http contract](https://pkg.go.dev/net/http):
`ReadHeaderTimeout` ограничивает только headers, `MaxHeaderBytes` не ограничивает
body, а `MaxBytesReader` требует отдельного body deadline. Общий server timeout,
handler cancellation, drain/join и TLS ownership должны проверяться на фактически
подключённом listener; наличие одного из этих параметров не доказывает все границы.

Для purpose writer проверены Context7 `/kubernetes/client-go` и закреплённый
[client-go v0.36.3](https://github.com/kubernetes/client-go/tree/v0.36.3/rest):
один typed `Secrets.Create` допускает несколько HTTP POST после `Retry-After`.
Purpose writer создаёт отдельный REST client на том же доверенном CoreV1 endpoint
и TLS transport, явно задаёт `MaxRetries(0)`, запрещает redirects и cookie jar.
REST client с внутренними atomics не копируется; общий TOKEN client не меняется.
Локальная TLS оснастка считает запросы при 429/503/409/302/307/308 и потерянном ACK;
контрольный typed SDK воспроизводит повтор. Это проверка одной сетевой отправки,
а не доказательство готовности setup owner, cleanup либо installed integration.

## Настройка областей грантов

Manager использует вкладку подключений и поле namespace: точное имя,
`@cluster` для cluster-scoped ресурса либо отдельный `@all-namespaces` только для
зарегистрированных list/watch. Для одного recipient и capability можно сохранить
несколько namespace grants; изменение/отзыв содержит точный ref выбранной строки.
Одна namespace не заменяет другую. Apply всегда требует EACH.

Assistant-generated configuration plans `CHANGE_INTEGRATION_GRANT`,
`CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT` и
`CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT` для Kubernetes в047 явно UNSUPPORTED:
ответ `KUBERNETES_CONFIGURATION_PLAN_UNSUPPORTED` ведёт к прямой manager-настройке.
Scalar currentGrantRef не выбирает первый из нескольких namespace grants.
Typed consumer этих configuration plans остаётся непокрытой частью #2145;
это не отсутствие runtime kubectl effect handler и не разрешение scalar fallback.

## Исторический stream и остановка закрытого execution

`BeginManagedKubernetesEffect.stream_history` возвращается только вместе с
`reconcile_only`; он не содержит допуска на новый transport, frame либо output.
Исходный hard deadline может истечь. Отсутствующий handshake receipt означает,
что handshake не был сообщён владельцу, и не доказывает его неуспех. Подтверждённые
handshake/terminal receipts сохраняют исходные binding, child и stream generations.
Owner state может закрыть stream как REVOKED/EXPIRED/CANCELED независимо от
сохранённого native local state; это не стирает доказанный remote outcome.
Потерянный локальный transport или output buffer не восстанавливается новым exec.

Служебный `ResolveManagedKubernetesOperation.cleanup_only` требует exact stream
и исходный claim. Ответ содержит только `cleanup_receipt` с digest исходного
binding и snapshot; credential locator, target snapshot и execution/output credit
отсутствуют. `execution_closed` означает авторитетно закрытый stream **либо**
исходную lease/invocation. Сам terminal stream не объявляет живую lease закрытой.
Отдельный cleanup repository port обязателен для readiness; его отсутствие
возвращает `Unavailable`, без перехода к обычному credential-bearing Resolve.

Контракт и преобразование history/cleanup проверяются отдельно от PostgreSQL
producer. Наличие этих DTO не подтверждает owner SQL, полный lifecycle047 либо
установленный путь Cancel; их квалификация остаётся обязательной.

Broker читает cleanup отдельным вариантом22 и сверяет исходные binding digest,
invocation, stream, generation и конечный набор owner states. Любое смешение
proof с обычным snapshot, credential locator, plan или input закрыто отклоняется.
Обычный Resolve не принимает cleanup даже вместе с пригодным snapshot.
Необратимое подтверждение закрытия не требует нового срока execution credit;
локальный Cancel дополнительно сравнивает snapshot с сохранённым native origin.
Broker обрабатывает history8 только в reconcile-only, отдельно от fresh grant.
Проверяет exact исходные child/effect/binding/target и registered native transport
pin, typed handshake/terminal receipts и hard deadline. Утерянный локальный
stream не открывается повторно: известный handshake закрывается как
`LOST/UNKNOWN/OUTPUT_LOST`, несообщённый — как `OWNER_STATE_LOST`, без positive
join, exit code либо выдуманного неуспеха handshake. Отсутствие stream в памяти
одной реплики не доказывает остановку writer другой реплики. Закрытие owner
запрещает дальнейшие frame/output admission; фактический join такого writer
новая реплика не заявляет.

Сохранённый native terminal receipt остаётся исходным фактом даже при более
строгом owner state. После записи terminal/effect receipt результат не выдаётся
немедленно: следующий read обязан получить fresh output admission. Неизвестный
ответ owner остаётся ошибкой; повтор native effect запрещён. PostgreSQL producer,
gateway continuation и installed replica-loss сценарий требуют отдельной приёмки.

### Подтверждённое ожидание и допуск результата gateway

Общий invocation claim переносит закрытые `credential_delivery_kind` и
`broker_identity_purpose`, immutable ref/revision. Неизвестный явно заданный
purpose не исчезает при Proto roundtrip и не становится TOKEN/NONE. Gateway
передаёт исходный claim consumer выбранного purpose; второй Claim не требуется.

| Ответ Execute broker                 | Проверка adapter                                                                                                     | Следующий шаг purpose consumer                                            |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `KUBERNETES_APPROVAL_REQUIRED`       | Только один ErrorInfo, точный domain, plan/child/Gate; без результата и без истёкшего caller context                 | Удержание исходного binding и ожидание решения; эффект ещё не допущен     |
| `KUBERNETES_RECONCILIATION_REQUIRED` | Только один ErrorInfo с plan/child/effect и положительной canonical generation; без output credit                    | Чтение исхода существующего effect, без повторного исполнения             |
| Завершённый resource result          | Exact JSON/digest/outcome/reason, effect generation, исходная invocation generation, свежий допуск не дольше30секунд | Отдельное outer completion; effect receipt сам не завершает invocation    |
| Завершённый health test              | Exact test ref, закрытая authentication projection; resource generation/output credit отсутствуют                    | Owner health completion уже подтверждён, generic completion не вызывается |
| Иной либо смешанный ответ            | Закрытый отказ; mutation остаётся UNKNOWN                                                                            | Никакого успеха, автоматического replay либо толкования raw error text    |

Эти проверки — часть связи gateway с существующим owner lifecycle. Они сами
по себе не доказывают wiring app activity, renew/stream loop, PostgreSQL-путь
или установленный вызов через сотрудника; для них нужна отдельная приёмка.
Для typed details проверена официальная документация gRPC-Go v1.83.2 через
Context7: `Documentation/rpc-errors.md` (`WithDetails`/`Details`).

Проверка подключения использует `ProbeKubernetes` и исходный metadata-only claim.
Его `input_digest` — назначенный owner хеш всего health snapshot; adapter не
подменяет его хешем пустого business input. Проверяются exact package, kind,
purpose, identity revision, исходная lease и срок. Broker получает только binding,
заново разрешает snapshot и подтверждает health completion. Business
`ExecuteKubernetes` health binding не принимает. Ошибка/потеря ACK сохраняет
`Attempted`, но не создаёт `Completed` и не повторяет запрос. Это ещё не
доказательство подключения purpose consumer к общему worker в установленной среде.

Клиент `ManagedKubernetesBrokerConnection` открывает generated adapter только
четыре зарегистрированных bounded unary RPC через существующий защищённый
канал; generic stream и чужие credential-projection методы отклоняются до него.
Frame exchange использует exact `kubernetes.stream.exchange` из production
registry клиента и trusted server profile. Проверка transport admission берёт
этот реестр, а не повторяет вручную строку handler. Старое имя не является alias.
Наличие этого adapter ещё не подтверждает app wiring или readiness живого пути.

### Локальная ёмкость и завершение Kubernetes consumer

`kubernetesConsumer` использует общий `serviceruntime.ActivityGate`, от одного
до четырёх мест. Резервирование предшествует единственному общему Claim RPC.
Конструктор требует исполняемый runner; без него consumer не создаётся.
Этот локальный механизм не выдаёт domain permissions и не заменяет owner lease.

| Переход                    | Локальное действие                                                                 | Граница доказательства                                                        |
| -------------------------- | ---------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Reserve                    | Занять одно место; при отмене/перегрузке не рекламировать capacity                 | Внешнее задание ещё не получено                                               |
| Отказ/пустой Claim         | Идемпотентный `ReleaseUnused`                                                      | Принятая операция этим методом не освобождается                               |
| Adopt                      | Проверить purpose/исходную lease, скопировать input, принять ровно один раз        | `RUNNING/NOT_STARTED`, без утверждения об effect                              |
| Отмена handoff context     | После принятия не влияет на app-owned activity                                     | Renew, Gate, stream и completion остаются обязанностью runner                 |
| Close                      | Закрыть admission, отменить app context, освободить только неиспользованные места  | Отмена не является join                                                       |
| Возврат runner             | Освободить место после всей локальной очистки, сохранить безопасный признак ошибки | Не доказывает quiescence внешнего writer                                      |
| Неопределённый сбой runner | Немедленно запретить Reserve/Adopt и закрыть `Failure()` для app-owned monitor     | Не ждать shutdown, не принимать следующие задания; failure не доказывает join |
| Wait                       | Требует Close и actual release всех activity                                       | Timeout остаётся unjoined; ошибка после join остаётся ошибкой                 |

`Joined()` отделяет факт локального завершения от результата `Wait`: зависимости
можно закрыть после подтверждённого join даже при сохранённой ошибке операции,
но итог процесса остаётся ошибкой. Синхронные health calls принадлежат join общего
worker. Проверки покрывают конкурентное принятие, отмену handoff, отсутствие
раннего освобождения, немедленную сигнализацию failure и ожидание задержанной
очистки. Runner возвращается только после join всей локальной работы; nil
дополнительно требует outer terminal state: recorded completion либо exact closed
metadata без выдуманного SUCCESS. Подтверждённый владельцем business
failure является нормальным terminal result, а не fatal lifecycle error.
Реальный runner с renew,
полным stream/output delivery и outer owner completion, app wiring и установленный
путь через сотрудника остаются обязательной отдельной квалификацией.

### RPC потребителя к владельцу invocation

Gateway использует существующее защищённое соединение и закрытый набор из
четырёх owner RPC: delivery contract probe, renew, metadata read и completion.
Ни одно из них не передаёт credentials либо разрешает broker effect method.
Metadata read не выдаёт result bytes или output credit.

`ReadKubernetesInvocation` проверяет исходные invocation/generation/input и
digest всего binding, закрытый state, полные plan/snapshot pins и согласованность
completion receipt. Неизвестный state, частичный plan или SUCCEEDED без recorded
completion отвергаются. `CompleteKubernetesInvocation` посылает только план из
такого readback; итог всех children и accepted result определяет owner. ACK обязан
совпасть с исходным plan и snapshot. Complete может вернуть exact closed metadata
после cancel/expiry без recorded completion; это остановка, не business SUCCESS.
Для существующего composite plan `RUNNING/completion_recorded=false` допускается
только как доказанный owner ответ о remaining children, без нового effect credit.
Переход к следующему child требует отдельного durable owner advance и свежего
Begin. Неизвестная ошибка RPC таким ответом не является. После потерянного ACK adapter не повторяет
completion: consumer читает тот же исходный binding. Уже recorded completion
повторно не отправляется.

`RenewKubernetesInvocation` ограничивает RPC меньшим из десяти секунд и известного
срока lease, сохраняет fence/generation и принимает только точную lease со свежим
сроком не более owner-контракта30секунд. Потерянный, неизвестный либо пришедший
после отмены ответ не расширяет локальный срок. Эти проверки защищают transport
consumer; отсутствие ошибки не заменяет последующий current owner admission.
Проверки с fixture owner покрывают lost ACK/readback без resubmit, чужие pins,
неизвестные поля, ложный успех, смену плана и истечение/отмену renewal. Они не
заменяют PostgreSQL all-child completion, действующий runner или live-проверку.

`runKubernetesRenewed` связывает execution, renewal и независимый watchdog через
существующий `WorkerGroup`. Продление начинается не позже трети оставшегося
срока; каждый ACK проходит описанную проверку исходной lease. Watchdog следит
только за последним подтверждённым сроком и отменяет execution даже при зависшем
renewal RPC. Поздний ACK не оживляет истёкшую activity. Возврат wrapper требует
positive join всех трёх workers: отмена execution не освобождает slot, пока
renewal либо очистка ещё работают. Внешний app shutdown наблюдает этот join в
своём bounded budget; истечение наблюдения не разрешает закрыть зависимости.
Если execution после отмены через независимый bounded read подтвердил exact
owner terminal state, ожидаемые lease-loss/cancellation причины считаются
урегулированными только после join. Каждая причина объединённой ошибки проверяется
отдельно: malformed renewal protocol и иные ошибки не скрываются за cancellation.
Проверяются продолжение после исходного срока, lost ACK, зависший RPC, задержанная
очистка и связь с actual claim adoption/consumer Close. Это ещё не admission
следующего composite child и не подтверждение удалённой остановки процесса.

Конечный `ActiveStream` snapshot сохраняет последний подтверждённый input ACK
после join всех writers и receipt calls. Повторное чтение конечного состояния
не обнуляет sequence, не повторяет stdin и не выдаёт retained output. Одновременно
готовые `done` и exchange lane возвращают один и тот же snapshot; выбор ветви
`select` не превращает уже завершённый поток в случайный transport error.
Регрессионная проверка воспроизводит ACTIVE ACK → native exit/асинхронный Close
→32 terminal reads; исходная реализация падает в обоих сценариях.

## Завершение исходного invocation

Gateway использует только зарегистрированные protected owner методы
`GetManagedKubernetesInvocation` и `CompleteManagedKubernetesInvocation`.
Первый возвращает exact original binding/plan/receipt metadata, без credential,
результата либо нового execution/output credit. Второй принимает только
owner-assigned plan tuple; outcome и bytes caller не назначает. Обычный
`CompleteIntegrationInvocation` и org-only `GetIntegrationInvocation` закрыты
для KUBE. `kubernetes_claimed_instance` назначается trigger при новом claim и
сохраняется после очистки live lease; чужие instance/fence/generation не могут
получить terminal readback.

| Ситуация                                              | Переход и результат                                                                                                           | Событие и чтение                                                                                                       |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Все children имеют applied immutable receipts         | Owner сверяет plan/input/observation digests и текущие grants, выводит terminal outcome и bounded result из сохранённых bytes | Existing integration receipt, closure trigger и TURN_PROGRESS фиксируются атомарно; exact binding Get подтверждает ACK |
| Успешный prefix и unattempted suffix                  | RUNNING, completion_recorded=false; это metadata известного незавершённого plan, не новый effect grant                        | Нового события нет; следующий child всё равно требует отдельный Begin и Gate                                           |
| ATTEMPTING либо отсутствующий receipt начатого effect | UNKNOWN_OUTCOME; повтор внешнего действия не допускается                                                                      | Remote uncertainty сохраняется, receipt/event не утверждают rollback либо остановку удалённого процесса                |
| NoPlan                                                | Только доказанные zero effects и zero OPEN Gates дают recorded FAILED                                                         | Closure/event в одной транзакции, Get возвращает immutable receipt                                                     |
| Cancel/expiry/reject до сохранения итогового receipt  | Terminal metadata с completion_recorded=false, без business success                                                           | Авторитетный exact binding read; существующие terminal paths закрывают graph                                           |
| Потеря ответа Complete                                | Get и exact same-plan Complete читают сохранённый receipt; новый Claim либо effect не создаётся                               | Повтор не создаёт событие или receipt                                                                                  |
| SQL/Scan failure                                      | Rollback без state/version advancement; infrastructure не превращается в revoke                                               | Unavailable, без нового события                                                                                        |

Для одиночного RESOURCE сохраняется точный accepted safe JSON: GET/list содержат
полезные разрешённые данные, WRITE — только metadata ACK. Несколько результатов
ограничены64KiB; превышение не заменяется ложным metadata-only SUCCESS.
Stream counts/exit summary не заменяют stdout/file output. Пока отсутствует
purpose-bound durable accepted output receipt, непустой OUTPUT закрывает final
SUCCESS. Protected output ingestion и artifact consumer остаются обязательной
частью полного unit; локальная проверка этого owner-среза не объявляет их READY.

Gateway вызывает завершение по уже прочитанным exact plan pins. При потере
transport acknowledgement выполняется один metadata read исходного binding,
а не повтор Complete либо provider effect. Только terminal state с теми же
plan/snapshot pins закрывает неопределённость. RUNNING после потерянного ответа
не разрешает следующий child: known-pending принимается лишь из положительно
проверенного Complete. Malformed response остаётся ошибкой протокола даже при
возможном последующем terminal state. Этот helper не отменяет invocation и не
доказывает join native stream; оба действия принадлежат полному app runner.

## Capacity и остановка worker

В установленном Runtime существует один `ClaimIntegrationInvocations`.
`capacity.ordinary/kubernetes/ssh` передают только число заранее зарезервированных
локальных slots. Owner применяет ограничения до commit lease, использует прежние
canonical eligibility predicates и одну транзакцию для supported classes.
Отсутствующий capacity сохраняет ordinary-only поведение. Нулевой KUBE slot
исключает KUBE до выбора; неизвестный либо отсутствующий typed owner port не
переходит к legacy Claim. `ssh` распознаётся, но ненулевое значение закрыто
`Unavailable` до квалификации SSH purpose Tx helper; `SSH_COMPOSE` остаётся
прежним отдельным видом и не переименовывается в SSH_TARGET.

Common delivery probe возвращает revision1 только при наличии code/schema для
capacity, original instance, finalize/readback и cancellation. Probe не читает
optional target registry и не выдаёт actor/resource authority. Normal KUBE
readiness отдельно проверяет owner storage и purpose path; revision0, missing
port и несовместимый serving peer не дают mixed startup admission.

`CancelManagedKubernetesInvocation(binding)` назначает outcome на сервере:
CANCELLED для graph без unsettled attempts, UNKNOWN_OUTCOME при незавершённом
внешнем effect. Exact durable binding не позволяет остановить replacement или
весь Run. Existing terminal trigger закрывает lease, child Gates, уведомления,
streams и frame credit в одной транзакции. Операция не обращается к target либо
credential и потому остаётся cleanup после revoke. Прежние immutable effect и
terminal receipts сохраняют remote truth; новый positive join не выдумывается.
События для этого технического stop нет: exact binding Get является
авторитетным metadata readback. После owner closure broker Cancel требует
отдельный exact cleanup receipt и сам завершает local transport с positive join.

Terminal closure также снимает WAITING_HUMAN у root Run после последнего
закрытого KUBE child Gate. Правило совпадает с canonical Gate resolver:
другой OPEN Gate сохраняет human wait; terminal Run не возвращается в RUNNING.
Native cancellation проверяет независимый sibling Gate и восстановление root
после его отдельного закрытия; lease expiry проверяет тот же trigger.

Gate sibling predicate читается только после отдельного root row mutex.
Тот же existing runtime root lock используется KUBE и generic Gate resolver.
Terminal trigger сначала закрывает свои Gate/notice rows, затем отдельным
statement блокирует root и новым statement проверяет оставшиеся OPEN Gates.
Так READ COMMITTED не переносит pre-lock NOT EXISTS snapshot через ожидание
конкурентного opener. Native regression удерживает actual event-sequence root
UPDATE opener до commit, подтверждает pg_blocking_pids и затем проверяет OPEN
sibling вместе с WAITING_HUMAN; прежний single UPDATE воспроизводимо FAIL.

## Native port-forward WebSocket

Низкоуровневый WSv4 parser сохранён для regression fixtures: exact ordered
1–16 mapping, проверка little-endian data/error port prefaces, bounded bytes и
положительный local join. Источник —
[kubelet v0.35.5](https://github.com/kubernetes/kubelet/blob/v0.35.5/pkg/cri/streaming/portforward/websocket.go),
проверенный после Context7 `/kubernetes/kubernetes`.

WSv4 не предоставляет нужный per-connection half-close и не выбирается рабочим
полным PF consumer. `PodStreamPreparer` отказывает до Begin/native Open с закрытой
`KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED`. Gateway передаёт только эту
разрешённую причину; CP заново доказывает original claim/source, unattempted plan
и operator protocol перед сохранением FAILED. Это business completion без
fabricated native effect/stream receipt. Потерянный completion ACK разрешает один
exact metadata Get. Автоматического переключения протокола нет.

## Копирование файлов: направление и native transport

Upload и download имеют отдельные closed input schemas. `cp.upload` требует
`artifactRef`/`artifactSHA256` исходного immutable artifact, `transferFormat`
(`FILE` или `TAR`), `remotePath`, `maximumBytes`, `timeoutSeconds` и optional
`container`. `cp.download` принимает те же параметры без artifact coordinates:
выходной artifact назначает сервер. Путь POSIX сохраняется буквально; clean
relative paths допустимы, traversal, controls и скрытая нормализация запрещены.

Общий `managedkubernetes.CopyProfile` задаёт одну fixed argv recipe для owner и
broker. FILE upload означает raw source file, который owner оборачивает в tar с
единственным destination basename; TAR upload означает проверенный архив regular
files/directories, извлекаемый в exact destination directory. Download передаёт tar:
для FILE owner извлекает единственный regular member в artifact, для TAR публикует
проверенный архив. Links, special entries, traversal, duplicate names и превышение
бюджета должны отклоняться **до upload effect либо download publication**. Произвольный
FILE_CONTENT из public Submit не является разрешённым source artifact.

`StreamOperations` → `PodStreamPreparer` → существующий v5 exec transport
сохраняют origin `cp.upload`/`cp.download`, выбранный Pod UID/container и свежую
identity. Command passthrough, shell и автоматический replay отсутствуют.
FILE_CONTENT upload отображается в stdin с half-close; download stdout возвращается
как FILE_CONTENT, а stderr и explicit exit остаются отдельными. Native byte ceiling
не превышает ни owner ceiling, ни `maximumBytes` (archive и остальные payload bytes
входят в общий предел). Эта операция не обещает изоляцию filesystem внутри
разрешённого контейнера от его собственных существующих symlinks.

Recipe сопоставлена с [официальным kubectl v1.35.5](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/kubectl/pkg/cmd/cp/cp.go)
и Context7 `/kubernetes/kubectl`. Native TLS fixtures проверяют wire argv, channels,
limits, original receipt, cancellation и one-attempt preparation; они не запускают
настоящий remote tar и не доказывают содержимое архива. Owner source validation,
archive normalization, captured-result publication и установленный сценарий ещё
обязательны перед допуском полного capability.

## Pod logs через API Kubernetes

`StreamOperations` использует для `kubernetes.logs` тот же owner plan, single-attempt
Begin, bounded frame ledger и cancel/join. Native adapter открывает один HTTPS GET
`pods/{name}/log` через выбранную private identity и закреплённый API endpoint.
Node transport и `nodes/proxy` не требуются. Container выбирается по явному полю,
штатной default-container annotation либо первому контейнеру, затем остаётся
закреплённым. Caller не задаёт URL, headers либо небезопасный TLS backend.

Перед открытием и каждым output credit заново проверяются credentials, cluster UID,
subject, `get pods/log`, `get pods`, protected-resource policy, исходный Pod UID и
выбранный container. API logs не имеет UID precondition: проверка incarnation перед
выдачей каждого блока обязательна. Обычное изменение resourceVersion не завершает
follow; замена Pod, отзыв права или истечение credit закрывают дальнейшую выдачу.

Closed options соответствуют [PodLogOptions Kubernetes v1.35.5](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/api/core/v1/types.go):
container, follow, previous, timestamps, sinceSeconds, tailLines и limitBytes.
`timeoutSeconds` задаёт локальный lifetime и не передаётся как вымышленный query.
Чтение ограничено owner/request byte ceilings и 32 KiB frames; clean HTTP EOF
отделён от cancel, timeout, truncated response и remote process exit. INPUT запрещён.
Историческая HTTP-квитанция проверяет исходные target/revision без выдуманного
kubelet pin, новой выдачи credentials или повторного GET.

`make test-managed-kubernetes-resource` проверяет настоящий TLS transport,
production preparation и отзыв доступа на synthetic API. Domain tests отдельно
проверяют historical reconciliation. Это не установленный E2E: устойчивое
сохранение/публикация вывода и сценарий сотрудника проверяются в составе полного unit.

### Private OUTPUT stage до ACK

`AcceptManagedKubernetesOutputFrame` — purpose-specific команда gateway под
исходным invocation binding. Caller передаёт только уже допущенный frame tuple,
ordinal и bounded payload; namespace/key/version, actor/project/run выводит owner.
Owner повторяет canonical source/Connection/identity/grant/runtime/lease predicate,
блокирует существующий stream и OUTPUT frame, сопоставляет payload SHA/size/channel
и текущий ordinal. Caller timestamp, произвольный object path и generic
`UploadArtifact` permission не участвуют.

| Переход                       | Устойчивый факт и допуск                                                         | Read/recovery                                                                          |
| ----------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Reservation                   | `ATTEMPTING` и server-assigned private key фиксируются до единственного PUT      | Повтор не начинает PUT; metadata read не возвращает bytes                              |
| Known PUT                     | Exact immutable version/ETag сохраняются как `STAGED`, даже после отмены RPC     | Независимый bounded context сохраняет факт; текущий допуск ещё отсутствует             |
| Admission                     | Повторная полная owner check плюс exact current ordinal/deadline дают `ACCEPTED` | Срок точно равен исходному DB/SB credit, не продлевается                               |
| Новый OUTPUT ordinal          | Уже известная exact version допускается заново под новым frame credit            | Второго PUT нет; прежний ordinal не разрешает новую доставку                           |
| Unknown PUT либо process loss | `UNKNOWN` либо неотчитанный `ATTEMPTING` сохраняет неопределённость              | Ни повторная загрузка, ни вывод об отсутствии объекта не разрешены                     |
| Damaged successful receipt    | `REJECTED`, без допуска и чужого object locator                                  | Нет adoption чужого key/version и нет ACK                                              |
| Stop/expiry/revoke            | Закрытый stream/lease либо изменённый ordinal лишает metadata read credit        | Исходный binding всё ещё читает безопасный статус; bytes и новая authority не выдаются |

Для stage не публикуется отдельное событие: authoritative read — exact original
binding/stream/sequence/ordinal `GetManagedKubernetesOutputFrame`. Долговечная
reservation учитывается в пределах 1 GiB и 32768 frames на organization, включая
неизвестные записи до их точной очистки. Эти пределы не расширяют меньшие лимиты
потока или четырёх доставок. Bytes не записываются в invocation JSON, аудит,
события либо prompt. Потерянный Accept ACK допускает одно metadata Get, без
повторной передачи payload. Его `ACCEPTED` проходит ту же lock chain
source→Connection→invocation→stream/effect→frame→stage и current child grant/pin
predicate; старый metadata snapshot после concurrent ordinal advance не выдаёт
credit. OUTPUT completion требует принятую private version текущего ordinal;
поздний exact ACK сохраняет факт, но UNKNOWN stage не становится доставленным.

Private stage не является полезным employee output или опубликованным artifact.
Публикация обязана отдельно доказать contiguous accepted bytes, фактические broker
ACK и terminal positive join для FULL, затем применить existing artifact scan/provenance/
retention/eligibility. Исторические captured artifact preview/download используют
текущие artifact/project/root actor права и не являются новым Kubernetes read либо
новым tool effect. Автоматическая очистка orphan stages, итоговая artifact assembly
и этот полезный user-result consumer остаются обязательной частью полного047;
одна stage-команда не объявляет их реализованными или `READY`.

### Общая доставка broker identity

Gateway использует единственный существующий `ClaimIntegrationInvocations`.
Перед Claim он резервирует ограниченные слоты зарегистрированных Kubernetes/SSH
consumers и передаёт владельцу точную свободную capacity. Обычная очередь и обе
purpose-очереди чередуются; отсутствие optional consumer не блокирует обычную
invocation-очередь и не объявляет соответствующий transport готовым. Общий cycle
также содержит health jobs: пока purpose health consumer не подключён, такая job
может задержать последующий invocation Claim. Это открытый дефект сборки, который
должен быть устранён до допуска профиля.

Startup и каждый Claim проверяют `delivery_contract_revision=1` через защищённый
`CheckManagedKubernetesOwnerReadiness(delivery_contract_only=true)`. Это проверка
протокола общей доставки, а не доступности Kubernetes target или OUTPUT storage.
Typed client использует строгий protobuf decoder: неизвестные и повторные поля,
неизвестная версия, повреждённый batch либо потерянный Claim ACK закрывают дальнейший
Claim. Неподтверждённый lease остаётся неопределённым до решения owner lifecycle;
повторный Claim не используется для угадывания его результата.

Полный batch проверяется до первого adoption. Health probes и исполнения
`BROKER_IDENTITY` направляются только в зарегистрированный purpose consumer,
с исходным binding, без token probe и generic completion. Shutdown сначала
закрывает admission, отменяет и положительно присоединяет consumers и HTTP handlers,
затем закрывает зависимости независимыми ограниченными contexts. Неподтверждённый
join либо fatal delivery error не скрывается за `context.Canceled`: процесс
завершается с ошибкой. Deployment предоставляет 60 секунд на последовательность
остановки; offline render проверяет оба web-профиля.

`make test-broker-identity-gateway` проверяет общий dispatcher, реальные gRPC bytes,
adoption Kubernetes consumer, отмену и exit policy на synthetic fixtures. В текущем
composition root подключены проверка delivery contract и health-only KUBE consumer.
Он использует существующий защищённый broker health path и purpose owner completion
независимо от INPUT/OUTPUT и не выдаёт invocation capacity. Поэтому due personal/shared
KUBE health не блокирует следующий ordinary Invocation Claim только из-за отсутствия
execution runner. До регистрации полного execution consumer обязательны реальный
private INPUT source, OUTPUT publication/readiness и установленный E2E. Отдельный зелёный dispatcher test
не подтверждает готовность всего Kubernetes или SSH unit.

### Watch с проверкой каждого выдаваемого блока

`kubernetes.watch` и `kubernetes.watch.all-namespaces` используют существующие
owner plan, one-attempt Begin, frame ledger и cancel/join. Native discovery выбранной
identity проверяет точные GVR/scope, verb `watch` и закреплённую OpenAPI schema.
Collection proposal не выдумывает UID/resourceVersion отдельного объекта и не
добавляет обязательное чтение всех объектов до открытия потока. Перед открытием
и каждым output credit заново проверяются identity, cluster, RBAC и schema digest.

Один HTTPS GET передаёт только типизированные label/field selectors, timeout,
resourceVersion, bookmarks и initial-events options. Для явно заданного
`sendInitialEvents`, включая `false`, автоматически задаётся `NotOlderThan`, как
требует [validator Kubernetes v0.35.5](https://github.com/kubernetes/apimachinery/blob/v0.35.5/pkg/apis/meta/internalversion/validation/validation.go).
Bookmark — metadata прогресса, не завершение бизнес-задачи. Переподключения,
автоматического повторного watch по RV и восстановления неизвестного хвоста нет.

Private JSON decoder ограничивает размер одного исходного event 2 MiB и общий
объём чтения owner byte budget. Выдаваемые bytes имеют отдельный такой же ceiling;
фильтрация не позволяет обойти предел исходного потока. До framing проверяются
event type, отсутствие duplicate JSON fields, registered GVK/schema и namespace.
Protected namespaces и связанные с ними Namespace/PV исключаются. Secret проходит
общую metadata/key-names-only projection; last-applied snapshot и managedFields
не выдаются. Bookmark сохраняет лишь RV и стандартный initial-events-end marker.
ERROR, неизвестный event, повреждение, превышение бюджета и отмена не становятся
clean EOF. Один sanitized event может занять несколько 32 KiB frames без усечения.

Сценарий: исходный actor/grant → owner collection child → fresh Begin → private
native watch → projection → fresh owner/native output credit → существующий OUTPUT
stage/ACK → защищённый результат. Receipt связывает API target/revision, без Node
transport. Историческое reconciliation не открывает новый watch и не выдаёт новых
прав. Context7 `/kubernetes/kubernetes` и source закреплённого API проверены;
synthetic TLS/preparation/reconciliation tests не заменяют установленный E2E и
готовность полного OUTPUT publication consumer.

### Контракт private INPUT delivery и совместимость OUTPUT

`ClaimManagedKubernetesInputFrame` зарегистрирован только у gateway и использует
исходный binding плюс точный stream ref. Caller не назначает actor, sequence,
source, payload либо срок. `DELIVER` содержит один owned frame с `kif_` ref,
original binding digest, generation, owner sequence, точными channel/SHA/size/EOF
и bounded source deadline. `NONE`, `UNKNOWN`, `CLOSED` не содержат frame, bytes
или execution credit. Raw `CONTROL` не является входным командным каналом.
`FILE_CONTENT` принадлежит только purpose-specific pinned artifact reader;
публичный Submit не может заменить утверждённый artifact произвольными bytes.

Owner обязан фиксировать one-attempt claim до чтения private объекта. Lost reply
не допускает второго payload delivery. Broker после получения bytes всё равно
делает `BeginINPUT` точного staged tuple перед единственным native write;
метаданные Claim не заменяют этот допуск. Unknown source claim не возвращается
в очередь того же stream: нужен явный Stop и новая invocation, без reuse старой
sequence/identity authority. Domain buffers очищаются после передачи owned copy,
в диагностике и JSON payload отсутствует. Для public STDIN/resize/port-forward
и private delivery используется общий `ValidatePublicStreamInput`; private file
source дополнительно требует immutable artifact/version/hash/archive proof.

Public List/Get/Submit/Stop связываются с current original-root visibility и
точными run/node/stream. Исторический metadata read и closure-only Stop не
требуют оживлять уже отозванные credential/grant. Новый Submit отдельно проверяет
полный current selected grant/source и active execution. Project visibility не
позволяет послать bytes процессу другого actor без существующего явного server
edge делегирования. Idempotency key не назначает authority; потерянный Submit ACK
сверяется одним Get exact key, Stop ACK — одним Get state, без payload retry.

Поле `CheckManagedKubernetesOwnerReadinessResponse.output_contract_revision=3`
имеет **значение** `managedkubernetes.OutputContractRevision=1`, не3. Default0,
отсутствующий owner readiness port и неизвестная revision закрывают Kubernetes
purpose readiness. Этот контракт включает output storage/ordinal ACK/publication
lifecycle; одних stage RPC недостаточно для положительного readiness. Поле2
`delivery_contract_revision=1` остаётся независимым capacity/finalization/readback
контрактом; его gateway-only probe не требует configured Kubernetes target и не
подтверждает OUTPUT storage.

Этот checkpoint материализует private DTO, fixed method registration, unary
client/handler, typed validation и закрытый отсутствующий repository port.
Durable INPUT producer/objects, public owner implementations, production Run
composition и полный OUTPUT publication readiness ещё не реализованы данным
контрактом и не объявляются `READY` либо `PASS` на основании mapper unit.

### Проверка подключения независимо от исполнения

Health-only конструктор использует тот же bounded lifecycle и синхронный probe,
принадлежащий общему worker join. Его ReserveClaim всегда закрыт; отсутствие
execution runner не становится stream readiness либо result publication. Broker
client добавляет только registered kubernetes.execute к прежней SSH Compose
projection, без новых stream/cancel/readiness permissions. Regression проходит
через настоящий processIntegrationWork и Adapter.ProbeKubernetes: validated
purpose-completed health receipt предшествует ordinary Claim с KUBE capacity0.
Это проверка local composition, не повтор native owner SQL/TLS qualification.

В данном root source SSH_TARGET adapter и shipped ssh-managed/ssh-execute ещё
отсутствуют: fresh supported SSH health producer не может пройти exact package
validation. Тест фиксирует эту границу; добавление SSH profile требует его actual
health consumer, а не fallback TOKEN. Полный SSH unit по-прежнему NOT READY.

### Отдельная core readiness identity setup/health

`CheckManagedKubernetesOwnerReadiness(identity_health_only=true)` сохраняет
точный broker caller/permission, отсутствие project, обязательные
cleanup/completion/cancel/delivery ports, delivery revision1 и прежний repository
predicate operator/schema/source. Этот профиль не требует OUTPUT и не объявляет
готовность resource execution либо streams. Общий delivery-only request
по-прежнему независим от configured target и не заменяет core checks.
Два флага одновременно закрыто отклоняются; gateway delivery caller не получает
права secret-broker на identity/core read. SQL, permissions и source proof не
меняются; query не создаёт state transition либо event.

Обычная full readiness дополнительно требует OUTPUT contract1; отсутствие
configured producer сохраняет false. SB setup/health startup и periodic checker
используют только отдельный typed core profile, проверяют revision1, strict reply,
local admission и прежний private Secret store. Corefalse, infrastructure error,
неизвестные поля, unsupported revision, store failure либо closed admission
по-прежнему закрывают checker. Поэтому отсутствие execution OUTPUT само по себе
не блокирует ordinary broker и authentication health. Синтетические проверки
`test-managed-kubernetes-health-readiness` не заменяют native owner SQL/TLS либо
полную INPUT/OUTPUT/PLAN/OFFLINE qualification.

### Публичная панель ввода и остановки

Публичная панель Kubernetes в подробностях активной сессии обнаруживает потоки
через bounded List по exact run/node и требует явный выбор stream. View сохраняет
owner-derived GVR/namespace/target/connection; watch не получает выдуманный Pod,
UID либо STDIN. Submit передаёт не более32KiB в защищённый owner endpoint с
purpose OCC и idempotency; при потерянном ACK допустим один metadata Get по
исходному ключу, без повторной передачи bytes. EOF закрывает STDIN отдельно от
Stop. Stop разрешает владельцу закрыть прежний tool даже после утраты execution
полномочий, не отменяя весь Run и не утверждая остановку удалённого процесса.

Gateway INPUT factory использует тот же protected owner client, что lifecycle;
Claim не повторяется при uncertainty. Runner сохраняет DeliveryExpiresAt при
Exchange и пропускает Claim для input-free streams. FILE_CONTENT по-прежнему
доступен только через owner-derived immutable artifact source. PORT_FORWARD
frame использует canonical KPF envelope с owner-assigned connection ref/generation
и approved slot. Legacy slot/emptyEOF закрыто отклоняется. Панель не заменяет
защищённый owner-local loopback bridge. Без
реального owner stage, private source, полезного OUTPUT и полного readiness
профиля эти локальные consumer checks не означают `READY` всего047.

### Native SPDY с точным protocol pin

`SPDY_V4` обслуживает exec/attach/cp, `PORT_FORWARD_SPDY_V1` — выбранные
port-forward mappings. `Prepare → fresh identity/SSR/SSAR → Begin → Open`
сохраняет исходный child, Pod UID/node, target revision, TLS SNI/CA/SPKI/IP и
receipt origin. Один POST upgrade использует ровно указанный operator protocol;
нет автоматического fallback после WebSocket, смены версии, redirect либо replay.
INPUT/OUTPUT по-прежнему проходят текущие owner admissions, protected staging и
accepted delivery; успешный local write сам по себе не подтверждает remote effect.

Используются существующие pinned client-go/apimachinery/streaming 0.36.3 и
moby/spdystream 0.5.1. Context7 проверен для client-go SPDY и moby connection options;
точное поведение сверено с локальными pinned исходниками. Stock Upgrader скрывает
raw socket, не передаёт framer bounds и читает refused response body целиком;
stock remotecommand executor возвращает отмену до join внутреннего worker.
Поэтому узкий adapter сохраняет SDK Dial/Negotiate/framing, добавляя bounded HTTP
headers 64KiB, compressed control frame 64KiB, максимум 32 headers и 4KiB на field.
SDK data frame ограничен 16MiB; application chunk — 32KiB, encrypted wire и total
bytes ограничены исходным stream budget/deadline. Границы application chunk не
выдаются за размер внутреннего SDK frame.

Закрытие socket предшествует reset; присоединяются SDK Serve/shutdown,
фиксированные readers/writer и ограниченные reset waiters. Последние закрывают
race SDK: remote reset после истечения admission waiter отправляется через
unbuffered channel. Peer push закрывает исходное соединение; raw diagnostics и
пустой status/EOF не становятся успешным exit. Положительный local join не
доказывает остановку remote process.

PF создаёт ноль TCP pairs при начальном SPDY upgrade и остаётся живым до
parent deadline/closure. Каждый OPEN проходит свежий original-root/grant/source
и INPUT admission; отдельный bounded factory разрешает этот OPEN после expiry
первоначального handshake credit, не продлевая parent deadline. Owner назначает
канонический `kpf_` ref и монотонное поколение до однократного stage PUT.

Один logical EACH Gate раскрывает immutable ports/targetPorts, timeout,
повторные/последовательные соединения и максимум16 одновременно активных.
Закрытые поколения не расходуют concurrent quota и не образуют lifetime cap.
Public List/Get/Open/Close требуют исходный actor/run/node; slot выбирает лишь
элемент server-derived profile, не удалённый URL либо произвольный порт.
Open/Close используют original stream.input_version и exact idempotency intent;
повтор Open сначала ищет прежний intent, затем только возвращает metadata.

Canonical KPF v1 имеет51-byte header: magic/version/action, slot, generation,
точный28-byte ref и native OUTPUT ordinal. DATA несёт непустой body; OPEN,
HALF_CLOSE и CLOSE имеют пустой body. INPUT ordinal всегда0; OUTPUT положительный.
Полный header/body входит в protected hash, frame и total byte budget.
Public INPUT принимает только DATA/HALF_CLOSE; OPEN/CLOSE ставит typed owner.

| Переход                           | Owner/native факт                                               | Readback и потеря ответа                                                                         |
| --------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| PREPARED → OPENING                | исходный stage допущен до native pair creation                  | Не OPEN ACK; вывод сам не снимает uncertainty                                                    |
| OPENING → OPEN                    | exact INPUT ACK                                                 | Один Get, повтор bytes/OPEN запрещён                                                             |
| HALF_CLOSE                        | закрыта локальная запись; обратное чтение продолжается          | Наблюдаемый read-end не различает FIN/RST и не означает remote success                           |
| CLOSE при действующих правах      | exact queued CLOSE, затем pair reset/join и ACK                 | CLOSED отдельно от NativeJoined; соседние пары сохраняются                                       |
| Close после потери grant          | closure-only exact connection CAS под original-root visibility  | Без нового INPUT/effect, fake principal либо whole-tool Stop; native status остаётся неизвестным |
| Close до ACK OPEN                 | Conflict/pending                                                | Не пропускать INPUT sequence и не подменять исход UNKNOWN                                        |
| Уже admitted OUTPUT + Close       | immutable retained tuple под текущим parent/root/grant/deadline | Разрешён исторический accepted stage/ACK без native reread/reopen                                |
| Новый native OUTPUT закрытой пары | typed CLOSED без credit/sequence/ACK                            | Durable PARTIAL loss по exact ref/generation/native ordinal/hash/bytes; sibling продолжает       |
| Parent Stop/revoke/expiry         | existing graph closure и отдельный typed native join            | Owner closure не доказывает remote quiescence                                                    |

PWA в активной сессии даёт явный mapping/connection selection, Open/Get/Close,
DATA и полузакрытие. UTF-8 черновик очищается после передачи; source bytes не
сохраняются в UI storage/console. Потерянный write ACK даёт один metadata Get;
malformed success блокирует новую запись до сверки. Visibility denial очищает
cached connections/profile. Local closure и native join показываются раздельно.
Это управление logical connections, а не браузерный TCP listener.

Remote pair join может опередить owner ACK OUTPUT CLOSE. Каждый fresh INPUT
OPEN SEND получает защищённый owner-selected snapshot не более16 exact
ref/generation/slot. Он включает CLOSED с pending INPUT CLOSE; прежний pending
Close запрещает следующий OPEN. Native удаляет только local join facts,
отсутствующие в этом snapshot, без FIFO-вытеснения и без удаления принятых
OUTPUT/loss receipts. Snapshot не выдаёт permissions и не приходит от public
caller. Admitted INPUT CLOSE потребляет только свой positively joined tuple,
без нового I/O. Истёкший frame context, DATA и чужие coordinates его не
используют; parent cleanup уничтожает private local state. Defensive bound32
закрывает parent при нарушении инварианта, вместо вытеснения нужного факта.

При заполненной native очереди explicit CLOSE снимает лишь ещё не
опубликованный zero-body HALF_CLOSE/CLOSE и возвращает его byte reservation.
Уже прочитанные DATA сохраняются в bounded candidate queue либо дают UNKNOWN;
никакой Close success через отбрасывание TCP bytes не разрешён. Pair SDK/readers
присоединяются до INPUT ACK; sibling connections сохраняются.

Отказ preflight до создания stream читается через existing Tool List/Get: owner
выдаёт только closed `safeReasonCode=KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED`
для exact FAILED port-forward. ToolPanel объясняет необходимость закреплённого
оператором `PORT_FORWARD_SPDY_V1`; raw provider diagnostics и разрешение retry в
эту проекцию не входят. Для остальных причин выдаётся `NONE`, неизвестные wire
значения закрыто отклоняются. Отдельного endpoint либо synthetic stream нет.

PF transitions не добавляют domain event: авторитетны scoped List/Get,
stream.input_version и original INPUT/OUTPUT receipts. PWA использует bounded
polling; владелец не получает права на новый effect из сохранённой metadata.

Обязательные оставшиеся consumer axes: actual protected live OUTPUT reader и
owner-local loopback bridge, full owner/native/API/PWA qualification, signed108
и installed full-unit приёмка. Наличие source callbacks либо16 native paths не
означает full kubectl PF READY. Owner/DDL regression имеет публичную точку
`make test-managed-kubernetes-port-forward-postgres` с prebuild трёх binaries до
Docker; миграция047 в этом профиле остаётся development, не full-schema pin.

Публичная локальная точка проверки — `make test-managed-kubernetes-resource`:
actual token/certificate TLS, fixed exec/copy/PF channels, Prepare/Open,
malformed/oversized negotiation/control/headers, server push, explicit nonzero exit,
resize, deadline/backpressure и positive join. Synthetic copy bytes не доказывают
artifact source/archive publication; live cluster этим контуром не используется.

### Текущий prerequisite проверки cluster identity

Текущий strict identity profile требует у **выбранной identity** `get` для
единственного cluster-scoped ресурса `namespaces/kube-system`. Этот GET сверяет
actual Namespace UID с operator-pinned `ClusterUID`; это не GET целевого namespace
для обычного CRUD и не proof UID-fence для создания объектов. Grant на Pods либо
иной namespaced resource сам по себе такого разрешения не даёт. До upload/health
UI должен явно показывать это требование; локальный export не вызывает Kubernetes
API и не выдаёт новую роль.

Отказ403 этого GET закрывает identity preflight до `SelfSubjectReview`, без
подстановки UID, operator credentials, другого account либо authenticated health.
Успешный health подтверждает только cluster/subject identity; каждый CRUD, exec,
watch и другой effect отдельно проверяет current RBAC и owner grant. Идентичность
кластера только по operator-attested UID/TLS endpoint без selected-identity GET —
отдельный возможный профиль, который сейчас не реализован и не объявляется готовым.

### INPUT: private stage и однократная выдача

Public actor из verified context разрешается как original root actor exact Run/Node/Invocation/Stream.
List/Get требуют текущую видимость Run/Connection; Stop закрывает только этот tool graph, без
проверки отозванных execution credentials и без утверждения остановки remote process. Submit
дополнительно повторяет canonical current source, claim, snapshot, child grant/pin и канал exact
зарегистрированной операции. Sequence и private object key назначает owner. Public FILE_CONTENT
закрыт: утверждённый artifact нельзя заменить произвольным stdin.

| Переход       | Устойчивый факт                                                                              | Ошибка/потеря ответа                                   | Доставка результата                                   |
| ------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------------------ | ----------------------------------------------------- |
| Submit        | ATTEMPTING intent до единственного PUT, затем QUEUED с exact object version                  | UNKNOWN, повтор PUT запрещён                           | Get с исходным idempotency key, metadata only         |
| Claim         | QUEUED → CLAIMED до единственного exact-version GET, source deadline ≤5s/lease/hard deadline | Повтор Claim возвращает UNKNOWN без bytes              | DELIVER только после повторной current owner проверки |
| BeginINPUT    | Exact staged tuple, ещё действующий source deadline, native admission timestamp              | ATTEMPTING frame не допускает resend                   | Existing frame metadata                               |
| CompleteINPUT | Exact admitted frame ACK и ACKNOWLEDGED stage в одной Tx                                     | Поздний факт сохраняется после Stop/expiry             | Public Get без payload                                |
| Stop/terminal | Invocation graph и все незавершённые input stages закрываются атомарно                       | CLAIMED сохраняет UNKNOWN, native join не выдумывается | Exact public Get и private cleanup receipt            |

Событий с private bytes либо object locators нет. Авторитетный read path — exact public Get и
private original-binding metadata. `input_version` меняется вместе со stage transition; OCC не
заменяет original-root eligibility. PORT_FORWARD payload содержит server-profile slot: owner
проверяет его по числу mappings в сохранённом canonical input, включая OUTPUT Accept. Private
стадии пока не означают готовность всего047: required artifact source reader, publication,
retention/purge и authority108 квалифицируются в том же unit.

## Итог invocation и отдельные факты stream

Итог внешней invocation не выводится только из remote exit либо принятого
`effect.outcome`. Владелец повторно читает сохранённый stream того же effect и
проверяет исходный typed `TerminalReceipt` существующим history validator, затем
сопоставляет exact `BuildStreamEffectResult` с принятыми bytes/digest/outcome.
`SUCCEEDED` внешней invocation требует одновременно owner stream `COMPLETED`,
локального `COMPLETED` и положительного `TransportJoined`. Например,
`localState=LOST` при известном `remoteOutcome=SUCCEEDED` и exit0 остаётся
`UNKNOWN_OUTCOME` даже без OUTPUT frames; исходный remote факт сохраняется в
result summary. Counts не доказывают полноту захвата: подтверждённый префикс и
PARTIAL artifact не повышают итог до SUCCESS. Полный вывод требует отдельного
неизменяемого capture/artifact receipt; пока публикация не реализована, прежний
закрытый отказ для непустого полного успешного OUTPUT сохраняется.

Непринятый PF output закрытого connection учитывается отдельным capture loss
fact: exact original stream, connection ref/generation, native event ordinal,
whole-envelope digest/size и закрытая причина. Повтор того же ordinal не
удваивает bytes, изменение tuple даёт conflict, одинаковые DATA с разными native
ordinals остаются разными потерями. Эти bytes не создают frame sequence, grant,
ACK либо успешный счётчик. Они участвуют в исходном общем байтовом бюджете,
а наличие loss fact делает итог захвата PARTIAL даже при позднем известном
remote exit0. Сумма потерь читается отдельным statement после stream lock.
Полный PF owner hook и artifact publication проверяются отдельной сквозной
квалификацией; сама таблица учёта не подтверждает их готовность.

## Матрица фоновой финализации capture

Это обязательный следующий срез полного unit, пока не готовый OUTPUT contract.
Consumer — существующая job `artifact-retention`; новый добровольный RPC runner
не является единственным путём. Источник — закрытый owner stream и прежние owner-accepted/broker-ACKed
private stages. Typed terminal и positive join обязательны для FULL; PARTIAL
утверждает только неизменяемый подтверждённый prefix, не native stop/join. Новых
native effects и OUTPUT credit этот путь не выдаёт. Actor, project, run, node,
connection и выбранная identity выводятся из исходного immutable owner graph.

| Переход                     | Авторитетная граница и неизменяемые данные                                                                                                                      | Результат и закрытие                                                                                                                                                                                                                    |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Enqueue после owner closure | Та же owner-транзакция stream; nullable native terminal digest, original binding/snapshot, stream/capture generations, sealed timestamp и output/ACK watermarks | Повтор closure не сдвигает seal. События нет: защищённый capture readback и обычный artifact event после публикации                                                                                                                     |
| Claim job                   | Existing artifact-retention PostgreSQL identity; точный capture snapshot, отдельные server lease/fence/generation/deadline                                      | Один текущий attempt. Original invocation fence не восстанавливается и не превращается в grant                                                                                                                                          |
| Renew                       | Exact task/workload/fence/generation и текущая незавершённая попытка                                                                                            | Только прежняя задача, bounded deadline; source snapshot неизменен                                                                                                                                                                      |
| Prepare                     | CP fixed protected method и current task lease; source GET только exact stage version/digest/size и ACK до sealed timestamp                                     | Typed normalization, inspection и AV; existing prepared_content фиксирует intent перед единственным PUT                                                                                                                                 |
| Retry/expiry                | Предыдущие task lease/grant закрываются в owner-транзакции, новый attempt получает собственное поколение                                                        | Readback исходного prepared intent; UNKNOWN никогда не разрешает второй PUT                                                                                                                                                             |
| Adoption                    | Exact sealed source, prepared receipt/AV и свежая eligibility исходного actor                                                                                   | ArtifactRevision + RUN_RESULT binding + ARTIFACT_AVAILABLE фиксируются атомарно; личный provenance не расширяется общим artifact.view                                                                                                   |
| Stop/root terminal          | Закрытие execution не стирает ранее принятый capture                                                                                                            | Подтверждённый contiguous prefix может стать PARTIAL artifact без утверждения join. Поздний ACK ранее допущенного frame создаёт следующую immutable capture generation и revision того же Artifact/channel; прежний seal не расширяется |
| Capture отказ/WAITING_OWNER | Закрытые reason/state, metadata readback исходной задачи                                                                                                        | Нет artifact success, новых effects либо подмены remote truth                                                                                                                                                                           |
| Retention/purge             | Source-retention pin удерживает ещё не опубликованный ACK prefix; затем existing exact-version prepared/artifact cleanup и подтверждённое отсутствие объекта    | Минимальные opaque origin/idempotency/deletion commitments сохраняются; source quotas освобождаются после подтверждённой очистки                                                                                                        |

Исторические preview/download используют текущую canonical artifact/project/root
actor eligibility, не новую native SSAR и не истёкший пятисекундный live credit.
`INITIATOR_REQUIRED` capture видим только исходному root actor при остальных
текущих правах. Project visibility допускается только для доказанного явно
выбранного `PROJECT_SHARED` SERVICE scope; неизвестный scope закрыт либо остаётся
root-only. Manager/reviewer не заменяют source actor. Проверки Get/List/search,
Run/Graph/event projections и runtime file catalog входят в ту же границу.


## Публикация подтверждённой истории и физическая квота

Оба release profile предоставляют только контейнеру control-plane дисковый
`emptyDir` размером512Mi по `/work/kubernetes-capture-spool` и передают этот путь
через `CONTROL_PLANE_KUBERNETES_CAPTURE_SPOOL_ROOT`. Процесс UID10001 создаёт
собственный подкаталог0700 и безымянные файлы0600; startup проверяет поддержку
`O_TMPFILE`. Общий `/tmp` и память контейнера для полного transcript не увеличены.
Этот том не хранит checkpoints: после потери Pod сборка заново читает точные
подтверждённые версии из object storage. Нехватка диска закрывает текущую сборку,
но не разрешает повторить внешний effect. `sizeLimit` не резервирует место узла.
Проверка обоих итоговых renders входит в `make test-broker-identity-gateway`;
она не заменяет установленную проверку файловой системы и публикацию через AV/S3.
Семантика проверена через Context7 Kubernetes и
[исходную схему v1.35.5](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/api/core/v1/types.go):
используются только `sizeLimit` и стандартный носитель, без новых alpha-полей.

Владелец capture выполняет чтение только точных версий принятых объектов,
нормализацию содержимого, проверку формата и настроенный антивирус. Режим
`disabled` сохраняется как `NOT_SCANNED`; его нельзя представлять как `CLEAN`.
Фоновая job использует четыре фиксированных метода существующего owner service:
`CheckManagedKubernetesCaptureReadiness`, `FinalizeManagedKubernetesCapture`,
`GetManagedKubernetesCapture` и `RenewManagedKubernetesCapture`. Она резервирует
локальный слот до Claim, поддерживает отдельный lease и завершает renewal worker
до закрытия соединений. Неопределённый ответ Finalize допускает один bounded
metadata Get исходной попытки; не повторный PUT либо native effect.

`prepared_content` сохраняет один immutable intent до PUT. Потеря подтверждения
записи требует положительного Head, затем точного versioned Get с проверкой
hash/size; отсутствие объекта не разрешает вторую запись. Все каналы принимаются
одной транзакцией вместе с ArtifactRevision, привязкой RUN_RESULT и событием
ARTIFACT_AVAILABLE. Публичный capture origin описывает подтверждённое покрытие,
исходный target/resource и известный native исход отдельно: PARTIAL с remote
exit 0 остаётся PARTIAL. Origin не содержит object key, fence, identity locator
или разрешение новых действий.

После публикации PARTIAL допустима очистка подтверждённого staging объекта.
Поздний ACK формирует новое поколение, которое проверяет каждый старый frame
по точной сохранённой канонической revision и читает лишь новый подтверждённый
хвост. Предыдущая revision не изменяется. Отсутствующая либо повреждённая версия
закрывает сборку; переход на latest object или выдача неподтверждённого хвоста
запрещены. Совместимость этого удержания с каноническим purge/restore должна быть
доказана до полной готовности: обычный DELETE объекта не заменяет retention pin.

Квота private stages относится к физически неочищенным намерениям записи:
1 GiB и 32768 объектов на организацию и направление. Исторические submission/ACK
metadata не расходуют эту квоту после подтверждённого exact-version удаления.
Переполнение возвращает закрытый `KUBERNETES_STAGING_QUOTA_EXCEEDED`; браузер не
должен автоматически повторять payload. Неопределённая запись и отсутствие
положительного deletion receipt сохраняют расход квоты. Cleanup имеет отдельный
монотонный fence и ограниченный автоматический повтор. Исчерпание повторов
требует явного разрешённого repair, не обнуления lifetime счётчиков.

Весь unit остаётся закрыт до компонентных проверок публикации, позднего ACK после
очистки, удержания и purge, исходной actor eligibility, публичного repair и
сквозной регистрации job/proof/deploy. Успешный тест in-memory store не доказывает
S3 durability, доставку policy108 или готовность живой среды.
