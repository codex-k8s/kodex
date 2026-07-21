# Bot-Service Runbook

## Назначение

Этот runbook описывает второй кодовый этап: `matter-codex` bot-service для Mattermost.

В этом PR сервис умеет:

- отвечать на кластерный `/healthz`;
- принимать Mattermost slash callback `/mattermost/slash/agents`;
- показывать Mattermost menu card по пустому `/agents`;
- принимать Mattermost menu action callback `/mattermost/actions/agents` для кнопок menu card;
- отвечать на `/agents status`;
- выполнять admin-команды `/agents repo add`, `/agents repo list`, `/agents token check`, `/agents locale get|set`, `/agents profile list`, `/agents prompt help|list|show|render|set`, `/agents openai auth|status|list|cleanup`;
- выполнять GitHub adapter/account команды `/agents github account list`, `/agents github check`, `/agents github branch`, `/agents github pr`;
- управлять через Mattermost dialog-кнопки metadata GitHub accounts: add/edit/delete account binding к существующему Kubernetes Secret без ввода raw token в Mattermost;
- принимать GitHub webhook callback `/github/webhook` с HMAC validation;
- автоматически регистрировать repo webhook при `/agents repo add github owner/name [default-branch]`, если GitHub token имеет hook write permission;
- выполнять Kubernetes runtime smoke-команды `/agents runtime smoke|status|cleanup|prune` через client-go, Job, PVC и подготовленный agent-runner image;
- выполнять Codex reviewer-команды `/agents review pr|status|cleanup`, запускающие отдельный Job/PVC для review существующего GitHub PR через OpenAI account, GitHub account и prompt template из agent profile;
- применять storage migrations и хранить repository/profile/audit metadata в PostgreSQL;
- создавать Mattermost repo-channel при добавлении repository;
- хранить Mattermost bot/slash tokens только в Kubernetes Secret;
- создавать базовую Mattermost control surface через `mmctl --local` внутри Mattermost pod: team, каналы и slash command.

## Env contract

Обязательные базовые ключи остаются теми же, что и для Mattermost bootstrap.

Новые ключи:

