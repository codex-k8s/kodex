---
id: RUN-MC-001
title: Runbooks MatterCodex
type: runbook-index
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Runbooks MatterCodex

- `fresh-install.md` — безопасная подготовка новой web-first установки;
- `control-plane.md` — schema, bootstrap, events и owner state;
- `control-api-gateway.md` — OIDC/CSRF/Origin, HTTP и resumable WebSocket;
- `runtime-controller.md` — role Pod и always-hot assistant;
- `agent-runner.md` — выполнение внутри promoted role image;
- `role-image-builder.md` и `image-supply-chain.md` — build/admission/promotion;
- `integration-gateway.md` — managed typed MCP integrations;
- `interaction-gateway.md` — optional Mattermost adapter;
- `automation-scheduler.md` — schedule occurrence worker;
- `internal-rpc-authority.md` и `egress-gateway.md` — security boundaries.

Runbooks не разрешают merge, deployment, reset или доступ к live credentials.
Изменение среды выполняет владелец отдельно, после read-only preflight, теми же
versioned manifests/scripts из репозитория.
