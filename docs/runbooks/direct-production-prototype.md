---
id: RUN-MC-015
title: Direct-production single-node prototype
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-09
---

# Direct-production single-node prototype

## Назначение

Профиль `direct-production single-node prototype` не имеет staging. Первый выпуск
создаёт в существующем production-кластере изолированные namespace:

- `mattercodex-ci` — ARC controller и repository-scoped ephemeral scale sets;
- `mattercodex-system` — новый dark-контур без Ingress и пользовательского трафика.

Legacy Mattermost, PostgreSQL, bot-service, Kaniko и registry в
`matter-kodex-prod` не изменяются. Они остаются авторитетным пользовательским
путём и rollback path. Новый build публикует в существующий Service
`matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000`, а workloads
используют node pull endpoint `localhost:5001` только с digest.

## Шлюзы

До merge допускаются только read-only preflight и локальный render. После merge
нужен отдельный owner gate на каждый production bootstrap/build/deploy. В Wave A
исполняется только `dark`. Режим `cutover` закрыто отклоняется до закрытия #241,
#237, #194, отдельного owner gate и материализации cutover manifest. Наличие
режима workflow не является разрешением на переключение.

## Code-first bootstrap

Одноразовый операторский шаг требует cluster-admin и не выполняется routine
runner:

```bash
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode preflight
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode apply \
  --github-app-id-file PATH --github-app-installation-id-file PATH \
  --github-app-private-key-file PATH
infra/arc/bootstrap.sh --context EXACT_CONTEXT --mode readback
```

Значения GitHub App передаются только файлами. Скрипт не печатает их и не
заменяет существующий Secret. Bootstrap создаёт namespace/RBAC, устанавливает
зафиксированные ARC charts и два scale set: `mattercodex-build` без Kubernetes
token и `mattercodex-deploy` с namespaced RBAC.

## Release и dark deploy

1. Вручную запустить `Build exact release` с exact lowercase 40-hex SHA.
2. Сохранить `build_run_id` и SHA-256 файла `release-lock.json`.
3. Проверить lock через `tools/release/validate-release-lock.sh`.
4. Вручную запустить `Deploy exact production release` с теми же SHA, run ID,
   lock digest и `mode=dark`.
5. Проверить `mattercodex-release-lock`, readiness foundation и отсутствие
   Ingress в `mattercodex-system`.
6. Сверить, что Deployment/StatefulSet/Ingress в `matter-kodex-prod` не менялись.

Release lock связывает source SHA, build run, закрытый список компонентов,
repository, image digest и node pull reference. Deploy дополнительно проверяет
provenance исходного workflow run и artifact digest.

## Secrets

Prototype временно использует materialized Kubernetes Secrets. Скрипт
`tools/deploy/bootstrap-direct-production-secrets.sh` генерирует отсутствующие
значения через `openssl rand`, передаёт их в Kubernetes из файлов и не выводит.
Существующие Secrets не ротируются автоматически; неожиданный набор ключей
закрыто останавливает операцию. Полный Vault lifecycle — #256.

## Bounded smoke

Признаки успеха dark deploy:

- StatefulSet PostgreSQL, Redis, NATS JetStream и S3-compatible storage готовы;
- bucket reconciler завершает readiness;
- release lock readback совпадает с requested SHA/digest;
- в новом namespace нет Ingress;
- legacy workloads и traffic path неизменны.

Это не integration/E2E, HA, backup restore drill или подтверждение hardened
supply chain. Наблюдаемость — #254, HA/DR — #255, supply chain/Vault — #256,
поддерживаемый полный тестовый контур — #216.

## Rollback

Dark rollback повторно применяет ранее сохранённый и валидированный exact release
lock с `mode=rollback` и отдельным owner gate. Он не выполняет schema down,
миграцию legacy data, удаление PVC или изменение legacy traffic. Если foundation
не стартует, прекращают rollout и исправляют вперёд; удаление новых ресурсов или
данных возможно только отдельным явно подтверждённым действием.
