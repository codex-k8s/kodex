---
id: RUN-MC-007
title: Диагностика и восстановление control-plane
type: runbook
status: approved
owner: sre
version: 1.5.0
updated: 2026-07-31
---

# Диагностика и восстановление control-plane

## Назначение и запреты

Runbook применяется при отказе startup/readiness, миграции, authority proof,
cache, turn lease или outbox relay. Он не разрешает deploy, production change,
ручное изменение доменных таблиц, сброс RLS/high-watermark или вывод secret
values.

Не печатать DSN, Redis password, NATS credentials, OIDC/JWS/JWK payload,
lease-signing key, TLS private key, Sentry DSN и содержимое Secret.

## Локальный реестр и сборщик

`deploy/k8s/base/image-supply-chain` создаёт TLS-only OCI registry, два
rootless BuildKit worker, owner-triggered build Job и retention CronJob. Pull,
push, admin и promotion работают в разных Pod/ServiceAccount/Vault roles;
pull видит только promoted PVC read-only, а staging PVC доступен только
push/admin. Итоговый render направляет
`control-plane` и `internal-rpc-authority` в отдельный node-reachable pull FQDN
и использует только digest. Internal push и admin DELETE доступны на разных
Service и имеют независимые Vault identities; pull endpoint физически read-only.

При отказе:

1. проверить готовность четырёх registry Deployment и наличие у каждого
   только его Vault CSI certificate/auth objects по именам;
2. проверить, что DaemonSet `mattercodex-registry-node-pull-readback` готов на
   каждом schedulable node, его image использует exact pull FQDN и digest;
3. проверить BuildKit `debug workers` probe с exact SNI/CA и отдельным probe
   client certificate; build Job обязан монтировать отдельный
   `mattercodex-role-image-builder-tls`, label сам по себе полномочий не даёт;
4. проверить retention job: он оставляет три лексикографически последних
   immutable tag вида `vYYYYMMDDHHMMSS-<git-sha>` и удаляет четвёртый и старше;
5. при неизвестном tag job должен завершиться ошибкой без удаления; pull/push
   credentials не должны проходить admin DELETE readback.

Не переключать BuildKit на insecure registry и не возвращать Kaniko в новый
контур. Bootstrap images закреплены digest; их зеркалирование в локальный
registry выполняется отдельной операционной поставкой до запрета внешнего pull.

Code-first bootstrap/readback после отдельного owner approval:

1. выполнить `tools/configure-image-supply-chain-pki.sh staging` тем же
   репозиторным кодом: server roles обязаны иметь только `ServerAuth`,
   BuildKit probe/builder — только `ClientAuth`, exact allowed names и bounded
   TTL; каждый CSI `pki*/issue/*` использует `method: PUT` и exact
   `common_name`/`alt_names`/`ttl`, CA читается через `pki/cert/ca`;
2. materialize утверждённый FQDN в canonical render; до apply сверить, что
   Vault public certificate SAN, `dockerconfigjson.auths` и ExternalDNS hostname
   содержат ровно этот FQDN, а internal certificate SAN содержит push/admin
   Service DNS;
3. дождаться registry pod и LoadBalancer address, затем сверить DNS→address и
   TLS chain/SNI без `insecure`/добавления CA на узлы;
4. Job из `tools/render-image-build-job.sh` использует client-only BuildKit
   mTLS и scoped staging-push, но не содержит promotion identity и не имеет
   egress к promotion endpoint; server/probe Pod не содержит client key;
5. отдельный Job из `tools/render-image-admission-job.sh` проверяет exact
   BuildKit provenance/source, формирует SBOM, применяет зафиксированную
   vulnerability policy, проверяет signature identity и выпускает bounded
   подписанный admission receipt/claim; только затем promotion container
   копирует exact digest и читает обратно image и receipt digest;
6. retention job использует только admin identity. Отрицательный readback
   обязан показать, что pull не может push/delete, push не может delete, а
   неавторизованный BuildKit client не проходит TLS handshake.

Rollback переключает только digest на ранее прочитанный через pull endpoint;
DNS, CA, auth scope и registry storage не откатываются. Если pull readback или
SAN расходится, workload остаётся неготовой, FQDN не подменяется Service DNS и
ручной trust/insecure fallback запрещён.

## Read-only preflight

1. Зафиксировать Git SHA, три независимых image digest и утверждённый pull FQDN.
2. Получить canonical render без apply:

