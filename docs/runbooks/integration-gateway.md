---
id: RUN-MC-012
title: Диагностика и восстановление integration-gateway
type: runbook
status: approved
owner: sre
version: 1.4.0
updated: 2026-08-07
---

# Диагностика и восстановление integration-gateway

## Назначение и запреты

Runbook применяется для startup/readiness, MCP admission, approval,
connection validation, continuation/execution, provider owner management и
Git reconciliation workers. Он не разрешает
deploy, production change, ручное решение approval, повтор внешнего действия,
изменение RLS/high-watermark или просмотр secret values.

Не печатать bearer, credential files, DSN, context/payload encryption keys,
JWS/JWK, TLS private keys, provider request/response, preview до redaction или
Sentry DSN. Диагностика использует только закрытые status/reason codes,
timestamps, generation и opaque UUID/digest.

## Read-only preflight

1. Зафиксировать Git SHA и immutable image digest.
2. Проверить metadata Deployment, CNPG `Cluster`, migration Job, Service,
   ServiceAccount, `Bundle`, `VaultConnection`, `VaultAuth`,
   `VaultStaticSecret|VaultPKISecret`, SecretProviderClass и NetworkPolicy без
   чтения Secret.
3. Проверить, что runtime Pod имеет только gateway credential volume, а
   agent Pod не имеет provider credential, payload keyset или gateway DSN.
4. Проверить не менее двух ready Pod CNPG, primary Service
   `integration-gateway-postgresql-rw` и exact destinations: PostgreSQL,
   control-plane, internal authority,
   Vault, identity SSO, telemetry, runtime `integration-egress-proxy` и
   platform `egress-gateway` на TCP/8080. Прямой provider/Git TCP/443 egress
   отсутствует.
5. Проверить, что authority publisher target имеет workload
   `integration-gateway`, а issuer/verifier используют только собственные
   Vault/readback/restore paths. Не читать key material.
6. Проверить две готовые реплики runtime `integration-egress-proxy`,
   platform `egress-gateway` и `provider-health-adapter`,
   downstream client-mTLS SAN gateway, upstream
   client identity proxy и exact закрытые `/readyz|/validate|/health` routes.
   `direct_response` отсутствует; proxy имеет egress только к adapter, adapter
   не имеет egress.
7. Проверить status/readback `integration-gateway-vault-ca`,
   `integration-egress-proxy-ca`, `provider-health-adapter-ca`, затем status
   `VaultConnection -> VaultAuth -> VaultStaticSecret|VaultPKISecret` и только
   после них generated Secret/workload. Значения Secret не читать.
8. Проверить закрытые provider/Git catalogs, mounted signer JWK files,
   read-only Git credential Vault prefix и отдельный provider credential Vault
   prefix только по metadata/path. Repository URL/ref/path разрешены только
   catalog-ом; credential value не читать.

## Startup и readiness

Startup barrier должен пройти до обслуживания MCP/API:

- strict YAML packages прочитаны без duplicate/unknown/null и имеют неизменный
  canonical digest;
- PostgreSQL DSN использует exact SNI/CA, а `session_user` совпадает с
  `integration_gateway_runtime_g<generation>` и не имеет `BYPASSRLS`;
- context key и payload keyset доставлены файлами и имеют безопасные mode;
- local authority issuer/verifier, control-plane resolver и защищённый
  `CheckReadiness` проходят тот же mTLS/application grant/signed context path,
  что рабочий RPC;
- continuation worker может выполнить тот же специализированный control-plane
  RPC path, что `SUSPEND`, decision, `BEGIN` и terminal команды;
- OIDC discovery/JWKS доступны по pinned HTTPS identity destination;
- egress proxy downstream/upstream CA и exact SNI совпадают, а active upstream
  health check достигает provider adapter;
- payload keyset повторно читается readiness path и содержит active key вместе
  со всеми ключами, нужными незавершённым invocation/continuation.
- management worker проходит PostgreSQL schema check, provider/Git catalogs,
  Codex app-server, Git executable, два exact Vault capability/readback path,
  signer и protected control-plane client. До startup barrier effect не
  claim-ится; активная fenced lease продлевается на всём внешнем действии.
