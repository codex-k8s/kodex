---
id: RUN-MC-015
title: Direct-production single-node prototype
type: runbook
status: approved
owner: sre
version: 1.8.2
updated: 2026-08-20
---

# Direct-production single-node prototype

Публичный SSO описан в `RUN-MC-016`
(`docs/runbooks/direct-production-sso.md`), а owner UI и его TLS/mTLS ingress
bridge - в `RUN-MC-020` (`docs/runbooks/direct-production-control-center.md`).

## Назначение

Профиль `direct-production single-node prototype` не имеет staging. Первый выпуск
создаёт в существующем production-кластере изолированные namespace:

- `mattercodex-ci` — изолированные ARC controller и build scale set;
- `mattercodex-ci-deploy` — отдельные ARC controller, deploy scale set и
  namespaced production credential;
- `mattercodex-system` — новый dark-контур без Ingress и пользовательского трафика.

Legacy Mattermost, PostgreSQL, bot-service, Kaniko и registry в
`matter-kodex-prod` не изменяются. Они остаются авторитетным пользовательским
путём и rollback path. Новый build публикует в существующий Service
`matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000`, а workloads
используют node pull endpoint `localhost:5001` только с digest.

Namespace `mattercodex-system` сохраняет суммарные requests не выше `8 CPU` и
`16Gi` memory. Отдельная quota `limits.memory=96Gi` является admission
headroom для одновременных `maxSurge` pod во время rolling update на owner
single-node с 126 GiB RAM; она не резервирует эту память и не заменяет контроль
node pressure. Значение оставляет около 30 GiB физического ceiling вне
application namespace для Kubernetes, legacy-контура и CI.

Exact release lock также содержит compatibility image `bot-service` той же Git
revision. Dark deploy не применяет этот образ к legacy Deployment. Он
используется только отдельной owner-approved операцией подготовки schema
`000041` по `RUN-MC-014`, с сохранением предыдущего PodTemplate и bounded
rollback. Image `legacy-data-migration` входит в тот же lock, но запускается
только отдельным owner-approved execution manifest после завершения source TLS
preflight; обычный dark deploy его не применяет.

## Шлюзы

До merge допускаются только read-only preflight и локальный render. После merge
нужен отдельный owner gate на каждый production bootstrap/build/deploy. Для
private repository gate не полагается на недоступные в части тарифов required
reviewers GitHub Environment. Оба scale set принадлежат только repository
`codex-k8s/matter-codex`, имеют разные exact labels и не используют organization
runner groups. Owner code-first записывает full 40-hex SHA и числовые owner actor
ID в repository Actions variables и ограничивает Environment веткой `main`.
Workflow допускает только `workflow_dispatch`, у которого и исходный
`actor`, и `triggering_actor` совпадают с owner-controlled ID; строка input/env не
является доказательством допуска. В Wave A исполняется только `dark`. Режим
`cutover` закрыто отклоняется до закрытия #241, #237, #194, отдельного owner gate
и материализации cutover manifest.

## Code-first bootstrap

Сначала owner с repository administration материализует из exact актуального
`main` и authenticated GitHub API actor три локальных файла с mode `0600`, затем
настраивает две Environment и repository variables. Файлы actor ID и SHA не
печатаются и не включаются в Git:

```bash
infra/github/materialize-actions-policy-inputs.sh \
  --output-directory /secure/path/actions-policy
infra/github/bootstrap-actions-policy.sh --mode preflight \
  --workflow-sha-file /secure/path/actions-policy/workflow-sha \
  --build-owner-actor-id-file /secure/path/actions-policy/build-owner-actor-id \
  --deploy-owner-actor-id-file /secure/path/actions-policy/deploy-owner-actor-id
infra/github/bootstrap-actions-policy.sh --mode apply \
  --workflow-sha-file /secure/path/actions-policy/workflow-sha \
  --build-owner-actor-id-file /secure/path/actions-policy/build-owner-actor-id \
  --deploy-owner-actor-id-file /secure/path/actions-policy/deploy-owner-actor-id
infra/github/bootstrap-actions-policy.sh --mode readback \
  --workflow-sha-file /secure/path/actions-policy/workflow-sha \
  --build-owner-actor-id-file /secure/path/actions-policy/build-owner-actor-id \
  --deploy-owner-actor-id-file /secure/path/actions-policy/deploy-owner-actor-id
```

