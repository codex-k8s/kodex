---
id: OPS-MC-006
title: Deployment и rollback
type: operations
status: proposed
owner: sre
version: 0.1.0
updated: 2026-07-16
---

# Deployment и rollback

## Pipeline

1. PR checks и review результата.
2. Owner human gate.
3. Reviewer merge.
4. Immutable images, SBOM, scan, signature и provenance.
5. Deploy candidate environment.
6. Migrations expand/backfill.
7. Smoke/E2E/analysis.
8. Production deployment human gate.
9. GitOps promotion того же digest.
10. Post-deploy checks и observation window.
11. Improver запускается по feedback завершенного цикла.

## Active agents

Перед изменением agent-runner/runtime-controller/image:

- platform публикует maintenance notice;
- проверяет active turns;
- после notice повторно проверяет, не появился ли новый turn;
- ожидает завершения или явно останавливает только согласованные runs;
- session archives сохраняются до pod recreation.

Control-plane backward-compatible deploy может выполняться rolling без остановки agent pods, если contract/version matrix это допускает.

## Strategies

- Standard Deployment rolling update для простых stateless services.
- Argo Rollouts blue-green/canary для high-risk interaction/control services.
- Preview/pre-promotion smoke до traffic switch.
- Автоматический abort по readiness/error/latency analysis.

Reference: https://argoproj.github.io/argo-rollouts/concepts/

## Database

Schema rollback не является штатным способом. Изменения проектируются так, чтобы предыдущая application version работала на расширенной schema в rollback window.

Порядок: `expand -> deploy dual-compatible code -> backfill -> switch reads/writes -> contract later`.

## Rollback evidence

Release хранит image digests, Git SHA, migration version, config revision, smoke results, approver и rollback target. Rollback завершается проверкой queue, Mattermost events, provider auth, session resume и scheduled runs.