```bash
tools/render-control-plane.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  sha256:<agent-runtime-image-digest> \
  registry-pull.<environment-domain> \
  > /tmp/control-plane-staging.yaml
```

3. Получить отдельный supply-chain render без apply:

```bash
tools/render-image-supply-chain.sh \
  staging \
  sha256:<control-plane-image-digest> \
  registry-pull.<environment-domain> \
  > /tmp/image-supply-chain-staging.yaml

tools/render-image-build-job.sh \
  staging \
  v<UTC-YYYYMMDDHHMMSS>-<exact-git-sha> \
  <read-only-source-pvc> \
  sha256:<context.tar-digest> \
  agent-runtime \
  > /tmp/agent-runtime-build.yaml

tools/render-image-admission-job.sh \
  staging \
  v<UTC-YYYYMMDDHHMMSS>-<exact-git-sha> \
  sha256:<context.tar-digest> \
  agent-runtime \
  sha256:<staging-image-digest> \
  <approved-admission-tools-image>@sha256:<digest> \
  <approved-vulnerability-policy-revision> \
  > /tmp/agent-runtime-admission.yaml
```

4. Сверить имена ServiceAccount, SecretProviderClass, ConfigMap, probes,
   selectors и exact destinations NetworkPolicy.
5. После отдельного разрешения на доступ к среде читать только metadata:

```bash
kubectl -n mattercodex-system get deploy,job,pod,svc,pdb,networkpolicy \
  -l app.kubernetes.io/name=control-plane
```

## Startup не проходит

Startup barrier обязан завершиться до bind gRPC listener. Проверять по
порядку:

- runtime и relay DSN доставлены отдельными файлами;
- PostgreSQL TLS использует exact SNI/CA, login principal имеет ровно нужное
  group membership, остаётся `NOSUPERUSER/NOBYPASSRLS`;
- migration schema version равна `20260731000600`;
- Redis использует TLS, exact SNI/CA и bounded database/pool;
- stream `CONTROL_PLANE` существует с точными двумя subjects, file storage,
  replicas окружения, `LimitsPolicy`, `DiscardOld`,
  `MaxMsgs=10000000`, `MaxBytes=34359738368`,
  `MaxMsgsPerSubject=5000000`, maximum message size 262144 bytes,
  max age 30 дней, dedup window 2 минуты, deny delete/purge и без
  mirror/source/republish/rollup/transform;
- policy revision, independently delivered proof trust/private key и локальный
  verifier #186 согласованы;
- OIDC discovery/JWKS доступны только по pinned HTTPS path.

Не обходить отказ readiness отключением dependency или permissive fallback.
Неожиданное завершение relay/readiness worker завершает процесс: повторяющиеся
restart следует расследовать по ограниченному error class, а не маскировать
ослаблением probes.

## PostgreSQL и миграция

Migration Job использует отдельный `control-plane-migrator` ServiceAccount и
Vault DSN. Production down отсутствует. При ошибке:

1. сохранить код SQLSTATE без query parameters;
2. проверить schema owner/runtime/relay role metadata;
3. проверить `FORCE RLS`, grants и version;
4. исправить миграцию новой forward-only migration.

Не запускать `SET SESSION AUTHORIZATION`, не выдавать runtime superuser,
`BYPASSRLS`, schema ownership или членство relay.

Runtime DSN обязан принадлежать exact `CURRENT`, `NEXT` или bounded
`PREVIOUS` LOGIN principal. Monotonic generation high-watermark и digest
GitOps intent переживают pod replacement и запрещают resurrection
`RETIRED` generation. Для каждого объявленного `CURRENT`/`NEXT`/`PREVIOUS`
смонтировать отдельный
`CONTROL_PLANE_POSTGRES_RUNTIME_{CURRENT,NEXT,PREVIOUS}_DSN_FILE`. CLI через
owner-only SECURITY DEFINER bootstrap создаёт только точное имя
`control_plane_runtime_g<generation>`, выдаёт controller ADMIN OPTION и лишь
затем согласует intent; runtime не получает `CREATEROLE`. Для promotion
добавить exact `CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_*`: CLI подключится через
сам `NEXT` principal и сохранит durable readback.
Повторный idempotent запуск выполняет promotion. Откат ConfigMap или Vault
credential не уменьшает high-watermark. На каждом transaction проверить server-side
`session_user`, generation/status/lifetime и одноразовый подписанный context,
связанный с backend PID и transaction ID. GUC не является диагностическим
способом установки tenant. При promotion прежний principal становится
`PREVIOUS`, затем `RETIRED`; reconciliation завершает его открытые backends.

