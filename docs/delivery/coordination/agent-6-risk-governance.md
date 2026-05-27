# Агент #6 — риски и релизы

## Стабильная зона ответственности

Агент #6 ведёт домен `risk-and-release-governance` и целевой сервис-владелец `governance-manager`.

## Границы

- В зоне агента: risk profiles, risk rules, risk assessments, risk factors, review signals, gate policy, gate requests, gate decisions, release decision package, release decisions, release safety-loop state и события `governance.*`.
- Не в зоне агента: project/release policy как проектная истина в `project-catalog`, flow/run/acceptance в `agent-manager`, provider-native зеркало в `provider-hub`, runtime jobs в `runtime-manager`, delivery/callbacks в `interaction-hub`, UI/gateway.

## Завершённый стартовый срез

- Issue: #322.
- Результат среза: docs-first пакет домена, сквозная сервисная граница `governance-manager`, карта Issue и план поставки.

## Завершённый контрактный срез

- Issue: #769.
- Результат среза: gRPC/AsyncAPI контракты `governance-manager`, generated Go contracts событий и protobuf, действия доступа и обновлённая карта поставки.
- Сервисный процесс, handlers, БД, миграции, evaluator, UI/gateway и межсервисные интеграции не входят в GOV-1.

## Завершённый сервисный срез

- Issue: #790.
- Результат среза: runnable skeleton `services/internal/governance-manager` с process/config, health/readiness/metrics, gRPC registration, доменным backlog-use-case и repository stub.

## Завершённый storage-срез

- Issue: #802.
- Результат среза: PostgreSQL-модель MVP-сущностей, real repository, service-local outbox и gRPC handlers для поддержанных storage-операций `governance-manager`.
- Полный rule evaluator, release decision engine, UI/gateway, deploy manifests и интеграции с соседними сервисами остаются следующими срезами.

## Завершённый gate lifecycle-срез

- Issue: #815.
- Результат среза: lifecycle gate request/decision `request/read/decision/cancel/expire`, access checks через `access-manager`, optimistic concurrency, idempotent replay и безопасные события `governance.gate.*`.
- Delivery/callback orchestration остаётся у `interaction-hub`; `governance-manager` хранит только governance state и safe refs.

## Завершённый risk evaluator-срез

- Issue: #827.
- Результат среза: risk classifier и policy evaluator в `governance-manager` работают по входным safe summaries/refs, локальным risk profiles/rules, deterministic risk class, matched rule refs, required gates, evidence refs и безопасным `governance.risk_assessment.*` событиям.
- Release decision engine, delivery/callback, provider write pipeline, project policy ownership, deploy orchestration и UI/gateway остаются вне среза.

## Завершённый release decision-срез

- Issue: #845.
- Результат среза: release decision package, release decision, blocking signal и release safety-loop state работают на PostgreSQL repository с access checks, idempotent replay, optimistic concurrency и safe `governance.release_*`/`governance.blocking_signal.*` outbox events.
- Интеграции с `agent-manager`, `provider-hub`, `interaction-hub`, `runtime-manager`, `project-catalog` и `operations-hub` остаются GOV-7.

## Завершённый release integration refs-срез

- Issue: #856.
- Результат среза: release decision package явно хранит safe `integration_refs` для project/repository/release line, provider Issue/PR/check/review, agent run/acceptance, runtime job/deploy, local risk assessment и gate refs.
- `governance-manager` нормализует domain/kind/ref/status/summary/digest/timestamp/version, проверяет только локальные governance refs и не сохраняет raw diff, provider payload, prompt/transcript, logs, workspace paths, secrets, kubeconfig или PII.
- Service-client чтения из `project-catalog`, `provider-hub`, `agent-manager`, `runtime-manager`, `interaction-hub`, provider write, delivery callbacks и deploy orchestration остаются следующими GOV-7 интеграционными срезами.

## Завершённый release package enrichment-срез

- Issue: #869.
- Результат среза: `BuildReleaseDecisionPackage` валидирует и обогащает локальные `governance` integration refs (`risk_assessment`, `review_signal`, `gate_request`, `gate_decision`, `release_decision_package`) bounded snapshot полями `status`, `summary`, `digest`, `observed_at`, `version`.
- Project/provider/agent/runtime refs остаются explicit refs: если вызывающая сторона не передала owner-domain summary, `governance-manager` добавляет safe summary diagnostic `explicit_ref_unvalidated`, но не читает соседние сервисы и не копирует чужой state.
- Service-client чтения из `project-catalog`, `provider-hub`, `agent-manager` и `runtime-manager`, provider write, delivery callbacks и deploy orchestration остаются следующими GOV-7 интеграционными срезами.

## Завершённый review signal refs-срез

- Issue: #886.
- Результат среза: `RecordReviewSignal` принимает provider/agent/interaction refs только как typed safe `evidence_refs`, проверяет `governance.signal.record`, нормализует evidence refs и сохраняет локальный source fingerprint.
- Повторная передача того же owner-domain evidence ref set возвращает уже записанный signal без нового outbox event; конфликтующий outcome/severity/summary по тому же fingerprint отклоняется.
- `governance-manager` не читает provider API, не копирует agent run/session state и не становится владельцем interaction delivery/callback фактов.

## Завершённый эксплуатационный срез

- Issue: без отдельного Issue.
- Результат среза: `governance-manager` получил первый backend deploy контур: Dockerfile со стадиями `prod` и `migrations`, Kubernetes manifests для Deployment/Service/ServiceAccount/migration Job, PostgreSQL database bootstrap, runtime env/secret inventory, smoke-проверку `/health/readyz`, runbook и monitoring.
- Контур зависит от PostgreSQL, `platform-event-log` и `access-manager`; project/provider/agent/runtime/interaction данные остаются explicit safe refs без service-client чтений и без переноса владения.

## Завершённый event-driven/read-model срез

- Issue: #907.
- Результат среза: `governance.*` события для risk assessment, review signal, gate, blocking signal, release package/decision и safety-loop несут safe metadata/refs: actor ref, request id, idempotency correlation, target/source refs, interaction/agent/runtime refs, status/outcome/reason code, bounded `safe_summary` и version.
- Соседние consumers реагируют через `platform-event-log` и `libs/go/eventconsumer`; authoritative lookup, access checks, optimistic concurrency и команды остаются через gRPC `GovernanceManagerService`.
- `governance-manager` не переносит delivery lifecycle, provider write, agent run/session state или bootstrap import внутрь своей БД.

## Ближайшие зависимости

| Домен | Что нужно согласовать |
|---|---|
| `projects-and-repositories` | Project/repository refs, services policy, branch rules, release policy, release line и risk profile refs. |
| `agent-orchestration` | Run/session/acceptance refs, role review signals и ожидание governance decision. |
| `provider-native-work-items` | PR/MR projections, changed file summary, provider review/comment/check refs и validation gate refs для provider write operations. |
| `runtime-and-fleet` | Job/deploy/postdeploy/cleanup signals и target environment refs. |
| `interaction-hub` | Delivery request/callback контракт для Human gate без владения decision state. |