- `MATTERCODEX_BOT_SERVICE_HOST` - optional, host публичного Ingress;
- `MATTERCODEX_BOT_SERVICE_SITE_URL` - optional, публичный URL bot-service;
- `MATTERCODEX_BOT_SERVICE_INTERNAL_URL` - обязательный при заданном bot- или slash-токене внутренний источник callback URL; допускается только `http`/`https` Kubernetes Service DNS с явным портом, без userinfo, query, fragment и произвольного path;
- `MATTERCODEX_MATTERMOST_INTERNAL_URL` - optional, внутренний URL Mattermost API для bot-service; нужен, если публичный Mattermost закрыт OAuth proxy;
- `MATTERCODEX_MATTERMOST_BOT_TOKEN` - нужен для provisioning Mattermost team/channels/slash command;
- `MATTERCODEX_MATTERMOST_ADMIN_TOKEN` - отдельный административный PAT только для создания и обслуживания bot identity ролей; bootstrap хранит его в Kubernetes Secret и не использует для публикации сообщений;
- `MATTERCODEX_MATTERMOST_SLASH_TOKEN` - optional, обычно заполняется provisioning script в Kubernetes Secret;
- `MATTERCODEX_GITHUB_SECRET` - optional, имя Kubernetes Secret для reviewer/user GitHub account;
- `MATTERCODEX_AGENT_GITHUB_SECRET` - optional, имя Kubernetes Secret для developer/agent GitHub account;
- `MATTERCODEX_GITHUB_TOKEN` - optional, GitHub token для bot-service и reviewer account; deploy-скрипты также принимают legacy `GITHUB_PAT`;
- `MATTERCODEX_GITHUB_USERNAME` и `MATTERCODEX_GITHUB_EMAIL` - нужны, если задан `MATTERCODEX_GITHUB_TOKEN`/`GITHUB_PAT`; GitHub login/email reviewer account; deploy-скрипты также принимают legacy `GITHUB_USERNAME`/`GITHUB_EMAIL`;
- `MATTERCODEX_AGENT_GITHUB_TOKEN`, `MATTERCODEX_AGENT_GITHUB_USERNAME`, `MATTERCODEX_AGENT_GITHUB_EMAIL` - optional GitHub credentials developer/agent account; если задан token, username/email обязательны; deploy-скрипты также принимают legacy `GIT_BOT_TOKEN`, `GIT_BOT_USERNAME`, `GIT_BOT_MAIL`;
- `MATTERCODEX_GITHUB_WEBHOOK_SECRET` - optional, secret для `/github/webhook`; deploy-скрипты также принимают legacy `GITHUB_WEBHOOK_SECRET`;
- `MATTERCODEX_LOCALE` - optional, стартовая локаль Mattermost-facing ответов bot-service; Go-дефолт `en`, deploy-скрипты для текущего контура по умолчанию ставят `ru`;
- `MATTERCODEX_DATABASE_DSN` - runtime DML DSN из ключа `bot-service-runtime-datasource` существующего PostgreSQL Secret; login обязан быть отдельным `NOSUPERUSER NOBYPASSRLS`, не владельцем схемы или таблиц, без `CREATEROLE`, `CREATEDB`, `REPLICATION`, `CREATE` в service schema и `TEMP`;
- `MATTERCODEX_MIGRATIONS_DATABASE_DSN` - migration/schema-owner DSN из ключа `mattermost-datasource` того же Secret; при включённых миграциях обязателен, должен указывать на тот же endpoint и базу, но на другой login;
- `MATTERCODEX_POSTGRES_RUNTIME_USER` и `MATTERCODEX_POSTGRES_RUNTIME_PASSWORD` - входные параметры foundation для отдельного runtime login; значения не печатаются и сохраняются только в PostgreSQL/Kubernetes Secret;
- `MATTERCODEX_STORAGE_MIGRATIONS_ENABLED` - optional, включает Go migrations на старте;
- `MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_ENABLED` - optional, включает ограниченную фоновую очистку истёкших строк capability;
- `MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_INTERVAL` - optional, задаёт интервал запуска очистки;
- `MATTERCODEX_INTERACTION_CAPABILITY_RETENTION` - optional, задаёт отсрочку после истечения capability до удаления;
- `MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_BATCH` - optional, задаёт верхнюю границу строк одного прохода;
- `MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES` - optional, лимит размера GitHub webhook payload;
- `MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT`, `MATTERCODEX_BOT_SERVICE_READ_TIMEOUT`, `MATTERCODEX_BOT_SERVICE_IDLE_TIMEOUT`, `MATTERCODEX_BOT_SERVICE_MAX_HEADER_BYTES` — обязательные положительные bounds входного HTTP transport; `READ_TIMEOUT` ограничивает полное чтение body, включая медленное chunked-соединение, а не только заголовки;
- `MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES` — предел полного JSON envelope MCP POST до `go-sdk`, чтения session/token и доменного допуска; предел обязан быть не меньше шестикратного `MATTERCODEX_CALLBACK_MAX_BYTES` плюс фиксированный запас на JSON escaping, metadata и envelope;
- `MATTERCODEX_MATTERMOST_HTTP_TIMEOUT`, `MATTERCODEX_MATTERMOST_HTTP_DIAL_TIMEOUT`, `MATTERCODEX_MATTERMOST_HTTP_TLS_HANDSHAKE_TIMEOUT`, `MATTERCODEX_MATTERMOST_HTTP_RESPONSE_HEADER_TIMEOUT`, `MATTERCODEX_MATTERMOST_HTTP_IDLE_CONN_TIMEOUT` — обязательные положительные bounds промышленного Mattermost transport; значения задаются ConfigMap и проверяются до старта;
- `MATTERCODEX_CALLBACK_MAX_BYTES`, `MATTERCODEX_CALLBACK_MAX_CHUNKS`, `MATTERCODEX_CALLBACK_MAX_CHUNK_BYTES`, `MATTERCODEX_CALLBACK_PUBLISH_CONCURRENCY`, `MATTERCODEX_CALLBACK_PUBLISH_DEADLINE` — server-owned bounds callback-публикации; значения задаются ConfigMap и проверяются до старта;
- `MATTERCODEX_IMAGE_BUILD_STRATEGY` - optional, способ сборки image в remote deploy; default `kaniko`; legacy `docker` требует `docker` или `nerdctl` прямо на целевом сервере;
- `MATTERCODEX_IMAGE_TAG` - optional, tag для bot-service и agent-runner image; при `kaniko` и `--apply` без явного значения deploy-скрипт генерирует уникальный tag из commit и UTC timestamp;
- `MATTERCODEX_IMAGE_REPOSITORY_PREFIX` - optional, registry path prefix для image;
- `MATTERCODEX_IMAGE_REGISTRY_MANAGED` - optional, при `true` render/apply создает встроенный MatterCodex registry в namespace;
- `MATTERCODEX_IMAGE_REGISTRY_NAME`, `MATTERCODEX_IMAGE_REGISTRY_IMAGE`, `MATTERCODEX_IMAGE_REGISTRY_STORAGE_SIZE`, `MATTERCODEX_IMAGE_REGISTRY_HOST_PORT` - optional, параметры встроенного registry;
- `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST` - optional, registry host, через который kubelet тянет image; default `localhost:<host-port>` для single-server контура;
- `MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST` - optional, registry host, в который Kaniko push'ит image изнутри кластера; default Kubernetes service DNS;
- `MATTERCODEX_KANIKO_IMAGE`, `MATTERCODEX_KANIKO_CONTEXT_PVC`, `MATTERCODEX_KANIKO_CONTEXT_STORAGE_SIZE` - optional, параметры Kaniko executor и PVC build context;
- `MATTERCODEX_KANIKO_CPU_REQUEST`, `MATTERCODEX_KANIKO_MEMORY_REQUEST`, `MATTERCODEX_KANIKO_MEMORY_LIMIT` - optional, ресурсы Kaniko Job; defaults `2000m`/`2Gi`/`24Gi`, CPU limit отсутствует; повышенный лимит нужен для snapshot большого agent-runner image и применяется только к временной build-job;
- `MATTERCODEX_KANIKO_JOB_TTL_SECONDS`, `MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS` - optional, lifecycle limits Kaniko Job. Успешный remote build удаляет Job сразу после push, чтобы завершённый pod не удерживал memory quota; TTL остается страховкой при прерывании deploy-клиента;
- `MATTERCODEX_RUNTIME_ENABLED` - optional, включает Kubernetes runtime adapter;
- `MATTERCODEX_RUNTIME_NAMESPACE` - optional, namespace для Job/PVC runtime-запусков;
- `MATTERCODEX_RUNTIME_SMOKE_IMAGE` - optional, legacy image setting; текущий smoke Job запускается через `MATTERCODEX_AGENT_RUNNER_IMAGE`;
- `MATTERCODEX_AGENT_RUNNER_IMAGE` - optional, image для smoke/developer/reviewer/auth Job; текущий default строится от `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST`;
- `MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE` - optional, при `true` install script собирает agent-runner image через выбранную `MATTERCODEX_IMAGE_BUILD_STRATEGY` перед deploy;
- `MATTERCODEX_CODEX_PACKAGE` - optional, npm package spec Codex CLI, который устанавливается в agent-runner image при сборке;
- `MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE` - optional, размер PVC рабочего каталога smoke-запуска;
- `MATTERCODEX_AGENT_SESSION_CPU_REQUEST`, `MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST` - optional, явные requests полноценного agent session pod; defaults `500m`/`1Gi`, чтобы Kubernetes не приравнял memory request к высокому limit;
- `MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT` - optional, индивидуальный memory ceiling полноценного agent session pod; default `64Gi` для single-node owner-инсталляции;
- `MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT` - optional, индивидуальный memory ceiling коротких служебных Job; default `4Gi`;
- `MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT` - optional, верхняя граница memory-backed `/dev/shm` каждого runner container; default `8Gi`;
- `MATTERCODEX_RUNTIME_JOB_TTL_SECONDS` - optional, TTL завершенных smoke Job;
- `MATTERCODEX_RUNTIME_LOG_TAIL_LINES` - optional, число последних строк pod log для `/agents runtime status`;
- `MATTERCODEX_RUNTIME_LIMITS_ENABLED` - optional, включает render/apply namespace `ResourceQuota` и `LimitRange` для runtime namespace; default `true`;
- `MATTERCODEX_RUNTIME_QUOTA_PODS`, `MATTERCODEX_RUNTIME_QUOTA_JOBS`, `MATTERCODEX_RUNTIME_QUOTA_PVCS` - optional, object count quota для pod, batch Job и PVC в runtime namespace; default PVC quota `120` оставляет запас для сохранённых сессий и шести параллельных волн;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_STORAGE` - optional, суммарная quota на requested PVC storage в runtime namespace;
- `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_CPU`, `MATTERCODEX_RUNTIME_QUOTA_REQUESTS_MEMORY` - optional, namespace quota на compute requests; дефолты для single-node owner-инсталляции: requests `28`/`96Gi`;
- `MATTERCODEX_OWNER_MATTERMOST_USERNAME` - optional username владельца owner-инстанса, которого bootstrap добавляет в приватные системные каналы координации; значение не хранится в репозитории;
- `MATTERCODEX_MATTERMOST_ADMIN_USERNAME`, `MATTERCODEX_MATTERMOST_ADMIN_EMAIL` - optional данные выделенного непользовательского lifecycle-admin аккаунта Mattermost; аккаунт остается обычным `system_admin`, потому что Mattermost не разрешает bot account создавать другие bot identity через REST API;
- `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_CPU`, `MATTERCODEX_RUNTIME_LIMIT_DEFAULT_REQUEST_MEMORY` - optional, container requests для pod без явных resources; дефолты agent container: request `500m`/`1Gi`;
- `MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT` - optional, ServiceAccount для agent/smoke Job;
- `MATTERCODEX_CODEX_AUTH_SECRET` - optional, base name для Kubernetes Secrets с Codex `auth.json`; для account `primary` будет создан secret `${MATTERCODEX_CODEX_AUTH_SECRET}-primary`;
- `MATTERCODEX_DEFAULT_TEAM_NAME` - optional, по умолчанию `agents`;
- `MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME` - optional;
- `MATTERCODEX_DEFAULT_CHANNELS` - optional, список `name:Display Name` через запятую.

Скрипты печатают только статус наличия токенов, не значения.

Foundation создаёт в том же `${MATTERCODEX_POSTGRES_SECRET}` три runtime-ключа: `bot-service-runtime-user`, `bot-service-runtime-password`, `bot-service-runtime-datasource`. Для существующего Secret скрипт сохраняет четыре исходных migration-owner ключа, добавляет отсутствующую полную runtime-тройку и закрыто прекращает работу при частично заполненной тройке. `bot-service` через migration DSN создаёт или ужесточает отдельный login, затем `000025` выдаёт только точные DML/sequence/function grants. Совпадение login, другой endpoint или отсутствие явного runtime password отклоняются до запуска сервиса; fallback на owner DSN отсутствует.

## PostgreSQL test lifecycle

`make test-go-postgres` и PostgreSQL-часть `make test-go-all` всегда запускаются через `cmd/postgres-test-target`. Если готовый target и bootstrap DSN отсутствуют, wrapper сам создаёт приватные временные `PGDATA`, Unix socket и loopback endpoint, выполняет offline-init proof registry до запуска endpoint, запускает найденный PostgreSQL server и выдаёт один структурированный one-shot proof. Для явного bootstrap одновременно обязательны `MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN` и `MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF`; отсутствие proof не имеет локального fallback. Wrapper атомарно потребляет proof до `CREATE DATABASE`, создаёт случайную database из `template0`, записывает связанный с server identity высокоэнтропийный ограниченный по времени target marker и передаёт дочернему процессу только `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN` и `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER`. После команды helper удаляет database только внутри зарегистрированного им generated private cluster; для внешнего endpoint он сохраняет database, печатает только её безопасное точное имя и передаёт очистку владельцу ephemeral endpoint/controller. Уже созданный target допустим лишь при одновременно заданных DSN и точном marker.

Admission сначала без подключения разбирает URL и keyword DSN одинаковым `pgx`, требует имя database, детерминированно связанное с marker, и запрещает `mattermost`, `postgres`, `template*`, настроенную production identity, альтернативный fallback endpoint и отсутствующий, несовпадающий или просроченный marker. Loopback и Unix socket не являются доказательством одноразовости bootstrap. Proof версии 1 содержит криптографический 256-bit nonce и его SHA-256, `issued_at`, `expires_at` с максимумом 10 минут и ограниченным clock skew, точные endpoint/server fingerprints, maintenance database, purpose, run ID и состояние `unconsumed`. Server fingerprint связывает system identifier и `data_directory`; endpoint fingerprint связывает настроенный и фактический адрес/порт. До любого bootstrap DDL одна условная запись exact registry переводит proof в consumed и одновременно резервирует точное высокоэнтропийное имя target, SHA-256 marker, OID владельца и время reservation. Exact registry имеет проверяемую схему и DB-trigger неизменяемости: reservation нельзя переписать, а наблюдаемый OID созданной database фиксируется только один раз как проверка согласованности ledger, но не как доказательство происхождения объекта. При гонке выигрывает один caller. Stale, future, reused, malformed, short, wrong endpoint/server/database/purpose proof и совпадение с production identity закрыто отклоняются до DDL.

Внешний CI endpoint дополнительно обязан входить в точный список `host:port` переменной `MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS` и иметь тот же заранее подготовленный CI bootstrap one-shot proof; статической marker/comment строки нет. Runtime helper не создаёт proof registry на произвольном сервере. Generated harness до запуска server создаёт exact registry и private authority sentinel внутри принадлежащего ему временного root, после запуска связывает in-process authority с точными приватными `PGDATA`/Unix socket, `system_identifier` и server fingerprint и удаляет эту authority до остановки server. Непосредственно перед helper-owned `CREATE EXTENSION`, `CREATE SCHEMA`, миграциями/ролями/проверками целевого дерева и каждым `DROP SCHEMA`/`DROP DATABASE` выполняется read-only сверка `current_database()`, фактических endpoint/server fingerprints и точного комментария target marker. `DROP DATABASE` дополнительно требует действующую generated private-cluster authority; allowlist, loopback, имя, owner, OID, comment или строка конфигурации её не заменяют. Ошибка закрыто прекращает изменение. DSN, credentials, proof, nonce и marker не выводятся.

С момента отправки `CREATE DATABASE` любая ошибка или отмена запускает внутри bootstrap ограниченную компенсирующую сверку на новом server-owned context, который не наследует отмену вызывающего контекста: не более четырёх попыток, до пяти секунд на попытку и до двадцати секунд суммарно. Каждая попытка создаёт новое подключение к maintenance database и заново сверяет endpoint, `system_identifier`, `data_directory`, exact consumed purpose/run claim, reservation, владельца, ledger и marker. Состояния `reserved/database_oid=0` и `created` до exact applied marker никогда не усваивают наблюдаемый catalog identity, не меняют ledger и не вызывают `DROP`: target сохраняется с явной ошибкой коллизии/объекта-сироты и требованием ручной очистки. После exact ledger state `marked` ограниченный `DROP` возможен только в зарегистрированном generated private cluster; внешний/shared endpoint всегда получает безопасную передачу очистки владельцу, включая успешный destroy и компенсацию. Неоднозначный generated `DROP` сверяется по отсутствию exact database. Чужой комментарий, изменённый owner/OID/registry или replacement сохраняется. Proof остаётся consumed и не используется повторно.

Перед созданием пакетной схемы общий helper берёт database-global advisory lock, выполняет единственную установку `vector` в `public` и проверяет `public.vector`. Каждый пакет получает собственную схему и `search_path=<schema>,public`; cleanup удаляет только эту схему и никогда не удаляет extension. Fresh database разрешена только внутри доказанного generated private cluster, не удаляется дочерним тестовым процессом по catalog identity и исчезает вместе со всем принадлежащим harness `PGDATA` после завершения команды. Default `make test-go-postgres` запускается без внешнего `GOFLAGS=-p=1`, затем повторяется; clean-start regression создаёт database из `template0`, доказывает отсутствие `vector`, несколько раз запускает конкурентную установку и проверяет ровно один extension и доступный type.

Ручная приёмка внешнего ephemeral endpoint отдельно от удаления generated `PGDATA`:

1. До запуска зафиксировать количество `mc_test_*` database без вывода DSN, marker или proof.
2. Последовательно внести отказ после `CREATE`, после записи OID в ledger и после применённого `COMMENT`, но до exact ledger state `marked`. Target обязан сохраниться, proof — остаться consumed, ledger — остаться `reserved|created`, а helper-owned `DROP` не должен вызываться. Владелец endpoint удаляет только сообщённое точное имя после ручной сверки.
3. Для exact `marked` target повторить успешный destroy, компенсацию, transient reconnect и неоднозначный ответ `DROP`. На внешнем endpoint каждый существующий target сохраняется и явно передаётся владельцу; на generated private cluster подтверждается ограниченная очистка.
4. После `CREATE` и после записи OID в ledger заменить target database объектом с тем же именем, владельцем, точным исходным OID и пустым comment. Bootstrap обязан завершиться безопасной ошибкой, replacement обязан сохраниться, proof не должен приниматься повторно; wildcard/prefix cleanup запрещён.

## Render

```bash
bash scripts/k8s/render-bot-service.sh --env-file .env --render-dir /tmp/matter-codex-bot-render
bash scripts/k8s/verify-rendered-objects.sh --render-dir /tmp/matter-codex-bot-render --expected-files 8 --expected-objects 18
```

Проверка перечисляет каждый непустой YAML document как `Kind/name`, исключает пустые документы и сравнивает число объектов с результатами `kubectl create --dry-run=client --validate=false`. Для полного профиля с управляемым registry, Kaniko и runtime limits ожидаются 8 файлов и 18 объектов; число разделителей `---` доказательством не является. Команда не обращается к Kubernetes API и ничего не применяет.

В render directory попадают:

- встроенный registry manifest, если `MATTERCODEX_IMAGE_REGISTRY_MANAGED=true`;
- PVC для Kaniko build context, если `MATTERCODEX_IMAGE_BUILD_STRATEGY=kaniko`;
- config ConfigMap;
- ResourceQuota/LimitRange для runtime namespace, если `MATTERCODEX_RUNTIME_LIMITS_ENABLED=true`;
- ServiceAccount/RBAC для bot-service runtime adapter и agent runner;
- Deployment;
- Service;
- Ingress.

Публичный Ingress использует точный список разрешённых маршрутов только для `/mattermost/slash/agents` и `/github/webhook`. Маршруты health/readiness, metrics, action/dialog callback, internal agent session и MCP доступны только через кластерный сервис Kubernetes. Mattermost получает action/dialog URL из `MATTERCODEX_BOT_SERVICE_INTERNAL_URL`, поэтому внешний Prefix `/` не требуется.

При `--apply` remote deploy по умолчанию использует `MATTERCODEX_IMAGE_BUILD_STRATEGY=kaniko`: локально создается только tar build context, он передается по SSH во временный pod с PVC, а image собирается Kaniko Job внутри кластера и push'ится во встроенный MatterCodex registry. Kubelet тянет готовые image из этого registry через `MATTERCODEX_IMAGE_REGISTRY_PULL_HOST`. Гигабайтные `docker save` archive через локальную сеть не передаются.

Перед применением Deployment installer сначала применяет его ConfigMap и управляемые Secret, затем получает только Kubernetes `uid` и `resourceVersion` всех подключённых pod inputs: config ConfigMap, bot-service Secret, PostgreSQL Secret и GitHub Secret. Из этих безопасных идентификаторов вычисляется аннотация `matter-codex.kodex.works/pod-input-revision`; содержимое Secret в hash, манифест, лог или вывод не попадает. Неизменные inputs не меняют PodTemplate и не создают rollout. Ротация любого подключённого Secret или ConfigMap меняет одну revision-аннотацию, а смена image меняет штатное поле container image; в обоих случаях Kubernetes создаёт ровно один rollout без дополнительного `rollout restart`.

Kaniko Job получает повышенные default resources для тяжелого `agent-runner` image и использует `--skip-unused-stages=true`, чтобы не строить нецелевые Dockerfile stages при `--target`.

Для проверки сборки без изменения bot-service Deployment используйте `scripts/remote/install-bot-service.sh --env-file .env --apply --build-only` и выставьте `MATTERCODEX_BOT_SERVICE_BUILD_IMAGE=false` или `MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE=false`, если нужно собрать только один image.

Legacy strategy `MATTERCODEX_IMAGE_BUILD_STRATEGY=docker` оставлена только для контуров, где `docker` или `nerdctl` есть прямо на целевом сервере. Remote installer не делает локальный Docker build/import fallback.

Agent runner image содержит явный non-root user UID/GID `10001`. Runtime Job дополнительно задает pod/container `securityContext`: `runAsNonRoot`, `runAsUser`, `runAsGroup`, `fsGroup`, `seccompProfile: RuntimeDefault`, dropped capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`. Writable paths отдаются через volumes: `/workspace` для run PVC, `/codex-home` для device-code auth, `/home/matter-codex` для `gh`/npm/cache и `/tmp` для временных файлов.

