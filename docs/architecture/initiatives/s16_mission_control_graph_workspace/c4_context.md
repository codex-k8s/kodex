---
doc_id: ARC-C4C-S16-0001
type: c4-context
title: "Sprint S16 Day 4 — C4 Context overlay for Mission Control graph workspace"
status: in-review
owner_role: SA
created_at: 2026-03-16
updated_at: 2026-03-16
related_issues: [480, 490, 492, 496, 510, 516, 519]
related_prs: []
approvals:
  required: ["Owner"]
  status: pending
  request_id: "owner-2026-03-16-issue-516-arch"
---

# C4 Context: Sprint S16 Day 4 Mission Control graph workspace

## TL;DR
- Mission Control graph workspace остаётся capability slice внутри `codex-k8s`, а не отдельной внешней graph-платформой.
- GitHub остаётся canonical provider for issue/PR/comment/review state, а platform domain остаётся owner graph truth, continuity lineage и next-step policy.

## Диаграмма (Mermaid C4Context)
```mermaid
C4Context
title Sprint S16 Day4 - Mission Control graph workspace context overlay

Person(owner, "Owner / product lead", "Ведёт несколько инициатив и выбирает следующий безопасный stage")
Person(operator, "Delivery operator / engineer", "Диагностирует continuity, run context и coverage/freshness")
Person(discussion, "Discussion-driven user", "Стартует с discussion или напрямую со stage issue")

System(system, "codex-k8s Mission Control graph workspace slice", "Primary multi-root graph workspace and continuity control plane")

System_Ext(github, "GitHub", "Issues, pull requests, comments, reviews, labels, webhooks")
System_Ext(k8s, "Kubernetes", "Runtime substrate for agent and background execution")
System_Ext(staff, "Staff UI", "Graph canvas, toolbar and drawer surfaces")

Rel(owner, system, "Uses", "HTTPS UI")
Rel(operator, system, "Uses", "HTTPS UI")
Rel(discussion, system, "Uses", "HTTPS UI")
Rel(system, github, "Reads provider state / routes provider-safe deep links and sync", "HTTPS / webhooks")
Rel(system, k8s, "Runs agent and reconciliation workloads", "Kubernetes API")
Rel(system, staff, "Publishes typed graph projection and next-step surfaces", "Staff/private API")
```

## Пояснения
- GitHub остаётся источником provider facts и human review/merge semantics, но не становится canonical owner graph relations и continuity completeness.
- Kubernetes обеспечивает runtime для `agent-runner` и `worker`, но не хранит graph truth.
- Staff UI остаётся visibility surface над typed projections `control-plane`, а не отдельным graph backend.

## Внешние зависимости
- GitHub: issue/pr/comment/review state, labels, provider-native collaboration и webhook echoes.
- Kubernetes: runtime для `agent-runner` и `worker`.
- Staff UI/API: отображает multi-root graph workspace и next-step surfaces, но не вычисляет domain semantics.

## Continuity after `run:arch`
- Issue `#519` (`run:design`) должен сохранить этот context overlay как baseline для typed transport/data contracts.
- Voice/STT, dashboard orchestrator agent, отдельная `agent` node taxonomy и full-history/archive остаются за пределами core context Wave 1.
