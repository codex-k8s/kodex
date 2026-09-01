---
id: REPO-MC-003
title: Jobs и workers
type: repository-readme
status: approved
owner: backend
version: 1.1.0
updated: 2026-08-28
---

# Jobs и workers

Worker исполняет attempt, но не становится владельцем aggregate. Task,
immutable input, grant, retry и terminal result принадлежат доменному сервису.

- [agent-runner](agent-runner/README.md) исполняет один claimed Turn и signed
  runtime handoff без transport и orchestration authority.
- [automation-scheduler](automation-scheduler/README.md) ограниченно будит
  server-owned schedule lifecycle через защищённый control-plane path.
- [role-image-builder](role-image-builder/README.md) собирает exact role image
  через изолированный BuildKit по server-owned fenced attempt.
- [backup-controller](backup-controller/README.md) создаёт verified PostgreSQL и
  S3 restore points, применяет retention и выполняет owner-gated restore drill.

Legacy migration/cutover jobs отсутствуют: fresh install использует одну
baseline schema и не переносит состояние прежнего bot-service.
