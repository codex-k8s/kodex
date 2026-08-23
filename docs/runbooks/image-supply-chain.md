---
id: RUN-MC-018
title: Диагностика автоматического admission образов ролей
type: runbook
status: approved
owner: sre
version: 1.0.1
updated: 2026-08-23
---

# Диагностика автоматического admission образов ролей

Runbook разрешает только read-only диагностику. Он не разрешает deploy,
promotion, создание phase Job вручную, изменение owner state, ослабление
admission policy или чтение secret values.

## Контракт

`image-admission-controller` автоматически поддерживает одну цепочку
`claim → scan → sign → admit` и отдельный ожидающий `promote`. Kubernetes
Job/PVC — устойчивый reconcile cursor, но не источник business lifecycle.
`control-plane` server-side выбирает artifact и выдаёт fenced claim каждой
защищённой фазе.

Controller имеет только Kubernetes API token с коротким TTL. Он не монтирует
application grant, registry, signing, Vault либо control-plane credentials.
Каждая phase Job запускается под собственной identity, а fail-closed
`ValidatingAdmissionPolicy` разрешает controller создать только точные images,
commands, env, volumes и ServiceAccount из immutable typed parameter resource.
Runtime читает отдельную immutable `ConfigMap`-проекцию; release render обязан
доказать точное равенство её `data` и `spec` typed resource.

## Read-only preflight

1. Зафиксировать exact Git SHA и release-lock SHA-256.
2. Проверить, что Deployment использует exact digest `image-admission`, одну
   replica со стратегией `Recreate`, `automountServiceAccountToken: false` и
   projected Kubernetes token не дольше 10 минут.
3. Проверить `/healthz` и cached `/readyz`. Probe не должен обращаться к
   `control-plane`, registry или другой business service.
4. Сверить Role: exact get immutable typed parameters и runtime `ConfigMap`;
   get/list/create Job; get/list/create/delete PVC. Secret, Pod, Deployment,
   RoleBinding, list/watch parameters и update/patch полномочия отсутствуют.
5. Сверить обе admission policy и binding: `failurePolicy: Fail`, действие
   `Deny`, exact controller username, namespace, typed `paramKind` и
   `parameterNotFoundAction: Deny`.
6. По metadata Job проверить одну активную admission chain, последовательность
   фаз, отдельный promotion и отсутствие чужих ServiceAccount. Не выводить env
   и volumes работающих Pod: достаточно сравнить canonical render в репозитории.

Диагностический render отдельной фазы не выполняет apply:

```bash
IMAGE_ADMISSION_POLICY_JSON='<read-only ConfigMap JSON without secrets>' \
  tools/render-image-admission-job.sh \
  production \
  v<UTC-YYYYMMDDHHMMSS>-<exact-release-git-sha> \
  claim \
  > /tmp/image-admission-claim.yaml
```

## Типовые отказы

- controller `/readyz` неготов: проверить только Kubernetes API reachability,
  RBAC readback и immutable policy revision. Соседний сервис не добавлять в
  readiness.
- Job отклонён admission policy: сравнить release-render с точным phase
  contract. Не расширять policy; исправить renderer либо несовпавший release
  material.
- `claim` завершён без работы: это bounded idle outcome. Controller создаст
  новую ожидающую phase после backoff; warning не должен повторяться на каждом
  опросе.
- `scan`, `sign` или `admit` failed: workspace удаляется guarded по UID,
  artifact остаётся непродвинутым, а повтор начинается с нового server-owned
  claim.
- `promote` failed: admission workspace не восстанавливать. Следующая phase
  получает свежий one-time promotion claim и durable evidence по exact OCI
  manifest digest.
- policy revision изменилась: новый run ID обязан включать новую revision;
  Job предыдущей revision не переиспользуется.

## Восстановление и rollback

Исправление выполняется только новым release render и Deployment rollout после
отдельного owner approval. Для rollback вернуть controller image на ранее
утверждённый exact digest, не откатывая policy revision, promoted artifacts или
owner state. Незавершённые claims закрывает только специализированный
`control-plane` lifecycle.