Runtime namespace получает `ResourceQuota` `matter-codex-runtime-quota` и `LimitRange` `matter-codex-runtime-container-defaults`. Quota ограничивает общее число pods, batch Jobs, PVC, суммарный requested storage и суммарные cpu/memory requests. LimitRange задает cpu/memory requests для containers без явных resources, чтобы quota admission не отклоняла agent Job.

Полноценные agent session pod явно задают requests `MATTERCODEX_AGENT_SESSION_CPU_REQUEST`/`MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST` и индивидуальный memory ceiling `MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT`. Явный request обязателен: иначе Kubernetes при заданном limit может приравнять request к `64Gi` и заблокировать параллельные волны. Короткие служебные `smoke`, Codex device-auth и auth-check Job задают requests `100m`/`128Mi` и ceiling `MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT`. CPU limits отсутствуют. Namespace-level `limits.memory` quota намеренно не задаётся, поэтому высокий ceiling одного pod не блокирует одновременное планирование до шести волн. Один runaway container при этом не может занять всю память узла, а memory-backed `/dev/shm` дополнительно ограничен `MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT`. Оператор сохраняет контроль requests, pod quota, node pressure и удаление старейших idle pod.

Если новый session pod отклонен ResourceQuota или не размещается scheduler из-за нехватки ресурсов, bot-service может удалить самый старый idle session pod без queued/running turn и повторить запуск. PVC и snapshot сессии сохраняются. Если безопасного кандидата нет, turn остается queued; активные agent pod механизм capacity reclaim не удаляет. Отказ ResourceQuota при создании самого session PVC отдельно возвращается из Kubernetes-адаптера как типизированная ошибка ёмкости без очистки PVC/Secret и без внутреннего повтора создания PVC.

