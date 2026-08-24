---
id: RUN-MC-006
title: Диагностика и восстановление internal-rpc-authority
type: runbook
status: approved
owner: sre
version: 1.2.7
updated: 2026-08-24
---

# Диагностика и восстановление internal-rpc-authority

## Когда применять

Инструкция используется при отказе готовности issuer/verifier, отклонении
снимка, ошибках устойчивой защиты от повтора, недоступности reconciler, сбое
жизненного цикла учётных данных Vault/PostgreSQL или перед контролируемой
ротацией. Она не разрешает production-развёртывание: для применения
production-манифеста требуется отдельный шлюз владельца.

Не выводить DSN, полезную нагрузку JWT/JWS целиком, проецируемый токен, пароль,
закрытый JWK, закрытый ключ сертификата или содержимое Kubernetes Secret.

Publisher получает отдельную Vault policy, построенную из точных KV paths
закрытого target registry в итоговом release render. Policy разрешает только
`read` и `update`: wildcard, `create`, `delete` и выход за namespace
`kv/data/mattercodex/internal-rpc-authority/` запрещены. Release `readback`
повторно строит ожидаемую policy из того же render и сравнивает её с фактически
обслуживаемой Vault policy и Kubernetes auth role.

## Предварительная проверка без изменений

1. Зафиксировать точные Git SHA и хэш образа.
2. Получить render той же версией кода:

```bash
KUBERNETES_API_CIDRS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh)"
KUBERNETES_API_PORTS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh --output ports)"
test -n "$KUBERNETES_API_CIDRS"
test -n "$KUBERNETES_API_PORTS"
scripts/render-internal-rpc-authority.sh \
  --environment staging \
  --image-ref ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:<digest> \
  --kubernetes-api-cidrs "$KUBERNETES_API_CIDRS" \
  --kubernetes-api-ports "$KUBERNETES_API_PORTS" \
  > /tmp/internal-rpc-authority-staging.yaml
```

Для production используется та же последовательность с
`--environment production` и отдельно утверждённым точным digest. Resolver
читает ClusterIP Kubernetes Service и готовые EndpointSlice; пустой,
невалидный либо устаревший вручную сохранённый набор не заменять вымышленным
адресом или правилом только по порту. После изменения Service/EndpointSlice
получить CIDR заново и повторить render/readback.

3. Проверить без значений Secret:

```bash
kubectl -n mattercodex-system get deploy,pod,job,svc,networkpolicy \
  -l app.kubernetes.io/name=internal-rpc-authority
kubectl -n mattercodex-system get endpoints \
  internal-rpc-authority-database-credential-reconciler
```

4. Сверить ServiceAccount, UID/GID pod, хэш образа, имена томов, selectors
   `NetworkPolicy` и точные назначения с итоговым render.
5. Проверить `/readyz` и ограниченные метрики через локальный port-forward. Не
   публиковать техническую точку доступа наружу.

## Классы отказа

### Телеметрия не готова

Все исполняемые процессы и восстановительные задачи закрыто отказываются
стартовать без доверенной точки OTLP TLS и файловой доставки Sentry DSN.
Проверить:

- `OTEL_EXPORTER_OTLP_ENDPOINT` указывает ровно на
  `otel-collector.observability.svc:4317`;
- TLS SNI равен
  `otel-collector.observability.svc.cluster.local`, CA читается из
  `internal-rpc-authority-otel-ca`;
- узел Sentry DSN равен `sentry-relay.observability.svc:8443`, DSN
  доставлен файлом из `internal-rpc-authority-sentry`;
- `NetworkPolicy` разрешает только соответствующие selectors pod и
  пространства имён, а не произвольное назначение на портах `4317` или `8443`.

Не выводить Sentry DSN при диагностике. Проверять только имя Secret, режим
файла, размер файла и совпадение ожидаемого узла. Панель
`mattercodex-internal-rpc-authority` показывает готовность обслуживаемого
состояния, ограниченные исходы gRPC и задержку p99. Оповещения
`InternalRPCAuthorityServedStateUnavailable`,
`InternalRPCAuthorityUnexpectedGRPCFailures` и
`InternalRPCAuthorityGRPCLatencyHigh` ведут в эту инструкцию.

При остановке OTel trace provider и сброс Sentry получают независимые
ограниченные контексты. Исчерпание одного бюджета не отменяет вторую операцию
очистки.

### UDS не готов

- корень обязан быть реальным каталогом `uid=29000`, `gid=29000`, с режимом
  `1770`;
- `issuer.sock` и `verifier.sock` должны быть сокетами, а не symlink;
- UID слушателей должны быть соответственно `29001` и `29002`;
- peer приложения обязан иметь точные зарегистрированные UID/GID;
- том — закрытый локальный для pod `emptyDir`, а не общий PVC/hostPath.

Удалять устаревший сокет вручную внутри работающего pod запрещено. Нужно
перезапустить pod: socket-init повторно проверит тип, владельца и режим, а
исполняемый процесс выполнит атомарные bind/rename.

### Снимок отклонён

