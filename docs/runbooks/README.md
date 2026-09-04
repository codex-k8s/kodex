---
id: RUN-MC-001
title: Runbooks Kodex
type: runbook-index
status: approved
owner: sre
version: 2.1.0
updated: 2026-09-04
---

# Runbooks Kodex

- `fresh-install.md` — безопасная подготовка новой web-first установки;
- `identity-and-management-surfaces.md` — Keycloak, OAuth2 Proxy, Grafana,
  Headlamp и owner recovery;
- `control-plane.md` — schema, bootstrap, events и owner state;
- `control-api-gateway.md` — OIDC/CSRF/Origin, HTTP и resumable WebSocket;
- `runtime-controller.md` — role Pod и always-hot assistant;
- `agent-runner.md` — выполнение внутри promoted role image;
- `role-image-builder.md` и `image-supply-chain.md` — build/admission/promotion;
- `integration-gateway.md` — managed typed MCP integrations;
- `local-integration-fixture.md` — local-only CRUD, approval и retry fixture;
- `interaction-gateway.md` — optional Mattermost adapter;
- `automation-scheduler.md` — schedule occurrence worker;
- `artifact-retention.md` — 30-day trash retention и exact object version purge;
- `session-archive.md` — snapshot/restore session JSONL, guarded PVC cleanup и GC;
- `secret-broker.md` — Runtime Secret operations, recovery и exact cleanup;
- `stt-tts-service.md` — STT authority/projection/OpenAI path без вывода
  аудио, transcript или credential;
- `internal-rpc-authority.md` и `egress-gateway.md` — security boundaries.

Runbooks не разрешают merge, deployment, reset или доступ к live credentials.
Изменение среды выполняет владелец отдельно, после read-only preflight, теми же
versioned manifests/scripts из репозитория.
