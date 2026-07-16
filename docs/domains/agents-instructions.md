---
id: DOM-MC-004
title: Agents & Instructions
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Agents & Instructions

## Назначение

Описывает reusable roles, конкретных ИИ-сотрудников, их assignments, prompts и versioned instruction bundles.

## В границах

- RoleDefinition catalog;
- Agent identity и presentation;
- workspace/room assignments;
- prompt template и locale policy;
- InstructionSet и versions;
- effective instruction composition;
- improver proposals.

## Instruction sources

- Git repository (`AGENTS.md` и связанные documents);
- UI-managed Markdown;
- GitOps-managed catalog;
- uploaded artifact bundle;
- integration-provided read-only knowledge.

Runtime materializer создает root `AGENTS.md` и связанные файлы независимо от наличия repository checkout.

## Prompt composition

Порядок контекста:

1. platform safety/runtime instruction;
2. RoleDefinition template, если задан;
3. locale и communication requirements;
4. InstructionSet manifest;
5. Workspace/Room context;
6. integrations/tools reference;
7. attachments manifest;
8. user/schedule/delegation instruction.

Пустой role template является валидным raw instruction mode.

Если `AGENTS.md` не задает язык, выбранная user/workspace locale применяется к ответам, PR/issues/comments, документации и code comments, где это уместно.

## Improver

Improver не изменяет canonical instructions напрямую. Он создает version proposal/PR с evidence categories, но без secret/raw private content. Proposal проходит review и human gate.

## Acceptance

- Один RoleDefinition переиспользуется несколькими Agents.
- Каждый Agent имеет отдельную bot identity.
- Instructions работают без Git repository.
- Измененная version применяется со следующего turn.
- Prompt preview показывает sources и порядок без secret values.
- Owner может откатить instruction version.
