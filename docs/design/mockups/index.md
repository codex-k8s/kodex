---
id: UX-MC-003
title: Индекс макетов web-first MatterCodex
type: product-design
status: approved
owner: product
version: 1.1.0
updated: 2026-08-23
---

# Индекс макетов web-first MatterCodex

Макеты к пакету промптов [UX-MC-002](../web-first-reset-prompt-pack.md).
19 экранов в двух размерах: **desktop 1440×1024** и **mobile 390×844**.
Один экран — один HTML-файл.

Утверждённые исходные canvas-экспорты владельца хранятся в репозитории:

- [полный набор экранов](../matter-codex-mockups.html);
- [детальный Live Run Workspace](../matter-codex-live-run.html).

Они являются binding UX source наравне с реестром ниже. Это HTML, а не PNG:
экспорт содержит самостоятельный интерактивный canvas и вложенные artboards.
Нормативные для реализации отдельные `*.dc.html` находятся в этом каталоге и
могут открываться локально без доступа к внешнему сервису.

Интерактивный холст со всеми макетами: <https://claude.ai/code/artifact/ab862d09-84c9-4501-be1e-dd936e9feda5>
(страница «Макеты экранов» — ряд на экран, слева desktop, справа mobile;
страница «Черновики направлений» — четыре исходных направления, из которых выбрано текущее).

## Именование

```
NN_slug_desktop.dc.html    десктопный макет экрана UX-NN
NN_slug_mobile.dc.html     мобильный макет того же экрана
```

`NN` совпадает с номером экрана в реестре UX-MC-002, `slug` — с именем из того же реестра.

## Реестр макетов

| ID | Экран | Route | Scope | Desktop | Mobile |
|---|---|---|---|---|---|
| `UX-01` | Первичная настройка | `/onboarding` | global | `01_onboarding_desktop.dc.html` | `01_onboarding_mobile.dc.html` |
| `UX-02` | Помощник MatterCodex | `/assistant` | global + Проект | `02_assistant_desktop.dc.html` | `02_assistant_mobile.dc.html` |
| `UX-03` | Главная | `/` | global | `03_home_desktop.dc.html` | `03_home_mobile.dc.html` |
| `UX-04` | Проекты | `/projects` | global | `04_projects_desktop.dc.html` | `04_projects_mobile.dc.html` |
| `UX-05` | Обзор Проекта | `/projects/:projectRef` | project | `05_project_overview_desktop.dc.html` | `05_project_overview_mobile.dc.html` |
| `UX-06` | ИИ-сотрудники | `/projects/:projectRef/agents` | project | `06_agents_desktop.dc.html` | `06_agents_mobile.dc.html` |
| `UX-07` | ИИ-сотрудник | `/projects/:projectRef/agents/:agentRef` | project | `07_agent_detail_desktop.dc.html` | `07_agent_detail_mobile.dc.html` |
| `UX-08` | Процессы | `/projects/:projectRef/workflows` | project | `08_workflows_desktop.dc.html` | `08_workflows_mobile.dc.html` |
| `UX-09` | Редактор Процесса | `/projects/:projectRef/workflows/:workflowRef` | project | `09_workflow_detail_desktop.dc.html` | `09_workflow_detail_mobile.dc.html` |
| `UX-10` | Новый запуск | `/projects/:projectRef/runs/new` | project | `10_new_run_desktop.dc.html` | `10_new_run_mobile.dc.html` |
| `UX-11` | Запуски | `/runs · /projects/:projectRef/runs` | global / project | `11_runs_desktop.dc.html` | `11_runs_mobile.dc.html` |
| `UX-12` | Live Run Workspace | `/runs/:runRef` | global + Проект | `12_live_run_desktop.dc.html` | `12_live_run_mobile.dc.html` |
| `UX-13` | Файлы и знания | `/projects/:projectRef/files` | project | `13_files_knowledge_desktop.dc.html` | `13_files_knowledge_mobile.dc.html` |
| `UX-14` | Автоматизации | `/projects/:projectRef/automations` | project | `14_automations_desktop.dc.html` | `14_automations_mobile.dc.html` |
| `UX-15` | Интеграции | `/integrations` | global | `15_integrations_desktop.dc.html` | `15_integrations_mobile.dc.html` |
| `UX-16` | Решения | `/decisions` | global | `16_decisions_desktop.dc.html` | `16_decisions_mobile.dc.html` |
| `UX-17` | Участники и доступ | `/administration/access · /projects/:projectRef/members` | global / project | `17_access_desktop.dc.html` | `17_access_mobile.dc.html` |
| `UX-18` | Администрирование | `/administration` | global | `18_administration_desktop.dc.html` | `18_administration_mobile.dc.html` |
| `UX-19` | Аудит и диагностика | `/administration/audit` | global | `19_audit_diagnostics_desktop.dc.html` | `19_audit_diagnostics_mobile.dc.html` |