- management provider/Git процессы получают exact
  `HTTPS_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080`
  и `NO_PROXY=localhost,127.0.0.1,::1,.svc,.svc.cluster.local`; readiness
  выполняет compatibility `GET /readyz` на том же TCP/8080. Итоговая
  `NetworkPolicy` разрешает consumer egress только к platform selector
  `name=egress-gateway,component=platform-egress`. Закрытый FQDN registry
  принадлежит platform policy; прямой внешний TCP/443, port 9090, wildcard,
  plaintext fallback и `skipTLSVerify` запрещены.

Не отключать issuer, OIDC, RLS, TLS verify, schema validation или dependency
readiness. Не подменять provider proxy прямым HTTPS egress.

Входящий `IntegrationResultService` на `9443` сохраняет только защищённый
`CheckReadiness`: mTLS peer должен быть exact `agent-runner`, а workload-local
verifier — подтвердить exact readiness operation. Result resolve/ack методы,
result bearer и result-access keyset удалены; их нельзя восстанавливать ручным
чтением БД или универсальным RPC. Structured result остаётся в существующем
session-scoped MCP invocation read, а continuation/rejoin принадлежит
control-plane.

## PostgreSQL и миграция

Migration Job использует отдельный migrator principal. Forward-only reconcile:

1. bootstrap допускает только `integration_gateway_runtime_g<generation>`;
2. `CURRENT|NEXT|PREVIOUS` имеют bounded lifetime и членство только в
   `integration_gateway_runtime`;
3. `RETIRED` получает `NOLOGIN`, revoke membership и termination;
4. retired principal/context key не может стать active снова;
5. runtime transaction устанавливает scope только подписанным одноразовым
   context, связанным с `session_user`, generation, backend PID и transaction.

Migration Job получает `CURRENT` и заранее доставленный `NEXT` DSN отдельными
Vault objects. Перед `NEXT -> CURRENT` reconciler соединяется именно новым DSN,
сверяет `session_user` и `runtime_identity_ready()`, затем одной serializable
transaction повышает verifier-owned high-watermark. Понижение generation,
`PREVIOUS -> CURRENT`, пропущенный readback и неизвестный served state закрыто
отклоняются. После promotion прежняя generation проходит только
`CURRENT -> PREVIOUS -> RETIRED`.

При crash повторить тот же migration Job с теми же generation и DSN files:
high-watermark делает reconcile идемпотентным. Не менять fence row и не
переназначать status вручную.

При ошибке сохранить только SQLSTATE и safe error class. Не выводить query
arguments. Не выполнять `goose down`, ручной `SET SESSION AUTHORIZATION` или
caller-set GUC.

## MCP admission

- отсутствие/неверный bearer: `401`;
- чужой durable `Mcp-Session-Id`: `403`;
- закрытая/истёкшая transport session: `404`;
- body, deadline, global/session concurrency или request count: bounded
  `4xx/429/504`;
- неизвестные definition/tool/risk/permission/generation: fail-closed до
  provider call.

При повторе проверять только request hash и semantic receipt. Нельзя создавать
новый idempotency key, если неизвестно, был ли внешний effect.

## Approval и execution

`PENDING_APPROVAL`, decision, attempt и result находятся в PostgreSQL и
переживают restart. Approval применим только к сохранённому request hash.
Одновременные approve/reject/expire/cancel/execute разрешаются row lock и
single-winner fence.

Cancel допускается только до `EXECUTING`: owner API требует exact permission,
а MCP-команда — совпадающую durable transport session. После начала provider
call cancel закрыто возвращает conflict и не маскирует возможный внешний effect.

При `UNKNOWN` не повторять действие вручную. Provider-idempotent tool может
использовать только тот же сохранённый provider key; прочий исход остаётся
`UNKNOWN` до отдельного audited repair contract. Credential rotation закрывает
новые claims прежнего generation и не позволяет старому attempt воскресить
grant.

