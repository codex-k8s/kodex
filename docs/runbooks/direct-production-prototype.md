---
id: RUN-MC-015
title: Direct-production single-node prototype
type: runbook
status: approved
owner: sre
version: 1.4.0
updated: 2026-08-10
---

# Direct-production single-node prototype

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
ровно 117 Secret и 20 ConfigMap в `mattercodex-system`. Для revision, на которой
введена policy, это 52 криптографически генерируемых, 60 детерминированно
выводимых, 2 полностью безопасно переиспользуемых и 23 внешних ресурса. Внешний
фрагмент принимается только как exact closed set из 23 ресурсов и 44 ключей:
лишний, отсутствующий или пустой ключ, `stringData`, другой namespace либо
неизвестный kind приводят к закрытому отказу. Значения не включаются в отчёт.
Фрагмент с правами слабее `0600` проверяется отдельным запуском с
`--external-material-file /secure/path/external.yaml` и также отклоняется.

Проверка `--context` читает только наличие и точные имена ключей разрешённых
legacy source Secret. Она не выводит значения и не изменяет
`matter-kodex-prod`. Классификация сама по себе не является materialization.

`tools/deploy/materialize-direct-production-application.sh` объединяет все
52 `cryptographically_generated`, 60 `deterministically_derived`, два полностью
`safely_reusable_from_existing_binding` и частичные reusable bindings внутри
двух external ресурсов. Он использует `umask 077`, secure temporary directory,
pinned `nsc`, operator/account JWT и минимальные NATS user permissions; создаёт
owner-only password store для 29 поколенческих PostgreSQL LOGIN principals и
verify-full DSN с exact hostname/CA; подписывает TLS identities общей prototype
CA; проверяет compact JWS/JWK/JWKS, ARN, CA и mapping digest semantics. Любой
неизвестный resource/key или неполный internal set отклоняется.

Foundation публикует NATS только через TLS и account resolver без basic-auth.
PostgreSQL principal Job исполняется до migrations и повторно после них, затем
закрывает прежние g1/g2 publisher/readback principals через `NOLOGIN` и
termination. Mattermost и bot-service доступны application clients только через
двухрепличные namespaced Envoy mTLS bridges. Их единственный plaintext hop имеет
exact legacy namespace/Pod selector/port; legacy workloads и Services не
изменяются. После migrations routine deploy сначала запускает publisher и
readback-attestor, ждёт непустой `snapshot.jws`, и только затем запускает
остальные consumers.

Текущий application render всё ещё требует исполняемый Vault endpoint
`vault.mattercodex-system.svc:8200`: publisher выполняет startup publication
через `VaultStaticRoleManager`/`SecretDelivery`, а authority sidecars используют
тот же delivery path. Одной внешней CA или Kubernetes Secret недостаточно.
Bootstrap закрыто останавливается до первой мутации, если exact Service и Ready
EndpointSlice отсутствуют. Для утверждённого Kubernetes-Secret prototype нужен
узкий Go-проход: явный profile-selected file/Kubernetes adapter для
`VaultStaticRoleManager` и `SecretDelivery` с закрытым path registry, digest
readback и fail-closed rotation; после него Vault preflight удаляется. Разворачивать
неутверждённый dev Vault или подменять ответы синтаксическими значениями запрещено.

## Bounded smoke

Признаки успеха dark deploy:

- StatefulSet PostgreSQL, Redis, NATS JetStream и S3-compatible storage готовы;
- bucket reconciler завершает readiness;
- все migration Jobs завершены, а application Deployment готовы и доступны;
- фактический набор release-managed объектов совпадает с render без
  отсутствующих или лишних объектов;
- running imageID каждого внутреннего контейнера совпадает с digest lock;
- release lock readback совпадает с requested SHA/digest;
- в новом namespace нет Ingress;
- legacy workloads и traffic path неизменны.

Это не integration/E2E, HA, backup restore drill или подтверждение hardened
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
точным service hostname/SNI и CA. Data policies содержат одновременно точные namespace и Pod
selectors. Build registry path допускает только Service port `5000`; Kubernetes
API для controller/listener/deployer материализуется как read-only discovered
exact `/32` destinations. Единственное внешнее `0.0.0.0/0:443` принадлежит
изолированному allowlist proxy без application credentials; application/build
Pods имеют egress только к самому proxy.

## Rollback

Dark rollback повторно применяет ранее сохранённый и валидированный exact release
lock с `mode=rollback` и отдельным owner gate. Он не выполняет schema down,
миграцию legacy data, удаление PVC или изменение legacy traffic. Если foundation
не стартует, прекращают rollout и исправляют вперёд; удаление новых ресурсов или
данных возможно только отдельным явно подтверждённым действием.