Затем одноразовый операторский ARC bootstrap требует cluster-admin и не
выполняется routine runner:

```bash
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode preflight \
  --workflow-sha-file PATH --build-owner-actor-id-file PATH \
  --deploy-owner-actor-id-file PATH
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode apply \
  --workflow-sha-file PATH --build-owner-actor-id-file PATH \
  --deploy-owner-actor-id-file PATH \
  --github-pat-file /secure/path/github-token
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode readback \
  --workflow-sha-file PATH --build-owner-actor-id-file PATH \
  --deploy-owner-actor-id-file PATH
```

GitHub App и PAT являются mutually exclusive file modes. PAT Secret имеет только
ключ `github_token`; App Secret — только три канонических App key. Скрипт не
печатает значения и не заменяет существующий Secret другого режима. ARC
preflight/apply/readback сначала повторяет GitHub policy readback; runners не
создаются при отсутствующем exact-SHA или owner actor variable. Bootstrap
создаёт два namespace, два независимых
controller и scale set, default-deny NetworkPolicy, allowlisted egress proxy,
динамически привязанный exact Kubernetes API egress и admission allowlist.
Repo-scoped routing дополнительно закрыт синхронным runner pre-job hook: до
первого workflow step он сверяет exact repository, `workflow_dispatch`, `main`,
workflow path/ref/SHA, source SHA, owner actor ID и job ID по read-only ConfigMap,
материализованному bootstrap из тех же owner policy inputs.
`mattercodex-build` не получает Kubernetes token; `mattercodex-deploy` получает
только production namespaced RBAC. `readback` требует Ready controller/listener,
ровно один idle EphemeralRunnerSet, repository URL, отсутствие `runnerGroup`, scale bounds,
ServiceAccount и отрицательную admission-проверку.

После успешного ARC apply/readback непригодная одноразовая repository variable
удаляется exact owner API вызовом того же code-first скрипта:

```bash
infra/github/bootstrap-actions-policy.sh \
  --mode retire-invalid-registration-variable \
  --workflow-sha-file PATH --build-owner-actor-id-file PATH \
  --deploy-owner-actor-id-file PATH
```

Build namespace имеет Pod Security audit/warn `restricted`, но enforce
`privileged`, потому что зафиксированный upstream rootless BuildKit требует
`hostUsers=false`, `procMount=Unmasked` и unconfined AppArmor/seccomp. Это не
расширяет произвольные workloads: fail-closed ValidatingAdmissionPolicy допускает
только exact ARC/controller/runner/proxy identities, ServiceAccount, volumes и
pinned images. Deploy и application namespace используют enforce `restricted`.

Отдельный owner-controlled production bootstrap применяет namespace security,
ServiceAccount/RBAC, Secret/CA interfaces и admission allowlist. Он генерирует
из канонического render параметризованный contract точных ServiceAccount, token
automount, volumes, container images, command/args, volumeMounts и Secret env для
каждого workload. Routine deploy не создаёт Secrets/Certificates, не читает Pod
logs и получает `get` только для exact Secret
`internal-rpc-authority-snapshot`, чтобы проверить publisher-owned переход от
пустого bootstrap sentinel к непустому JWS. Удалять он может только пять точно
названных migration Jobs и owner-defined Job поколенческих PostgreSQL principals:

```bash
umask 077
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode preflight \
  --external-material-file /secure/path/external.yaml
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode apply \
  --external-material-file /secure/path/external.yaml
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode readback
```

Внешний фрагмент передаётся только локальным файлом `0600`, не журналируется и
содержит ровно доказанные внешние bindings. Единый materializer сохраняет
существующие generated values, безопасно копирует exact legacy bindings,
выводит derived values из owner-only root и создаёт полный закрытый набор
application file/env/TLS interfaces. Bootstrap генерирует внутренние foundation
credentials, проверяет готовность exact TLS Certificates, допускает пустым
только `internal-rpc-authority-snapshot/snapshot.jws` до первого publisher
readback и пишет безопасный
`mattercodex-bootstrap-readiness` без значений credentials.
Restore-role trust не принимается как внешний произвольный JWS: materializer
генерирует отличающиеся `CURRENT`/`NEXT` ES256 signer keys, подписывает bundle
manifest signer ключом из offline ceremony и ограничивает его срок действия.

## Release и dark deploy

