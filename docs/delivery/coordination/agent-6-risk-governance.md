# Агент #6 — риски и релизы

## Стабильная зона ответственности

Агент #6 ведёт домен `risk-and-release-governance` и целевой сервис-владелец `governance-manager`.

## Границы

- В зоне агента: risk profiles, risk rules, risk assessments, risk factors, review signals, gate policy, gate requests, gate decisions, release decision package, release decisions, release safety-loop state и события `governance.*`.
- Не в зоне агента: project/release policy как проектная истина в `project-catalog`, flow/run/acceptance в `agent-manager`, provider-native зеркало в `provider-hub`, runtime jobs в `runtime-manager`, delivery/callbacks в `interaction-hub`, UI/gateway.

## Завершённый стартовый срез

- Issue: #322.
- Результат среза: docs-first пакет домена, сквозная сервисная граница `governance-manager`, карта Issue и план поставки.

## Текущий контрактный срез

- Issue: #769.
- Результат среза: gRPC/AsyncAPI контракты `governance-manager`, generated Go contracts событий и protobuf, действия доступа и обновлённая карта поставки.
- Сервисный процесс, handlers, БД, миграции, evaluator, UI/gateway и межсервисные интеграции не входят в GOV-1.

## Ближайшие зависимости

| Домен | Что нужно согласовать |
|---|---|
| `projects-and-repositories` | Project/repository refs, services policy, branch rules, release policy, release line и risk profile refs. |
| `agent-orchestration` | Run/session/acceptance refs, role review signals и ожидание governance decision. |
| `provider-native-work-items` | PR/MR projections, changed file summary, provider review/comment/check refs и validation gate refs для provider write operations. |
| `runtime-and-fleet` | Job/deploy/postdeploy/cleanup signals и target environment refs. |
| `interaction-hub` | Delivery request/callback контракт для Human gate без владения decision state. |