## Remote dry-run

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --dry-run=server
```

Если Mattermost token еще не задан, Secret не создается, а Deployment использует optional secret refs.

Если GitHub token или webhook secret заданы, deploy-скрипты создают Kubernetes Secret для reviewer/user account. При наличии token в Secret также кладутся `github-username` и `github-email`. Если задан `GIT_BOT_TOKEN` или `MATTERCODEX_AGENT_GITHUB_TOKEN`, deploy-скрипты создают отдельный Secret для developer/agent account с ключами `github-token`, `github-username`, `github-email`. Значения не печатаются.

Codex/OpenAI account authorization не выполняется deploy-скриптом. Основной путь - Mattermost команды ниже.

## Codex/OpenAI account authorization

Developer runner не использует raw API key. Для Codex CLI создается Kubernetes Secret с `auth.json`, полученным через device-code авторизацию из Mattermost.

Первичная авторизация account `primary`:

```text
/agents openai auth primary
/agents openai status primary
/agents openai cleanup primary
/agents openai delete primary
```

Ожидаемый результат:

- `auth primary` создает metadata для OpenAI account и временный Kubernetes Job `mc-codex-auth-primary`;
- `status primary` показывает ссылку `https://auth.openai.com/codex/device` и одноразовый code;
- владелец открывает ссылку в браузере, вводит code и подтверждает account;
- повторный `/agents openai status primary` сохраняет `auth.json` в Secret `${MATTERCODEX_CODEX_AUTH_SECRET}-primary`, помечает account как `authorized` и удаляет auth Job;
- содержимое `auth.json` не выводится в Mattermost, логи, PR или prompt;
- `cleanup primary` удаляет только временный auth Job;
- `delete primary` удаляет OpenAI account metadata, временный auth Job и созданный auth Secret. Удаление блокируется, если account используется agent profile.

Несколько аккаунтов поддерживаются через разные имена:

```text
/agents openai auth reviewer-plus
/agents openai status reviewer-plus
/agents openai list
/agents openai delete reviewer-plus
```

В кнопочном UX то же действие доступно через `/agents` -> `Аккаунты` -> `OpenAI`: кнопки `Auth account`, `Status account`, `Cleanup auth` и `Delete account` открывают формы с именем account. `Delete account` требует подтверждение `delete`.

Agent profile хранит `openai_account_name` и `github_account_name`. Seed profile `reviewer` использует OpenAI account `primary` и GitHub account `primary`; seed profile `developer` использует OpenAI account `primary` и GitHub account `agent`. Agent Job монтирует только Secret выбранных accounts.

## GitHub account metadata

GitHub token создается владельцем с нужными scopes и хранится в Kubernetes Secret. Bot-service не принимает raw token через Mattermost account dialog; dialog управляет только metadata binding:

- account name;
- Kubernetes Secret name;
- optional GitHub username;
- optional git author email;
- status `configured` или `disabled`.

Кнопочный путь:

```text
/agents -> Аккаунты -> GitHub -> Добавить
/agents -> Аккаунты -> GitHub -> Изменить
/agents -> Аккаунты -> GitHub -> Удалить
```

Ожидаемый результат: `Добавить` и `Изменить` сохраняют account metadata в PostgreSQL, `Удалить` удаляет только metadata row после подтверждения `delete`. Kubernetes Secret не удаляется и значение token нигде не печатается.

## Health-only install

Этот режим можно раскатать без Mattermost bot token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Ожидаемый результат для настроенного production-контура: объекты Kubernetes существуют, Deployment готов, публичные slash endpoint и GitHub webhook возвращают устойчивый `401` без токена или подписи, а публичный `/healthz` возвращает `404`. Ответ `503` означает незавершённую конфигурацию и не считается успешным smoke. Кластерные probes продолжают проверять health/readiness через сервис Kubernetes.

Проверка storage migrations после deploy:

```bash
set -euo pipefail
. scripts/lib/env.sh
mattercodex_load_env_file .env
mattercodex_validate_base_env
NAMESPACE_Q="$(mattercodex_shell_quote "$MATTERCODEX_NAMESPACE")"
REMOTE_KUBECTL="$(mattercodex_remote_kubectl_command)"
mattercodex_ssh "$REMOTE_KUBECTL -n $NAMESPACE_Q exec statefulset/mattermost-postgres -- psql -U mattermost -d mattermost -Atc 'select version_id, is_applied from goose_db_version order by id;'"
```

Ожидаемый результат: в выводе есть примененная версия `8|t`.

## Agent prompt templates