1. После owner policy readback owner вручную запускает `Build exact release` с
   exact lowercase 40-hex SHA, совпадающим с pinned workflow SHA и текущим
   `main`.
2. Сохранить `build_run_id` и SHA-256 файла `release-lock.json`.
3. Проверить lock через `tools/release/validate-release-lock.sh`.
4. Вручную запустить `Deploy exact production release` с теми же SHA, run ID,
   lock digest и `mode=dark`.
5. Проверить `mattercodex-release-lock`, exact resource set, завершение migration
   Jobs, Ready/Available foundation и application workloads, Bound PVC, exact
   running imageID digest и отсутствие Ingress в `mattercodex-system`.
6. Сверить, что Deployment/StatefulSet/Ingress в `matter-kodex-prod` не менялись.

Release lock связывает source SHA, build run, закрытый список компонентов,
repository, image digest и node pull reference. Deploy дополнительно проверяет
provenance исходного workflow run и artifact digest.
Artifact uploader закреплён на proxy-safe `actions/upload-artifact` v7.0.1:
эта версия не пересылает request headers Envoy proxy при установлении HTTPS
CONNECT к exact GitHub Actions blob storage destination.

Preflight до первой мутации проверяет все фактически используемые
`get|list|watch|create|patch|update` permissions, exact-name delete для migrations,
запрет `Secret`/`pods/log`, server-side admission всего render и отрицательный
forged Secret mount. Повторный release проверяет migration Jobs через
server-side dry-run delete/recreate без фактического удаления.

## Secrets

Prototype временно использует materialized Kubernetes Secrets. Скрипт
`tools/deploy/bootstrap-direct-production-secrets.sh` генерирует отсутствующие
значения через `openssl rand`, передаёт их в Kubernetes из файлов и не выводит.
Существующие Secrets не ротируются автоматически; неожиданный набор ключей
закрыто останавливает операцию. Secret/CA bootstrap отделён от routine deploy.
Полный Vault lifecycle — #256.

Перед owner gate закрытый набор application interfaces классифицируется без
получения значений credentials:

```bash
umask 077
tools/deploy/classify-direct-production-application-material.sh \
  --output /secure/path/application-material-classification.json \
  --context EXACT_CONTEXT
```

Классификатор заново получает interface render из текущего checkout и требует
ровно 130 Secret и 19 ConfigMap в `mattercodex-system`. Для текущей revision это
68 криптографически генерируемых, 70 детерминированно выводимых, 2 полностью
безопасно переиспользуемых и 9 внешних ресурсов. Внешний фрагмент принимается
только как exact closed set из 9 ресурсов и 14 ключей:
лишний, отсутствующий или пустой ключ, `stringData`, другой namespace либо
неизвестный kind приводят к закрытому отказу. Значения не включаются в отчёт.
Фрагмент с правами слабее `0600` проверяется отдельным запуском с
`--external-material-file /secure/path/external.yaml` и также отклоняется.

Проверка `--context` читает только наличие и точные имена ключей разрешённых
legacy source Secret. Она не выводит значения и не изменяет
`matter-kodex-prod`. Классификация сама по себе не является materialization.

`tools/deploy/materialize-direct-production-application.sh` объединяет все
68 `cryptographically_generated`, 70 `deterministically_derived`, два полностью
`safely_reusable_from_existing_binding` и три exact reusable bindings. Он
использует `umask 077`, secure temporary directory,
pinned `nsc`, operator/account JWT и минимальные NATS user permissions; создаёт
owner-only password store для 29 поколенческих PostgreSQL LOGIN principals и
verify-full DSN с exact hostname/CA; подписывает TLS identities общей prototype
CA; проверяет compact JWS/JWK/JWKS, ARN, CA и mapping digest semantics. Любой
неизвестный resource/key или неполный internal set отклоняется.

Client CA для `integration-gateway` и `interaction-gateway` детерминированно
копируется из единственного external binding
`ConfigMap/internal-rpc-authority-vault-ca`; отдельное значение у владельца не
запрашивается. Семь target `*-manifest-trust/bundle.jws` создаются пустыми как
publisher-owned resources и заполняются publisher из одного проверенного
external manifest root bundle. Для них запрещён owner-supplied дубликат.

