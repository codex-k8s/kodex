---
id: DOM-MC-005
title: Providers & Accounts
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Providers & Accounts

## Назначение

Абстрагирует AI runtime providers, authentication, account pools, model capabilities и usage/limit observations.

OpenAI/Codex device-code account является первым provider adapter, но универсальные domains не используют `auth.json` и `config.toml` как свои модели.

## В границах

- ProviderDefinition/capabilities;
- account registration/authorization/revocation;
- safe account labels/status;
- account pools и selection policies;
- model/runtime capability validation;
- usage observations и freshness;
- provider-specific materialization adapter.

## Account selection

Новая session выбирает account:

- явно пользователем;
- fixed agent binding;
- из pool по `least_used`, `weighted` либо будущей policy.

Кандидат должен быть enabled, authorized, разрешен Agent/Workspace, поддерживать model и иметь достаточно свежий health/limit status.

## Affinity

После первого turn session account immutable. Account нельзя подменить при resume. Reauthorization того же logical account обновляет auth revision. Перенос на другой account создает новую session и явный context handoff.

## Безопасность

- Raw auth хранится в secret backend.
- UI показывает label, provider identity, masked metadata, status и observation time.
- Token/account values отсутствуют в logs и prompt.
- Authorization diagnostics не трактуют временную provider error как expired auth без подтвержденного признака.

## Acceptance

- Несколько accounts работают одновременно.
- Новые sessions балансируются по policy.
- Existing session всегда возобновляется исходным account.
- Expired account дает actionable reauthorization UI.
- Stale limits помечаются как stale и не выдаются за актуальные.