DDL-владелец `control_plane_owner` остаётся `NOLOGIN/NOCREATEROLE`. Только
`control_plane_role_controller` имеет `CREATEROLE`, `pg_signal_backend`,
ADMIN OPTION на точные зарегистрированные LOGIN и табличные права lifecycle;
runtime role не получает эти полномочия. Миграционный bootstrap нового
поколения обязан выдать controller ADMIN OPTION до включения generation в
intent, иначе reconcile закрыто отклоняется. Благодаря этому `NOLOGIN`, revoke
и termination выполняются внутри одного forward-only reconcile без
permission rollback; readback сверяет catalog membership/status и открытые
retired sessions. Не выдавать controller владение схемой, runtime DSN или
общий доступ приложению.

## Authority proof или OIDC

- caller обязан иметь exact gateway SPIFFE identity;
- OIDC token обязан иметь один issuer/audience, bounded `iat/nbf/exp`, UUID
  subject/org/project/JTI и ненулевую session revision;
- tenant/project/permission выводятся server-side;
- proof key должен совпадать с exact `CURRENT` generation в independently
  delivered trust;
- mutation policy/key files оставляет pod not ready до controlled restart;
- same idempotency key с другим session/digest отклоняется.

Не копировать bearer/JWS/JWK в Issue или лог.

## Turn или process stuck

Lease хранится в PostgreSQL с workload ID, authority generation, immutable
attempt, expiry и version fence. Следующий
`ClaimTurn` под одной serializable transaction:

1. блокирует просроченные claimed turns;
2. удаляет только совпавшую stale lease;
3. завершает прежнюю attempt как `EXPIRED`, создаёт следующий номер attempt и
   возвращает turn в строгую FIFO queue;
4. фиксирует audit/outbox;
5. выдаёт новую lease; `RenewTurn` принимает только exact
   workload/generation/attempt/token/fence.

Не менять state/lease вручную. Если recovery не проходит, проверить clock,
RLS scope, OCC conflict и `turn_leases` metadata без token hash.

Owner gate не изменяется отдельно от process: request pin-ит root
initiator/session/turn/attempt/input/delivery/recipient и переводит process в
`WAITING_OWNER`. `interaction-gateway` сначала фиксирует exact immutable
delivery ID, payload digest, channel/root/post identity и durable receipt;
approve/reject без подтверждённой delivery закрыто отклоняются.
`interaction-gateway` отдельным `ExpireOwnerGate` poll с новым idempotency key
получает одну выбранную PostgreSQL просроченную строку: transaction row lock и
OCC version являются claim/fence, а crash откатывает весь переход. Expiry
атомарно терминализирует gate/turn/attempt/process/occurrence/ScheduledRun и
claims; delivery query использует PostgreSQL time и никогда не возвращает
просроченную карточку. `CHANGES_REQUESTED` завершает прежний turn/attempt,
сохраняет неизменяемый feedback receipt и создаёт свежие revision/input/turn в
том же ProcessRun/root; scheduled run переходит в `CONTINUATION` до terminal
readback нового хода.
Manual retry/cancel используют специализированные команды и отзывают старый
lease/grant.

## Artifact scan и schedule occurrence

`PENDING` artifact не используется как input/result. Внешний scanner вызывает
только `RecordArtifactScan` под exact workload/SPIFFE/permission и передаёт
совпадающие digest, scan policy/version, evidence и idempotency key. Допустимы
`PENDING`→`SCANNING`→`CLEAN|QUARANTINED|FAILED`; attach/enqueue разрешены
только для `CLEAN`.

Schedule хранит exact target/prompt/runtime revision/session policy/room/
notification/max execution duration snapshot, timezone/calendar,
delivery/retry/dead-letter и overlap policy. При stuck occurrence проверить
attempt, claimant/generation/token hash/expiry и predecessor. Expiry создаёт
следующую attempt с bounded backoff. `FORBID` не сдвигает schedule watermark,
пока есть open occurrence; `SKIP` сдвигает его и оставляет terminal
`SKIPPED` receipt; `QUEUE` сохраняет все occurrence в FIFO. Coalesce допустим
только для `FORBID`/`SKIP`. Claim повторно сверяет pinned target,
prompt/runtime revision и room и использует exact maximum execution lease.
Ручной запуск исключённых
Kubernetes/Mattermost/MCP/Codex действий запрещён.
Успешный claim атомарно создаёт или разрешает execution session, свежую
`RuntimeRevision`, `Turn` и для цели `PLAYBOOK` корневой `ProcessRun`;
`ScheduledRun` сохраняет exact версии occurrence/session/turn/process/revision
и effective input для каждой attempt. Completion не принимает outcome от
scheduler: он перечитывает terminal Turn/Process; retry завершает прежний run
и создаёт новый отслеживаемый attempt. Источник хода и process lineage содержат exact occurrence. Owner gate из
такого process повторяет schedule/occurrence и закрыто сверяет active
occurrence перед решением.