Сверить только метаданные: исходные ревизию/хэш, ревизию/хэш predecessor,
ревизию набора ключей, ревизию политики, поколение подписанта, `kid` и срок
действия.
Проверить:

- JWS манифеста подписан независимым корнем и каноничен;
- точный workload имеет один `CURRENT` и ограниченные `NEXT`/`PREVIOUS`;
- закрытый ключ issuer соответствует обслуживаемому открытому JWK;
- verifier не получает закрытый ключ issuer;
- доверие proof и относящийся к роли ключ владения readback доставлены отдельно;
- resolver-sidecar сверил proof private key с exact `CURRENT` записью
  independently delivered proof trust и получил отдельный receipt;
- publisher promotion видит полный набор issuer/verifier/resolver readback для
  той же source revision/digest, а обслуживаемый Kubernetes Secret имеет
  совпадающий `resourceVersion` readback;
- до конца 24-часового validity window текущего snapshot опубликована следующая
  source revision; истечение окна без полной promotion закрывает readiness;
- верхняя отметка PostgreSQL не выше предлагаемой ревизии.

Изменение без увеличения ревизии и откат не обходить. При пропущенной ревизии
publisher обязан дать корректную цепочку predecessor/истории, иначе workload
остаётся неготовым.

### Защита от повтора или устойчивое хранилище недоступны

Проверить TLS `verify-full`, точное имя сервера, `session_user`, `SET ROLE` и
доступность таблиц:

- `authority_snapshot_watermarks`;
- `authority_replay_reservations`;
- `authority_proof_watermarks`;
- `authority_proof_reservations`.

Не очищать верхнюю отметку. Истёкшее резервирование удаляет только относящийся
к роли фоновый обработчик после срока хранения: issuer не имеет `DELETE` к
резервированиям verifier, verifier не имеет `DELETE` к резервированиям issuer.
Запасной путь в памяти/`emptyDir` запрещён.

Потребление readback challenge работает в `READ COMMITTED`: SQL-функция до
проверки receipt берёт transaction-scoped advisory lock по idempotency key, а
затем блокирует точные строки challenge и intent через `FOR UPDATE`. Поток
`40001` на `authority_readback_attestation_receipts` при одновременном запуске
разных workload означает возврат `SERIALIZABLE` predicate contention. Такой
отказ устраняется восстановлением адресных блокировок и уровня изоляции, а не
увеличением startup probe или ослаблением replay/readback-проверок.

### Reconciler не готов

Проверить:

- проецируемый токен ServiceAccount имеет аудиторию `vault` и TTL 600 секунд;
- Vault Kubernetes auth role также закрепляет точную аудиторию `vault`;
- каждый связанный `SecretProviderClass` содержит точный параметр
  `audience: vault`; отсутствие параметра создаёт токен с иной аудиторией и
  приводит к `invalid audience` до чтения Secret;
- Vault доступен только по HTTPS с точным SNI и CA;
- PostgreSQL доступен только по TLS `verify-full`;
- активная аренда с ограждением принадлежит одной реплике;
- выведенное сервером контрольное чтение содержит ровно publisher/attestor
  `CURRENT`+`NEXT`;
- `session_user` совпадает с principal reconciler, capability активирована
  только через точный `SET ROLE`.

Если Kubernetes login в Vault успешен, но audit не содержит ни одного чтения
`database/static-roles/<role>`, а таблицы lease и rotation intent пусты,
проверить source-валидацию зарегистрированного набора. Vault role и database
config используют имена с дефисами, PostgreSQL principal — unquoted identifier
с подчёркиваниями. Применять к этим трём значениям одну маску запрещено. Не
включать `LOGIN` вручную: после исправления exact release reconciler обязан сам
создать fenced lease, записать intent и провести поколения через утверждённый
lifecycle.

Значение Secret не копировать в окружение и не сравнивать в выводе shell.

На fresh install offline ceremony обязана до запуска workload создать и
доставить:

- manifest trust и подписанный restore-role trust с одним `CURRENT` и одним
  `NEXT` signer;
- закрытый и открытый JWK независимого PITR evidence signer;
- минимальную policy reconciler для чтения активных static roles/credentials,
  ротации только продвигаемого `g4` и удаления уже выведенных `g1`/`g2`.

Отсутствующий `restore-role-trust.jws`, публичная часть PITR evidence либо
Vault path из этого закрытого набора является дефектом bootstrap. Не создавать
их вручную в кластере: повторно выполнить code-first fresh material и
`configure-core`/`configure-policies` из того же release SHA.

Если lease и intent созданы, но intent остаётся в `CREATED`, проверить ошибку
`permission denied to alter role`. Security-definer lifecycle обязан иметь
`CREATEROLE` и `ADMIN OPTION` только на закрытый набор generation roles
`ira_publisher_g1..g5` и `ira_readback_attestor_g1..g5`; для этого членства
`INHERIT` и `SET` запрещены. Нельзя выдавать reconciler, runtime principal или
definer права на произвольные PostgreSQL roles. Исправление подтверждается
вызовом reconcile/retire под реальными `session_user` и capability в
одноразовой PostgreSQL, а не ручным `ALTER ROLE` в живой базе.

