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
- Результат среза: `BuildReleaseDecisionPackage` валидирует и обогащает локальные `governance` integration refs (`risk_assessment`, `review_signal`, `gate_request`, `gate_decision`, `release_decision_package`) ограниченным snapshot с полями `status`, `summary`, `digest`, `observed_at`, `version`.
- Project/provider/agent/runtime refs остаются explicit refs: если вызывающая сторона не передала owner-domain summary, `governance-manager` добавляет safe summary diagnostic `explicit_ref_unvalidated`, но не читает соседние сервисы и не копирует чужой state.
- Service-client чтения из `project-catalog`, `provider-hub`, `agent-manager` и `runtime-manager`, provider write, delivery callbacks и deploy orchestration остаются следующими GOV-7 интеграционными срезами.

## Завершённый review signal refs-срез

- Issue: #886.
- Результат среза: `RecordReviewSignal` принимает provider/agent/interaction refs только как typed safe `evidence_refs`, проверяет `governance.signal.record`, нормализует evidence refs и сохраняет локальный source fingerprint.
- Повторная передача того же owner-domain evidence ref set возвращает уже записанный signal без нового outbox event; конфликтующий outcome/severity/summary по тому же fingerprint отклоняется.
- `governance-manager` не читает provider API, не копирует agent run/session state и не становится владельцем interaction delivery/callback фактов.

## Завершённый эксплуатационный срез

- Issue: без отдельного Issue.
- Результат среза: `governance-manager` получил первый backend deploy контур: Dockerfile со стадиями `prod` и `migrations`, Kubernetes manifests для Deployment/Service/ServiceAccount/migration Job, PostgreSQL database bootstrap, runtime env/secret inventory, проверку готовности `/health/readyz`, runbook и monitoring.
- Контур зависит от PostgreSQL, `platform-event-log` и `access-manager`; project/provider/agent/runtime/interaction данные остаются explicit safe refs без service-client чтений и без переноса владения.

## Завершённый event-driven/read-model срез

- Issue: #907.
- Результат среза: `governance.*` события для risk assessment, review signal, gate, blocking signal, release package/decision и safety-loop несут safe metadata/refs: actor ref, request id, idempotency correlation, target/source refs, interaction/agent/runtime refs, status/outcome/reason code, bounded `safe_summary` и version.
- Соседние consumers реагируют через `platform-event-log` и `libs/go/eventconsumer`; authoritative lookup, access checks, optimistic concurrency и команды остаются через gRPC `GovernanceManagerService`.
- `governance-manager` не переносит delivery lifecycle, provider write, agent run/session state или bootstrap import внутрь своей БД.

## Завершённый срез потребителя provider review signal

- Issue: #919.
- Результат среза: `governance-manager` потребляет стабильное событие `provider.comment.synced` из `provider-hub` через `libs/go/eventconsumer` и превращает `review_state=approved/changes_requested` в локальный `RecordReviewSignal`.
- Сервис сохраняет только provider work item ref, provider comment/comment projection evidence ref, outcome/severity, ограниченный summary, actor/request refs и idempotency correlation; raw provider payload, comment body, diff, webhook body и provider API response не читаются и не сохраняются.
- `agent-manager` acceptance/follow-up events и `interaction-hub` response events не маппятся в review signal без отдельного согласованного outcome/gate boundary.

## Завершённый срез потребителя interaction gate decision

- Issue: #930.
- Результат среза: `governance-manager` потребляет стабильное событие `interaction.request.response_recorded` из `interaction-hub` через `libs/go/eventconsumer` и превращает answered Human gate response для `owner_service=governance_manager` в локальный `SubmitGateDecision`.
- Сервис обрабатывает только `request_kind=human_gate`, локальный gate request ref и `response_action=approve/reject`; остальные владельцы подтверждаются без записи, а неподдержанные action получают безопасный permanent diagnostic без retry storm.
- Сохраняются только actor ref, interaction request/response refs, safe source ref, response digest summary, outcome, event/request ref и idempotency fingerprint; raw response text, callback body, delivery payload, prompt/transcript, logs, workspace paths и secrets не читаются и не сохраняются.

## Завершённый срез runtime/deploy evidence refs

- Issue: без отдельного Issue.
- Результат среза: `governance-manager` принимает safe runtime/deploy evidence для существующего release decision package через `RecordReleaseRuntimeEvidence`.
- Команда дозаписывает только `runtime_refs`, ограниченные `evidence_refs` и `integration_refs` домена `runtime` с `status`, коротким `summary`, `digest`, `observed_at`, `version`/etag и опциональным `error_code`; raw logs, stdout/stderr, kubeconfig, Kubernetes payload, deploy scripts, workspace paths и secrets не сохраняются.
- Идемпотентный replay с тем же входом не создаёт новую версию или событие, конфликтующий снимок для того же `domain/kind/ref` отклоняется, `closed` package не меняется.
- Прямой consumer для `runtime.job.*` не включается: стабильные события `runtime-manager` пока не несут согласованную безопасную привязку к `release_decision_package_id` или локальному gate/package ref.