Prompt template относится к профилю агента и хранится в PostgreSQL. Bot-service рендерит template перед созданием Job с текущей Mattermost locale и передает готовый Markdown prompt в agent pod через ConfigMap. Agent runner не содержит prompt-текстов в Go-коде. В prompt доступны placeholders locale contract (`.Locale.Code`, `.Locale.Language`) и имена GitHub env (`.GitHub.TokenEnv`, `.GitHub.UsernameEnv`, `.GitHub.EmailEnv`), но системные шаблоны намеренно не раскрывают account alias/login. Миграция `000032` удаляет такие исторические placeholders из сохраненных шаблонов; агент проверяет только фактическую возможность выполнить требуемую GitHub-операцию.

Базовые seed templates лежат в `services/external/bot-service/internal/domain/service/prompt_seeds/*.md`, а предыдущие штатные версии — в `prompt_seeds/history/v*/`. На старте bot-service после migrations запускает версионированный seeder: он создаёт отсутствующие templates, а при новой версии заменяет только profile/role bodies, которые побайтно равны одной из предыдущих Markdown-версий. Role copy обновляется независимо от текущего состояния общего profile template. Любое отредактированное в Mattermost значение сохраняется; роли с прямым `cluster-admin` не изменяются фоновым upgrade и требуют отдельного управляемого обновления. SQL migrations владеют только schema/profile metadata и сравнением значений, но не содержат Markdown prompt bodies. В коробке сидятся роли `director`, `manager`, `architect`, `developer`, `reviewer`, `docs`, `sre`, `qa-bot`, `ui-designer`, `improver`, `pm-delivery`, `analyst` и `mattercodex-admin`.

Agent-runner image содержит browser tooling для ролей `developer`, `ui-designer` и `qa-bot`: `chromium`, `playwright`, `@playwright/test`, `@playwright/mcp` и `wait-on`. Основной путь browser smoke/e2e в agent pod - Playwright CLI/API; системный `chromium` доступен для диагностики и версионных проверок. Browser artifacts следует сохранять в `/workspace/artifacts/screenshots` или `/workspace/artifacts/playwright`; `playwright-mcp` используется только если роль явно включает его в Codex `config.toml`.

Стартовый OSS-набор:

- `developer/developer_smoke`;
- `developer/implement_task`;
- `developer/fix_review`;
- `reviewer/review_pr`;
- `manager/coordinate_task`;
- `architect/architecture_task`;
- `docs/documentation_task`;
- `sre/operations_task`;
- `qa-bot/regression_task`;
- `improver/feedback_improvement`;
- `pm-delivery/delivery_status`;
- `analyst/analysis_task`;
- `mattercodex-admin/admin_task`.

Управление через Mattermost:

```text
/agents prompt help reviewer review_pr
/agents prompt list
/agents prompt show reviewer review_pr
/agents prompt render reviewer review_pr
/agents prompt set reviewer review_pr <markdown template>
```

Ожидаемый результат:

- `prompt help` показывает доступные placeholders и template-функции;
- `prompt set` ожидает Markdown с Go `text/template` placeholders, test-render'ит его на sample data и сохраняет только если render успешен;
- `prompt render` позволяет проверить сохраненный или переданный inline template без запуска agent Job; строка с языком владельца должна меняться при `/agents locale set en|ru`.

## Mattermost bot bootstrap

Если `MATTERCODEX_MATTERMOST_BOT_TOKEN` еще не создан, можно выполнить полный bootstrap без пароля администратора через `mmctl --local` внутри Mattermost pod:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```

Скрипт:

- включает `ServiceSettings.EnableUserAccessTokens` и `ServiceSettings.EnableBotAccountCreation`;
- Mattermost Deployment должен содержать `MM_SERVICESETTINGS_ALLOWEDUNTRUSTEDINTERNALCONNECTIONS` с host из `MATTERCODEX_BOT_SERVICE_INTERNAL_URL`, иначе Mattermost заблокирует slash callback во внутренний Kubernetes Service;
- создает service user `MATTERCODEX_MATTERMOST_BOT_USERNAME`;
- конвертирует service user в bot;
- генерирует `MATTERCODEX_MATTERMOST_BOT_TOKEN`;
- создает отдельный обычный `system_admin` аккаунт `MATTERCODEX_MATTERMOST_ADMIN_USERNAME` и генерирует `MATTERCODEX_MATTERMOST_ADMIN_TOKEN` для жизненного цикла role bot;
- создает team, дефолтные каналы и slash command `/agents`;
- сохраняет bot token и slash token в Kubernetes Secret;
- перезапускает bot-service Deployment.

Bot, admin и slash tokens сохраняются в одном Kubernetes Secret, не выводятся и используются раздельно. Публикации и listener работают от `MATTERCODEX_MATTERMOST_BOT_TOKEN`; административный PAT доступен только операциям создания, конвертации и выдачи токенов role bot.

## Mattermost provisioning через готовый token

Отдельный provisioning-скрипт через готовый Personal Access Token удалён вместе с предыдущей реализацией. На текущем Go-срезе поддержан один безопасный bootstrap path через `mmctl --local`:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```

Если token уже есть в Kubernetes Secret, повторный deploy bot-service использует существующий Secret и не печатает его значение.

## Ручная проверка владельцем

1. Открыть Mattermost.
2. Перейти в team `agents`.
3. Проверить каналы `agents-control`, `agents-runs`, `agent-alerts`, `agents-audit`.
4. В канале `agents-control` выполнить:

```text
/agents
```

Ожидаемый результат: channel-visible menu card с кнопками `Projects`, `Аккаунты`, `Репозитории`, `Roles`, `Chats`, `Advanced`, `Runtime`, `System`, `Help`. Нажатия по кнопкам должны обновлять эту же карточку на выбранный раздел и показывать короткий ephemeral-статус. Кнопка `Назад` возвращает главное меню. В главном меню поля OpenAI и GitHub показывают счетчик готовых accounts в формате `готово/всего`.

Проверка account menu:

- `Аккаунты` -> `OpenAI`: карточка должна показать кнопки `Список accounts`, `Auth account`, `Status account`, `Cleanup auth`, `Delete account`, `Назад`; кнопки auth/status/cleanup/delete открывают dialog с именем account и возвращают результат в dialog.
- `Аккаунты` -> `GitHub`: карточка должна показать кнопки `GitHub accounts`, `Добавить`, `Изменить`, `Удалить`, `Check matter-codex`, `Webhook matter-codex`, `Назад`; `Добавить/Изменить/Удалить` открывают формы metadata CRUD.

Typed-команды остаются fallback-интерфейсом для точной ручной проверки:

```text
/agents status
```

Ожидаемый результат: ephemeral ответ `matter-codex: online` без вывода секретов. В текущем deploy-контуре ответы по умолчанию русские, потому что `MATTERCODEX_LOCALE` задается как `ru`.

Дополнительная проверка storage/admin-команд:

```text
/agents token check
/agents locale get
/agents locale set en
/agents token check
/agents locale set ru
/agents profile list
/agents prompt help reviewer review_pr
/agents prompt render reviewer review_pr
/agents openai list
/agents repo add github codex-k8s/matter-codex main
/agents repo list
```

Ожидаемый результат: команды отвечают ephemeral-сообщениями, `locale set en` переключает ответы на английский, `locale set ru` возвращает русские ответы, profile list показывает OpenAI и GitHub account для профиля, prompt render показывает sample-render без сохранения секретов, repository появляется в списке, а Mattermost создаёт/показывает канал `repo-codex-k8s-matter-codex`.

Дополнительная проверка GitHub adapter:

```text
/agents token check
/agents github check codex-k8s/matter-codex
/agents github branch dry-run codex-k8s/matter-codex matter-codex-smoke main
/agents github pr dry-run codex-k8s/matter-codex main main Smoke PR dry run
/agents github pr status codex-k8s/matter-codex 4
/agents github webhook ensure codex-k8s/matter-codex
```

Ожидаемый результат:

- token check показывает `github token: configured`;
- repo check показывает default branch и безопасные permission-флаги;
- branch dry-run показывает base sha и `changes: none`;
- PR dry-run проверяет head/base refs и не создает PR;
- PR status показывает state, draft/merged, reviews/comments fetched.
- webhook ensure создает или обновляет repo webhook, если token имеет hook write permission; при нехватке прав команда возвращает безопасную ошибку без вывода token/secret.

При `/agents repo add github owner/name [default-branch]` bot-service также пытается выполнить webhook ensure автоматически и добавляет строку `webhook: ...` в ответ.

Кнопочная проверка CRUD репозиториев:

1. В Mattermost выполнить `/agents`.
2. Открыть `Репозитории`.
3. Нажать `Добавить репо`, заполнить `Провайдер=GitHub`, `Репозиторий=codex-k8s/matter-codex`, `Ветка=main`, отправить форму.
4. Проверить, что карточка меню обновилась результатом добавления или обновления репозитория.
5. Нажать `Изменить репо`, указать тот же репозиторий и ветку, отправить форму.
6. Нажать `Удалить репо`, указать тестовый репозиторий и ввести `delete` в поле подтверждения.

Удаление в этом сценарии удаляет только запись `matter_codex_repositories`. Канал Mattermost и GitHub webhook не удаляются.

Дополнительная проверка Kubernetes runner foundation:

```text
/agents token check
/agents runtime smoke smoke-manual
/agents runtime status smoke-manual
/agents runtime cleanup smoke-manual
/agents runtime prune 24h
```

Ожидаемый результат:

- token check показывает `kubernetes runtime: configured`;
- runtime smoke возвращает run id, Job и PVC без вывода секретов; Job использует `matter-codex-agent-runner`;
- runtime status показывает Job/PVC, pod phase и короткий log tail smoke Job;
- runtime cleanup удаляет Job и PVC.
- runtime prune по умолчанию работает в dry-run режиме и показывает старые завершенные Job/PVC/ConfigMap. Session PVC и Secret токена показываются отдельно в режиме `inventory-only` с диагностическими причинами и не удаляются даже при `--apply`.

Проверка runtime quota/limits после deploy:

```text
/agents runtime smoke quota-manual
/agents runtime status quota-manual
/agents runtime cleanup quota-manual
```

На кластере `kubectl -n <namespace> get resourcequota matter-codex-runtime-quota` и `kubectl -n <namespace> get limitrange matter-codex-runtime-container-defaults` должны показывать примененные объекты. Runtime smoke должен проходить с `smoke-ok`; это подтверждает, что defaults и quota не блокируют agent Job.

Проверка apply-режима retention cleanup на завершенном smoke run:

```text
/agents runtime smoke prune-manual
/agents runtime status prune-manual
/agents runtime prune 1s
/agents runtime prune 1s --apply
/agents runtime status prune-manual
```

Ожидаемый результат:

- первый `runtime prune 1s` показывает `mode: dry-run` и не удаляет ресурсы;
- `runtime prune 1s --apply` удаляет завершенный Job/PVC/ConfigMap только если run уже завершен;
- оба запуска показывают для session PVC/Secret режим `inventory-only`, нулевые счётчики удаления и причины `containment`; непроверенные PostgreSQL/S3 отражаются как `unknown_db` и `unknown_s3`, а не как утверждение о наличии архива;
- активные Job не удаляются и учитываются как skipped;
- после apply `runtime status prune-manual` возвращает, что run не найден.

Ручная приёмка сдерживания сессионных данных выполняется только в изолированной тестовой установке:

1. Создать старый синтетический session PVC и соответствующий синтетический token Secret без действующих учётных данных.
2. Запустить `/agents runtime prune <возраст>` и затем `/agents runtime prune <возраст> --apply`.
3. Повторить предварительный просмотр и убедиться, что PVC и Secret существуют в неизменном виде, их удаление равно нулю, а результат не утверждает наличие архива.
4. Воспроизвести исчерпание квоты на создание session PVC и убедиться, что вызывающий код различает неустранимый вытеснением вид `AgentSessionCapacityError`, `ChatRunService` выполняет одну попытку создания PVC без вытеснения простаивающего pod и `CleanupAgentSession`, а сохранённые PVC/Secret не получают `delete` или `patch`.
5. Проверить аудит: допустима запись инвентаризации, но отсутствует запись, утверждающая разрешённое или выполненное удаление session PVC/Secret.

Эта проверка не является разрешением на промышленный deploy или Kubernetes apply.

Дополнительная проверка Codex reviewer agent:

Перед проверкой нужен authorized OpenAI account из профиля `reviewer`, настроенный GitHub account `primary` и существующий открытый GitHub PR.

```text
/agents openai list
/agents review pr codex-k8s/matter-codex <pr-number> review-manual
/agents review status review-manual
```

Ожидаемый результат:

- review pr возвращает run id, PR number, Job и PVC;
- в ответе review pr указан OpenAI account `primary`;
- через некоторое время `review status` показывает pod phase и artifacts `pr-url`, `review-decision`, `review-submitted`;
- Codex reviewer получает `gh` с env `GH_TOKEN`/`GITHUB_TOKEN`, `GITHUB_USERNAME`/`GITHUB_USER`, `GITHUB_EMAIL` и должен сам публиковать inline review comments от reviewer account; если он не отправил review сам, runner отправляет fallback summary review;
- log tail не содержит значений OpenAI/GitHub/Mattermost секретов.

После проверки удалить Kubernetes resources:

```text
/agents review cleanup review-manual
```

Cleanup удаляет только Kubernetes Job/PVC, а не GitHub review/comment.

Проверка webhook reject без корректной подписи:

```bash
set -euo pipefail
. scripts/lib/env.sh
mattercodex_load_env_file .env
mattercodex_validate_base_env
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H 'Content-Type: application/json' \
  -H 'X-GitHub-Event: ping' \
  --data '{}' \
  "${MATTERCODEX_BOT_SERVICE_SITE_URL%/}/github/webhook"
```

Ожидаемый результат: `401`.

## Ручная приёмка callback delivery и title

1. В тестовом Mattermost запустите дочернюю работу с обычным кириллическим/латинским title у согласованной границы. В корневом сообщении, callback prompt и двух audit-сообщениях title должен оставаться обычным текстом; prompt обязан показывать его как однострочные недоверенные JSON-данные.
2. Через MCP по одному передайте title с Markdown-ссылкой, backticks/fence, `@channel`, angle/HTML, обычным URL, backslash, bidi/control/zero-width и попыткой новой секции. Каждый вызов должен вернуть ошибку до чтения session/token, DB, dispatcher и Mattermost; исходное значение не должно появиться в журнале.
3. На одноразовом PostgreSQL target выполните сценарные тесты callback: двойной сетевой отказ с исправленным повтором, первая доставка успешна/вторая ошибочна, зависшая первая попытка, DB mark failure после network success, конкурентные повторы, перезапуск сервиса, `MaxConns=1`, revoke/remap. Итог — ровно две подтверждённые публикации без повторного внешнего идентификатора; до исправления вызов возвращает ошибку.
4. Для операционной сверки выбирайте только безопасную проекцию состояния и не копируйте payload или bindings:

```sql
select destination, publication, status, attempt_count,
       lease_expires_at is not null and lease_expires_at <= now() as lease_expired,
       last_error_code, mattermost_post_id <> '' as has_post_binding,
       last_attempt_at, delivered_at, updated_at
from matter_codex_agent_delegation_callback_deliveries
where delegation_id = $1 and callback_run_id = $2
order by destination, publication;
```

Ожидаемые стабильные состояния: все строки `delivered` для успеха; `pending` после подтверждённого сетевого отказа; `pending/confirmation_ambiguous` после потери DB mark, если БД доступна; временный `in_flight` только при действующей lease или недоступности БД; `blocked/final_binding_denied` после revoke/remap. Ручное изменение строк запрещено.

## Безопасность

