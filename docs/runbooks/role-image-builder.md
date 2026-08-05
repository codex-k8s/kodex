---
id: RUN-MC-009
title: Диагностика role-image-builder
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-05
---

# Диагностика role-image-builder

Runbook не разрешает deploy, promotion, удаление registry content или ручное
изменение owner state. Не выводить application grants, lease/claim tokens,
installation block, context contents, Docker credentials, TLS keys и secret
values.

## Read-only preflight

1. Зафиксировать exact Git SHA и проверить readiness `role-image-builder`, его
   issuer sidecar, `control-plane`, rootless BuildKit и четыре registry scope.
2. Сверить, что builder Pod использует ServiceAccount `role-image-builder`,
   `automountServiceAccountToken: false`, read-only context PVC и не имеет
   promotion/admin/node-pull credentials или egress.
3. Проверить owner metadata `RoleImageRecipe`/`ImageBuild`/`ImageArtifact` через
   защищённый API без чтения installation block или claim tokens.
4. Сверить exact policy revision/digest, staging reference, manifest digest,
   provenance digest и current attempt/fence. Payload ID или annotation не
   являются доказательством полномочий.

## Типовые отказы

- `CONTEXT_INVALID`: проверить exact digest имени tar, source sentinel,
  traversal/symlink/special-file. Содержимое context не печатать.
- `BUILDKIT_FAILED`: проверить exact SNI/CA/client certificate, rootless worker
  readiness, base-pull и staging-push scope. Insecure fallback запрещён.
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
  signature, policy revision/digest и promotion readback. Ослаблять проверку в
  runtime-controller запрещено.

## Rollback

Вернуть workload image на ранее утверждённый exact digest и остановить новые
claims. Policy, generation и promoted registry content откатывать нельзя.
Незавершённые claims закрываются только owner lifecycle командами.
