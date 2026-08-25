---
id: RUNBOOK-MC-015
title: Регламент эксплуатации Egress Gateway
type: runbook
status: approved
owner: SRE
version: 1.0.0
updated: 2026-08-07
---

# Egress Gateway Runbook

## Назначение и ownership

`egress-gateway` — platform deployable в namespace `kodex-system`.
Repository owner управляет schema, immutable policy, expected revision/digest,
образом, Service и endpoint labels. Gateway не владеет provider management
lifecycle и не имеет application credentials. Production rollout, rollback и
любое обращение к live cluster выполняются только после отдельного owner OK.

## Безопасный readback

Technical Service
`egress-gateway-technical.kodex-system.svc.cluster.local:9090` с
`publishNotReadyAddresses=true` публикует:

- `/healthz` — жив ли собственный процесс, без сетевых вызовов;
- `/readyz` — одновременно ли process `READY`, policy `ACTIVE` и resolver
  `VALIDATED`;
- `/policy` — только `processState`, `policyState`, `revision`, canonical
  SHA-256 `digest` и `resolverState`;
- `/metrics` — bounded-cardinality service metrics.

Эти endpoints доступны только точному monitoring namespace/Pod selector.
Readback не принимает hostname, policy, destination или credentials от caller.

Тот же consumer URL
`http://egress-gateway.kodex-system.svc.cluster.local:8080` обслуживает
HTTP CONNECT и узкую compatibility-проверку: только bodyless `GET /readyz` без
query получает `204` при том же effective readiness либо `503`. Иные методы и
пути на `8080` закрыто отклоняются; consumer не получает доступ к `9090`.

## Alerts и первичная диагностика

### EgressGatewayUnavailable

1. Проверить число available replicas и причину restart без вывода env values.
2. Сверить image digest и immutable ConfigMap revision с review-approved
   rollout.
3. Проверить, что probes используют `9090`, а Service выбирает точные endpoint
   labels.

### EgressGatewayNotReady или EgressGatewayPolicyInactive

1. Получить `/policy` через утверждённый monitoring path.
2. Для `policyState != ACTIVE` сверить expected revision/digest Deployment с
   canonical digest review-approved `policy.json`. Не менять ConfigMap на
   месте.
3. Для `resolverState != VALIDATED` проверить exact DNS NetworkPolicy к
   `kube-system` и bounded A/AAAA ответы. Не добавлять plaintext, host resolver
   или stale fallback.
4. Сверить каждую scraped replica: alert сравнивает число `up` с числом
   `ready=true`/`policy_active=1`, поэтому старая healthy replica не скрывает
   новую `INVALID`/`NOT_READY` replica, а полностью отсутствующий ряд
   готовности явно считается нулём.

### EgressGatewayUnsafeDNSAnswers

1. Считать событие возможным rebinding или ошибкой authoritative DNS.
2. Не добавлять rejected address в allowlist и не очищать проверки
   special-purpose ranges.
3. Убедиться по метрикам, что после reject не было нового successful literal
   dial. Hostname/IP в labels и logs отсутствуют по контракту.

### EgressGatewayClientHelloRejections

Проверить настройку `HTTPS_PROXY` consumer и наличие обычного видимого SNI.
Missing, duplicate, malformed, mismatched SNI и ECH закрыто отклоняются. Не
включать TLS termination, MITM certificate или ослабление hostname/CA checks.

### EgressGatewayLiteralDialFailures

Проверить доступность внешнего `TCP/443` после успешной DNS validation и
resource pressure gateway. Не добавлять fixed SaaS IP, hostname dial или
вторичное DNS-разрешение в `net.Dialer`.

### EgressGatewayConnectionLimitRejections

Проверить `active_connections`, global/per-source limits и длительность
tunnels. Alert означает устойчивые rejects с закрытой причиной
`connection_limit`; readiness при saturation намеренно может оставаться `1`.
Не увеличивать лимиты без проверки memory/file-descriptor budget.

## Проверка сетевой границы

- Consumer egress выбирает только namespace `kodex-system`, Pod labels
  `egress-gateway`/`platform-egress` и `8080/TCP`.
- Gateway ingress принимает CONNECT только от точного
  `integration-gateway` workload selector, а technical path — от точного
  monitoring selector.
- Gateway egress содержит только exact kube-dns `TCP/UDP 53` и отдельно
  документированное destination-less `TCP/443` L3/L4 exception.
- Consumer и другие application Pods не получают `0.0.0.0/0`, `::/0` либо
  destination-less внешний `443`.

После rebase PR #243 consumer удаляет локальный management egress proxy и его
OCI/Kustomize target, направляет `ManagementEgressProxyURL`/`HTTPS_PROXY` на
platform Service `:8080`, сохраняет внутренние service zones в `NO_PROXY` и не
меняет принадлежащий `integration-gateway` management lifecycle.

## Code-first rollout и rollback

Environment render создаётся только через
`tools/render-egress-gateway.sh staging|production <sha256> <registry-fqdn>`.
Renderer отклоняет zero/mutable digest, неизвестное окружение, cluster-local
registry host, отсутствие единственного image input и любой оставшийся zero
digest. Он не выполняет apply.

Новая policy revision изменяет review-approved `policy.json`, expected
revision/digest и получает новое hash-suffixed immutable ConfigMap name;
Deployment reference переключается Kustomize автоматически. Существующий
immutable объект не редактируется и не удаляется. Rollback повторно выбирает
ранее review-approved render с прежними image digest, expected revision/digest
и прежним content-addressed ConfigMap, создавая обычный rollout.

## Failure policy

Invalid, partial или digest-mismatched policy не открывает readiness и не
обслуживает CONNECT; technical runtime остаётся доступным для ограниченного
`policyState=INVALID` readback без loaded revision/digest. DNS failure не использует stale snapshot; readiness
восстанавливается только после успешной полной refresh validation.

После rollback повторно сверяются `/policy`, readiness, Service endpoints и
NetworkPolicy. Удалять CNI deny rules или давать consumer прямой внешний HTTPS
для ускорения восстановления запрещено.