- `.env` не коммитится.
- MCP POST с `Content-Length` выше server-owned предела и chunked body, фактически превысивший предел, получает `413` до `go-sdk`, чтения session/token, DB, dispatcher, locks и публикации. Полный `ReadTimeout` ограничивает slow chunked body; допустимые POST на точной границе и GET/SSE сохраняются.
- Mattermost tokens не попадают в manifests render output.
- GitHub token, username, email и webhook secret хранятся в Kubernetes Secret и не попадают в ConfigMap.
- Slash token, полученный из Mattermost API, пишется во временный файл с правами `0600`, затем в Kubernetes Secret.
- Логи provisioning показывают только безопасные статусы `exists/created/updated`.
- Action/dialog callback принимает только одноразовую серверную capability с ограниченным сроком действия и точной привязкой к операции, ресурсу, каналу, фактическому `post_id`, субъекту и области. Capability удостоверяет callback, но не предоставляет право: отдельный допуск проверяет активного пользователя, членство в канале, точный закрытый набор операций и существующий серверный ресурс. В PostgreSQL хранится только SHA-256-хеш. Чистая исправимая проверка полей dialog выполняется до обращения к capability repository, допуска и подготовки `response_url`; ошибка не вызывает DB/Mattermost/Kubernetes/dial и не погашает capability. После успешной проверки выполняются удостоверение, допуск и SSRF-подготовка, и только затем — финальный атомарный переход в `consumed`. Повтор, истечение, подделка и подмена субъекта дают закрытый отказ.
- Capability для action-карточки создаются в состоянии `pending`, становятся `unused` только после подтверждённого обновления точного Mattermost post и переводятся в `revoked` при ошибке или несовпадении ответа. Поэтому даже сценарий «Mattermost применил обновление, затем вернул ошибку» не оставляет пригодных кнопок.
- Истёкшие строки capability в состояниях `pending`, `unused`, `consumed` и `revoked` удаляются идемпотентными ограниченными пакетами только при `expires_at < now - retention`. `pending` и любые другие строки внутри отсрочки, строка ровно на границе и действующая `unused` capability сохраняются. Очистка не затрагивает сессии, PVC, Secret, квоты или иные runtime-ресурсы; результат ненулевого прохода и ошибка видны в структурированном журнале без значений capability.
- Поля `user_name`, `prompt`, `labels`, `state` и `submission` не предоставляют права. Новое назначение `cluster-admin` запрещено. Миграция `000025` фиксирует точное состояние существующих профилей и ролей, влияющее на полномочия: состояние включения; bot identity; развёрнутые состояния учётных записей и credentials OpenAI/GitHub; SHA-256 содержимого, UID и resourceVersion Secret без raw values; привязки репозиториев, переменных и сессий; Kubernetes и `sandbox`/`config`; а также фактически потребляемый instruction-контекст profile template, project, chat и role (`name`, `description`, `advanced_settings`, `prompt_template`, `work_policy`, `settings`, `system_purpose`, root issue и тип). Изменение снимка и повторное включение после понижения прав, выключения, блокировки поставщиком или удаления закрыто отклоняются, а отзыв сохраняется монотонно. Миграция `000031` разрешает только атомарно создать новый immutable session binding для уже замороженной точной пары role/chat; новая роль, новый чат, изменённая зависимость и повторное использование отозванного `session_key` остаются запрещены. Перед каждым административным adapter callback final guard повторно сверяет БД и текущее безопасное описание Secret; изменение значения при прежнем ref/key и пересоздание Secret отклоняются до Job/Secret/account-status/publish side effects. Для frozen session token один проверенный `Get` одновременно возвращает bytes и точную integrity tuple; runner создаёт отдельный collision-safe immutable Secret с защитным finalizer, а Pod ссылается только на эту версию. На каждой границе повторно проверяются исходная tuple, immutable data, session label и finalizer. Retention снимает finalizer отдельным metadata-only patch с UID/resourceVersion fence и удаляет Secret с тем же UID и post-mutation resourceVersion, не передавая token в Pod spec, patch или журнал. Raw values не сохраняются в БД, аудит, prompt или журнал.
- Агент может межкомнатно запустить уже зафиксированную роль с `cluster-admin` через `mattermost_start_agent_thread`, если relationship policy разрешает связь и точные server-side role/chat/participant/dependency/Secret проверки проходят на каждой границе. Это не является способом создать новое назначение. Ошибка `cluster-admin assignment is not present in the server-side profile` при таком запуске означает несовпадение либо отзыв точного снимка, а не необходимость обхода через запуск роли в текущем чате.
- Если Job `codex-auth-check` не планируется из-за нехватки CPU, memory или pod capacity, runner возвращает типизированную ошибку ёмкости до общего таймаута. Bot-service под общей блокировкой удаляет только самый старый проверенный idle agent pod, сохраняет его PVC, архив и сессию, затем один раз повторяет auth-check. При отсутствии безопасного кандидата запуск закрыто останавливается без начала reauth.
- После применения `000025`–`000032` exact N-1 runtime login нормально читает profiles/roles и выполняет обычный repository bootstrap/DML при отключённом callback route. Самодекларируемый GUC больше не участвует в допуске. Runtime login не может создавать `pg_temp`, DDL, удалять callback outbox/manifest, менять immutable delivery plan или отключать триггеры; функции имеют фиксированный доверенный `search_path` с `pg_temp` последним, а триггеры закрыто блокируют прямое расширение frozen state, повторное использование или перепривязку frozen `session_key`, обход scoped delivery fence, неполный callback manifest и обратный переход из `delivered`. Единственный bootstrap новой административной сессии выполняется узкой `SECURITY DEFINER` функцией после точной проверки уже замороженной role/chat binding и в одной транзакции с immutable session binding. Владелец схемы, superuser и `BYPASSRLS` явно находятся вне защищаемой runtime-стороны и не используются приложением.
- Перед созданием уникального индекса `000026` берёт блокировку таблицы от конкурентных записей и выполняет обезличенную предварительную проверку. При унаследованных дубликатах миграция возвращает только код `MCV26_DUPLICATE_SESSION_KEY_GROUPS` и число групп, полностью откатывается и оставляет версию `goose` `25`. Для исправления остановите записи и работайте локально через временную таблицу соответствий с внутренним суррогатным идентификатором строки: определите число групп и число лишних строк агрегатами, выберите каноническую строку внутри каждой группы через `row_number()`, перенесите внешние ссылки по внутренним идентификаторам и удалите лишние строки. Не выбирайте и не перенаправляйте в терминал, журнал или тикет значения `session_key`, `role_id` и иные идентификаторы. После исправления повторите только агрегатную предварительную проверку и `goose up`.
- Dialog capability и DB-зависимая повторная валидация вместе с business mutation выполняются в одной PostgreSQL-транзакции. Исправимая ошибка и проигранная гонка откатывают consume, поэтому исправленная отправка с тем же state проходит ровно один раз; до неё нет Mattermost, Kubernetes, dial или прикладных DB side effects.
- `000025`, `000026`, `000027`, `000028`, `000029`, `000031` и `000032` являются forward-only: каждый `goose down` закрыто возвращает ошибку, версия остаётся последней применённой, а frozen state, inventory, scoped fence, callback outbox, manifest, triggers, bootstrap function и очистка identity placeholders физически сохраняются; повторный `up` после неуспешного `down` является успешным no-op.
- Для callback `ReturnToRequester` точный prompt и две точные audit-публикации с UTF-8 byte chunks строятся и проверяются до enqueue. Для ordinary и `cluster-admin` ролей exact source/child reread и guards, подготовка turn/process/run, запись `CallbackRunID`, ровно две строки неизменяемого delivery plan миграции `000028` и canonical count/hash manifest миграции `000029` выполняются одной repository transaction; вложенные frozen guards используют её savepoints. Deferred constraint требует ровно одной строки каждой обязательной destination и откатывает отсутствующие, лишние, повторные или несовпадающие строки. Старый malformed plan без manifest не маскируется и не дополняется. Ошибка после durable transition возвращается вызывающему, а не отбрасывается. Повтор с существующим `CallbackRunID` не создаёт новый callback и работает только с незавершёнными строками.
- Delivery claim использует `FOR UPDATE SKIP LOCKED`, ограниченную lease и последовательность chunks внутри destination. Каждая фактическая попытка выполняется с отдельным server-owned deadline внутри точного source+child final guard с каноническим порядком `session → role → chat → participant → dependency → delivery fence`; binding и revocation проверяются непосредственно перед network boundary. Committed revoke/remap оставляет строку `blocked` с `last_error_code=final_binding_denied`; состояние не считается успешным. Глобальная блокировка и отдельное соединение в обход guarded repository запрещены.
- `pending_post_id` не считается долговечной гарантией Mattermost. Адаптер до отправки и после неоднозначной ошибки ищет детерминированную callback identity в точном треде, затем полностью сверяет channel/root/message и exact client-owned `matter_codex_*` props. Из server-owned props Mattermost 11.6 разрешён только `from_bot` точного строкового значения `"true"`; отсутствие, иной тип/значение и любое неожиданное server/client поле закрыто возвращают ошибку. Если сеть была успешна, а DB mark завершился ошибкой, worker по возможности освобождает строку в `pending/confirmation_ambiguous`; при общей недоступности БД она остаётся `in_flight` до истечения lease. Следующий повтор выполняет сверку и не создаёт второй post. Успех возвращается только когда manifest остаётся точным и все обязательные строки имеют `delivered`; непустое подмножество не считается полной доставкой.
- `delegation.Title` ограничен 512 UTF-8 bytes и 200 runes, нормализуется в NFC и обрабатывается как непрозрачное недоверенное значение. Markdown/link/mention/HTML/code-разделители, backslash, CR/LF, управляющие, bidi/format, zero-width и autolink-like значения отклоняются до доменных чтений. Все четыре Mattermost/prompt sink повторно применяют тот же renderer; в межагентском prompt title находится в однострочном JSON с явной меткой данных. Исходное небезопасное legacy-значение не журналируется.
- Строки callback delivery не очищаются по времени в MVP и сохраняются вместе с delegation; внешний ключ запрещает физическое удаление родителя, пока существует любая строка плана. Для диагностики разрешены только `destination`, `publication`, `status`, `attempt_count`, признак истечения lease, `last_error_code`, признак наличия post binding и timestamps. Не выбирайте `message`, `props`, `payload_sha256`, channel/root, внешний идентификатор или исходный title в терминал и тикеты. Для безопасного повтора повторно вызовите `mattermost_return_to_requester`; не меняйте status/lease/post binding вручную.
- Откат приложения выполняется без `goose down` только на заранее проверенный exact N-1 SHA/дайджест с reader/runtime, совместимыми со схемой `000025`–`000029`, `000031` и `000032`. Физически удалять fence/outbox/manifest/triggers/functions, восстанавливать identity placeholders или откатывать меры PR #74/#75 запрещено. Exact N-1 не обрабатывает новый outbox, поэтому на время отката callback route выключается; незавершённые строки, manifests, fence и revocations сохраняются для исправления вперёд.
- `response_url` разрешён только для настроенного источника Mattermost. Проверяются протокол, hostname, port, DNS-адреса и каждое перенаправление; IP-литерал, loopback, link-local, metadata, произвольный приватный или кластерный адрес назначения и DNS rebinding не приводят к исходящему HTTP-запросу.
- bot-service Deployment запускается non-root, с dropped Linux capabilities, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true` и `seccompProfile: RuntimeDefault`.
- Ресурсы bot-service настраиваются через `MATTERCODEX_BOT_SERVICE_CPU_REQUEST`, `MATTERCODEX_BOT_SERVICE_MEMORY_REQUEST` и `MATTERCODEX_BOT_SERVICE_MEMORY_LIMIT`. Значения по умолчанию: requests `100m`/`512Mi`, memory limit `8Gi`, CPU limit отсутствует; увеличенный memory limit нужен для кратковременных пиков при приеме крупных Codex session snapshots.
- bot-service получает namespace-scoped Role на создание/чтение/удаление runtime Job/PVC, чтение pod/log, `pods/exec` для чтения готового `auth.json` из auth Job и `create/get/list/update/patch/delete` для Secret. Сдерживание session retention использует для token Secret только инвентаризацию и не вызывает `patch` или `delete`; наличие namespace-wide разрешения не является разрешением доменного действия и не создаёт включающего пути. Wildcard API groups, resources и verbs не выдаются.
- Runtime namespace получает namespace-level ResourceQuota/LimitRange с owner-instance defaults и env overrides, потому что MVP namespace общий для Mattermost, bot-service и agent Job. CPU/memory requests сохраняют scheduler accounting; aggregate `limits.memory` quota отсутствует, CPU limits не задаются, но каждый agent/utility container имеет высокий индивидуальный memory ceiling и ограниченный `/dev/shm`. Оператор обязан контролировать node pressure и число одновременно работающих и прогретых pod.
- ServiceAccount agent runner создается без automount token; smoke pod также явно отключает automount.
- Codex smoke/auth/developer/reviewer Job запускаются без automount service account token и с non-root securityContext.
- Codex developer/reviewer Job получает Codex `auth.json` выбранного OpenAI account и GitHub token/username/email выбранного GitHub account только через Kubernetes Secret volume mount.
- Developer/reviewer prompt templates хранятся в PostgreSQL, редактируются через Mattermost и передаются agent pod как отрендеренный Markdown через ConfigMap.
- `CODEX_HOME/config.toml` задает `shell_environment_policy` с минимальным environment для команд, которые запускает Codex: `gh` получает только нужные GitHub env, без Mattermost/OpenAI/Kubernetes secret values.
- Codex agent внутри isolated Kubernetes Job запускается с `sandbox_mode = "danger-full-access"`, потому что `workspace-write` требует `bubblewrap`, который в текущем Kubernetes pod падает до выполнения shell-команд. Изоляционная граница MVP для agent run: отдельный pod, отдельный PVC, отключенный automount service account token и минимальные Secret volume mounts.
- Developer runner реализован отдельным Go binary в подготовленном image и сам выполняет push/PR после `codex exec`; prompt contract запрещает Codex агенту пушить branch или создавать PR напрямую, но разрешает отвечать на review threads через `gh` при соответствующей задаче.
- Reviewer runner реализован отдельным Go binary в подготовленном image и дает Codex reviewer доступ к `gh` для inline review comments; если Codex не отправил review сам, runner отправляет fallback summary review после `codex exec`.
- В текущем наборе манифестов `NetworkPolicy` отсутствует. PR-0 не расширяет сеть и фиксирует это состояние снимком; изоляция исходящего трафика остаётся явным риском начального профиля до отдельного инфраструктурного изменения.

## Сессии, role bots и `runs`

- OpenAI account записывается в agent session при ее создании. Изменение account у роли не переносит существующую Codex session: для нового account требуется новый корневой Mattermost thread.
- Статус `blocked` означает подтвержденную cyber-safety блокировку поставщика. Runner не выполняет автоматический обход; пользователь получает ссылку на Trusted Access и продолжает измененную задачу в новом thread.
- Для каждого проекта bootstrap создает публичный канал `runs`. В нем служебный MatterCodex account создает по одной обновляемой карточке на turn; карточки имеют ссылки на trigger, рабочий thread и все parent run cards.
- Role identity всегда должна быть Mattermost bot account. При старте bot-service существующая обычная учетная запись роли конвертируется в bot; fallback с созданием обычного пользователя отсутствует.
- Mattermost не поддерживает read-only состояние отдельного thread. Для контролируемого закрытия истории MatterCodex ставит `thread_context.status = closed`: UI Mattermost по-прежнему позволяет отправить текст, но bot-service не создает turn и просит начать новый корневой thread. Архивация всего канала для этой задачи не применяется.

## Production gaps после MVP

Актуальные production gaps и последовательность их закрытия ведутся в `docs/roadmap/epics-and-waves.md` и `docs/operations/**`.