При graceful shutdown terminal receipt записывается независимым bounded
context до остановки PostgreSQL. После аварийной остановки lifecycle worker
переводит `EXECUTING`, который старше одной минуты, вместе с attempt/result/audit
в `UNKNOWN`; recovery не выполняет повторный provider call.

## Continuation и rejoin

Invocation, approval и `SUSPEND` effect создаются одной owner transaction.
Проверять разрешено только opaque ID, action, bounded state, version/fence,
attempt count и timestamps; encrypted application grant не читать.

Ожидаемые состояния после подтверждённой команды:

| Команда | Approval | Execution | Continuation |
| --- | --- | --- | --- |
| `SUSPEND` | `PENDING` | `NOT_STARTED` | `SUSPENDED` |
| `APPROVE` | `APPROVED` | `NOT_STARTED` | `SUSPENDED` |
| `BEGIN` | `APPROVED` | `EXECUTING` | `SUSPENDED` |
| `SUCCEED` | `APPROVED` | `SUCCEEDED` | `READY` |
| `FAIL` | `APPROVED` | `FAILED` | `READY` |
| `REJECT` | `REJECTED` | `NOT_APPLICABLE` | `READY` |
| `CANCEL` | `CANCELLED` | `NOT_APPLICABLE` | `READY` |
| `EXPIRE` | `EXPIRED` | `NOT_APPLICABLE` | `READY` |

Mismatch invocation/request digest, stale version/fence либо неизвестное
состояние — закрытая ошибка; вручную менять continuation row или пропускать
команду запрещено. Исходный `AGENT_SESSION_GRANT` допускает только admission и
первый `SUSPEND`. После сохранённого `continuation_id` его expiry не завершает
business approval: control-plane на каждый decision/BEGIN/terminal выдаёт
свежий узкий grant до server-owned deadline. Если initial handoff исчерпал 32
попытки, он закрывается terminal/dead-letter без ручного перевыпуска bearer.

Provider не должен вызываться до подтверждённого `BEGIN`. Если
`provider_dispatched_at` уже установлен, повторный dispatch запрещён даже при
отсутствии terminal result: lifecycle фиксирует `UNKNOWN`, затем отправляет
`FAIL` с безопасным reference/digest.

После terminal `READY` control-plane материализует server-owned continuation
turn. Structured invocation result читается через существующий session-scoped
MCP operation; отдельные gateway `ResolveIntegrationResult`,
`AcknowledgeIntegrationResult`, result bearer и caller-selected continuation
ID отсутствуют. AsyncAPI/NATS replay для этого пути отсутствует намеренно. При
stale session/grant нельзя изменять owner state или cursor вручную.

## Connection validation

Gateway отправляет credentials только exact mTLS egress proxy operation
`POST /validate`. Наружу допускаются только `OK`,
`CREDENTIAL_UNAVAILABLE`, `UNAUTHORIZED`, `FORBIDDEN`,
`ENDPOINT_UNAVAILABLE`, `TIMEOUT`, `PROTOCOL_ERROR` и timestamp. Raw body,
headers, target path и credential value не сохраняются и не логируются.

Envoy не отвечает локально: `/validate` и `/health` достигают exact
`provider-health-adapter` по upstream mTLS. `VALID` допустим только после
сравнения фактического credential adapter; неверный credential возвращает
`UNAUTHORIZED`. `/readyz` использует тот же upstream cluster/CA/SNI и
становится отрицательным, если adapter или его credential недоступны.

## Provider authorization и connection lifecycle

Device attempt имеет только переходы `PENDING -> CODE_ISSUED ->
AUTHORIZED|DENIED|EXPIRED|FAILED|CANCELLED`. Проверять можно opaque attempt и
connection ID, version/generation, state, `code_expires_at`, `expires_at` и
safe failure category. `verification_url`/`user_code` допустимы только до
`code_expires_at`; encrypted DB field, provider `loginId`, Vault value и
`auth.json` не читать.

