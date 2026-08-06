---
id: UX-MC-001
title: Макеты MatterCodex Control Center
type: product-design
status: approved
owner: product
version: 1.0.0
updated: 2026-08-06
---

# Макеты MatterCodex Control Center

`UX-MC-001` фиксирует визуальное направление служебной PWA из
[Issue #194](https://github.com/codex-k8s/matter-codex/issues/194). Макеты
определяют композицию экранов, информационную иерархию, плотность интерфейса,
состояния и базовое визуальное оформление. Функциональные правила, полномочия и
API определяются утвержденными продуктовыми, архитектурными и контрактными
документами. При расхождении макета с ними реализация следует документам.

Все данные в макетах синтетические. HTML-файлы статические: они не выполняют
JavaScript, не загружают внешние шрифты и не обращаются к сети. PNG используются
для быстрого просмотра в GitHub, а HTML - для проверки исходного размера экрана
в браузере.

## Экраны

| Область | HTML | Предпросмотр |
| --- | --- | --- |
| Обзор и оперативное состояние | [overview.html](overview.html) | [PNG](previews/overview.png) |
| Рабочие области | [workspaces.html](workspaces.html) | [PNG](previews/workspaces.png) |
| Рабочая область, чаты и репозитории | [workspace-overview.html](workspace-overview.html) | [PNG](previews/workspace-overview.png) |
| ИИ-сотрудники и роли | [agents.html](agents.html) | [PNG](previews/agents.png) |
| Учетные записи моделей | [provider-accounts.html](provider-accounts.html) | [PNG](previews/provider-accounts.png) |
| Интеграции и согласования | [integrations.html](integrations.html) | [PNG](previews/integrations.png) |
| Инструкции и шаблоны | [instructions.html](instructions.html) | [PNG](previews/instructions.png) |
| Автоматизации и расписания | [automations.html](automations.html) | [PNG](previews/automations.png) |
| Запуски, lineage и owner gate | [runs.html](runs.html) | [PNG](previews/runs.png) |
| Эксплуатация, инциденты и ресурсы | [operations.html](operations.html) | [PNG](previews/operations.png) |
| Резервное копирование и восстановление | [backups.html](backups.html) | [PNG](previews/backups.png) |
| Образы и runtime ролей | [role-images.html](role-images.html) | [PNG](previews/role-images.png) |
| Диагностика и аудит | [diagnostics-audit.html](diagnostics-audit.html) | [PNG](previews/diagnostics-audit.png) |
| Мобильная компоновка и состояния | [mobile-validation.html](mobile-validation.html) | [PNG](previews/mobile-validation.png) |
| Темная тема и состояния | [dark-theme-validation.html](dark-theme-validation.html) | [PNG](previews/dark-theme-validation.png) |

Mobile и dark validation sheets намеренно повторяют ключевые элементы основных
экранов. Они проверяют адаптивную компоновку, контраст и визуальную семантику
состояний, а не задают дополнительные пользовательские сценарии.

## Инварианты реализации

- базовые действия выполняются формами, кнопками и понятными именами без ввода
  внутренних идентификаторов;
- секреты отображаются только в замаскированном виде;
- все применимые сценарии имеют явные `loading`, `empty`, `error`, `forbidden`
  и `ready` состояния;
- устаревший ответ не перезаписывает более новое состояние интерфейса;
- принадлежность конфигурации `managed_by=ui|git`, revision и drift берутся из
  авторитетного ответа сервера;
- desktop, mobile, светлая и темная темы сохраняют одинаковую семантику и
  доступность действий;
- Vue-компоненты не воспроизводят бизнес-правила control plane и используют
  типизированные adapters поверх сгенерированных OpenAPI/AsyncAPI clients.

Структура и стек реализации задаются `FE-DOC-001` и `REPO-DOC-001`.

## Ручная проверка

1. Открыть каждый HTML-файл локально в браузере без подключения к сети.
2. Сопоставить основные экраны с mobile и dark validation sheets.
3. Проверить отсутствие наложений, обрезанного текста, раскрытых секретов и
   элементов, требующих знания внутренних идентификаторов.
