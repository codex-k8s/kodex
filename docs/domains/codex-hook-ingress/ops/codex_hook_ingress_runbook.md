---
doc_id: RB-CK8S-CODEX-HOOK-INGRESS-0001
type: runbook
title: "codex-hook-ingress — runbook: deploy, диагностика и rollback"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [868]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-codex-hook-ingress-deploy"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Runbook: codex-hook-ingress — deploy, диагностика и rollback

## TL;DR

- Симптом: `codex-hook-ingress` не стартует, не проходит readiness, не отдаёт metrics или начал возвращать безопасные отказы `hook.rate_limited`, `hook.backpressure`, `hook.payload_rejected`, `hook.route_disabled`, `hook.route_unsupported`, `hook.owner_unavailable`.
- Быстрая диагностика: проверить image, `Deployment`, `ConfigMap`, `/health/readyz`, `/health/livez`, `/metrics`, route/failure policy и bounded ops feed settings.
- Быстрое восстановление: исправить image/env, перезапустить `Deployment/codex-hook-ingress`, выполнить Go checks или общий deploy/diagnostic runner; при неудачном rollout откатить image tag или вернуть предыдущий ConfigMap.

## Когда использовать

- После сборки и публикации образа `codex-hook-ingress`.
- После изменения config, route registry, sanitizer limits, rate limits, ops feed policy или Kubernetes manifests.
- При росте rejected/redacted/dropped/downstream_failed/disabled/unsupported diagnostics.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Локально для deploy/diagnostic checks нужны `go`, `kubectl` и `curl`.
- Для сборки образа нужен доступ к Docker daemon и mirror image `golang-alpine`.
- Smoke не требует секреты, DSN, kubeconfig value, provider payload или real hook emitter payload. Если используется `KUBECONFIG`, не выводить его содержимое.

## Сборка образа

```bash
scripts/build-codex-hook-ingress-images.sh
```

Скрипт собирает только `codex-hook-ingress` prod image. Он использует image/version из `services.yaml`, уже экспортированные env или явно переданный `KODEX_BUILD_ENV_FILE` с non-secret build overrides. `bootstrap/host/config.env` автоматически не читается; migration image не создаётся, потому что у сервиса нет собственной БД.

## Проверки

Для `codex-hook-ingress` нет активного shell smoke-сценария. Health, readiness,
metrics и logical `SubmitHookEvent` проверяются Go tests или будущим Go
integration runner. Shell допускается только как тонкая обвязка общего
deploy/diagnostic tooling.

Readiness подтверждает, что process, domain service и in-process logical `SubmitHookEvent` handler собраны. Smoke не вызывает physical `SubmitHookEvent`, потому что HTTP/gRPC transport для этой операции не выбран.

## Диагностика rollout и health

```bash
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/codex-hook-ingress service/codex-hook-ingress configmap/codex-hook-ingress-config
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/codex-hook-ingress
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/codex-hook-ingress
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/codex-hook-ingress
kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/codex-hook-ingress 18088:8080
curl -fsS http://127.0.0.1:18088/health/livez
curl -fsS http://127.0.0.1:18088/health/readyz
curl -fsS http://127.0.0.1:18088/metrics
```

Readiness не проверяет доступность owner services по сети: текущий сервис использует owner ports/stubs и безопасные route diagnostics. Downstream unavailable, disabled или unsupported route не считается успешной доставкой hook event.

## Диагностика sanitizer и route policy

| Симптом | Вероятная причина | Что проверить |
|---|---|---|
| `hook.payload_rejected` | Forbidden field, secret-like value, unsafe capability ref или path-like ref | Sanitizer contract, `safe_summary`, payload digest, route diagnostic code без raw payload. |
| `hook.rate_limited` | Fixed-window burst исчерпан | `KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW`, `KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST`, частоту событий от source/run. |
| `hook.backpressure` | Bounded ops feed не принял событие до dispatch | `KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY`, `KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION`, число replicas. |
| `hook.route_disabled` | Route выключен конфигурацией | `KODEX_CODEX_HOOK_INGRESS_DISABLED_ROUTES` и expected route plan. |
| `hook.route_unsupported` | Owner route не зарегистрирован в registry | Согласованность route registry и доменных контрактов. |
| `hook.owner_unavailable` | Owner port/stub вернул unavailable или timeout | Diagnostic route result; не прикладывать raw downstream error. |

В логи, Issue и PR нельзя добавлять raw `tool_input`, `tool_response`, prompt, stdout/stderr, transcript, session dump, provider payload, kubeconfig, tokens, secrets, `SKILL.md`, manifest payload или workspace paths.

## План отката

- Вернуть предыдущий image tag `codex-hook-ingress` через rendered manifest или `kubectl rollout undo deployment/codex-hook-ingress`.
- Если отказ связан только с config, вернуть предыдущий `ConfigMap` и перезапустить `Deployment`.
- Не создавать и не откатывать БД, migrations или platform-event-log ради `codex-hook-ingress`: текущий deploy-контур не владеет persistent state.
- Не менять `agent-manager`, `governance-manager`, `interaction-hub`, `runtime-manager`, `provider-hub` или MCP-контуры при incident в ingress без отдельного подтверждённого owner-side сбоя.

## Проверка результата

- `Deployment/codex-hook-ingress` доступен.
- `/health/readyz` и `/health/livez` возвращают успешный ответ.
- `/metrics` доступен.
- Go tests и будущий Go integration runner подтверждают health/readiness/metrics
  без shell-доменной логики.
- Нет роста rejected/backpressure/downstream_failed diagnostics после восстановления baseline traffic.

## Пост-действия

- Если была авария, создать Issue с safe symptoms, root cause и корректирующими действиями.
- Если обнаружен пробел в limits, route diagnostics, manifests, runbook или runbook, обновить документацию вместе с исправлением.
- Не прикладывать значения env, токенов, kubeconfig, private endpoints или raw hook payload.

## Апрув

- request_id: `owner-2026-05-27-codex-hook-ingress-deploy`
- Решение: approved
- Комментарий: runbook фиксирует эксплуатационный контур `codex-hook-ingress` после CHI-8.
