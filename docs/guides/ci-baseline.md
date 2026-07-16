---
id: GUIDE-MC-006
title: CI baseline
type: guide
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# CI baseline

## Для каждого PR

- format/lint Markdown и проверка ссылок;
- `git diff --check`;
- Go format, vet/staticcheck, unit/integration tests по scope;
- Vue lint, typecheck, unit tests и build по scope;
- contract lint/breaking change/generated diff;
- migration validation;
- Helm lint/template/schema checks;
- secret scan;
- dependency/license scan;
- container build для измененных deployables;
- SBOM и vulnerability scan;
- PR description с automated и manual checks.

## Main/release

- build один раз и publish immutable image digest;
- подпись/provenance;
- deploy candidate environment;
- smoke и E2E;
- human environment gate;
- GitOps promotion того же digest;
- post-deploy verification;
- automatic abort/rollback для stateless workload при failed analysis.

## Generated code

CI запускает generators и требует clean worktree. Разница generated code без изменения source contract блокирует PR.

## Documentation-only PR

Не запускает дорогие image builds без причины, но обязательно проверяет Markdown, internal links, document metadata, forbidden secrets и consistency indexes.

## Human gate

CI success не заменяет owner acceptance. Merge action выполняется reviewer после финального owner OK в согласованном процессе результата.
