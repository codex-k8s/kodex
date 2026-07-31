---
id: RUN-MC-007
title: Диагностика и восстановление control-plane
type: runbook
status: approved
owner: sre
version: 1.1.0
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

## Read-only preflight

1. Зафиксировать Git SHA и оба image digest.
2. Получить canonical render без apply:

```bash
tools/render-control-plane.sh \
  staging \
  sha256:<control-plane-image-digest> \
  sha256:<internal-rpc-authority-image-digest> \
  > /tmp/control-plane-staging.yaml
```

3. Сверить имена ServiceAccount, SecretProviderClass, ConfigMap, probes,
   selectors и exact destinations NetworkPolicy.
4. После отдельного разрешения на доступ к среде читать только metadata:

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
- migration schema version равна `20260731000200`;
- Redis использует TLS, exact SNI/CA и bounded database/pool;
- stream `CONTROL_PLANE` существует с точными двумя subjects, file storage,
  replicas окружения, `LimitsPolicy`, `DiscardOld`, maximum message size,
  max age 7 дней, dedup window 2 минуты, deny delete/purge и без mirror/source/
  republish/rollup/transform;
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
`PREVIOUS` LOGIN principal. На каждом transaction проверить server-side
`session_user`, generation/status/lifetime и одноразовый подписанный context,
связанный с backend PID и transaction ID. GUC не является диагностическим
способом установки tenant. При promotion прежний principal становится
`PREVIOUS`, затем `RETIRED`; reconciliation завершает его открытые backends.

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
`WAITING_OWNER`; approve/reject/expire атомарно переводят gate и process.
Manual retry/cancel используют специализированные команды и отзывают старый
lease/grant.

## Artifact scan и schedule occurrence

`PENDING` artifact не используется как input/result. Внешний scanner вызывает
только `RecordArtifactScan` под exact workload/SPIFFE/permission и передаёт
совпадающие digest, scan policy/version, evidence и idempotency key. Допустимы
`PENDING`→`SCANNING`→`CLEAN|QUARANTINED|FAILED`; attach/enqueue разрешены
только для `CLEAN`.

Schedule хранит exact target kind/id/version/digest, timezone/calendar,
delivery/retry/dead-letter и overlap policy. При stuck occurrence проверить
attempt, claimant/generation/token hash/expiry и predecessor. Expiry создаёт
следующую attempt с bounded backoff; `FORBID`/`SKIP` не допускают второй
active occurrence, `QUEUE` сохраняет FIFO. Ручной запуск исключённых
Kubernetes/Mattermost/MCP/Codex действий запрещён.

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