Foundation публикует NATS только через TLS и account resolver без basic-auth.
PostgreSQL principal Job исполняется до migrations и повторно после них, затем
закрывает прежние g1/g2 publisher/readback principals через `NOLOGIN` и
termination. Mattermost и bot-service доступны application clients только через
двухрепличные namespaced Envoy mTLS bridges. Их единственный plaintext hop имеет
exact legacy namespace/Pod selector/port; legacy workloads и Services не
изменяются. После migrations routine deploy сначала запускает publisher и
readback-attestor, ждёт непустой `snapshot.jws`, и только затем запускает
остальные consumers.

Principal bootstrap создаёт Goose metadata table до мигратора, назначает ей
ограниченные права и атомарно добавляет единственную applied baseline-запись
`version_id=0`, только если таблица пуста. Поэтому чистая БД и повторный запуск
используют один путь: существующая migration history не переписывается, а
пустая предсозданная таблица не приводит к `no next version found`.

Direct-production render задаёт только сочетание
`INTERNAL_RPC_AUTHORITY_SECRET_BACKEND=direct-production-kubernetes-file` и
`INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE=direct-production-single-node-prototype`.
Любой другой backend, профиль или сочетание закрыто отклоняется; автоматического
перехода с Vault на Kubernetes нет. Обычные профили сохраняют Vault semantics.

В prototype publisher выполняет только `get` и `update` заранее созданных exact
Secret из закрытого target registry и подтверждает `resourceVersion`, semantic
version, canonical digest и полный readback. Direct-production registry содержит
только постоянные runtime target. Завершённая одноразовая
`legacy-data-migration` исключена из routine rotation и возвращается только
отдельной owner-approved migration wave по `RUN-MC-014`. Publisher Role
выводится из registry; иные verbs и `list|watch|create|delete|patch`
отсутствуют. Wave A активирует два
profile-selected target с отдельными identity и Secret:

- `verifier/integration-gateway` — четыре verifier-документа в
  `internal-rpc-authority-integration-gateway-verifier-delivery`;
- `issuer/runtime-controller` — четыре issuer-документа в
  `internal-rpc-authority-runtime-controller-issuer-delivery`.

`issuer/runtime-s3-restore-exchanger` исключён из exact dark registry вместе с
delivery Secret и PostgreSQL principal. Routine sidecar и init container
получают только read-only file mount, не получают Kubernetes API token и не
могут адресовать произвольный Secret/path.

Reconciler получает `get` только на
exact g1–g5 publisher/readback credential Secrets и `get|update` только на
`internal-rpc-authority-prototype-static-role-state`; состояние связывает
source revision/digest, principal, generation, status и monotonic high-watermark. g1/g2 обязаны быть
одновременно `NOLOGIN`, отозваны в PostgreSQL и недоступны как Secret.
Authority sidecar не получает Kubernetes API authority и читает четыре exact
JSON-документа своего target из read-only Secret volume; resolver получает ещё
два exact readback-документа. Routine deployer не получает этих прав.

Owner materializer заранее создаёт exact publisher/reconciler-owned Secret и
допустимые пустые ключи, а затем сохраняет runtime-owned keys при повторном
запуске. Неизвестный
Secret, key, logical path, operation, digest, generation или CAS conflict
приводит к закрытому отказу. Readiness authority использует тот же backend
readback, что и рабочая публикация.

Если до первого публичного cutover неудачная bootstrap-попытка успела записать
authority revision, но не создала ни одного delivery receipt и snapshot
readback, повторный запуск с более новой registry revision закрыто отклоняется.
Для этого единственного pre-cutover случая используется owner-gated команда:

```bash
KUBECONFIG=/secure/path/kubeconfig \
  tools/deploy/reset-direct-production-authority-bootstrap.sh \
  --owner-approved \
  --revision EXACT_GIT_SHA \
  --context EXACT_CONTEXT
```

Команда сверяет чистый exact checkout, отсутствие публичного ingress
`control.kodex.works`, отсутствие обслуженного authority state, останавливает
только зависящие от authority workloads, пересоздаёт только новую БД
`internal_rpc_authority` из чистого `template0` и удаляет только
publisher/reconciler-owned runtime
material из закрытых policy. После появления хотя бы одного delivery/readback
или публичного ingress этот путь необратимо запрещён. Затем владелец повторяет
application materialization и routine dark deploy. Команда не предназначена
для rollback, ротации либо восстановления работающего контура.

В direct-production application runtime adapters A–D заменяют живой Vault
на закрытые Kubernetes/file границы:

