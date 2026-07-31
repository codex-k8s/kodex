# Control-plane

`control-plane` — авторитетный внутренний сервис конфигурации и управляющего
состояния MatterCodex. Он реализует Issue
[#187](https://github.com/codex-k8s/matter-codex/issues/187) как один
развёртываемый unit.

Сервис владеет:

- проектами, командами, чатами, ролями и профилями prompt;
- метаданными credential bindings, repositories/workspaces и integrations;
- неизменяемыми runtime revisions;
- sessions, turns и process lineage;
- schedules, owner gates, memory records и work claims;
- метаданными artifacts, но не их байтами.

Secret values остаются во внешнем Vault/Kubernetes secret storage.
`control-plane` не вызывает Mattermost, MCP, Codex и Kubernetes API, не
reconcile-ит runtime и не реализует внешний HTTP API.

## Сквозные границы

```text
control-api-gateway
  -> exact mTLS + OIDC first call
  -> control-plane AuthorityProofResolver
  -> server-side project/permission resolution in PostgreSQL
  -> short-lived authority proof
  -> workload-local #186 issuer/verifier path
  -> ControlPlaneService full method
  -> caster -> domain service -> repository port
  -> PostgreSQL transaction
       aggregate + idempotency receipt + audit + optional outbox fact
  -> Redis read-through cache by PostgreSQL-owned epoch
  -> outbox relay -> exact NATS JetStream stream/subject
```

Actor, organization, project, permission, workload и SPIFFE identity не
принимаются в business request. Они выводятся из проверенного контекста
Issue #186. Для OIDC first call сервис дополнительно проверяет exact mTLS
caller, issuer, единственную audience, `iat`/`nbf`/`exp`, максимальный TTL,
session revision и JTI. Project authority разрешается внутри PostgreSQL
tenant boundary до подписи proof.

## Контракты и consumers

- Proto: `contracts/proto/controlplane/v1/control_plane.proto`;
- generated Go: `internal/generated/controlplane/v1`;
- AsyncAPI: `contracts/asyncapi/control-plane/v1/asyncapi.yaml`;
- authority policy: `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`.

Внешний mapping принадлежит будущему `control-api-gateway`; этот unit
публикует только внутренний gRPC. Runtime operations `ClaimTurn`,
`CompleteTurn` и `ClaimDueSchedules` присутствуют в versioned contract, но
deny-by-default policy не открывает их до появления зарегистрированных
`agent-runner`/`automation-scheduler` proof producers. Ручной обход отсутствует.

Публикуются только два факта с утверждёнными consumers:

| Факт | Условие | Consumer | Delivery |
| --- | --- | --- | --- |
| `control_plane.runtime_configuration_changed` | durable изменение project/team/chat/role/prompt/binding/workspace/integration/runtime/session/turn | `runtime-controller` | at-least-once, consumer inbox/cursor |
| `control_plane.schedule_changed` | durable изменение schedule/high-watermark | `automation-scheduler` | at-least-once, consumer inbox/cursor |

Для process runs, owner gates, memory, work claims и artifact metadata
спекулятивные события не публикуются: авторитетный путь — `GetResource` и
`ListResources`. Delete/cancel/terminal/retry каждого агрегата видны через
тот же versioned read path. Outbox фиксируется в транзакции команды; relay не
публикует из transport/domain кода и удаляет запись только после exact
JetStream acknowledgement. Потерянный acknowledgement безопасно повторяет
тот же `event_id`.

## Доменные инварианты

| Область | Инвариант |
| --- | --- |
| Все commands | semantic idempotency key + canonical request digest, OCC и audit фиксируются атомарно |
| Project | ID и owner назначает сервер; tenant create требует owner claim; slug стабилен |
| Team/Role | stable key неизменяем; роли и prompt profiles разрешаются в том же project |
| Credential binding | хранится только URI metadata; purpose/principal неизменяемы; revision растёт ровно на один |
| Integration | definition identity неизменяема; version движется только вперёд |
| Runtime revision | manifest/image/prompt/bindings неизменяемы после создания |
| Session | agent/provider и server-owned turn sequence не переписываются |
| Turn | runtime revision обязателен; FIFO sequence резервируется вместе с session update; lease fenced и bounded |
| Turn recovery | expired claimed lease под блокировкой возвращается в queue; retry увеличивает attempt; stale lease отклоняется |
| Process run | parent/root/playbook/policy образуют неизменяемую lineage; result можно установить один раз до terminal transition |
| Schedule | cron xor interval, timezone, misfire и overlap — закрытые значения; occurrence уникален по schedule/time; high-watermark сдвигается атомарно |
| Owner gate | result digest и expiry pin-ятся при создании; решение и terminal state фиксируются одной командой |
| Memory | scope/provenance неизменяемы; `content_sha256` обязан совпадать с content |
| Work claim | process/turn binding неизменяем; активный exact process/turn claim уникален |
| Artifact metadata | storage reference/digest/retention metadata неизменяемы; create принимает только `PENDING`, binary scan не принадлежит сервису |

Reference resolution выполняется внутри текущих organization/project RLS
settings; cross-tenant и hidden resource дают одинаковый `NotFound`.

## Данные и cache

PostgreSQL — единственный источник истины. Миграция создаёт schema
`control_plane`, отдельного `NOLOGIN/NOSUPERUSER/NOBYPASSRLS` owner, runtime
и relay group roles, `FORCE RLS`, constraints и точные grants. Login
principals создаёт и ротирует environment-owned Vault database engine; они
должны иметь только соответствующее group membership. Readiness проверяет
schema version, membership, `NOSUPERUSER` и `NOBYPASSRLS`.

SQL хранится по одному именованному запросу в
`internal/repository/postgres/controlplane/sql`. Command transaction использует
`SERIALIZABLE`; query path — `READ ONLY` transaction с transaction-local RLS
scope.

Redis хранит только bounded resource snapshots:

- key:
  `control-plane:v1:resource:<organization>:<project|tenant>:<epoch>:<id>`;
- TTL не более минуты, value не более 128 KiB;
- authoritative cache epoch увеличивается в той же PostgreSQL transaction;
- cache miss, corruption или Redis error отступает к PostgreSQL;
- ownership, permissions, idempotency, leases и high-watermarks в Redis не
  хранятся.

## Startup, readiness и shutdown

До bind gRPC listener сервис синхронно проверяет:

1. runtime и relay PostgreSQL roles/schema;
2. Redis TLS path;
3. exact JetStream stream (`CONTROL_PLANE`, subjects, replicas, file storage,
   maximum message size);
4. independently delivered proof private key/trust и policy revision;
5. тот же локальный verifier #186, который обслуживает рабочие RPC.

После barrier запускаются relay и периодический readiness reconcile.
Неожиданное завершение любого worker закрыто завершает процесс; orchestrator
не получает внешне живую реплику без relay/readiness loop. При остановке
readiness сначала закрывается, workers отменяются и join-ятся до закрытия
PostgreSQL/Redis/NATS; gRPC и HTTP получают bounded shutdown. Tracing shutdown
и Sentry flush используют независимые бюджеты.

Метрики не содержат tenant/resource ID и используют закрытые labels.
Dashboard — `mattercodex-control-plane`. Alerts ведут в абсолютный HTTPS
runbook URL.

## Конфигурация

Значения ниже — имена, не secret values.

| Переменная | Назначение |
| --- | --- |
| `CONTROL_PLANE_GRPC_LISTEN`, `CONTROL_PLANE_TECHNICAL_LISTEN` | внутренние listeners |
| `CONTROL_PLANE_TLS_CERTIFICATE_FILE`, `CONTROL_PLANE_TLS_PRIVATE_KEY_FILE`, `CONTROL_PLANE_TLS_CLIENT_CA_FILE` | exact workload mTLS |
| `CONTROL_PLANE_POSTGRES_DSN_FILE`, `CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE` | runtime/relay DSN files |
| `CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME`, `CONTROL_PLANE_POSTGRES_CA_FILE`, `CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS` | PostgreSQL TLS/pool |
| `CONTROL_PLANE_REDIS_ADDRESS`, `CONTROL_PLANE_REDIS_TLS_SERVER_NAME`, `CONTROL_PLANE_REDIS_CA_FILE`, `CONTROL_PLANE_REDIS_USERNAME`, `CONTROL_PLANE_REDIS_PASSWORD_FILE`, `CONTROL_PLANE_REDIS_DATABASE`, `CONTROL_PLANE_REDIS_POOL_SIZE` | bounded Redis cache |
| `CONTROL_PLANE_NATS_URL`, `CONTROL_PLANE_NATS_TLS_SERVER_NAME`, `CONTROL_PLANE_NATS_CA_FILE`, `CONTROL_PLANE_NATS_CREDENTIALS_FILE`, `CONTROL_PLANE_NATS_STREAM`, `CONTROL_PLANE_NATS_REPLICAS` | exact JetStream publisher |
| `CONTROL_PLANE_AUTHORITY_POLICY_FILE` | versioned deny-by-default policy |
| `CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE`, `CONTROL_PLANE_PROOF_TRUST_FILE`, `CONTROL_PLANE_PROOF_SIGNER_GENERATION` | independently checked proof signer |
| `CONTROL_PLANE_LEASE_SIGNING_KEY_FILE` | turn lease HMAC key |
| `CONTROL_PLANE_OIDC_TLS_SERVER_NAME`, `CONTROL_PLANE_OIDC_CA_FILE` | pinned OIDC discovery/JWKS TLS |
| `POD_UID` | relay lease owner |
| `CONTROL_PLANE_*_TIMEOUT`, `CONTROL_PLANE_*_INTERVAL`, `CONTROL_PLANE_CACHE_TTL`, `CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT` | bounded lifecycle limits |
| `OTEL_*`, `SENTRY_DSN_FILE`, `SENTRY_EXPECTED_HOST` | shared observability runtime |

Secret files должны быть absolute regular files без разрешений для `other`.
DSN/JWK/credentials/keys и payload credentials не логируются.

## Deploy и миграции

Base находится в `deploy/k8s/base/control-plane`, environment overlays — в
`deploy/k8s/overlays/{staging,production}/control-plane`. Canonical render
требует два реальных image digest и закрыто отказывает при placeholder:

```bash
tools/render-control-plane.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  > /tmp/control-plane-staging.yaml
```

Команда только рендерит; она не применяет manifest. Для production заменить
`staging` на `production` и использовать отдельно утверждённые digests.

Migration Job запускает `control-plane-cli migrate expand` до rollout. Down
не является production rollback. После опубликованной forward migration
rollback выполняется совместимым предыдущим образом либо новой компенсирующей
forward migration по отдельному Issue.

JetStream stream и Vault database/static credentials являются
environment-owned зависимостями. Их exact contract проверяется startup
barrier; сервис не создаёт и не ослабляет broker/Vault ресурсы.
RBAC Role/RoleBinding намеренно отсутствуют: application и migration
containers не обращаются к Kubernetes API; CSI delivery выполняет
environment-owned driver.

## Ручная приёмка

Без deploy можно:

1. собрать оба binary;
2. выполнить `buf build` и проверить воспроизводимый codegen;
3. проверить YAML/JSON parse и canonical render с двумя тестовыми ненулевыми
   digests;
4. убедиться, что render содержит non-root/read-only workload, migration Job,
   deny-all и только exact-destination NetworkPolicy;
5. проверить, что `Closes #187` относится только к одному draft PR.

Фактические PostgreSQL/Redis/NATS/Vault/Kubernetes проверки и staging rollout
требуют отдельного разрешения и окружения.

## Prototype policy и ограничения

Активен профиль `Prototype`: comprehensive coverage, integration/E2E,
contract/deploy/render/lifecycle/oracle suites и полный baseline не входят в
этот PR. Поддерживаемая волна тестирования отслеживается в
[Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

Не входят в unit: внешний OpenAPI/HTTP gateway, runtime reconciliation,
automation execution, Mattermost/MCP/Codex process, binary artifact storage и
secret values.

Эксплуатация и восстановление описаны в
[`docs/runbooks/control-plane.md`](../../../docs/runbooks/control-plane.md).

## Проверенные внешние источники

Context7 был вызван для PostgreSQL, pgx, goose, gRPC/Protobuf, Redis, NATS,
OpenTelemetry, Sentry, Kubernetes и Vault, но вернул quota error. Использован
fallback на официальные primary docs:

- [PostgreSQL row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)
  и [transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html);
- [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5) и
  [goose](https://github.com/pressly/goose);
- [gRPC Go](https://grpc.io/docs/languages/go/) и
  [Protocol Buffers](https://protobuf.dev/);
- [Redis Go client](https://redis.io/docs/latest/develop/clients/go/) и
  [NATS JetStream](https://docs.nats.io/nats-concepts/jetstream);
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/),
  [Sentry Go](https://docs.sentry.io/platforms/go/),
  [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
  [Kustomize](https://kubectl.docs.kubernetes.io/references/kustomize/) и
  [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/).
