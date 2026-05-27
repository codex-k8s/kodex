---
doc_id: MON-CK8S-CODEX-HOOK-INGRESS-0001
type: monitoring
title: "codex-hook-ingress — наблюдаемость"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [868]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-codex-hook-ingress-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Наблюдаемость: codex-hook-ingress

## TL;DR

- Дашборды: ingress overview, sanitizer outcomes, route diagnostics, rate limit/backpressure и Kubernetes rollout.
- Метрики: `/metrics`, hook result counters, sanitizer/route diagnostics, payload/latency buckets, readiness и pod restarts.
- Логи: только ids, route, result, error class, size bucket и correlation id без raw payload и секретов.
- Алерты: readiness down, рост rejected/redacted/dropped/downstream_failed/disabled/unsupported, sustained rate limit/backpressure и частые restarts.

## Источники данных

- HTTP health: `/health/livez`, `/health/readyz`.
- HTTP metrics: `/metrics`.
- Kubernetes: `Deployment/codex-hook-ingress`, `Service/codex-hook-ingress`, pod status, restarts и events.
- In-process diagnostics: bounded ops feed counters accepted/rejected/redacted/dropped/downstream_failed/disabled/unsupported, payload size bucket и latency bucket.
- Логи приложения: structured summaries без raw prompt, raw tool input/output, stdout/stderr, transcript, provider payload, kubeconfig, tokens, secrets, `SKILL.md` и workspace paths.

## Дашборды

| Название | Ссылка | Для чего | Owner |
|---|---|---|---|
| Codex hook ingress overview | TBD | Readiness, traffic, result mix, latency buckets и restarts. | SRE |
| Sanitizer safety | TBD | Rejected/redacted/truncated reasons, payload buckets и unsafe capability refs. | SRE |
| Route diagnostics | TBD | Owner target, route result, disabled/unsupported/downstream_failed и fail-closed decisions. | SRE |
| Backpressure and rate limits | TBD | Fixed-window saturation, ops feed depth/drops и overload symptoms. | SRE |

## Golden signals

- Latency: logical submit/domain handler latency bucket and route latency bucket.
- Traffic: hook events by event kind, source class, route target and result.
- Errors: `hook.payload_rejected`, `hook.payload_too_large`, `hook.invalid_binding`, `hook.rate_limited`, `hook.backpressure`, `hook.owner_unavailable`, `hook.decision_timeout`.
- Saturation: feed depth, dropped count, rate limit rejects, pod restarts and p95/p99 handler latency.

## Safe labels

Разрешённые labels/dimensions:

- `hook_event_name`;
- `route`;
- `owner_target`;
- `result`;
- `reject_reason`;
- `payload_size_bucket`;
- `latency_bucket`;
- `source_kind`.

Запрещены в labels и logs: `event_id` как high-cardinality label, raw prompt, command, file path, stdout/stderr, transcript path, provider payload, kubeconfig, token, secret, private endpoint, `SKILL.md`, package manifest payload и materialized workspace path.

## Routine health

- Liveness: `/health/livez` возвращает успешный ответ.
- Readiness: `/health/readyz` подтверждает, что process, domain service и logical command handler готовы.
- Metrics: `/metrics` доступен для scrape по Kubernetes annotations.
- Smoke: `scripts/smoke-codex-hook-ingress.sh` проходит без secret/DSN requirements.

## Алерты

- Readiness недоступен дольше установленного окна.
- Pod restarts или rollout не завершается.
- Rejected/redacted/dropped резко выросли относительно baseline.
- `hook.rate_limited` или `hook.backpressure` держатся выше expected hook traffic baseline.
- `downstream_failed`, `disabled` или `unsupported` route diagnostics растут после включения owner routes.
- `hook.decision_timeout` или `fail_closed` выросли для `PermissionRequest`/risky `PreToolUse`.
- Payload size bucket `gt_64KiB` появился в accepted path: проверить sanitizer, потому что envelope limit должен отклонять такие события.

## Открытые вопросы

- Конкретные Prometheus recording rules и alert rules закрепляются вместе со штатным observability stack.
- Persistent operations history или integration с operations-hub остаётся отдельным CHI-6b решением; текущая feed bounded и in-memory.
- Physical `SubmitHookEvent` transport dashboards появятся только после отдельного transport contract.

## Апрув

- request_id: `owner-2026-05-27-codex-hook-ingress-deploy`
- Решение: approved
- Комментарий: monitoring-документ входит в эксплуатационный контур CHI-8.