| Consumer                                   | Exact binding                                                                                                                       | Authority и readback                                                                                                                                                                      |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `integration-gateway` provider store       | `Secret/integration-gateway-provider-credentials`, единственный key `state.json`                                                    | main container получает projected token с API-server audience по умолчанию; поле `audience` намеренно отсутствует для совместимости с настройкой конкретного кластера; Role даёт ровно `get` и `update` одного Secret; `resourceVersion` CAS, generation, canonical digest и readback проверяются adapter и materializer |
| `integration-gateway` Git store            | `Secret/integration-gateway-git-credentials/state.json`                                                                             | owner-materialized read-only aggregate с exact `credential_secret_ref`/version из repository catalog; API token для чтения не используется                                                |
| `integration-gateway` OIDC verifier        | `ConfigMap/integration-gateway-oidc-provider`: `provider-snapshot.json`, `provider-snapshot.sha256`, `provider-snapshot.generation` | owner-materialized public JWKS snapshot; только `RS256`, exact issuer/audience, unique public `kid`, pinned generation и canonical SHA-256; network discovery для этого consumer отключен |
| `interaction-gateway` bot credential store | `Secret/interaction-gateway-bot-credentials`, единственный key `state.json`                                                         | отдельные ServiceAccount и Role с ровно `get` и `update`; UUID binding, immutable active value, monotonic revoke, CAS/digest/readback                                                       |

Publisher и readback generation principals не получают capability roles от
owner bootstrap. Их `LOGIN` и membership полностью принадлежат fenced database
credential lifecycle; bootstrap выдаёт lifecycle definer только bounded
`ADMIN OPTION` без `INHERIT` и `SET`. Повторный owner bootstrap обновляет пароли
generation principals, но сохраняет их текущее `LOGIN`/`NOLOGIN` состояние и
не создаёт и не отзывает их capability membership. После migrations bootstrap удаляет
дубли bounded administrator membership от migration principals, сохраняя
активной owner-цепочку, и подтверждает одну каноническую grant от `postgres` с
точными option. Поэтому удаление дубля не каскадирует generation grants,
выданные lifecycle reconciler.

Два writable aggregate Secret заранее создаются materializer с
`schema_version=1`, `generation=1`, пустым `records` и верным digest.
Повторная materialization сохраняет уже обслуживаемое CAS состояние.
VAP и render фиксируют exact token/CA/file volumes; sidecar и init containers
эти mounts не получают. Kubernetes API egress выводится в bootstrap из
фактических `Service/kubernetes` и ready EndpointSlice как exact `/32`.
`list|watch|create|delete|patch`, `pods/log`, Certificate и cluster-scoped доступа нет.
Authority publisher/reconciler/verifier/issuer также используют direct
Kubernetes/file profiles; их игнорируемые Vault env/mounts удалены из
итогового render.

Owner-approved Wave A dark profile явно задаёт
`CONTROL_PLANE_RUNTIME_ARCHIVE_RESTORE_CAPABILITY=disabled` и
`RUNTIME_ARCHIVE_RESTORE_CAPABILITY=disabled`. В этом профиле:

- control-plane не читает archive/restore signing keys и не выпускает их
  workload tickets;
- runtime-controller отклоняет restore materialization и не создаёт archive,
  restore, rehydrate либо S3 credential broker Jobs;
- admission закрыто отклоняет соответствующие Pod, Secret и action, а
  cluster VAP запрещает создать такие workload, identity, RBAC и egress;
- exact render не содержит archive/restore Deployment, Service,
  ServiceAccount, Role/RoleBinding, NetworkPolicy, authority sidecar, init,
  provider profile, management identity, HMAC, KMS/role material или readiness
  expectation;
- dynamic Jobs не получают storage/profile/KMS env. Scale-to-zero не считается
  выключением capability.

