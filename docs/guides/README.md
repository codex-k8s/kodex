---
id: GUIDE-MC-001
title: Руководства разработки Kodex
type: guide-index
status: approved
owner: architect
version: 1.1.0
updated: 2026-07-31
---

# Руководства разработки Kodex

| Код             | Файл                                        | Назначение                                                                                        |
| --------------- | ------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `GUIDE-MC-001`  | `docs/guides/README.md`                     | Индекс.                                                                                           |
| `REPO-DOC-001`  | `docs/guides/repository-structure.md`       | Структура монорепозитория и полного unit.                                                         |
| `GO-DOC-001`    | `docs/guides/backend-go.md`                 | Каноническая структура Go service/gateway/job.                                                    |
| `GO-DOC-002`    | `docs/guides/postgresql-goose.md`           | PostgreSQL, goose, named SQL и repository adapter.                                                |
| `GO-DOC-003`    | `docs/guides/observability-go.md`           | Lifecycle, logs, metrics, traces и Sentry.                                                        |
| `GO-DOC-004`    | `docs/guides/event-delivery-go.md`          | Transactional outbox, relay, NATS и inbox.                                                        |
| `GO-DOC-005`    | `docs/guides/interservice-communication.md` | Proto/gRPC и межсервисные события.                                                                |
| `GO-DOC-006`    | `docs/guides/shared-go-libraries.md`        | Правила общих Go libraries.                                                                       |
| `FE-DOC-001`    | `docs/guides/frontend-vue.md`               | Правила Vue Control Center.                                                                       |
| `INFRA-DOC-001` | `docs/guides/infrastructure.md`             | Kubernetes, images и GitOps.                                                                      |
| `GUIDE-DOC-003` | `docs/guides/distributed-security.md`       | mTLS, JWS/JWKS, replay и secrets.                                                                 |
| `GUIDE-DOC-004` | `docs/guides/delivery-waves.md`             | Один unit на PR, три review и human gate.                                                         |
| `GUIDE-DOC-005` | `docs/guides/rpc-http-error-contract.md`    | Единый error contract RPC/HTTP.                                                                   |
| `GUIDE-DOC-006` | `docs/guides/protected-lifecycle.md`        | Полномочия и полный жизненный цикл графа фонового выполнения.                                     |
| `GUIDE-MC-006`  | `docs/guides/ci-baseline.md`                | Минимальные CI gates.                                                                             |
| `GUIDE-MC-007`  | `docs/guides/contract-quality.md`           | OpenAPI/AsyncAPI/Proto.                                                                           |
| `GUIDE-MC-008`  | `docs/guides/documentation.md`              | Правила документации и agent instructions.                                                        |
| `GUIDE-MC-009`  | `docs/guides/go-test-contours.md`           | Герметичный и обязательный PostgreSQL-контуры, внесение отказов и матрица синтетических секретов. |

Эти документы являются адаптированной локальной копией технического профиля,
зафиксированного в `GOV-DOC-004`. Детальные старые правила
`docs/design-guidelines/**` применяются только в непротиворечащей части и не
переопределяют этот индекс.

Если реализация требует нарушить руководство, автор останавливается и готовит ADR либо решение владельца до написания кода.
