---
id: RUN-MC-009
title: Диагностика role-image-builder
type: runbook
status: approved
owner: sre
version: 1.2.1
updated: 2026-08-24
---

# Диагностика role-image-builder

Runbook не разрешает deploy, promotion, удаление registry content или ручное
изменение owner state. Не выводить application grants, lease/claim tokens,
installation block, context contents, Docker credentials, TLS keys и secret
values.

## Read-only preflight

1. Зафиксировать exact Git SHA и проверить readiness `role-image-builder`, его
   issuer sidecar, `control-plane`, userns BuildKit и четыре registry scope.
   У BuildKit проверить `hostUsers: false`, namespace-root и
   `privileged: true`; последнее допустимо только внутри Pod user namespace.
2. Сверить, что builder Pod использует ServiceAccount `role-image-builder`,
   `automountServiceAccountToken: false`, private bounded `emptyDir` и не имеет
   promotion/admin/node-pull credentials или egress.
3. Проверить owner metadata `RoleImageRecipe`/`ImageBuild`/`ImageArtifact` через
   защищённый API; полный installation block доступен только owner-scoped
   version-pinned recipe readback и не читается в обычном status path.
4. Сверить exact policy revision/digest, staging reference, manifest digest,
   provenance digest и current attempt/fence. Payload ID или annotation не
   являются доказательством полномочий.
5. Проверить `image-admission-controller`: `/healthz` отвечает за процесс,
   `/readyz` читает локальный readiness snapshot, ServiceAccount имеет только
   get/list/create Job/PVC и get immutable policy. Registry, Secret publisher и
   control-plane credentials в Pod отсутствуют.
6. Сверить обе `ValidatingAdmissionPolicy`: exact controller caller, Deny,
   immutable ConfigMap parameter и закрытые image/command/env/volume/
   ServiceAccount контракты. Не обходить policy ручным созданием phase Job.

## Типовые отказы

- `MATERIALIZATION_FAILED`: по bounded `diagnosticCode` различить
  `INPUT_FETCH_REJECTED`, `INPUT_DIGEST_MISMATCH` и `ARCHIVE_REJECTED`; проверить
  exact OCI manifest/layer media type, size/digest, mTLS SNI/CA и pull-only
  identity. Содержимое и credential не печатать.
- `BASE_PULL_FAILED`, `SOLVE_FAILED`, `INSTALLATION_FAILED`,
  `RUNTIME_FINALIZATION_FAILED` или
  `STAGING_PUSH_FAILED`: сверить соответствующую достигнутую фазу, userns
  readiness, trusted base/ABI и раздельные registry scopes. Raw BuildKit output
  и insecure fallback запрещены.
- истёкший lease: выполнить owner `EXPIRE`, затем `RETRY`; старая attempt не
  должна продолжать build или complete.
- admission `BLOCKED`: читать только bounded vulnerability verdict/evidence
  digests. Promotion для rejected artifact запрещён.
- promotion mismatch: оставить artifact непригодным, сверить exact digest и
  registry image manifest, staging/promoted OCI admission-receipt subject,
  owner-bound content/manifest digests и оба readback digest;
  не перепривязывать tag вручную.
- истёкший promotion claim: повторно запустить только claim/promote phase;
  admission PVC для этого не нужен; `control-plane` должен server-side выбрать
  artifact, повысить fence/generation, а старый claim отклонить.
- RuntimeRevision не создаётся: сверить promoted reference, admission receipt,
  signature, policy revision/digest, role runtime contract revision/digest и
  promotion readback. Затем проверить exact два init/три containers и тот же
  promoted repository в runtime-controller, broker, webhook и
  `ValidatingAdmissionPolicy`. Ослаблять проверку запрещено.
- node pull недоступен: сверить внешний DNS/SAN/CA, exact rendered node CIDR,
  отдельную node client identity, forward-only credential generation и
  готовность DaemonSet на каждом node. Открывать ingress `0.0.0.0/0`/`::/0`
  или использовать push/admin credential запрещено.

## Rollback

Вернуть workload image на ранее утверждённый exact digest и остановить новые
claims. Policy, generation и promoted registry content откатывать нельзя.
Незавершённые claims закрываются только owner lifecycle командами.