Foundation S3-compatible storage остаётся только для активных instruction и
interaction consumers. Routine workloads не получают management/STS endpoint.
Включать archive/restore, добавлять AIStor license/entitlement либо выдавать
static/shared/root credential запрещено до обязательного follow-up
[#310](https://github.com/codex-k8s/matter-codex/issues/310). Там выбирается
лицензированный AIStor либо поддерживаемый OSS backend, фиксируются immutable
image/digest, per-execution TTL `900`, policy intersection, immediate terminal/
cancel/delete/retry revoke/readback, KMS и передача storage/profile/KMS env в
dynamic Jobs. Cutover остаётся запрещён.

Active classification содержит 149 resources: 68
`cryptographically_generated`, 70 `deterministically_derived`, 2
`safely_reusable_from_existing_binding` и 9 `truly_external_credential`.
Canonical classification SHA-256 —
`54381587e31a411d239b625423e38f70fa97b99449789b23ccf7b1bdbe4bd816`.
Внешний closed set состоит из 9 resources / 14 keys. В нем остаются Mattermost mapping,
`integration-gateway-provider-health-credential`, Git aggregate, OIDC provider
snapshot и `mattercodex-oidc-ca` для сетевых OIDC consumers control plane.
Readiness application grants и их ES256 trust roots генерируются владельцем
профиля. Отдельный `application-grant-rotator` обновляет пять readiness-only
grants раз в минуту с TTL четыре минуты и имеет `patch` только на их точные
Secret names. Рабочие grants с session/turn/process authority этим bootstrap
не заменяются.
Runtime archive/restore KMS/role identifiers в external fragment отсутствуют.
Внутренние credential в этот
set не входят. Отсутствие любого key отклоняет materializer.

Context7 при первоначальной проверке runtime storage boundary был недоступен по
квоте; решение Wave A не утверждает конкретный storage backend. Исследование и
официальные источники фиксируются в #310 вместе с выбором backend.

## Bounded smoke

Признаки успеха dark deploy:

- StatefulSet PostgreSQL, Redis, NATS JetStream и S3-compatible storage готовы;
- `control-plane-broker-bootstrap` создал либо подтвердил exact поток
  `CONTROL_PLANE` с одной репликой и лимитом `4 GiB`;
- endpoint instruction object store использует exact TLS port `9000`, а
  NetworkPolicy разрешает тот же endpoint;
- bucket reconciler завершает readiness;
- все migration Jobs завершены, а application Deployment готовы и доступны;
- фактический набор release-managed объектов совпадает с render без
  отсутствующих или лишних объектов;
- readback подтверждает `disabled` у обоих capability selector и отсутствие
  archive/restore workload, identity, Secret, egress и readiness ожиданий;
- running imageID каждого внутреннего контейнера совпадает с digest lock;
- release lock readback совпадает с requested SHA/digest;
- в новом namespace нет Ingress;
- legacy workloads и traffic path неизменны.

Это не integration/E2E, HA, archive/restore readiness, backup restore drill или подтверждение hardened
supply chain. `role-image-builder` и динамический `agent-runner` намеренно не
запускаются в dark до materialization hardened supply chain #256; их образы
остаются в release lock, но не выдаются за работающий контур. Для
`role-image-builder` release build выбирает exact Dockerfile target `runtime`:
deferred `admission-runtime` с внешним `ADMISSION_TOOLS_IMAGE` в Wave A не
собирается и не подменяется placeholder-образом. Наблюдаемость —
#254, HA/DR — #255, supply chain/Vault — #256,
поддерживаемый полный тестовый контур — #216.

Foundation PostgreSQL использует только `hostssl` с SCRAM и явный
`hostnossl reject`; PostgreSQL и Redis probes проходят authenticated TLS с
точным service hostname/SNI и CA. В single-node профиле bootstrap читает
единственный IPv4 `PodCIDR` и материализует его как ingress boundary для
межподовых соединений: это переносимый fallback для CNI, которые не сохраняют
source pod identity при проверке ingress. Исходящий путь остается ограничен
точными destination selectors и портами, а identity подтверждают TLS,
PostgreSQL roles и прикладные credentials. Build registry path допускает только Service port `5000`; Kubernetes
API для controller/listener/deployer материализуется как read-only discovered
exact `/32` destinations. Единственное внешнее `0.0.0.0/0:443` принадлежит
изолированному allowlist proxy без application credentials; application/build
Pods имеют egress только к самому proxy.

## Rollback

Риск Wave A — terminal session PVC остаётся retained и не получает durable S3
archive; storage lifecycle сознательно неполон до #310. Включение capability
требует нового code-first профиля и отдельного owner gate.

Dark rollback повторно применяет ранее сохранённый и валидированный exact release
lock с `mode=rollback` и отдельным owner gate. Он не выполняет schema down,
миграцию legacy data, удаление retained PVC или изменение legacy traffic. Если foundation
не стартует, прекращают rollout и исправляют вперёд; удаление новых ресурсов или
данных возможно только отдельным явно подтверждённым действием.