Долгий polling удерживает одну fenced lease, продлеваемую PostgreSQL clock.
Потеря lease отменяет app-server process и закрывает внешний исход как
`UNKNOWN`; новая реплика не запускает тот же provider authorization повторно.
Restart/reauthorize создаёт новую attempt и immutable credential generation.
После app-server completion gateway записывает credential в deterministic
Vault ref с `cas=0`, выполняет version/content-digest readback и сохраняет
generation как `PENDING` вместе с durable provider-reference intent. Active
pointer, binding, masked metadata и eligibility меняются одной transaction
только после совпавшего control-plane mutation и последующих Get/List по exact
resource ID/version/projection digest. Raw token не входит в payload intent,
audit, receipt, diagnostics или лог.

Revoke сначала одной owner transaction повышает revoke generation, закрывает
pending attempts/leases/eligibility, помечает все credential generations
`REVOKED` и создаёт durable effect. Worker повторно сверяет exact generation,
JIT-читает только active credential для однократного
`account/logout`+`account/read`, уничтожает все immutable Vault versions этого
connection и архивирует control-plane reference. При неизвестном provider
outcome повтор logout запрещён. Provider, Vault destroy/readback и
control-plane archive/readback имеют отдельные durable checkpoints; crash
возобновляет только незавершённые безопасно повторяемые фазы. Effect не
становится terminal, пока Vault и control-plane фазы не подтверждены;
итоговый `UNKNOWN` относится только к уже неоднозначному logout.

Provider-reference mutation подписывается только после Vault/provider
readback. `command_intent_sha256` producer и control-plane consumer вычисляют
одним helper из verified actor/organization/project, workload, exact method,
target и typed spec. Изменившийся field, stale generation либо reused JTI
отклоняется. Успех допустим только при совпадении mutation и последующих
Get/List по resource ID, version и projection digest.

Provider pool desired policy/weights хранит control-plane. Локальная строка
gateway — durable owner command/effect journal и fresh observation projection,
а не второй desired source. `stale|unknown` connection не eligible;
`least_used|weighted` selection bounded, deterministic и overflow-safe. Нельзя
исправлять mismatch ручным SQL или привязывать connection из другого project.

## Git source binding и reconciliation

Owner request содержит только catalog keys, target kind/stable key, OCC и
idempotency. Repository URL/ref/path, repository connection, credential opaque
ref/version/digest назначает closed server catalog. Fetch выполняется через
`git init` + exact `fetch --depth=1 --no-tags origin <ref>` с TLS verification,
запрещёнными redirects и allowlisted proxy; credential читается JIT из
read-only Vault prefix и передаётся через private askpass file.

После fetch gateway фиксирует immutable commit, monotonically increasing
source revision, content digest и encrypted bounded snapshot. Из typed document
и verified owner authority вычисляется общий semantic intent, затем signer
выпускает purpose-bound `GitReconciliationReceipt` и вызывается ровно один из
`ReconcileGitRoleDefinition|Agent|InstructionSet|ProviderPool`. Caller не
передаёт spec/source digest/receipt. Успех фиксируется только из returned
resource ID/version/projection digest.

После возможного RPC dispatch timeout/transport error reconciliation становится
`FAILED` с `OUTCOME_UNKNOWN`; повторная подпись или автоматический второй apply
запрещены. Сверять разрешено только binding/reconciliation state, commit,
revision/digests, receipt ID/digest и control-plane typed readback. Raw Git
document, credential, header и remote output не выводить.

## Vault CA и Kubernetes API egress

Единственный source Vault CA находится в trust-manager trust namespace.
Namespace `mattercodex-system` заранее создаёт environment bootstrap; этот
deployable не владеет и не дублирует cluster Namespace.
`deploy/k8s/base/vault-ca-delivery` доставляет overlap bundle в Secret
`integration-gateway-vault-ca`; `VaultConnection` обязан показать ready
status с exact address/SNI и `skipTLSVerify: false` до `VaultAuth` и secret CR.
В итоговом render все `Bundle` обязаны быть cluster-scoped без
`metadata.namespace`, а их target ограничен namespace selector
`mattercodex-system`.
CA values не копировать и не создавать вручную. При ротации сначала обновить
source overlap bundle, дождаться readback target digest/status, затем менять
server leaf и удалять прежнюю CA только после полного overlap window.

