---
id: REPO-MC-001
title: MatterCodex
type: repository-readme
status: approved
owner: manager
version: 1.1.0
updated: 2026-08-24
---

# MatterCodex

MatterCodex — web-first платформа управления ИИ-сотрудниками и выполняемыми ими
Процессами для продаж, поддержки, финансов, юридической работы, контента,
аналитики, разработки и операционной деятельности.

Основной интерфейс — `services/staff/control-center`. Пользователь может без
внешних интеграций создать Проект и ИИ-сотрудника, запустить Agent/Workflow,
наблюдать live graph, продолжить Session, принять Human Gate и скачать artifact.

## Runtime

- `control-plane` владеет продуктовым состоянием, authorization, lifecycle,
  audit, idempotency, graph/events и transactional outbox;
- `runtime-controller` запускает каждый обычный turn в новом Kubernetes Pod из
  exact promoted Docker image роли;
- `role-image-builder` собирает индивидуальные окружения ролей через rootless
  BuildKit, supply chain проверяет SBOM/provenance/signature и promotion;
- защищённый `agent-runner` внутри role image запускает model provider и
  разрешённые типизированные MCP-инструменты;
- системный помощник использует отдельный always-hot runtime;
- Mattermost — optional interaction adapter, а GitHub, GitLab, Kubernetes,
  CRM/ERP/email/storage — равноправные необязательные integrations.

## Документация

- [навигация и источники истины](docs/README.md);
- [продуктовая модель](docs/product/README.md);
- [целевая web-first архитектура](docs/architecture/web-first-platform-reset.md);
- [runtime и role Pod](docs/architecture/runtime-controller.md);
- [домены](docs/domains/README.md);
- [утверждённые HTML-макеты](docs/design/mockups/index.md);
- [эксплуатационные профили](docs/operations/deployment-profiles.md);
- [чистая установка](docs/runbooks/fresh-install.md);
- [Keycloak и административные интерфейсы](docs/runbooks/identity-and-management-surfaces.md).

Поддерживаемые Kustomize profiles:

- `deploy/k8s/profiles/web-only`;
- `deploy/k8s/profiles/web-with-mattermost`.

Public domain, Origin, OIDC, registry и external hosts передаются параметрами
развертывания; репозиторий не фиксирует домен конкретного владельца.

## Лицензия

AGPL и коммерческая лицензия.
