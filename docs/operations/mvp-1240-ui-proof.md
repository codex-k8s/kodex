---
id: OPS-DOC-1240
title: Интерфейс MVP - карта реализации и проверок
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Интерфейс MVP

Тематическая поставка по [Issue #1240](https://github.com/codex-k8s/kodex/issues/1240).
Исходная ревизия: `997ff620dcaaeb2330adceda1f38d6e10cfe3304`.
Владелец разрешил тематические PR и локальные проверки первой волны.
Источник приёмки: `docs/operations/mvp-1031-acceptance.md`; подробности владельца
из локального backlog сверены, но ignored-файлы не включаются в поставку.

## Исправленные исполняемые пути

- Общий async selector отменяет старый запрос, очищает страницы/cursors/total
  и не принимает поздний ответ после смены `contextKey` или loader.
  Смена контекста очищает selection; закрытый selector не начинает чтение.
  `contextKey` задаётся вызывающим компонентом для своего owner-контекста;
  backend повторно проверяет полномочия независимо от этого UI key.
- ProjectPicker перемонтируется при смене route, закрывая старый dropdown.
- IntersectionObserver подключается и к уже существующему sentinel;
  загрузка/ошибка отключают автоповтор, завершение страницы вновь включает
  наблюдение. Закрытая или скрытая история чата не подгружает страницы.
- OrganizationCatalog имеет scroll всей выборки и конечный sentinel.
  Server-side запросы по 30 элементов доступны и когда каждая группа проекта
  меньше высоты собственного scroll. Кнопка ручного повтора сохраняется.
- История AssistantWorkspace автоматически догружается отдельно в desktop
  колонке и mobile/tablet overlay. Контекст открывает отдельный inspector,
  который использует существующий проверенный серверный descriptor.
  Escape закрывает inspector с возвратом фокуса, сохраняя сам чат.
  Tablet сохраняет ширину 92vw.
- RunsPage использует четыре независимых каталога: QUEUED; RUNNING/CANCELLING;
  WAITING_HUMAN; SUCCEEDED/FAILED/CANCELLED. Каждый запрос передаёт свои
  `states`, exact `projectRef`, query, страницу 40 и cursor. Повтор/ошибка
  одной колонки не блокирует остальные. Смена проекта отменяет все четыре
  запроса и отбрасывает поздние ответы; realtime перечитывает первые страницы.
  Прежний `useRunCatalogStore` остаётся совместимым для других потребителей.
- Поиск выводит `ProblemNotice`, включая локализованный
  `errors.INVALID_SEARCH_RESULT` на ru/en. Проверенные ранее generation fence,
  abort и исчерпывающая маршрутизация четырёх типов сохранены.

## Полная карта требований блока

Пути PWA ниже относительны `services/staff/control-center/src`.
«Есть путь» означает наличие реализации и перечисленного локального покрытия;
это не утверждение о работоспособности развернутой среды.

| Требование | Исполняемый путь и локальное доказательство | Остаток приёмки |
| --- | --- | --- |
| MVP-UI-01 | `app/i18n/index.ts`, `index.test.ts`, `shared/ui/server-message-catalog.test.ts`: полнота ru/en; добавлена ошибка поиска | Console реального релиза на ru/en |
| MVP-UI-02 | `app/AppShell.vue`, `shared/ui/RealtimeStatus.vue`, `StatusBadge.vue`; unit presentation/layout | Переключение live reconnect/status на всех viewport |
| MVP-UI-03 | `shared/ui/PageFrame.vue`, `app/styles/base.css`, layout tests страниц; synthetic геометрия общих карточек и чата | Все detail screens на 1280/1440/1920/2560/2900 |
| MVP-UI-04 | `pages/HomePage.vue`, `HomePage.layout.test.ts`: адаптивные секции | Реальные пустые/частично заполненные dashboard snapshots |
| MVP-UI-05 | `features/workboard/components/WorkboardSection.vue`, `features/home/result-catalog.ts`, `features/workboard/gate-catalog.ts`; соответствующие unit | Server totals и развёрнутые списки при concurrent changes |
| MVP-UI-06 | `pages/HomePage.vue`, layout test: верхние быстрые действия и выбор проекта | Доступность и возврат каждого действия на узком экране |
| MVP-UI-07 | `shared/config/pwa-contract.test.ts`, runtime/config boundary | Публичные manifest/icons/SW exact deployment, redirect/CORS NOT RUN |
| MVP-UI-08 | `features/runtime/store.ts` и tests, readiness/agents read paths; Workboard локализован | Активный/stale environment, actual route/RPC/deploy NOT RUN |
| MVP-UI-09 | `features/assistant/components/AssistantWorkspace.vue`, assistant store/api/context tests; новый browser fixture | Provider-created имена/turns, uploads, rename/archive и authority на живой среде |
| MVP-UI-10 | `features/projects/ProjectList.vue`, `e2e/cards.synthetic.spec.ts`: серверные агрегаты и actions | Actual projection freshness и permission |
| MVP-UI-12 | `app/AppShell.vue`, navigation test: picker между брендом и поиском | Header в живой локализованной сессии |
| MVP-UI-13 | `features/projects/ProjectPicker.vue`, `shared/ui/AsyncEntityPicker.vue`, async collection tests | Каталог проектов большого объёма, route close и права |
| MVP-UI-14 | `shared/ui/VoiceTextarea.vue`, `VoiceInputButton.vue`, `CodeEditor.vue`, voice-input tests | Полный STT lifecycle принадлежит #1243, live STT NOT RUN |
| MVP-UI-15 | `features/agents/detail/TemplateVariableCatalog.vue`, редакторы инструкций, editor/layout tests | Editor/preview/materialized preview с длинными реальными данными |
| MVP-UI-16 | `features/agents/detail/PromptTargetPreview.vue`, prompt-context tests; gateway `prompt_context_endpoints_test.go` существует | Все scopes/ranges/type errors, line/column и exact pinned revision live NOT RUN |
| MVP-UI-19 | Общий AsyncEntityPicker и `features/providers` catalog/model tests | Provider readiness/auth/model семантика принадлежит #1241; live credentials NOT RUN |
| MVP-UI-20 | `shared/ui/code-editor-keymap.ts`, `features/assistant/code-editor.test.ts`, agent editor tests | Tab/Shift+Tab/undo/accessibility во всех редакторах |
| MVP-UI-21 | Конфигурационный overlay входит в соседний #1238, не интерфейсное владение #1240 | Сверка результата первой волны и overlay acceptance |
| MVP-UI-22 | `shared/ui/AsyncEntityPicker.vue`, `async-entity-context.test.ts`, `AsyncEntityPicker.test.ts`; browser pagination/focus | Multi-select и конкретные disabled reasons в рабочем контексте |
| MVP-UI-23 | Agent detail environment navigation; точные environment формы принадлежат #1239 | UI action/return URL плюс backend deny при отсутствии permission |
| MVP-UI-24 | `pages/RunsPage.vue`, `features/workboard/run-board.ts`, `run-board.test.ts`, `RunsBoard.vue`: независимые server cursors и отмена | Большие live колонки и concurrent state transitions |
| MVP-UI-25 | `features/files/FilesWorkspace.vue`, `FilesWorkspace.layout.test.ts`: trash/selection/details | Active/trash/selection с реальными файлами |
| MVP-UI-26 | Тот же FilesWorkspace, AttachmentComposer и tests | Upload/DnD и storage lifecycle live NOT RUN |
| MVP-UI-27 | `app/navigation-context.ts`, `features/catalogs/OrganizationCatalog.vue`, API/store tests; sentinel всей выборки | Tenant/membership changes и все восемь каталогов live |
| MVP-UI-28 | `features/agents/catalog/AgentCard.vue`, cards synthetic: один readiness badge, активная работа | Live runtime projection и ссылка actual run |
| MVP-UI-29 | `features/agents/detail/avatar.test.ts`, `AgentDetailPanels.test.ts`; CP `avatar_component_test.go` содержит атомарный lifecycle | PostgreSQL/object store, scan rejection/cleanup/retry NOT RUN |
| MVP-UI-32 | `features/workflows/catalog/WorkflowCard.vue`, cards synthetic: агрегаты и permission actions | Actual aggregate projection без N+1 |
| MVP-UI-33 | `pages/WorkflowDetailPage.vue`, workflowLaunch и cards tests; форма запуска | Полный launch admission принадлежит runtime/model контуру |
| MVP-UI-38 | `pages/DecisionsPage.vue`, `DecisionsPage.test.ts`, i18n | Счётчик и полное доступное имя с живыми решениями |
| MVP-UI-53 | `features/search/model.ts`, `model.test.ts`, platform store tests: 499/500, Enter, clear, abort/stale; локализованная contract problem | Browser search против реального gateway и четырёх owner kinds |
| MVP-UI-54 | `canonicalSearchRoute`, `shared/api/search-result.ts`, model tests; gateway `search_endpoint_test.go` существует | Четыре exact URL/detail requests, отсутствие workflows/agt request |

## Проверки

Локальные команды выполняются из `services/staff/control-center`:

```sh
npm run test:unit
npm run typecheck
npm run build
npm run build:synthetic
npx playwright test --config playwright.synthetic.config.ts e2e/ui-proof.synthetic.spec.ts e2e/cards.synthetic.spec.ts
```

Scoped ESLint применяется ко всем изменённым TypeScript/Vue файлам,
Prettier - ко всем изменённым handwritten файлам. Точное число тестов,
результаты команд и SHA публикуются в PR после выполнения.

Новый `ui-proof` fixture использует настоящие компоненты и искусственные
DTO; внешние/API запросы закрыто запрещены. Он проверяет selector pages/focus,
историю без кнопки «ещё», inspector Escape/focus, геометрию чата и отсутствие
page overflow на 390, 768, 1280, 1440, 1920, 2560, 2900 px в ru/en.
`cards.synthetic.spec.ts` проверяет server-owned card DTO без сетевых запросов.
Synthetic PASS не является staging PASS. Go-код/контракты не изменены;
Go unit/build, PostgreSQL component, live E2E, deploy, SSH и provider calls
в этой поставке NOT RUN.

Context7: `/websites/vuejs`, Vue watcher cleanup и AbortController,
официальный источник `https://vuejs.org/guide/essentials/watchers`.

## Ручная проверка, риски и rollback

После отдельно разрешённого deploy сверить frontend/backend exact revisions;
пройти все строки канонической приёмки своего блока в ru/en с разрешённым
actor. Для списков подготовить более одной страницы в разных проектах;
проверить фильтр, late response, повтор cursor, ошибки, logout, route change,
keyboard, поиск и восстановление фокуса. Проверить четыре колонки с разными
cursors, чтобы прокрутка одной не загружала соседнюю.

Риск: Kanban делает до четырёх ограниченных read-запросов вместо одного.
Подтверждение производительности при live объёмах остаётся отдельной приёмкой.
UI `contextKey` не источник полномочий. Полномочия и серверные lifecycle не
ослаблены. Для отката вернуть предыдущий frontend build; миграций нет.
Секреты/персональные данные не использовались и не раскрывались.

Старый OPEN Issue сам по себе не признан актуальным дефектом или основанием
для закрытия. Без exact current acceptance evidence массовое закрытие
исторических Issues не предлагается; root получает эту карту для точечной сверки.

## Фактически выполнено 2026-09-08

- `npm run test:unit`: PASS, 230 файлов, 1331 тест, 5.82 s.
- `npm run build`: PASS, включает `vue-tsc --build --force` и production Vite
  build; bundler 1.95 s. Сохраняется предупреждение о chunks больше 500 kB.
- `npm run build:synthetic`: PASS, отдельная сборка чистой browser-оснастки.
- Указанная выше scoped Playwright команда: PASS, 18 тестов, 10.9 s.
- Scoped ESLint с `--max-warnings 0`: PASS; Prettier `--check` всех 18
  изменённых handwritten frontend/fixture/config файлов: PASS.
- Финальный SHA и ссылка на PR фиксируются в GitHub; эти проверки локальные,
  не GitHub CI и не staging acceptance.
