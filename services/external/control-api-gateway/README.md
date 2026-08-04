---
id: SVC-MC-013
title: Control API gateway
type: service
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-04
---

# Control API gateway

`control-api-gateway` — внешняя owner HTTP/WebSocket boundary Issue
[#191](https://github.com/codex-k8s/matter-codex/issues/191). Gateway проверяет
OIDC, выдаёт короткую защищённую browser session, применяет CORS/CSRF/rate
limits и преобразует запросы в сгенерированный gRPC client `control-plane`.

Gateway не читает PostgreSQL, Redis, NATS или Vault API напрямую, не хранит
бизнес-состояние и не принимает `actor`, organization, project, owner или
tenant из payload. Эти поля разрешает `AuthorityProofResolverService` по
проверенному OIDC subject и авторитетному состоянию `control-plane`.

## Контракты и codegen

- source OpenAPI: `contracts/openapi/control-api-gateway/v1/openapi.yaml`;
- generated HTTP server/models:
  `internal/transport/http/generated/control_api_gateway.gen.go`;
- source AsyncAPI: `contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml`;
- generated WebSocket models: `internal/transport/websocket/generated`;
- internal RPC: `contracts/proto/controlplane/v1/control_plane.proto`;
- authority policy: существующий профиль `control-api-gateway` в
  `deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json`.

OpenAPI требует `Idempotency-Key` для каждой state-changing команды и
`If-Match: "<positive-version>"` для update/transition/delete. Gateway
передаёт их без ослабления. `control-plane` сначала разрешает ресурс внутри
trusted owner/tenant boundary, затем проверяет OCC и receipt. Create не
принимает project/owner/ownership: project и owner разрешает `control-plane`,
а UI transport server-side назначает `managed_by=UI`. Git-owned объект
обновляется только с явным `detachGitManagement`; source/revision из browser
payload не принимаются.

## Сквозная карта сценариев

Во всех строках actor source — подписанный OIDC `sub` с `sid`, `jti` и
монотонной `session_revision`; authority source — resolver control-plane,
который server-side выводит actor/organization/project/ownership. Downstream
вызывается по exact mTLS SNI/CA и короткоживущему internal context из локального
issuer. Payload identity не используется.

| Требование и инициатор | Внешняя операция | Gateway mapping / internal RPC | Владелец, version/idempotency | Ответ, переход, событие или read path |
| --- | --- | --- | --- | --- |
| #191, owner создаёт сессию | `POST /api/v1/session`, OIDC bearer | signature/issuer/audience/time/session claims → AES-256-GCM cookie + CSRF | OIDC владеет credential; gateway владеет только сроком session envelope | `204`; бизнес-перехода/события нет; каждый запрос повторно проверяет bearer |
| #191, owner завершает сессию | `DELETE /api/v1/session`, cookie+Origin+CSRF | проверка той же boundary, удаление обеих cookies | gateway; операция не изменяет control-plane | `204`; server-side credential остаётся у OIDC, просроченная cookie закрыто отклоняется |
| #191, PRD-MC-001/005, owner создаёт проект | `POST /api/v1/projects` | `control.project.create` → `CreateProject` | control-plane назначает ID/owner; semantic receipt по `Idempotency-Key` | `201`+`ETag`; state/idempotency/audit и каждый обязательный outbox fact атомарны; client перечитывает REST/WS snapshot |
| #191, owner читает проекты | `GET /api/v1/projects` | `control.project.list` → `ListProjects` | control-plane eligibility и pagination token | `200`; только authoritative read path, события не требуются |
| #191, owner создаёт UI-конфигурацию | `POST /api/v1/resources` для `CHAT|CREDENTIAL_BINDING|REPOSITORY_WORKSPACE|INTEGRATION` | закрытый kind/spec caster → `control.resource.create` → `CreateResource` | control-plane назначает project/owner; `Idempotency-Key`; secret value запрещён | `201`+`ETag`; audit и каждый применимый outbox fact принадлежат owner transaction; WS получает свежий read snapshot |
| #191, owner читает ресурс/список | `GET /api/v1/resources[/{id}]` | `control.resource.get|list` → `GetResource|ListResources` | resource сначала разрешён control-plane внутри текущей области | `200`+`ETag` для single; скрытый и отсутствующий одинаково `404`; authoritative read path |
| #191, owner обновляет UI-конфигурацию | `PUT /api/v1/resources/{id}` | closed kind/spec → `control.resource.update` → `UpdateResource` | owner resolution → `If-Match` → idempotency; Git-owned требует явный detach | `200`+новый `ETag`; audit/read snapshot; parallel version даёт `412` |
| #191, owner меняет lifecycle | `POST /api/v1/resources/{id}/transition` | closed state/reason → `control.resource.transition` → `TransitionResource` | owner resolution → OCC/idempotency; полный lifecycle проверяет control-plane | `200`; недопустимый переход `409`; authoritative resource/audit read path |
| #191, owner удаляет конфигурацию | `DELETE /api/v1/resources/{id}` | `control.resource.delete` → `DeleteResource` | owner resolution → OCC/idempotency; tombstone/связи принадлежат control-plane | `200`; state/tombstone/audit фиксируются owner transaction; WS перечитывает snapshot |
| #191, owner управляет authority-bearing team/role/prompt | `POST /api/v1/access-resources` | closed kind/action/spec → `control.access.manage` → `ManageAccessResource` | специализированная команда; create без caller owner, остальные с resource ID+`If-Match` | `200`; self-grant/универсальный CRUD запрещены; audit/read snapshot |
| #191, owner наблюдает runs | `GET /api/v1/runs` | `control.resource.list` → `ListResources(PROCESS_RUN)` | control-plane владеет run lifecycle/version | `200`; авторитетный read path, gateway не выводит terminal state самостоятельно |
| #191, owner наблюдает incidents | `GET /api/v1/incidents` | `control.audit.list` → `ListAuditEvents(action=record_runtime_incident)` | control-plane владеет audit metadata | `200`; отдельное UI-событие отсутствует, authoritative audit read path |
| #191, owner читает configuration changes/audit | `GET /api/v1/configuration-changes|audit` | `control.audit.list` → `ListAuditEvents`; gateway фильтрует закрытый action set | control-plane владеет audit; actor metadata уже server-derived | `200`; credential/token/secret values отсутствуют |
| #191, owner читает diagnostics | `GET /api/v1/diagnostics` | `control.diagnostics.get` → `GetDiagnostics` | control-plane владеет schema/outbox/lease metadata | `200`; только ограниченный authoritative read path |
| #191, owner подписывается на realtime | `WSS /api/v1/realtime`, subprotocol `mattercodex.control.v1` + CSRF | та же session boundary; `RUNS/RESOURCES` → `ListResources`, `INCIDENTS/CONFIGURATION_CHANGES` → `ListAuditEvents` | connection-local sequence не является domain version; каждый item несёт server version | полные replace-snapshot до 100 items; reconnect = resubscribe/read current; NATS consumer и локальная БД отсутствуют |

## Authority и lifecycle matrix

| Состояние/переход | Проверка | Закрытый отказ |
| --- | --- | --- |
| Session create | exact HTTPS OIDC issuer/audience, `sub/sid/jti` UUID, positive revision, остаток TTL ≥ 1 минута | cookie не выдаётся |
| Session use | AES-GCM current/previous key, expiry, повторная OIDC verify, совпадение subject/sid/revision | `401`, bearer не передаётся в RPC |
| Mutation | exact allowlisted `Origin`, session cookie, double-submit CSRF и digest внутри encrypted envelope | `403`, RPC не вызывается |
| Session rotation | новые cookies шифруются current key; current и previous принимаются на overlap | lower/unknown key не принимается; rollback key не материализуется gateway |
| Session expiry/logout | локальный deadline закрывает HTTP/WS; logout удаляет обе cookies | rejoin только через новый OIDC/session create |
| Create | closed command registry; owner/project отсутствуют в request | unknown/protected kind → `400`, RPC не вызывается |
| Update/transition/delete | resource ID передаётся как locator; control-plane делает owner resolution до OCC/receipt | hidden/missing → одинаковый `404`; version → `412`; lifecycle → `409` |
| WS subscribe | exact Origin, session, CSRF subprotocol+cookie, ≤4 channel, ≤8 kind, bounded frame/connection | policy close без snapshot |
| WS retry/reconnect | новый authority proof на каждый poll; reconnect читает current state | gateway не replay-ит устаревший snapshot и не синтезирует gap |
| Rate/concurrency | fixed window на verified subject, bounded LRU keys, общий semaphore | `429`+bounded `Retry-After`; arbitrary key не становится metric label |

## Материализация рабочего пути

| Часть | Исполняемая материализация |
| --- | --- |
| Producer profile | существующий `control-plane.oidc`: OIDC bearer metadata, resolver exact mTLS SPIFFE, server-resolved actor/tenant/project/ownership |
| Client operation profile | `controlplaneclient.ControlAPIGatewayOperations`; только реализованные full methods и существующие operation IDs policy |
| Generated adapters | OpenAPI std HTTP server, AsyncAPI Go models, generated control-plane gRPC client |
| Consumer effect | REST response либо connection-local replace-snapshot; browser не подтверждает domain effect и не влияет на state owner |
| Readiness | OIDC discovery/key verifier создан; session keys/TLS прочитаны; `controlplaneclient.Check` проходит resolver → local issuer → protected `CheckReadiness` |
| Deploy ownership | Dockerfile, Kustomize base/overlays, Service/Ingress TLS passthrough, issuer component, Vault Secrets Operator, exact NetworkPolicy, PDB, metrics/dashboard/alerts |
| Failure policy | startup fail-closed; readiness снимается при protected path failure; HTTP/WS unknown error detail → `INTERNAL`; graceful cancel/join до client/telemetry shutdown |

## Security boundary

- public HTTP использует только TLS 1.3; cookies имеют `__Host-`, `Secure`,
  `SameSite=Strict`, session cookie дополнительно `HttpOnly`;
- session envelope зашифрован AES-256-GCM, ограничен по размеру и сроку;
  bearer никогда не логируется и не сохраняется вне encrypted cookie;
- OIDC HTTP client запрещает proxy/redirect, принимает только pinned HTTPS
  origin, exact SNI и CA;
- control-plane client использует exact SNI/CA, client certificate, application
  bearer, resolver proof, local issuer и internal authorization context;
- `mTLS` не заменяет OIDC/session, permission, owner resolution или replay
  protection;
- metric `route/status/channel/outcome` нормализованы закрытыми множествами;
  IDs, paths, actions и внешние значения не являются labels;
- configuration projection принимает только фактические control-plane audit
  actions `create|update|transition|delete|create_schedule|manage_schedule_*`;
- audit/diagnostics не содержат bearer, cookie, CSRF, key, DSN или secret value.

## Конфигурация и secret delivery

Все `CONTROL_API_GATEWAY_*_FILE` — абсолютные regular files без разрешений
`other`. Значения не входят в manifest, README, логи или ошибки. Vault Secrets
Operator атомарно материализует public TLS, current/previous session keys и
readiness application grant в Kubernetes Secrets с rollout restart. Уже
принятый `internal-rpc-authority-control-api-gateway-issuer` component
доставляет отдельную control-plane workload identity и trust material;
gateway монтирует их только для чтения и не создаёт параллельный auth
primitive. CA доставляются отдельными ConfigMap. Public TLS и client
certificate имеют bounded TTL; session key rotation forward-only использует
двухшаговый current+previous rollout overlap; deployed profile требует оба
key-файла.

NetworkPolicy разрешает только ingress controller, Prometheus, DNS,
control-plane, identity SSO, Vault, OTel, Sentry и точные issuer component
destinations. Правил только по порту, wildcard egress, plaintext fallback и
`skipTLSVerify` нет.

## Локальная проверка

```bash
make gen-control-api-gateway-openapi-go
make lint-control-api-gateway-asyncapi
make gen-control-api-gateway-asyncapi
(cd services/external/control-api-gateway && go test -run '^$' ./...)
kubectl kustomize deploy/k8s/overlays/staging/control-api-gateway
kubectl kustomize deploy/k8s/overlays/production/control-api-gateway
```

Фактические OIDC/Vault/control-plane/Kubernetes и staging проверки требуют
отдельного разрешения. Поддерживаемый integration/E2E/deploy/lifecycle контур
отложен в [Issue #216](https://github.com/codex-k8s/matter-codex/issues/216).

## Проверенная актуальная документация

Context7 вызван для `coreos/go-oidc`, `coder/websocket` и `oapi-codegen`, но
вернул `Monthly quota exceeded`; документация через Context7 была недоступна.
Проверены официальные первичные источники:

- [go-oidc](https://github.com/coreos/go-oidc) — provider discovery,
  `IDTokenVerifier`, issuer/audience/signature/time verification;
- [coder/websocket](https://pkg.go.dev/github.com/coder/websocket) — exact
  origin patterns, subprotocol, read limit, ping и bounded context I/O;
- [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) — v2 config,
  models и `std-http-server` generation;
- [Redocly CLI](https://redocly.com/docs/cli/commands/lint/) — source OpenAPI
  validation с recommended rules;
- [AsyncAPI CLI](https://www.asyncapi.com/docs/tools/cli) — validate и Go
  model generation;
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
  [Kustomize](https://kubectl.docs.kubernetes.io/references/kustomize/) и
  [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/);
- [Vault Secrets Operator](https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso)
  и [Vault PKI issue API](https://developer.hashicorp.com/vault/api-docs/secret/pki#generate-certificate-and-key).

Эксплуатация описана в
[`docs/runbooks/control-api-gateway.md`](../../../docs/runbooks/control-api-gateway.md).
