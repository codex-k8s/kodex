---
id: ADR-MC-008
title: Role images и BuildKit
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-008. Role images и BuildKit

## Решение

RoleImageRecipe имеет canonical hash. Existing signed image digest переиспользуется; отсутствующий image собирается BuildKit в isolated builder contour. Typed package lists предпочтительнее shell; install script — administrator-reviewed escape hatch.

Kaniko исключается из production target как archived/unmaintained upstream.

Image допускается к runtime после SBOM, scan, provenance и signature gate. Runtime использует digest.

## Последствия

- Нужен OCI registry и build cache production profile.
- Builder отделяется от agent runtime и credentials.
- Изменение recipe автоматически меняет RuntimeRevision.
