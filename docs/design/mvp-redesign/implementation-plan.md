---
id: DESIGN-DOC-003
title: План реализации MVP-редизайна Kodex Control Center
type: implementation-plan
status: approved
owner: manager
version: 1.0.0
updated: 2026-08-28
---

# План реализации MVP-редизайна Kodex Control Center

## Цель

Реализовать согласованный UI не как набор статических экранов, а как полные
вертикальные пользовательские сценарии с авторитетными API, realtime,
хранилищем, аудитом и безопасными интеграциями. Результат должен работать на
локальном hot-reload стенде и пройти browser E2E.

## Источники истины

1. Решения владельца и утвержденные `docs/product/**`.
2. Доменные и архитектурные инварианты `docs/domains/**`,
   `docs/architecture/**`, `docs/decisions/**`.
3. Инженерные правила `AGENTS.md` и `docs/guides/**`.
4. Визуальный и сценарный baseline `DESIGN-DOC-002`.

Если макет требует недопустимого полномочия или противоречит доменному
инварианту, исправляется интерфейс и сквозной контракт, а не security boundary.

## Блоки реализации

### 1. Оболочка и общие controls

- стабильный AppShell и project-scoped navigation;
- растянутый поиск и правая группа действий;
- Menu/Popover, Select, Combobox, AsyncEntityPicker, modal, drawer и bottom
  sheet с keyboard/focus contract;
- общий list/grid, badges, background refresh и realtime reconnect;
- единые responsive tokens и dark theme.

### 2. Главная и Проект

- Attention Center с независимыми секциями и частичными ошибками;
- Workboard Проекта;
- Kanban/list view запусков;
- явные source, initiator, executor и переходы без потери Проекта.

### 3. Запуски и файлы

- новый Run с async file/session picker;
- S3-compatible upload, scan state, version, provenance и bindings;
- live graph с pan/zoom/fit, inspector и activity drawer;
- Human Gate, cancel, retry, reconnect и безопасные artifacts;
- автоматическое, но редактируемое название запуска.

### 4. ИИ-сотрудники, runtime и окружения

- grid/table и сгенерированный нейтральный avatar с fallback;
- Markdown instructions editor и immutable revisions;
- provider/account policy/model/runtime/environment;
- безопасный validated `config.toml` overlay и effective config;
- CRUD/versioning/build state рабочих окружений.

### 5. Kodex и автоматизации

- контекстный Kodex без legacy full-screen page;
- типизированные инструменты текущего экрана;
- полные редактируемые планы и atomic apply receipt;
- конфликт revision без частичного применения;
- CRUD/versioning schedule, pause/resume/archive автоматизаций.

### 6. Интеграции и RBAC

- versioned YAML packages, connections, capabilities, grants и approvals;
- GitHub-сценарий через отдельный тестовый репозиторий и bot credential;
- локальный внешний mock-service с read/write/approval операциями и журналом;
- OIDC groups, system/custom roles, bindings и effective access;
- обязательный scope: один Проект и запуск только выбранных ИИ-сотрудников.

## Локальный контур

- Используется текущий local k3s и `tools/dev/**` как единственный code-first
  путь развертывания.
- Локальные зависимости и fixtures создаются идемпотентно и удаляются
  `tools/dev/reset-local.sh` без production-действий.
- S3-compatible storage запускается локально; credentials остаются только в
  локальных Secret/env и не коммитятся.
- Внешний mock-service изолирован отдельным Deployment/Service либо compose
  profile, имеет synthetic data и не принимает production credentials.
- GitHub-тест использует минимальный repository-scoped bot token; token не
  попадает в prompt, log, artifact или browser response.

## E2E-блоки

1. OIDC login, organization/project access и forbidden path.
2. Создание Проекта, ИИ-сотрудника, окружения и revision инструкций/runtime.
3. Загрузка/выбор/preview файла, новый Run и продолжение сессии.
4. Realtime Run, graph, activity, tool details, Human Gate, cancel и retry.
5. Создание, изменение, pause/resume/archive автоматизации.
6. Kodex context, редактирование плана, conflict и audit receipt.
7. YAML integration, connection, grants, approval и mock-service call.
8. GitHub read/write в изолированном репозитории от `kodex-agent`.
9. OIDC group binding, custom role, agent instance scope и effective access.
10. Responsive desktop/tablet/mobile, dark theme, keyboard и reconnect без
    визуального reload.

Каждый блок выполняется пакетно: сценарии запускаются, дефекты накапливаются,
исправляются одной связанной серией изменений и блок повторяется. После этого
выполняется общий regression новых и затронутых сценариев.

## Граница готовности

- все канонические маршруты открываются и соответствуют выбранным макетам;
- нет необъяснимых placeholder, неработающих controls и скрытых изменений;
- backend не подменяется frontend fixture в успешном E2E;
- realtime reconnect не перезагружает страницу;
- секреты не видны в UI, логах и artifacts;
- локальный стенд разворачивается и сбрасывается репозиторными командами;
- E2E-отчет содержит точный SHA, PASS/FAIL/NOT RUN и ссылки на evidence;
- владельцу переданы URL и ручной чеклист приемки.
