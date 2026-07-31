# Control-plane

`control-plane` — авторитетный внутренний сервис конфигурации и управляющего
состояния MatterCodex. Он реализует Issue
[#187](https://github.com/codex-k8s/matter-codex/issues/187) как один
развёртываемый компонент.

Сервис владеет:

- проектами, командами, чатами, ролями и профилями запросов;
- метаданными привязок учётных данных, репозиториев, рабочих пространств и
  интеграций;
- неизменяемыми ревизиями среды исполнения;
- сессиями, ходами и родословной процессов;
- расписаниями, шлюзами владельца, памятью и заявками на работу;
- метаданными артефактов, но не их байтами.

Значения секретов остаются во внешнем хранилище Vault/Kubernetes.
`control-plane` не вызывает Mattermost, MCP, Codex и Kubernetes API, не
согласует среду исполнения и не реализует внешний HTTP API.

## Сквозные границы

```text
control-api-gateway
  -> точные mTLS и первый OIDC-вызов
  -> control-plane AuthorityProofResolver
  -> серверное разрешение проекта и полномочий в PostgreSQL
  -> короткоживущее доказательство полномочий
  -> локальный для рабочей нагрузки путь issuer/verifier #186
  -> полный метод ControlPlaneService
  -> caster -> доменный сервис -> порт репозитория
  -> транзакция PostgreSQL
       агрегат + подтверждение идемпотентности + аудит + необязательный факт outbox
  -> сквозной кэш Redis по принадлежащей PostgreSQL эпохе
  -> ретранслятор outbox -> точные поток и subject NATS JetStream
```

Actor, организация, проект, полномочия, рабочая нагрузка и SPIFFE-идентичность
не принимаются в бизнес-запросе. Они выводятся из проверенного контекста
Issue #186. Для первого OIDC-вызова сервис дополнительно проверяет точного
mTLS-клиента, issuer, единственную audience, `iat`/`nbf`/`exp`, максимальный
TTL, ревизию сессии и JTI. Полномочия проекта разрешаются внутри границы
организации PostgreSQL до подписи доказательства.

## Контракты и потребители

- Proto: `contracts/proto/controlplane/v1/control_plane.proto`;
- сгенерированный публичный Go API: `libs/go/controlplaneapi/gen/controlplane/v1`;
- переиспользуемая промышленная композиция клиента: `libs/go/controlplaneclient`;
- AsyncAPI: `contracts/asyncapi/control-plane/v1/asyncapi.yaml`;
- политика полномочий: `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`.

Внешнее отображение принадлежит будущему `control-api-gateway`; этот компонент
публикует только внутренний gRPC. Политика deny-by-default регистрирует
отдельных производителей доказательств и точные идентичности клиентов для gateway, `agent-runner`,
`automation-scheduler`, внешнего `artifact-scanner`, `interaction-gateway`,
`runtime-controller` и локального `memory-indexer`. Последний индексирует
локальную проекцию pgvector без внешнего сервиса embeddings, scanner владеет
сканированием байтов, а `control-plane` — метаданными и автоматом состояний.
Неизвестные производитель, назначение учётных данных, рабочая нагрузка,
SPIFFE ID, полный метод, audience или полномочие закрыто отклоняются.

`controlplaneclient` выполняет полный путь потребителя: точный mTLS к
`control-plane`, проверку прикладного разрешения конкретной рабочей нагрузки
через `AuthorityProofResolver`, локальный UDS issuer Issue #186, interceptor
полного метода и readiness через тот же защищённый RPC. Конкретный компонент
потребителя обязан смонтировать своё разрешение, сокет issuer и файлы mTLS и
вызвать один из закрытых профилей операций (`AgentRunnerOperations`,
`AutomationSchedulerOperations`,
`ArtifactScannerOperations`, `RuntimeControllerOperations`,
`OwnerGateDeliveryOperations`, `MemoryIndexerOperations`). Consumer
Deployments не принадлежат Issue #187 и здесь не подменяются фиктивными
развёртываемыми компонентами.

Публикуются только два факта с утверждёнными потребителями:

| Факт | Условие | Потребитель | Доставка |
| --- | --- | --- | --- |
| `control_plane.runtime_configuration_changed` | устойчивое изменение project/team/chat/role/prompt/binding/workspace/integration/runtime/session/turn | `runtime-controller` | at-least-once, inbox и курсор потребителя |
| `control_plane.schedule_changed` | устойчивое изменение расписания и верхней границы | `automation-scheduler` | at-least-once, inbox и курсор потребителя |

Для процессов, шлюзов владельца, памяти, заявок на работу и метаданных артефактов
спекулятивные события не публикуются: авторитетные пути — `GetResource`,
`ListResources`, `SearchResources`, `ListAuditEvents` и `ListTombstones`.
Удаление, отмена, завершение и повтор каждого агрегата сохраняют tombstone,
аудит и подтверждение. Outbox фиксируется в транзакции команды; ретранслятор
не публикует из транспортного или доменного кода. После устойчивого JetStream
`PubAck` строка остаётся с потоком, последовательностью, признаком дубликата и
ограниченным сроком очистки. Потерянное подтверждение безопасно повторяет тот
же `event_id`.

## Доменные инварианты

| Область | Инвариант |
| --- | --- |
| Все команды | семантический ключ идемпотентности, канонический digest запроса, OCC и аудит фиксируются атомарно |
| Проект | ID и владельца назначает сервер; создание в организации требует полномочия владельца; slug стабилен |
| Команда, роль и prompt | общий CRUD не управляет полномочиями; отдельная административная команда проверяет полномочие вида, назначаемое подмножество и запрещает самостоятельное включение и повышение |
| Управляемая конфигурация | каждый project/team/chat/role/prompt/binding/workspace/integration/schedule хранит `managed_by=UI|GIT`; Git-объект обновляется только тем же источником с возрастающей ревизией, а переход к UI требует явного `detach_git_management` и отдельного устойчивого полномочия |
| Привязка учётных данных | хранится только URI метаданных; назначение и principal неизменяемы; ревизия растёт ровно на один |
| Интеграция | идентичность определения неизменяема; версия движется только вперёд |
| Ревизия среды исполнения | перед каждым ходом сервер разрешает точные сессию и разрешение роли, активные chat/prompt/привязку провайдера и только связанные с ролью workspace/integration/credential; создаётся неизменяемый снимок с версиями, digest, политикой, образом и предшественником; `runtime-controller` читает его через отдельный авторизованный RPC |
| Сессия | привязку провайдера сервер выбирает из точного разрешения роли; общий create/update/transition запрещён; close/cancel/archive/cleanup имеют отдельный закрытый автомат состояний, OCC, подтверждение, аудит и tombstone |
| Ход | неизменяемый закреплённый снимок, строгий FIFO и один активный ход на сессию; claim/renew/complete связывают рабочую нагрузку, попытку, поколение полномочий, срок и fence |
| Восстановление хода | истечение срока или ручной повтор создаёт новую неизменяемую попытку; отмена и завершение отзывают аренду, устаревшие workload/generation/token отклоняются |
| Процесс | дочерний процесс наследует принадлежащие серверу корневые actor/org/project/session/turn/attempt/revision, проверяет точного активного родителя и ребро запуска; enqueue повторно проверяет полную родословную |
| Расписание | закрытые цели `AGENT|PLAYBOOK`, точные role/playbook/prompt/runtime/session/room/notification/deadline; получение в одной транзакции создаёт либо разрешает сессию, свежую RuntimeRevision, Turn и при `PLAYBOOK` корневой ProcessRun; `FORBID` не сдвигает верхнюю границу, `SKIP` оставляет конечное подтверждение, `QUEUE` сохраняет FIFO |
| Шлюз владельца | запрос закрепляет корневого инициатора, process/session/turn/attempt/input, schedule/occurrence и точного получателя; доставка имеет неизменяемые ID, digest, Mattermost post и устойчивое подтверждение; решение допускается только после доставки и атомарно с процессом, аудитом и outbox |
| Память | область, владелец, процесс, рабочая нагрузка и происхождение назначаются сервером; FTS ищет title/content с ранжированием и курсором; проекция pgvector связывает точные content/resource/model version и digest |
| Заявка на работу | владелец, процесс, рабочая нагрузка, задача и попытка выводятся сервером и неизменяемы; активная заявка точного процесса или хода уникальна |
| Метаданные артефакта | только `RegisterArtifact` создаёт `PENDING`; точный scanner переводит `SCANNING`→`CLEAN`/`QUARANTINED`/`FAILED`; прикреплять и использовать разрешено только точный `CLEAN` digest |

Ссылки разрешаются внутри текущих настроек RLS организации и проекта;
межорганизационный и скрытый ресурсы дают одинаковый `NotFound`.

## Данные и кэш

PostgreSQL — единственный источник истины. Миграция создаёт схему
`control_plane`, отдельного владельца `NOLOGIN/NOSUPERUSER/NOBYPASSRLS`,
групповые роли среды исполнения и ретранслятора, `FORCE RLS`, ограничения и
точные разрешения. Поколения LOGIN `CURRENT`/`NEXT`/ограниченное
`PREVIOUS`/`RETIRED` материализуются принадлежащим окружению жизненным циклом
Vault. Устойчивая монотонная верхняя граница и намерение не допускают
воскрешения поколения после отката метаданных ConfigMap/Vault; повышение
требует фактического чтения через DSN principal `NEXT`.
Вывод из эксплуатации выполняет `NOLOGIN`, отзыв членства и серверное
завершение открытых соединений. Каждый statement использует одноразовый
привязанный HMAC контекст транзакции и заново связывает `session_user`,
поколение, состояние, organization/project/actor, PID соединения и ID
транзакции. GUC и `SET SESSION AUTHORIZATION` не являются источником
полномочий. Readiness проверяет схему
`20260731000300`,
membership, `LOGIN`, `NOSUPERUSER` и `NOBYPASSRLS`.

SQL хранится по одному именованному запросу в
`internal/repository/postgres/controlplane/sql`. Транзакция команды использует
`SERIALIZABLE`; путь запроса — транзакцию `READ ONLY` с локальной для
транзакции областью RLS.

Redis хранит только ограниченные снимки ресурсов:

- ключ содержит SHA-256 точного пространства имён
  `organization+project+kind+id+epoch`;
- строгая оболочка повторяет organization/project/kind/id/version, digest ключа
  и проекции; неизвестное поле или несовпадение никогда не возвращает кэш;
- TTL не более минуты, value не более 128 KiB;
- авторитетная эпоха кэша увеличивается в той же транзакции PostgreSQL;
- промах, повреждение или ошибка Redis приводит к чтению PostgreSQL;
- владение, полномочия, идемпотентность, аренды и верхние границы в Redis не
  хранятся.

## Запуск, готовность и остановка

До привязки gRPC listener сервис синхронно проверяет:

1. роли и схему PostgreSQL для среды исполнения и ретранслятора;
2. путь Redis с TLS;
3. точный поток JetStream (`CONTROL_PLANE`, subjects, replicas, файловое
   хранилище, `LimitsPolicy`, `DiscardOld`, максимальный срок 30 дней, окно
   дедупликации 2 минуты,
   `MaxMsgs=10000000`, `MaxBytes=34359738368`,
   `MaxMsgsPerSubject=5000000`, максимальный размер сообщения 262144 байта,
   запрет delete/purge, отсутствие mirror/source/republish/rollup/transform);
4. независимо доставленные закрытый ключ и доверие доказательства, ревизию
   политики;
5. тот же локальный verifier #186, который обслуживает рабочие RPC.

После барьера запускаются ретранслятор и периодическое согласование readiness.
Неожиданное завершение любого worker закрыто завершает процесс; orchestrator
не получает внешне живую реплику без циклов ретранслятора и readiness. При
остановке readiness сначала закрывается, workers отменяются и присоединяются
до закрытия PostgreSQL/Redis/NATS; gRPC и HTTP получают ограниченную остановку.
Остановка tracing и сброс Sentry используют независимые бюджеты.

Метрики не содержат ID организации или ресурса и используют закрытые labels.
Dashboard — `mattercodex-control-plane`. Alerts ведут на абсолютный HTTPS URL
runbook.

## Конфигурация

Значения ниже — имена, а не значения секретов.

| Переменная | Назначение |
| --- | --- |
| `CONTROL_PLANE_GRPC_LISTEN`, `CONTROL_PLANE_TECHNICAL_LISTEN` | внутренние listeners |
| `CONTROL_PLANE_TLS_CERTIFICATE_FILE`, `CONTROL_PLANE_TLS_PRIVATE_KEY_FILE`, `CONTROL_PLANE_TLS_CLIENT_CA_FILE` | точный mTLS рабочей нагрузки |
| `CONTROL_PLANE_POSTGRES_DSN_FILE`, `CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE` | файлы DSN среды исполнения и ретранслятора |
| `CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_DSN_FILE` | точный DSN `NEXT` только для миграции и обязательного чтения перед повышением |
| `CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME`, `CONTROL_PLANE_POSTGRES_CA_FILE`, `CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS` | TLS и пул PostgreSQL |
| `CONTROL_PLANE_POSTGRES_PRINCIPAL_NAME`, `CONTROL_PLANE_POSTGRES_PRINCIPAL_GENERATION`, `CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID`, `CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE` | точное поколение среды исполнения и доказательство контекста транзакции |
| `CONTROL_PLANE_REDIS_ADDRESS`, `CONTROL_PLANE_REDIS_TLS_SERVER_NAME`, `CONTROL_PLANE_REDIS_CA_FILE`, `CONTROL_PLANE_REDIS_USERNAME`, `CONTROL_PLANE_REDIS_PASSWORD_FILE`, `CONTROL_PLANE_REDIS_DATABASE`, `CONTROL_PLANE_REDIS_POOL_SIZE` | ограниченный кэш Redis |
| `CONTROL_PLANE_NATS_URL`, `CONTROL_PLANE_NATS_TLS_SERVER_NAME`, `CONTROL_PLANE_NATS_CA_FILE`, `CONTROL_PLANE_NATS_CREDENTIALS_FILE`, `CONTROL_PLANE_NATS_STREAM`, `CONTROL_PLANE_NATS_REPLICAS` | точный издатель JetStream |
| `CONTROL_PLANE_AUTHORITY_POLICY_FILE` | версионированная политика deny-by-default |
| `CONTROL_PLANE_APPLICATION_GRANT_TRUST_DIR` | независимо доставленные публичные JWK точных разрешений производителей |
| `CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE`, `CONTROL_PLANE_PROOF_TRUST_FILE`, `CONTROL_PLANE_PROOF_SIGNER_GENERATION` | независимо проверенный signer доказательств |
| `CONTROL_PLANE_LEASE_SIGNING_KEY_FILE` | HMAC-ключ аренды хода |
| `CONTROL_PLANE_OIDC_TLS_SERVER_NAME`, `CONTROL_PLANE_OIDC_CA_FILE` | закреплённый TLS discovery/JWKS OIDC |
| `POD_UID` | владелец аренды ретранслятора |
| `CONTROL_PLANE_*_TIMEOUT`, `CONTROL_PLANE_*_INTERVAL`, `CONTROL_PLANE_CACHE_TTL`, `CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT` | ограниченные пределы жизненного цикла |
| `OTEL_*`, `SENTRY_DSN_FILE`, `SENTRY_EXPECTED_HOST` | общая среда наблюдаемости |

Файлы секретов должны быть абсолютными обычными файлами без разрешений для
`other`. DSN, JWK, учётные данные, ключи и их содержимое не логируются.

## Развёртывание и миграции

База находится в `deploy/k8s/base/control-plane`, наложения окружений — в
`deploy/k8s/overlays/{staging,production}/control-plane`. Канонический render
требует два реальных digest образов и закрыто отказывает при placeholder:

```bash
tools/render-control-plane.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  > /tmp/control-plane-staging.yaml
```

Команда только рендерит; она не применяет manifest. Для production нужно
заменить `staging` на `production` и использовать отдельно утверждённые digest.

Общая база `deploy/k8s/base/image-supply-chain` материализует локальный
OCI registry только с TLS, два rootless worker BuildKit и ежедневную задачу
хранения. Все прикладные образы в итоговом render ссылаются на локальный
registry по digest. Теги обязаны иметь вид `vYYYYMMDDHHMMSS-<git-sha>`; задача
оставляет текущую и две предыдущие версии каждого репозитория `mattercodex/*`
и закрыто отказывается удалять неизвестный формат. Три начальных образа
(`registry`, `moby/buildkit`, `regctl`) закреплены публичными OCI digest;
после начальной загрузки оператор зеркалирует их в тот же локальный registry.
CA доставляется через Vault CSI и используется клиентами BuildKit и хранения
без отключения TLS.

Варианты сборщика Kubernetes сверены с официальными источниками: standalone
BuildKit, Shipwright Build/BuildRun и Tekton Tasks. В соответствии с
`ADR-MC-008` выбран прямой BuildKit как минимальный авторитетный backend;
Shipwright и Tekton остаются возможными оркестраторами поверх него, но не
создают второй источник истины. Старый Kaniko template сохранён только для
legacy-контура и не включён в новую базу.

Migration Job запускает `control-plane-cli migrate expand` до rollout и
атомарно согласует `CURRENT`/`NEXT`/`PREVIOUS`, активный ключ контекста и
выведенные из эксплуатации сессии. При наличии `NEXT` наложение GitOps обязано
одновременно доставить `CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_*` и отдельный файл
DSN: CLI сначала подключается именно этим LOGIN и сохраняет readback, только
следующее идемпотентное согласование может повысить его до `CURRENT`. Миграции
`20260731000200` и `20260731000300` явно forward-only: downgrade отклоняется,
потому что потерял бы RLS fences, верхнюю границу и readback principal,
попытки, подтверждения и происхождение вектора. Откат приложения выполняется
только совместимым образом; откат схемы — новой компенсирующей forward
миграцией.

Поток JetStream и учётные данные Vault database/static принадлежат окружению.
Их точный контракт проверяется стартовым барьером; сервис не создаёт и не
ослабляет ресурсы брокера или Vault. RBAC Role/RoleBinding намеренно
отсутствуют: контейнеры приложения и миграции не обращаются к Kubernetes API;
доставку CSI выполняет драйвер окружения.

## Ручная приёмка

Без deploy можно:

1. собрать оба бинарных файла и публичные модули клиента и API;
2. выполнить `buf build` и проверить воспроизводимую генерацию кода;
3. проверить разбор YAML/JSON и канонический render с двумя тестовыми
   ненулевыми digest;
4. убедиться, что render содержит рабочую нагрузку non-root/read-only,
   Migration Job, deny-all и только NetworkPolicy с точными назначениями;
5. сравнить все методы Proto с политикой полномочий, а группы ошибок —
   с `contracts/errors/v1/rpc-http-mapping.yaml`;
6. проверить, что `Closes #187` относится только к одному PR.

Фактические проверки PostgreSQL/Redis/NATS/Vault/Kubernetes и staging rollout
требуют отдельного разрешения и окружения.

## Политика прототипа и ограничения

Активен профиль `Prototype`: полное покрытие, integration/E2E,
contract/deploy/render/lifecycle/oracle suites и полный baseline не входят в
этот PR. Поддерживаемая волна тестирования отслеживается в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

Не входят в компонент: внешний OpenAPI/HTTP gateway, согласование среды
исполнения, выполнение автоматизаций, процессы Mattermost/MCP/Codex, хранение
байтов артефактов и значения секретов.

Эксплуатация и восстановление описаны в
[`docs/runbooks/control-plane.md`](../../../docs/runbooks/control-plane.md).

## Проверенные внешние источники

Context7 был вызван для PostgreSQL, pgx, goose, gRPC/Protobuf, Redis, NATS,
OpenTelemetry, Sentry, Kubernetes и Vault, но вернул quota error. Использован
резервный путь к официальной первичной документации:

- [PostgreSQL row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html),
  [transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html)
  и [full-text search](https://www.postgresql.org/docs/current/textsearch.html);
- [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5) и
  [goose](https://github.com/pressly/goose);
- [gRPC Go](https://grpc.io/docs/languages/go/) и
  [Protocol Buffers](https://protobuf.dev/);
- [Redis Go client](https://redis.io/docs/latest/develop/clients/go/) и
  [NATS JetStream](https://docs.nats.io/nats-concepts/jetstream);
- [pgvector](https://github.com/pgvector/pgvector);
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/),
  [Sentry Go](https://docs.sentry.io/platforms/go/),
  [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
  [Kustomize](https://kubectl.docs.kubernetes.io/references/kustomize/) и
  [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/);
- [BuildKit](https://github.com/moby/buildkit),
  [Distribution registry](https://distribution.github.io/distribution/),
  [regctl](https://regclient.org/usage/regctl/),
  [Shipwright Build](https://shipwright.io/docs/build/) и
  [Tekton Tasks](https://tekton.dev/docs/pipelines/tasks/).
