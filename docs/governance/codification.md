---
id: GOV-DOC-001
title: Кодификация документов и задач
type: governance
status: approved
owner: manager
version: 2.0.0
updated: 2026-08-23
---

# Кодификация документов и задач

Документ задает устойчивые идентификаторы и реестр документации.

## Frontmatter

```yaml
---
id: AREA-DOC-001
title: Название
type: guide
status: approved
owner: architect
version: 0.1.0
updated: 2026-07-28
---
```

Обязательные поля:

- `id` — непереиспользуемый идентификатор;
- `title` — человекочитаемое название;
- `type` — назначение документа;
- `status` — `draft`, `review`, `approved` или `deprecated`;
- `owner` — роль-владелец;
- `version` — версия содержания;
- `updated` — дата смыслового изменения.

В PR к слиянию включаются только готовые документы со статусом `approved`.

## Области

| Префикс    | Назначение           |
| ---------- | -------------------- |
| `ROOT`     | корневой обзор       |
| `AGENT`    | инструкции агентам   |
| `GOV`      | управление и процесс |
| `PRD`      | продукт              |
| `ARCH`     | архитектура          |
| `DOM`      | домены               |
| `ADR`      | решения              |
| `CONTRACT` | контракты            |
| `REPO`     | структура            |
| `GO`       | Go/backend           |
| `FE`       | frontend             |
| `INFRA`    | инфраструктура       |
| `DEPLOY`   | развертывание        |
| `K8S`      | Kubernetes           |
| `SVC`      | компоненты и API     |
| `TOOL`     | инструменты          |
| `OPS`      | эксплуатация         |
| `RUN`      | runbook              |
| `ACC`      | приемка              |
| `QA`       | тестирование         |
| `GUIDE`    | общий гайд           |
| `TPL`      | шаблон               |

Задача получает код `<AREA>-<NNN>`. Большая работа оформляется эпиком с
подзадачами, каждая из которых дает один проверяемый результат.

## Реестр

| Код                | Файл                                                        |
| ------------------ | ----------------------------------------------------------- |
| `AGENT-DOC-001`    | `AGENTS.md`                                                 |
| `DOC-MC-001`       | `docs/README.md`                                            |
| `GOV-DOC-001`      | `docs/governance/codification.md`                           |
| `GOV-DOC-002`      | `docs/governance/open-decisions.md`                         |
| `GOV-DOC-003`      | `docs/governance/testing-strategy.md`                       |
| `GOV-DOC-004`      | `docs/governance/project-template-adoption.md`              |
| `CONTRACT-DOC-002` | `contracts/proto/README.md`                                 |
| `CONTRACT-DOC-003` | `contracts/asyncapi/README.md`                              |
| `CONTRACT-MC-004`  | `contracts/authorization/README.md`                         |
| `SVC-DOC-002`      | `contracts/README.md`                                       |
| `PRD-MC-001`       | `docs/product/README.md`                                    |
| `PRD-MC-002`       | `docs/product/personas.md`                                  |
| `PRD-MC-003`       | `docs/product/business-processes.md`                        |
| `PRD-MC-004`       | `docs/product/user-scenarios.md`                            |
| `PRD-MC-005`       | `docs/product/requirements.md`                              |
| `ARCH-MC-001`      | `docs/architecture/README.md`                               |
| `UX-MC-002`        | `docs/design/web-first-reset-prompt-pack.md`                |
| `UX-MC-003`        | `docs/design/mockups/index.md`                               |
| `ARCH-DOC-002`     | `docs/architecture/technology-stack.md`                     |
| `ARCH-MC-002`      | `docs/architecture/high-level-architecture.md`              |
| `ARCH-MC-003`      | `docs/architecture/domain-map.md`                           |
| `ARCH-MC-004`      | `docs/architecture/service-boundaries.md`                   |
| `ARCH-MC-005`      | `docs/architecture/integration-map.md`                      |
| `ARCH-MC-006`      | `docs/architecture/data-model.md`                           |
| `ARCH-MC-007`      | `docs/architecture/runtime-and-sessions.md`                 |
| `ARCH-MC-008`      | `docs/architecture/attachments-and-artifacts.md`            |
| `ARCH-MC-009`      | `docs/architecture/automations-and-playbooks.md`            |
| `ARCH-MC-010`      | `docs/architecture/runtime-controller.md`                   |
| `ARCH-MC-011`      | `docs/architecture/web-first-platform-reset.md`             |
| `DOM-MC-001`       | `docs/domains/README.md`                                    |
| `OPS-MC-001`       | `docs/operations/README.md`                                 |
| `ADR-MC-000`       | `docs/decisions/README.md`                                  |
| `ADR-DOC-004`      | `docs/decisions/0014-domain-events-transactional-outbox.md` |
| `GUIDE-MC-001`     | `docs/guides/README.md`                                     |
| `GUIDE-DOC-003`    | `docs/guides/distributed-security.md`                       |
| `GUIDE-DOC-004`    | `docs/guides/delivery-waves.md`                             |
| `GUIDE-DOC-005`    | `docs/guides/rpc-http-error-contract.md`                    |
| `GUIDE-DOC-006`    | `docs/guides/protected-lifecycle.md`                        |
| `REPO-DOC-001`     | `docs/guides/repository-structure.md`                       |
| `GO-DOC-001`       | `docs/guides/backend-go.md`                                 |
| `GO-DOC-002`       | `docs/guides/postgresql-goose.md`                           |
| `GO-DOC-003`       | `docs/guides/observability-go.md`                           |
| `GO-DOC-004`       | `docs/guides/event-delivery-go.md`                          |
| `GO-DOC-005`       | `docs/guides/interservice-communication.md`                 |
| `GO-DOC-006`       | `docs/guides/shared-go-libraries.md`                        |
| `SVC-MC-005`       | `services/internal/runtime-controller/README.md`            |
| `SVC-MC-017`       | `services/jobs/agent-runner/README.md`                      |
| `RUN-MC-008`       | `docs/runbooks/runtime-controller.md`                       |
| `FE-DOC-001`       | `docs/guides/frontend-vue.md`                               |
| `INFRA-DOC-001`    | `docs/guides/infrastructure.md`                             |
| `SVC-DOC-001`      | `services/README.md`                                        |
| `SVC-MC-003`       | `services/internal/internal-rpc-authority/README.md`        |
| `RUN-MC-006`       | `docs/runbooks/internal-rpc-authority.md`                   |
| `SVC-MC-013`       | `services/external/control-api-gateway/README.md`           |
| `RUN-MC-013`       | `docs/runbooks/control-api-gateway.md`                      |
| `RUN-MC-001`       | `docs/runbooks/README.md`                                  |
| `RUN-MC-002`       | `docs/runbooks/fresh-install.md`                            |
| `RUN-MC-007`       | `docs/runbooks/control-plane.md`                            |
| `RUN-MC-012`       | `docs/runbooks/integration-gateway.md`                      |
| `RUN-MC-016`       | `docs/runbooks/automation-scheduler.md`                     |
| `RUN-MC-022`       | `docs/runbooks/interaction-gateway.md`                      |

При добавлении управляемого документа реестр обновляется в том же PR.