## Завершённый срез чтения runtime/deploy evidence

- Issue: без отдельного Issue.
- Результат среза: `GetReleaseDecisionPackage` и `ListReleaseDecisionPackages` дают интерфейсу владельца и персонала безопасный снимок runtime/deploy evidence из release package без нового контракта.
- Поверхность чтения содержит связанные runtime job refs, deploy/postdeploy refs, status, короткий безопасный `summary`, `error_code`, `observed_at`, digest/version, package version, `release_candidate_ref` и связи с gate request/decision через `integration_refs`.
- Для `job|deploy|postdeploy` принимаются только статусы `pending`, `claimed`, `running`, `succeeded`, `failed`, `cancelled`, `timed_out`; повтор с тем же fingerprint/digest идемпотентен, конфликтующий fingerprint отклоняется, устаревший status не перезаписывает сохранённый факт.
- `governance-manager` не читает Kubernetes, deploy scripts, БД `runtime-manager` и provider payload; исходные логи и полные отчёты остаются у домена-владельца.

## Завершённый срез agent evidence refs

- Issue: #957.
- Результат среза: `governance-manager` принимает safe agent acceptance/review/runtime evidence для существующего release decision package через `RecordReleaseAgentEvidence`.
- Команда дозаписывает только `agent_context`, ограниченные `evidence_refs` и `integration_refs` доменов `agent`, `runtime` и `governance`: session/run/stage/acceptance/human gate refs, runtime job refs, локальные review signal/gate refs, status, короткий `summary`, `digest`, `observed_at` и version.
- Для `agent` refs проверяются известные lifecycle statuses acceptance/run/human gate/session; повтор с тем же fingerprint/digest идемпотентен, конфликтующий fingerprint отклоняется, устаревший status не перезаписывает сохранённый факт, `closed` package не меняется.
- `governance-manager` не читает БД `agent-manager`, runtime/Kubernetes, provider payload, prompt body, transcript, raw tool input/output, stdout/stderr, workspace paths, секреты и большие логи.

## Завершённый срез потребителя agent acceptance evidence

- Issue: #972.
- Результат среза: `governance-manager` потребляет `agent.acceptance.completed` и `agent.acceptance.failed` из `agent-manager` через `libs/go/eventconsumer`, когда событие содержит явный `governance_release_decision_package_ref`.
- Consumer читает только локальный release package для expected version и вызывает существующий `RecordReleaseAgentEvidence`; сохраняются acceptance/session/run/stage refs, runtime job ref, status, короткий `summary`, `digest`, `observed_at`, version и event idempotency fingerprint.
- Событие без package ref подтверждается без записи; некорректная package ref, status не по типу события, неизвестный package или конфликтующий fingerprint получают безопасный permanent diagnostic без retry storm.
- `governance-manager` не делает implicit lookup release package по project/repository/run и не читает БД `agent-manager`, runtime/Kubernetes, provider API, prompt/transcript, raw tool input/output, stdout/stderr, workspace paths, секреты или большие логи.

## Завершённый срез срез сводки чтения governance

- Issue: #976.
- Результат среза: `governance-manager` отдаёт `GetGovernanceSummary` для safe модели чтения по target/project/release/package/integration ref.
- Summary содержит pending/completed decisions, risk class, review outcome, gate request/decision outcome, release package/decision state, blocking/safety-loop state, linked provider/agent/runtime evidence refs, короткие `safe_summary`, timestamps, digest/version и partial diagnostics.
- `staff-gateway` и будущий `web-console` получают готовую доменную сводку и не вычисляют governance-правила; gateway endpoint и UI остаются отдельными срезами.
- Сырые provider payload, prompt/transcript, raw tool input/output, stdout/stderr, runtime logs, workspace paths, секреты и большие детали не возвращаются.

## Ближайшие зависимости

| Домен | Что нужно согласовать |
|---|---|
| `projects-and-repositories` | Project/repository refs, services policy, branch rules, release policy, release line и risk profile refs. |
| `agent-orchestration` | Run/session/acceptance refs, role review signals и ожидание governance decision; explicit agent evidence refs уже принимаются командой governance, `agent.acceptance.completed`/`failed` подключены только как release package evidence при явном package ref, а summary умеет находить package по сохранённому agent `integration_ref`. Для прямого review/risk signal нужен typed governance outcome. |
| `provider-native-work-items` | PR/MR projections, changed file summary, provider review/comment/check refs и validation gate refs для provider write operations. |
| `runtime-and-fleet` | Runtime/deploy refs уже принимаются командой по известному release package и видны в summary через `integration_refs`; для прямого consumer нужен owner event с безопасной package/gate привязкой. |
| `interaction-hub` | Delivery request/callback контракт для Human gate без владения decision state; ответ владельца уже принимается через `interaction.request.response_recorded` только для локального governance gate decision. |
