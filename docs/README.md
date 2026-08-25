---
id: DOC-MC-001
title: Документация Kodex
type: documentation-index
status: approved
owner: architect
version: 2.2.0
updated: 2026-08-25
---

# Документация Kodex

Kodex — web-first платформа управления ИИ-сотрудниками. Единственным
основным пользовательским интерфейсом является Control Center. Mattermost,
GitHub, GitLab, Kubernetes, CRM и другие внешние системы подключаются как
необязательные интеграции и не входят в core readiness.

## Источник истины

- `product/` — продуктовая формула, персоны, сценарии и требования;
- `architecture/` — доменная модель, границы сервисов, runtime, MCP,
  role images, realtime и интеграции;
- `domains/` — агрегаты, команды, события и инварианты владения;
- `decisions/` — принятые архитектурные решения;
- `design/` — утверждённый UX-контракт, prompt pack и HTML-макеты;
- `guides/` и `design-guidelines/` — обязательные правила реализации;
- `governance/` — процесс поставки и фактический профиль проверок;
- `operations/` — поддерживаемые профили, SLO, безопасность и восстановление;
- `runbooks/` — инструкции диагностики и чистого развертывания.

Git history является архивом прежней Mattermost-first реализации. В текущей
документации нет legacy migration, compatibility, dual-write, dark cutover или
параллельного старого контура.

## Канонические точки входа

- продуктовый reset: `architecture/web-first-platform-reset.md`;
- требования: `product/requirements.md`;
- пользовательские сценарии: `product/user-scenarios.md`;
- role images и запуск Pod: `architecture/runtime-controller.md`;
- runtime MCP: `architecture/integration-map.md`;
- будущие устанавливаемые приложения и источники каталога:
  `product/installable-applications.md` и
  `architecture/installable-applications.md`;
- профили развертывания: `operations/deployment-profiles.md`;
- чистая установка: `runbooks/fresh-install.md`;
- identity, OAuth2 Proxy, Grafana и Headlamp:
  `runbooks/identity-and-management-surfaces.md`.

Все предлагаемые к слиянию документы имеют `status: approved`. Каждый документ
явно указывает, относится ли решение к активному профилю или к утверждённой
будущей модели `POST-MVP`. Утверждённый будущий документ не является
доказательством runtime-реализации и не расширяет текущий release scope.