CNPG instance manager получает Kubernetes API egress только через
`tools/deploy/kubernetes-api-egress.sh`. Сначала выполнить `discover`, затем
`render` и `validate` с exact `--context`, namespace `mattercodex-system`,
policy name и selector `cnpg.io/cluster=integration-gateway-postgresql`.
Скрипт читает `Service/default/kubernetes` и ready IPv4 EndpointSlice и строит
две additive `/32` policy. `apply` разрешён только после отдельного owner OK и
`MATTERCODEX_OWNER_APPROVED=true`; в этом runbook автоматическое применение не
разрешается. После owner-approved apply обязательны `readback` и отсутствие
diff. Статический `component=kube-apiserver`, широкий CIDR и перенос policy
между kube contexts запрещены.

## Ротация payload keyset и TLS credential

Ротация выполняется только вперёд с overlap. Сначала доставить новый key ID или
сертификат вместе с прежним material, дождаться успешной readiness после
фактической загрузки и только затем переключить active generation. Старый
payload key удаляется лишь после завершения всех invocation/continuation,
зашифрованных этим key ID. TLS CA/certificate меняются с двухсторонним overlap
и exact SNI; plaintext fallback и `skipTLSVerify` запрещены.

Если keyset не читается либо active key отсутствует, readiness обязана стать
отрицательной. Не менять `EncryptedApplicationGrant` или result payload вручную
и не переиздавать grant для обхода старого key ID.

Continuation transition keyset имеет `revision`, `high_watermark`,
`served_generation`, `kid` и ровно один `CURRENT`; допускается только соседний
`PREVIOUS` до `accept_until` и подготовленный `NEXT`. Сначала доставить
`CURRENT+NEXT`, затем атомарно переключить signer и verifier на новое поколение,
оставив прежний ключ `PREVIOUS` не меньше максимального срока уже выданных
transition grants. PostgreSQL verifier fence подтверждает exact served keyset
digest. Отдельный result-access keyset удалён. Unknown, `NEXT`, `RETIRED`,
пропущенное или rollback generation закрыто отклоняются. После crash
повторяется тот же revision/digest; старый snapshot или та же revision с другим
digest запрещены.

## Alerts

- `IntegrationGatewayUnavailable`: проверить доступность реплик и Service;
- `IntegrationGatewayNotReady`: проверить startup barrier в порядке выше;
- `IntegrationGatewayContinuationFailures`: проверить issuer/control-plane path,
  grant deadline и durable effect lease без чтения payload;
- `IntegrationGatewayProviderFailures`: проверить bounded validation code,
  credential generation и mTLS egress proxy без повторного provider effect;
- `IntegrationGatewayUnknownOutcomes`: внешний dispatch уже мог состояться;
  не повторять действие, сверить только safe receipt/digest и открыть отдельный
  audited repair flow;
- `IntegrationGatewayEgressProxyUnavailable`: проверить mTLS proxy/adapter
  readiness и CA/SNI без прямого provider egress;
- `IntegrationGatewayManagementFailures`: проверить fenced management effect,
  Vault/Codex/Git/control-plane safe category и generation; неизвестный внешний
  outcome не повторять;
- при отрицательной management readiness проверить platform `egress-gateway`
  replicas, compatibility `/readyz`, exact consumer selector:8080 и FQDN
  policy без расширения allowlist либо прямого HTTPS egress;
- `IntegrationGatewayPostgresqlUnavailable`: проверить минимум две ready CNPG
  replicas и exact PostgreSQL TLS path.

Каждый alert обязан ссылаться на абсолютный URL этого runbook.

## Rollback

Rollback приложения — возврат предыдущего immutable image digest после
остановки новых claims. Миграция не откатывается. Pending approvals,
execution/continuation, provider authorization, Git reconciliation intents и
receipts сохраняются; запрещено удалять их, возвращать active credential
pointer назад, перепривязывать application grant либо повторять provider/Git
effect вручную. Новый provider state до cutover #196 не синхронизируется с
legacy `bot-service`. При несовместимой схеме исправление выполняется новой
forward-only migration.
