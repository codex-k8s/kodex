# Общий prompt: макеты MVP-редизайна Kodex Control Center

Ты работаешь в локальном репозитории `/home/s/projects/matter-codex`. У тебя нет
предыдущего контекста обсуждения. Выполни задачу полностью по источникам и
тематическим prompt-файлам ниже.

## Цель

Подготовить согласованный набор high-fidelity HTML-макетов целевого MVP Kodex
Control Center. Это рабочая B2B-платформа управления ИИ-сотрудниками для любого
вида бизнеса, а не IT-only продукт и не маркетинговый сайт.

Макеты должны сохранить узнаваемый спокойный operational-характер текущего
интерфейса, но устранить его структурные и UX-дефекты. Не проектируй реализацию
Vue и не меняй production-код. Результат этой задачи - только HTML-макеты,
индекс и краткое описание принятых решений.

## Сначала прочитай

1. `/home/s/projects/matter-codex/AGENTS.md`.
2. `/home/s/projects/matter-codex/docs/product/requirements.md`.
3. `/home/s/projects/matter-codex/docs/product/user-scenarios.md`.
4. `/home/s/projects/matter-codex/docs/architecture/web-first-platform-reset.md`.
5. `/home/s/projects/matter-codex/docs/guides/frontend-vue.md`.
6. `/home/s/projects/matter-codex/docs/design/mockups/index.md`.
7. `/home/s/projects/matter-codex/docs/design/kodex-mockups.html`.
8. `/home/s/projects/matter-codex/docs/design/kodex-live-run.html`.
9. Текущие отдельные макеты в
   `/home/s/projects/matter-codex/docs/design/mockups/*.dc.html`.
10. Текущие Vue-экраны в
    `/home/s/projects/matter-codex/services/staff/control-center/src/pages/` и
    оболочку в
    `/home/s/projects/matter-codex/services/staff/control-center/src/app/AppShell.vue`.

Текущие HTML и Vue-экраны являются источником состава продукта и визуальной
преемственности, но не источником известных дефектов. Не копируй неудобную
компоновку, локальную фильтрацию больших списков, сломанные radio, прыгающие
loading-состояния, неясные подписи или legacy-поведение помощника.

## Тематические задания

Прочитай все файлы до начала генерации:

1. `01-foundation-and-shell.md` - дизайн-система, оболочка и общие controls.
2. `02-home.md` - три варианта главной.
3. `03-project-workboard.md` - три варианта обзора Проекта.
4. `04-new-run.md` - новый запуск и выбор файлов/сессии.
5. `05-live-run.md` - граф, inspector и подробный ход работы.
6. `06-agents-and-runtime.md` - сотрудники, аватары, инструкции и runtime.
7. `07-kodex-assistant.md` - новый контекстный Kodex и редактируемые планы.
8. `08-files-automations-environments.md` - файлы, автоматизации и окружения.
9. `09-integrations.md` - интеграции, подключения, grants и Human Gate.
10. `10-access-rbac.md` - участники, роли, OIDC-группы и effective access.
11. `11-responsive-states-and-validation.md` - mobile, состояния и dark theme.

## Обязательные продуктовые решения

- Не сохраняй legacy-страницу полноэкранного помощника. Новый Kodex открывается
  из FAB как правый drawer на desktop и bottom sheet на mobile. История
  диалогов находится внутри нового интерфейса, а не в старом экране.
- Главная строится вокруг внимания пользователя, а Проект - вокруг текущей
  работы, решений и результатов.
- Большие выборки используют серверный поиск, cursor pagination и infinite
  scroll. Макет должен явно показывать эти паттерны.
- Run Workspace использует интерактивный canvas, inspector выбранного узла и
  отдельный drawer истории сообщений и инструментов.
- Сотрудник может иметь сгенерированный Kodex аватар, provider/account policy,
  model, runtime, environment и безопасный `config.toml` overlay.
- План Kodex показывает все create/update/delete параметры и редактируется до
  применения. Скрытых изменений быть не должно.
- В MVP входят S3-compatible files, session archives и backups, YAML-based
  integrations и scoped enterprise RBAC.
- Семантического vector search в текущем MVP нет. Не рисуй управление chunks,
  embeddings или несуществующей vector database.

## Визуальная преемственность

Сохрани характер текущих утверждённых макетов: светлая холодная нейтраль,
синий акцент, компактная типографика, тонкие границы, небольшие радиусы,
содержательные статусы и плотная рабочая компоновка. Не копируй существующие
экраны один в один. Не фиксируй новые визуальные решения только потому, что они
случайно присутствуют в текущем frontend.

Запрещены маркетинговые hero, декоративные градиенты, bokeh/orbs, огромные
заголовки, карточки внутри карточек, бессмысленные иллюстрации и псевдотекст.
Используй русские пользовательские тексты и нейтральные синтетические данные.

## Формат результата

Создай новый каталог:

`/home/s/projects/matter-codex/docs/design/mvp-redesign/`

Не изменяй и не удаляй старые макеты. В новом каталоге создай:

- `index.md` с реестром файлов, маршрутами, назначением и отличиями от старого UX;
- `design-system.html` с оболочкой, controls и состояниями компонентов;
- все HTML-файлы, перечисленные в тематических prompts;
- `validation.html` с responsive, accessibility и dark-theme проверкой.

Каждый HTML должен открываться напрямую в браузере без dev server, не требовать
внешней сети и иметь стабильный viewport. Desktop: `1440x1024`. Mobile:
`390x844`. Для интерактивных примеров добавь минимальный локальный JavaScript:
dropdown, drawer, tabs, list/grid, поиск, выбор, plan editing, canvas pan/zoom.

Сначала создай три варианта главной и обзора Проекта. Затем используй один
единый design system во всех остальных экранах. Не заканчивай на описании:
нужны сами HTML-макеты и проверка их открытия.
