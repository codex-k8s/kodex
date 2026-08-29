---
id: DESIGN-DOC-002
title: Канонический MVP-редизайн Kodex Control Center
type: design
status: approved
owner: owner
version: 1.1.0
updated: 2026-08-29
---

# Канонический MVP-редизайн Kodex Control Center

Каталог фиксирует целевой пользовательский интерфейс MVP. Макеты являются
визуальным и сценарным baseline для реализации, а утвержденные продуктовые,
доменные и архитектурные документы остаются источником бизнес-инвариантов и
границ полномочий.

## Принятые варианты

- Главная: `home-a-attention-center-desktop.html` (`Attention Center`). Сначала
  показываются Human Gate, инциденты, остановленные запуски и истекающая
  авторизация, затем текущая работа, Проекты и результаты.
- Обзор Проекта: `project-a-workboard-desktop.html` (`Workboard`). Основные
  секции - внимание, выполняемая работа, результаты и ресурсы Проекта.
- `project-c-run-kanban-desktop.html` используется как дополнительный вид
  списка запусков, а не как обзор Проекта по умолчанию.
- `home-b-project-portfolio-desktop.html`,
  `home-c-activity-workbench-desktop.html` и
  `project-b-operational-dashboard-desktop.html` остаются сравнительными
  design references. Они не являются legacy UI или отдельным контрактом.
- Старый полноэкранный помощник не сохраняется. Kodex открывается как drawer на
  desktop и bottom sheet на mobile.

## Реестр маршрутов и макетов

| Область | Канонические файлы | Целевой маршрут |
|---|---|---|
| Design system | `design-system.html`, `validation.html` | служебный baseline |
| Главная | `home-a-attention-center-desktop.html`, `home-recommended-mobile.html` | `/` |
| Проект | `project-a-workboard-desktop.html`, `project-recommended-mobile.html` | `/projects/:projectRef` |
| Запуски | `project-c-run-kanban-desktop.html` | `/projects/:projectRef/runs` |
| Новый запуск | `new-run-desktop.html`, `new-run-file-picker-desktop.html`, `new-run-session-picker-desktop.html`, `new-run-mobile.html` | `/projects/:projectRef/runs/new` |
| Live Run | `live-run-desktop.html`, `live-run-activity-drawer-desktop.html`, `live-run-tool-details-desktop.html`, `live-run-mobile.html` | `/projects/:projectRef/runs/:runRef` |
| ИИ-сотрудники | `agents-grid-desktop.html`, `agents-table-desktop.html`, `agents-mobile.html` | `/projects/:projectRef/agents` |
| ИИ-сотрудник | `agent-profile-desktop.html`, `agent-instructions-desktop.html`, `agent-runtime-desktop.html`, `agent-environment-picker-desktop.html` | `/projects/:projectRef/agents/:agentRef` |
| Kodex | `kodex-drawer-project-desktop.html`, `kodex-drawer-agent-desktop.html`, `kodex-plan-editor-desktop.html`, `kodex-plan-conflict-desktop.html`, `kodex-bottom-sheet-mobile.html` | контекст текущего маршрута |
| Файлы и корзина | `files-list-desktop.html`, `files-grid-desktop.html`, `file-preview-desktop.html`, `files-automations-mobile.html` | `/projects/:projectRef/files`, `/projects/:projectRef/files/trash` |
| Автоматизации | `automations-desktop.html`, `automation-editor-desktop.html`, `files-automations-mobile.html` | `/projects/:projectRef/automations` |
| Окружения | `environments-desktop.html`, `environment-editor-desktop.html` | `/administration/environments` |
| Образы | требуется дополнить при реализации `DESIGN-DOC-003` | `/administration/images` |
| Интеграции | `integrations-catalog-desktop.html`, `integration-package-desktop.html`, `integration-connection-desktop.html`, `integration-grants-desktop.html`, `integration-approval-desktop.html`, `integrations-mobile.html` | `/integrations` |
| Доступ | `access-members-desktop.html`, `access-groups-desktop.html`, `access-roles-desktop.html`, `access-role-editor-desktop.html`, `access-effective-desktop.html`, `access-agent-scope-desktop.html`, `access-mobile.html` | `/administration/access/**` |

## Общие решения

- Верхний поиск занимает доступную ширину; решения, realtime-status и профиль
  закреплены справа.
- Меню и dropdown закрываются по outside click и `Escape`; большие выборки
  используют серверный поиск, cursor pagination и infinite scroll.
- Переподключение realtime не заменяет подтвержденные данные skeleton-экраном.
- Название запуска предлагает Kodex, если пользователь не задал его сам.
- Run Workspace состоит из canvas, inspector и отдельного activity drawer.
- Runtime ИИ-сотрудника разделяет provider, account policy, model, runtime,
  environment и versioned `config.toml` overlay.
- План Kodex полностью перечисляет create/update/delete, редактируется до
  применения и не содержит скрытых изменений.
- В MVP нет semantic vector search и управления embeddings/chunks.
- Файлы, архивы сессий и backup используют S3-compatible storage.
- Все диалоги, запуски, продолжения Session, Process inputs и Human Gates
  используют общий AttachmentSet: drag-and-drop, очередь без продуктового
  лимита числа файлов, workspace manifest и типизированные prompt variables.
- Удалённые файлы 30 дней доступны в корзине; явная очистка удаляет exact S3
  version, а активный immutable input не меняется без отмены Run.
- Образы и окружения являются разными versioned сущностями: окружение pin-ит
  promoted image digest и задаёт env, secret refs, tools, network и RBAC.
- Интеграции описываются versioned YAML package и выдают ограниченные grants.
- OIDC отвечает за identity и группы, Kodex - за прикладные allow-bindings до
  конкретного экземпляра сущности.

## Проверенные состояния

`validation.html` содержит artboards и ссылки для desktop, tablet, mobile,
dark theme, reduced motion, first loading, background refresh, empty, error,
forbidden, offline snapshot, conflict, running, Human Gate, cancel и partial
section failure. Интерактивные примеры дополнительно проверяются browser E2E в
реализации: статический макет не считается доказательством backend-поведения.

## Отличия от прежнего UX

- метрики не образуют равноправную карточную сетку без приоритета;
- project-scoped route и активный пункт навигации сохраняются при переходах;
- источник запуска, инициатор и исполнитель показываются раздельно;
- файлы выбираются через полноценный resource picker;
- ход работы Run вынесен из постоянной колонки;
- ИИ-сотрудники представлены карточками с аватарами и плотной таблицей;
- рабочие окружения получили отдельный CRUD/versioning workspace;
- автоматизации можно изменять, приостанавливать, возобновлять и архивировать;
- доступ объясняется через роли, bindings, scopes и effective access, а не
  через неописанные checkbox.

## План реализации

Полный утверждённый scope D1-D20, исполнимые этапы, зависимости, локальный
стенд и E2E-критерии зафиксированы в `implementation-plan.md`.