Если identities уже записаны, а intent всё ещё остаётся в `CREATED`, сверить
digest в `authority_runtime_database_identities` с canonical digest rotation
intent. Baseline и target registered set намеренно различаются, но baseline
identities атомарно записываются с canonical digest всего целевого перехода;
post-transaction readback обязан фильтровать по этому же digest. Повторное
вычисление digest только от baseline даёт ложный `registered set incomplete` и
запрещено. Проверка repository должна выполнять этот сценарий на одноразовой
PostgreSQL с заведомо различными digest.

Если intent дошёл до `NEXT_STAGED`, session readback совпадает со staged
digest, но pod-template annotations отсутствуют, проверить exact egress
reconciler к Kubernetes API. Bounded RBAC `patch` недостаточен без двух
materialized правил `NetworkPolicy`: host CIDR ClusterIP с TCP/443 и host CIDR
готовых EndpointSlice с их фактическими TCP-портами. На кластере с enforcement
после DNAT правило только для ClusterIP не пропускает запрос к control plane.
Широкий service CIDR, правило только по порту и ручное добавление annotations
запрещены: production render получает текущие Service и EndpointSlice через
installation variables и материализует один и тот же exact endpoint rule для
reconciler, publisher/restore, runtime-controller и image-admission controller.

## Контролируемая ротация

1. Подтвердить готовность `CURRENT` и доставку `NEXT`.
2. Проверить криптографическое контрольное чтение всех фактически
   обслуживаемых workload.
3. Атомарно опубликовать следующий снимок с хэшем predecessor.
4. Дождаться готовности обслуживаемого состояния каждой реплики.
5. Перевести прежний `CURRENT` в ограниченное состояние `PREVIOUS`.
6. После перекрытия и отсутствия активных сессий установить principal процесса
   `RETIRED`, затем `NOLOGIN`, отозвать точное членство, выполнить ротацию
   Vault и ограниченное дренирование.
7. Повтор старого JTI, старого поколения подписанта и прежнего пароля обязан
   закрыто отклоняться.

После обновления TLS-сертификата, PostgreSQL DSN либо LOGIN principal выполнить
последовательный перезапуск соответствующего компонента. Обновлённый Kubernetes
Secret не считается контрольным чтением: нужно проверить фактически
обслуживаемый сертификат, `session_user`, `current_user` после точного
`SET ROLE` и готовность на каждой новой реплике до удаления перекрытия.

Авария до публикации не меняет обслуживаемое состояние. Авария после
публикации использует верхнюю отметку PostgreSQL и повторное точное контрольное
чтение. PITR/откат, понизивший состояние ниже внешней опорной точки, оставляет
компонент неготовым до разрешённого владельцем восстановления.

Для PITR операторская задача принимает имя exact CNPG `Backup`, recovery
target и идентификаторы команды, но не принимает заявленный человеком digest.
Она через отдельный projected Kubernetes token читает immutable `Backup` и
source `Cluster`, связывает UID/resourceVersion/generation, provider
`backupID`, server, WAL timeline, LSN и plugin metadata в канонический digest.
PITR executor повторяет тот же authoritative readback, создаёт новый Cluster
с exact `recoveryTarget.backupID` и числовым `targetTLI`, а completion evidence
содержит обе Kubernetes identity. Mutation, исчезновение поля, смена source,
digest mismatch или readback другого Cluster закрыто прекращают операцию.
Повтор с тем же immutable intent идемпотентно читает существующий Cluster;
rollback требует нового restore ID/epoch и не переписывает опубликованное
evidence.

Периодический PITR executor выполняет работу только при точной устойчивой фазе
`PREPARED`. Пустая координация и штатные `OPEN`, `QUIESCING`, `COMPLETED`
являются успешным no-op. Неизвестная сохранённая фаза закрыто отклоняется.
Постоянные failed Job в фазе `OPEN` означают ошибку dispatch, а не запрос
восстановления.

## Миграции

Задача миграции запускается до rollout и использует отдельный ServiceAccount и
Secret с DSN. До rollout выполняется поддерживаемый disposable PostgreSQL
component-контур из `GOV-DOC-003`; он не использует DSN живой среды.

Перед staging владелец отдельно утверждает фактическую миграционную проверку и
одноразовое окружение. Production CLI не предоставляет `down`: откат схемы
выполняется только новой компенсирующей однонаправленной миграцией после
отдельного Issue, проверенной резервной копии и шлюза владельца.

## Откат

Образ приложения можно вернуть на предыдущий проверенный хэш только если он
понимает уже опубликованную версию контракта и его ревизии
подписанта/политики не ниже устойчивой верхней отметки. Снимок, верхняя отметка,
резервирования защиты от повтора и поколения учётных данных назад не
откатываются.

При несовместимости оставить workload неготовым, остановить rollout и
восстановить новую совместимую версию. Удаление таблиц, сброс watermark,
повторное использование выведенных из обращения ключа/principal и
незашифрованный запасной путь/TLS-skip запрещены.