## Redis

Redis не является authority. Key и strict envelope связывают exact
organization/project/kind/id/version/epoch и оба digest. При
unknown-field/mismatch/corruption/error cached data не возвращается: ключ
удаляется, чтение идёт в PostgreSQL. Readiness остаётся закрытой, пока Redis
недоступен. Не восстанавливать cache из backup и не копировать tenant
snapshots вручную; epoch в PostgreSQL делает старые keys недостижимыми.

## Outbox и NATS

Relay использует отдельный least-privilege PostgreSQL principal. Ошибка
publish увеличивает attempt, применяет capped exponential backoff и после
25 неудач оставляет terminal record для расследования. Earliest
unpublished/terminal/backoff/in-flight predecessor блокирует следующий event
того же ordering key; другие keys продолжают доставку. Успешный exact
JetStream `PubAck` сохраняет stream/sequence/duplicate receipt и bounded
cleanup deadline; строка не удаляется в finalize. Потерянный response повторяет
тот же event ID, а broker deduplication и consumer inbox/cursor обеспечивают
at-least-once.

Terminal predecessor не исправляется прямым SQL и не пропускается. Оператор с
отдельными `controlplane.outbox.read` и `controlplane.outbox.repair` сначала
читает bounded metadata через `ListOutboxFailures`, устраняет внешнюю причину,
затем вызывает `RepairOutboxEvent` с exact event/sequence/attempts,
idempotency key, reason и SHA-256 evidence. SECURITY DEFINER функция имеет
fixed `search_path`, повторно сверяет tenant/project и отсутствие более раннего
predecessor, ограничивает repair count пятью и атомарно сохраняет repair
receipt/audit; payload не возвращается. Terminal gauge и critical alert остаются
активными до requeue/PubAck, но repair RPC остаётся достижимым, чтобы startup
не создавал неустранимый цикл. Подменять этот протокол ручным `UPDATE`, пропуском sequence
или удалением terminal row запрещено.

Outbox delivered receipt очищается не ранее 31 дня, то есть позже
30-дневного JetStream retention. Любое отличие `MaxMsgs`, `MaxBytes`,
`MaxMsgsPerSubject`, retention или dedup contract закрывает readiness; нельзя
обходить его уменьшением ожидаемых значений в application.

Проверять только event ID, event name, aggregate type/version, attempt,
lease expiry и error class. Payload может содержать business metadata и не
должен попадать в Issue.

## Наблюдаемость

Dashboard: `mattercodex-control-plane`.

Alerts:

- `ControlPlaneUnavailable`;
- `ControlPlaneNotReady`;
- `ControlPlaneInternalRPCFailures`;
- `ControlPlaneGRPCLatencyHigh`.

Каждый alert содержит абсолютный
`https://github.com/codex-k8s/matter-codex/blob/main/docs/runbooks/control-plane.md`.
Labels метрик ограничены operation/code/kind/action и не содержат tenant,
resource ID или произвольный input.

## Остановка и rollback

При штатной остановке:

1. readiness закрывается;
2. relay/readiness workers cancel и join;
3. gRPC/HTTP завершаются в bounded budgets;
4. NATS drain, Redis/OIDC/authority/PostgreSQL close выполняются до telemetry;
5. tracing shutdown и Sentry flush получают независимые contexts.

Application rollback допустим только к образу, который понимает уже
опубликованные Proto/schema/policy revisions. Schema, authority policy,
proof generation, audit и outbox назад не откатываются. При несовместимости
оставить workload not ready и подготовить forward fix.

## Prototype policy

В текущей фазе runbook не запускает integration/E2E/contract/deploy/render/
lifecycle/oracle suites или полный baseline. Отдельная поддерживаемая тестовая
волна ведётся в [Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).
Live PostgreSQL/Redis/NATS/Vault/Kubernetes и staging acceptance требуют
отдельного разрешения.
