---
id: DOM-MC-010
title: Images & Supply Chain
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Images & Supply Chain

## Назначение

Владеет RoleImageRecipe, build request, immutable image digest, cache, SBOM, provenance, scan и signing status.

## Recipe

Recipe содержит:

- pinned base image reference/digest;
- target platforms;
- typed OS/language/tool packages;
- browser/testing capabilities;
- optional administrator-reviewed install script;
- build-time network/registry policy;
- metadata для prompt tools catalog.

Hash вычисляется по canonical recipe, base digest, build inputs, platform и builder version. Если signed image с таким hash доступен, повторная сборка не запускается.

## Builder

Kaniko не используется в production baseline, поскольку upstream архивирован. BuildKit выполняет build в отдельном namespace/service account. Rootless mode предпочтителен; privileged fallback допускается только изолированно и документируется.

Builder не получает production runtime credentials. Package registry token, если нужен, выдается как scoped short-lived build secret и не попадает в image layers/logs.

## Publication gate

Image доступен agents после:

- successful build;
- SBOM generation;
- vulnerability policy;
- provenance record;
- signature verification;
- push в approved OCI registry.

## Acceptance

- Одинаковый recipe переиспользует digest.
- Изменение script/tool/base меняет hash.
- Failed scan блокирует use и дает actionable status.
- Runtime запускает digest, а не mutable tag.
- Prompt tools list соответствует фактическому image manifest.