Дополнительно: `Main.dc.html` — обложка холста с картой макетов, палитрой,
типографикой и перечнем состояний.

## Дизайн-система

Светлая тема, холодная нейтраль, один акцент.

| Роль | Значение |
|---|---|
| Акцент (действие, активное состояние) | `#1B6FC4`, тёмный `#14589B`, подложка `#E8F1FB` |
| Успех (готов, завершён, в сети) | `#1A7A3C`, подложка `#E7F4EC` |
| Внимание (решение человека, ожидание) | `#8A6410`, подложка `#FDF8EC` |
| Ошибка (сбой, изоляция, отмена) | `#A32E2E`, подложка `#FCEDED` |
| Текст | основной `#10161E`, вторичный `#4E5A68`, приглушённый `#7C8794` |
| Поверхности | белая `#FFFFFF`, панельная `#FBFCFD`, полотно графа `#F7F9FB`, поле `#F3F6F9` |
| Границы | `#DFE4EA`, усиленная `#C9D1DA`, волосяная `#EFF2F5` |
| Шрифты | IBM Plex Sans 400/500/600 + IBM Plex Mono для чисел и времени |
| Радиусы | 6 / 8 / 10 / 12 px |
| Высота контролов | desktop 32 px (мелкие 26 px), mobile 44–48 px |

Правила, соблюдённые во всех макетах:

- статус передаётся текстом и иконкой, не только цветом;
- кнопки «Обновить» нет ни на одном экране — данные приходят через realtime;
- не показываются UUID, digests, provider ID, локаторы Kubernetes, идентификаторы
  Mattermost, секреты и сырые ответы провайдеров;
- недоступная необязательная интеграция показана как отдельное ограничение и
  никогда не переводит основной результат в состояние ошибки;
- на mobile таблицы превращены в списки, боковые панели — в секции и нижние
  панели действий, зоны касания не меньше 44×44;
- на mobile не рисуется фальшивая системная строка iOS и фальшивая клавиатура.

## Исходники и сборка

Файлы `*.dc.html` — генерируемые. Источник:

| Файл | Что содержит |
|---|---|
| `_lib.mjs` | токены, иконки, атомы и оболочки (desktop shell, mobile shell) |
| `_d00.mjs` | обложка холста |
| `_dNN.mjs` | содержимое desktop-экранов |
| `_mNN.mjs` | содержимое mobile-экранов |
| `build.mjs` | сборка всех `*.dc.html`, `canvas.json` и этого файла |

```bash
cd docs/design/mockups && node build.mjs
```

Исключение: `12_live_run_desktop.dc.html` написан вручную — это эталон, с которого
началась дизайн-система, он не перегенерируется из `build.mjs`.

## Состояния вне основных artboards

UX-MC-002 перечисляет около тридцати отдельных состояний (loading, empty, error,
forbidden, offline, conflict, ongoing operation) — например
`01_onboarding_bootstrap_error`, `12_live_run_gate_open`,
`16_decision_stale_winner`. Здесь показано основное ready-состояние каждого
экрана. Полный контракт этих состояний обязателен для production Control Center
независимо от наличия отдельного статического artboard; отсутствие отдельного
HTML не означает перенос состояния за пределы reset.
