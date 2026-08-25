---
id: SVC-DOC-001
title: Deployable-компоненты
type: service-index
status: approved
owner: developer
version: 1.0.0
updated: 2026-07-28
---

# Deployable-компоненты

- `internal/` — внутренние доменные Go-сервисы;
- `external/` — внешние gateway;
- `jobs/` — workers и периодические jobs;
- `staff/control-center/` — PWA владельца и операторов Kodex.

Каждый deployable имеет отдельный module/package boundary, Dockerfile,
Kubernetes profile, README и runbook.
