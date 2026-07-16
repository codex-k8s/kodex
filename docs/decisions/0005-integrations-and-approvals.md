---
id: ADR-MC-005
title: Integration modes и approvals
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-005. Integration modes и approvals

## Решение

Поддерживаются два явных режима:

- direct runtime tool/env для доверенных capabilities без гарантированного human gate;
- managed MCP gateway для controlled/dangerous actions.

В managed mode credential остается в gateway. Capability имеет grants, constraints, risk class, approval policy и idempotency contract. Agent запускает других agents только через платформенный MCP, а bot mentions не являются execution trigger.

## Последствия

- YAML-MCP definitions можно переиспользовать как integration packages.
- Нельзя выдавать direct credential и одновременно обещать обязательный approval той же mutation.
- Integration Gateway становится security boundary и требует отдельного threat model.
