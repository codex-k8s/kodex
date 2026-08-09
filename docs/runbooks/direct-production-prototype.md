---
id: RUN-MC-015
title: Direct-production single-node prototype
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-09
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
нужен отдельный owner gate на каждый production bootstrap/build/deploy. Gate
состоит из GitHub Environment с обязательным reviewer, запретом self-review и
branch policy `main`; строка input/env не является доказательством допуска. В Wave A
исполняется только `dark`. Режим `cutover` закрыто отклоняется до закрытия #241,
#237, #194, отдельного owner gate и материализации cutover manifest. Наличие
режима workflow не является разрешением на переключение.

## Code-first bootstrap

Сначала owner с GitHub organization administration настраивает две Environment и
два non-default runner group, каждый с единственным разрешённым workflow из
`refs/heads/main`:

```bash
infra/github/bootstrap-actions-policy.sh --mode preflight \
  --build-reviewer-team-id-file PATH --deploy-reviewer-team-id-file PATH
infra/github/bootstrap-actions-policy.sh --mode apply \
  --build-reviewer-team-id-file PATH --deploy-reviewer-team-id-file PATH
infra/github/bootstrap-actions-policy.sh --mode readback \
  --build-reviewer-team-id-file PATH --deploy-reviewer-team-id-file PATH
```

Затем одноразовый операторский ARC bootstrap требует cluster-admin и не
выполняется routine runner:

```bash
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode preflight
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode apply \
  --github-app-id-file PATH --github-app-installation-id-file PATH \
  --github-app-private-key-file PATH
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode readback
```

Значения GitHub App передаются только файлами. Скрипт не печатает их и не
заменяет существующие Secrets. Bootstrap создаёт два namespace, два независимых
controller и scale set, default-deny NetworkPolicy, allowlisted egress proxy,
динамически привязанный exact Kubernetes API egress и admission allowlist.
`mattercodex-build` не получает Kubernetes token; `mattercodex-deploy` получает
только production namespaced RBAC. `readback` требует Ready controller/listener,
ровно один idle EphemeralRunnerSet, repository URL, runner group, scale bounds,
ServiceAccount и отрицательную admission-проверку.

Build namespace имеет Pod Security audit/warn `restricted`, но enforce
`privileged`, потому что зафиксированный upstream rootless BuildKit требует
`hostUsers=false`, `procMount=Unmasked` и unconfined AppArmor/seccomp. Это не
расширяет произвольные workloads: fail-closed ValidatingAdmissionPolicy допускает
только exact ARC/controller/runner/proxy identities, ServiceAccount, volumes и
pinned images. Deploy и application namespace используют enforce `restricted`.

Отдельный owner-controlled production bootstrap применяет namespace security,
ServiceAccount/RBAC, Secret/CA interfaces и admission allowlist. Routine deploy
не читает и не создаёт Secrets/Certificates:

```bash
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode preflight
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode apply \
  --application-material-file PATH
infra/direct-production/bootstrap.sh --context EXACT_CONTEXT --mode readback
```

Файл materialized Secrets/CA ConfigMaps передаётся только локальным путём, не
журналируется и должен содержать точное закрытое множество application
file/env/TLS interfaces. Bootstrap
генерирует внутренние foundation credentials, проверяет готовность exact TLS
Certificates, отсутствие пустых Secret data и пишет безопасный
`mattercodex-bootstrap-readiness` без значений credentials.

## Release и dark deploy

1. После Environment approval вручную запустить `Build exact release` с exact
   lowercase 40-hex SHA, совпадающим с текущим `main`.
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

## Secrets

Prototype временно использует materialized Kubernetes Secrets. Скрипт
`tools/deploy/bootstrap-direct-production-secrets.sh` генерирует отсутствующие
значения через `openssl rand`, передаёт их в Kubernetes из файлов и не выводит.
Существующие Secrets не ротируются автоматически; неожиданный набор ключей
закрыто останавливает операцию. Secret/CA bootstrap отделён от routine deploy.
Полный Vault lifecycle — #256.

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
остаются в release lock, но не выдаются за работающий контур. Наблюдаемость —
#254, HA/DR — #255, supply chain/Vault — #256,
поддерживаемый полный тестовый контур — #216.

## Rollback

Dark rollback повторно применяет ранее сохранённый и валидированный exact release
lock с `mode=rollback` и отдельным owner gate. Он не выполняет schema down,
миграцию legacy data, удаление PVC или изменение legacy traffic. Если foundation
не стартует, прекращают rollout и исправляют вперёд; удаление новых ресурсов или
данных возможно только отдельным явно подтверждённым действием.
