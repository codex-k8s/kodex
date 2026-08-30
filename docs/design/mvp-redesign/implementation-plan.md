---
id: DESIGN-DOC-003
title: Расширенный план доведения Kodex MVP
type: implementation-plan
status: approved
owner: manager
version: 2.1.0
updated: 2026-08-30
---

# Расширенный план доведения Kodex MVP

## Статус документа

Это утверждённый владельцем план продолжения Issue #992 и PR #1006. Владелец
принял все рекомендуемые варианты D1-D16 и расширил MVP единым файловым
контрактом D17-D20 для всех диалогов, запусков, продолжений Session и Process.
Production-действия документом не разрешаются: реализация и полная проверка
выполняются на локальном k3s-контуре.

Текущий прототип не считается legacy. Схемы, API и UI можно заменять без
слоёв совместимости и переноса тестовых данных, если новая модель проще,
безопаснее и соответствует утверждённому MVP.

## Цель

Довести локальный web-first Kodex до целостного MVP, в котором владелец может:

- создать Проект, ИИ-сотрудников, Процессы, окружения, образы и интеграции;
- настроить инструкции, runtime, инструменты, секреты, сетевой доступ и права;
- запустить одиночную задачу или Процесс из нескольких агентских сессий;
- в реальном времени понимать состояние графа, диалоги, tool calls и решения;
- управлять файлами, результатами, автоматизациями и Human Gates;
- прикладывать любое число файлов к каждому диалогу, запуску, продолжению
  Session и входу Process с гарантированной материализацией в workspace;
- делегировать настройки помощнику Kodex через полный редактируемый план;
- проверить все основные сценарии на локальном k3s-контуре без production.

## Исходное состояние

- Канонические макеты и принятые решения находятся в
  `docs/design/mvp-redesign/`.
- Рабочая ветка: `kodex-agent/issue-992-mvp-integration`, draft PR #1006.
- Текущий SHA до этого плана: `f32309b5908ea5b3b17639778a29d2208672efc5`.
- Локальный контур использует k3s, SeaweedFS S3 и два авторизованных аккаунта
  OpenAI Codex.
- Исправлена балансировка новых сессий по пулу provider accounts.
- Последний прерванный E2E-прогон успел завершить семь сценариев. Остались:
  нестабильный OIDC callback в файловом сценарии, зависимый от него сценарий
  отсутствующей возможности `Файлы` и незавершённый live Process E2E.
- Текущий UI частично реализует редизайн, но ещё содержит показанные владельцем
  проблемы ширины, компоновки, dropdown, файлов, помощника, решений и live run.

## Неподвижные решения

- Сразу строится новая каноническая модель. Старые DTO, маршруты, таблицы и UI
  не сохраняются ради совместимости с тестовым прототипом.
- Секретные значения не попадают в prompt, event, audit payload, frontend state,
  логи, метрики, tracing и обычные API read-модели.
- Один общий realtime transport обслуживает UI; reconnect не перезагружает
  маршрут и не сбрасывает локальное состояние формы.
- Имена запусков, сессий, диалогов и создаваемых помощником сущностей задаются
  явно. Если пользователь не задал имя, его предлагает агент в структурированном
  результате, а платформа сохраняет безопасное резервное имя.
- Все формы поддерживают create, edit, disable/archive и delete там, где это
  разрешает lifecycle сущности.
- Все списки с неограниченным числом элементов используют server-side search,
  cursor pagination, автоподгрузку и стабильные размеры popup.
- Все изменения помощника сначала оформляются полным редактируемым планом и
  применяются только после подтверждения пользователя.
- В каждом composer, picker и файловой карточке действует один контракт
  drag-and-drop, upload progress, retry, remove и безопасного удаления.
- Продукт не ограничивает суммарное число вложений. Upload выполняется очередью
  bounded chunks и batches в пределах installation quota, поэтому один HTTP
  request или prompt никогда не становится неограниченным.
- Knowledge/vector scope не расширяется в этой волне, пока не подтверждена
  каноническая модель индекса и provenance. Файлы и источники знаний остаются
  отдельными понятиями.

## Целевая продуктовая модель

### 1. Образ ИИ-сотрудника

`RoleImage` является переиспользуемым объектом Проекта или Организации:

- имя, назначение, иконка, владелец и область видимости;
- immutable revisions;
- исходный Dockerfile;
- обязательный runtime contract Kodex;
- build context только из разрешённых артефактов;
- статус сборки, digest, SBOM/provenance и диагностика;
- список обнаруженных и проверенных executable;
- ссылки на окружения, использующие конкретную promoted revision.

Несколько окружений могут ссылаться на одну ревизию образа. Изменение
Dockerfile создаёт новую ревизию и не меняет уже опубликованный digest.

### 2. Окружение

`RuntimeEnvironment` связывает:

- точную promoted revision образа;
- обычные env values;
- ссылки на версии секретов;
- выбранный поднабор доступных инструментов и пользовательские описания;
- resource requests/limits и разрешённые volume kinds;
- сетевую политику;
- Kubernetes workload identity/RBAC profile в границах полномочий пользователя;
- runtime variables для шаблона инструкций;
- immutable revision, readiness и список использующих окружение сотрудников.

Окружение не устанавливает пакеты. Оно выбирает готовый образ и ограничивает
его runtime-возможности.

### 3. Инструменты

Образ доказывает наличие executable. Окружение разрешает только часть
обнаруженных инструментов и задаёт для них `name`, `command`, `description`,
`usage_hint`. Материализованный prompt получает только реально разрешённый
набор. Readiness закрыто отклоняет окружение, если заявленный executable
отсутствует в выбранном image digest.

### 4. Секреты

Нужна отдельная граница `secret-broker`, а не Kubernetes credentials у
control-center:

- принимает create/rotate/reveal от авторизованного control plane;
- пишет versioned immutable Kubernetes Secret в единый installation runtime
  namespace `kodex-runtime`;
- возвращает только descriptor, version и безопасный `display_hint`;
- использует ServiceAccount из `kodex-system`, которому отдельный RoleBinding
  выдаёт минимальные права только внутри `kodex-runtime`;
- пишет audit-факт операции без значения;
- выдаёт секрет workload только через ссылку RuntimeRevision;
- не хранит plaintext или обратимо зашифрованную копию в PostgreSQL.

В PostgreSQL хранятся имя, тип, scope, Kubernetes reference, version, owner,
timestamps, rotation state и `display_hint`. Reveal выполняется по `D4-B`:
требует отдельного permission и свежей OIDC re-auth, а значение возвращается
одноразовым ответом secret-broker.

### 5. Переменные шаблона инструкций

Каталог переменных имеет типизированные пространства имён:

- `system.*`: locale, time, platform capabilities;
- `user.*`: безопасные сведения текущего инициатора;
- `organization.*`;
- `project.*`;
- `agent.*`;
- `environment.*`;
- `runtime.*`;
- `tools`: коллекция разрешённых инструментов;
- `files` и `inputs`: только выданные текущему запуску descriptors.

Редактор показывает каталог, типы, примеры, обязательность и область действия.
Есть validate и preview на синтетическом безопасном контексте. Секреты в
каталоге отсутствуют.

### 6. Выполнение

Принятая модель: одна graph node представляет одну логическую агентскую
Session. Turn, attempt, retry, continuation и tool calls показываются внутри
timeline этой Session. Новая Session того же ИИ-сотрудника создаёт новую node.

Каждый event содержит session, turn, attempt, parent edge, actor, timestamp и
bounded payload. Полный prompt доступен только пользователю с отдельным правом,
так как может содержать проектные и персональные данные, но никогда секреты.

### 7. Файлы, AttachmentSet и workspace

Каждое пользовательское сообщение в Kodex, Run chat, продолжении Session,
Process/Workflow input и Human Gate comment может ссылаться на один
`AttachmentSet`. Набор содержит точные immutable `ArtifactRevision` refs,
порядок, purpose, actor, Project, message/turn binding и manifest digest.

Новые файлы загружаются напрямую в общий S3 lifecycle, проходят scan и только
после `CLEAN` становятся доступными для отправки. Уже существующие Project
files можно добавить в набор без копирования body. В composer нет продуктового
лимита на количество файлов: frontend ставит их в очередь, а backend принимает
ограниченные chunks/batches и применяет installation quotas по размеру и
хранилищу.

Перед turn платформа materialize-ит разрешённый snapshot read-only в каталог
`/workspace/input/<attachmentSetRef>/`, безопасно нормализует имена и создаёт
`manifest.json` и `README.md` с ref, исходным именем, media type, размером,
digest и локальным путём. Коллизии имён разрешаются платформой; path из browser
никогда не становится filesystem path напрямую.

Шаблонизатор предоставляет типизированные значения:

- `.input.files` - файлы текущего сообщения или запуска;
- `.input.files_dir`, `.input.files_count`, `.input.manifest_path`;
- `.session.files` - все доступные текущей Session immutable inputs;
- `.run.files` - явно связанные с Run входы и результаты, разрешённые actor;
- `.workflow.files` - входной snapshot текущего Process;
- `.project.files` - только явно выбранные Project files, а не весь каталог.

Каждый descriptor содержит `name`, `media_type`, `size`, `sha256`, `path`,
`source`, `version` и `purpose`. Для небольшого набора шаблон может вывести
каждый путь циклом. Если файлов много, platform-controlled system block сообщает
каталог, количество и путь к manifest. При continuation такой блок о новых
файлах добавляется всегда, даже если пользовательский template не использовал
`.input.files`, поэтому новые входы нельзя потерять из-за ошибки шаблона.

Удаление является двухфазным:

1. `Delete` выставляет `deleted_at`, `purge_after = deleted_at + 30 days`,
   отзывает новые bindings/materialization и переносит Artifact в корзину.
2. `Restore` до `purge_after` возвращает прежнюю revision и access bindings.
3. Retention job после 30 дней или явная команда `Очистить корзину` удаляет
   S3 object/version и переводит запись в `PURGED`; минимальный audit tombstone
   без имени и content metadata остаётся по policy.

Уже materialized immutable input работающей Session не удаляется скрыто.
Интерфейс показывает затронутые активные Runs и для немедленного отзыва
предлагает их отменить. Новые turns и Sessions удалённый файл не получают.

## План реализации

### Блок 0. Зафиксировать решения и выровнять документы

- Зафиксировать принятые D1-D20 в документации и contracts.
- Обновить `docs/design/mvp-redesign/`, продуктовые, доменные, архитектурные и
  security документы до изменения контрактов.
- Обновить Issue #992 и PR #1006 с новой областью и ручной проверкой.
- Явно удалить требования совместимости с тестовым прототипом.

### Блок 1. Общая UI-оболочка

- Исправить один общий WebSocket connection и восстановление по event cursor;
  исключить `location.reload` и повторный mount route при reconnect.
- Растянуть search до доступной ширины; решения, connection state и user menu
  закрепить справа.
- Ввести единый `PageFrame` без искусственного узкого max-width.
- Ввести размеры модалок `sm`, `md`, `lg`, `xl`, `full` вместо глобального
  одинакового размера.
- Сделать общий accessible dropdown/popover: outside click, Escape, focus
  return, viewport collision, фиксированная ширина, перенос максимум на две
  строки.
- Добавить async combobox: debounce, server search, cursor pagination,
  infinite scroll, loading/error/empty states.
- Исправить размеры кнопок, radio/checkbox, focus и disabled states.
- Оставить FAB Kodex на всех поддерживаемых экранах.

### Блок 2. Главная и обзор Проекта

- Реализовать принятую компоновку главной из канонического редизайна: внимание
  пользователя, активная работа, последние Проекты и быстрый запуск без
  дублирующих KPI-карточек.
- Перестроить обзор Проекта в рабочую доску: важные решения, активные Процессы,
  последние результаты и ИИ-сотрудники.
- Бейджи состояния выровнять справа.
- Заменить непонятный правый столбец источника запуска на подписанное поле
  `Запущено через` с понятным icon+label и tooltip.
- При переходе из Проекта сохранять project context во всех create/list/detail
  маршрутах; sidebar выделяет тип текущей сущности и остаётся кликабельным.

### Блок 3. Файлы, результаты и новый запуск

- Растянуть Files workspace на доступную ширину.
- Заменить tabs `Файлы / Источники знаний / Результаты` на фильтр типа рядом с
  остальными фильтрами.
- Сузить details panel, дать списку/плитке большую часть ширины.
- Кнопку назвать `Загрузить`.
- Реализовать grid/list toggle, иконки типов, server search, фильтры, cursor
  pagination и infinite scroll.
- File preview сделать `xl` или `full`, без горизонтального overflow; preview
  занимает основное пространство, metadata - компактную правую панель.
- В новом запуске использовать тот же file picker, но с multi-select,
  selected counter и виртуализацией длинного списка.
- Добавить общий `AttachmentComposer` во все диалоги, Run/Session continuation,
  Process input и Human Gate: drag-and-drop всей поверхности, file picker,
  очередь без продуктового ограничения по количеству, progress/retry/cancel.
- До отправки крестик только отсоединяет файл от сообщения; отдельное действие
  с красной иконкой корзины запускает подтверждение удаления Artifact.
- На каждой file card/row/preview и в chat attachment есть delete action с
  tooltip и подтверждением; bulk delete доступен после выбора нескольких файлов.
- Добавить `/projects/:projectRef/files/trash`: cursor list, срок до purge,
  restore, bulk restore, purge selected и `Очистить корзину` с усиленным
  подтверждением.
- Реализовать `AttachmentSet`, bindings к message/turn/run/workflow/gate,
  manifest и безопасную materialization в workspace.
- Выровнять `Запустить` и `Отмена`, исправить выбор новой/существующей Session.
- Показывать lineage/version/source файлов и проверять capability `Файлы` на
  backend, а не только скрывать checkbox.

### Блок 4. Образы, окружения и секреты

- Добавить contracts, migrations и сервисный lifecycle для `RoleImage`,
  `RoleImageRevision`, build и promotion.
- В UI сделать каталог образов, dependency view `образ -> окружения`,
  Dockerfile editor с подсветкой, build status/log и revision history.
- Расширить Runtime Environment editor: image revision, env, secrets, tools,
  resources, volumes, network и RBAC profile.
- Реализовать secret-broker, metadata API, create/rotate/delete и выбранный
  owner reveal flow.
- В UI по умолчанию получать только descriptor и masked hint.
- Перегенерировать RuntimeRevision перед каждым turn из актуальных immutable
  revisions image/environment/secret grants/config.
- При изменении окружения не мутировать активную Session задним числом: новый
  turn получает новую RuntimeRevision, а UI показывает различия.
- Managed OAuth refresh фиксировать как следующую immutable credential
  revision через provider-sidecar callback, runtime-controller Secret readback
  и control-plane CAS. Rotating account выполняет один provider turn, API-key
  account использует отдельный bounded concurrency limit.

Матрица OAuth lifecycle:

| Переход            | Authority и condition                                                | Атомарный результат                                                     | Ошибка                            |
| ------------------ | -------------------------------------------------------------------- | ----------------------------------------------------------------------- | --------------------------------- |
| `claim`            | authorized account, exact current credential, capacity под row lock  | immutable RuntimeRevision и CLAIMED lease                               | node остается QUEUED              |
| `refresh detected` | provider-sidecar, exact execution ticket и изменившийся digest       | bounded callback с новым snapshot                                       | provider turn закрыто завершается |
| `materialize`      | runtime-controller, old Secret UID/RV/SHA и same provider account ID | новая immutable Secret с exact readback                                 | current revision не меняется      |
| `commit`           | control-plane, lease/fence/generation и CAS прежней revision         | новая credential revision и account current pointer в одной transaction | stale callback отклоняется        |
| `retry callback`   | те же lease и exact Secret metadata                                  | идемпотентный readback уже активной revision                            | несовпадающий повтор отклоняется  |
| `terminal/expiry`  | owner transition lease                                               | capacity освобождается; следующий turn pin-ит current revision          | частичная activation запрещена    |

### Блок 5. ИИ-сотрудники

- Перестроить список сотрудников в responsive tiles с avatar, purpose,
  readiness, model/runtime и environment.
- Avatar: upload/crop/remove, S3 object revision, безопасный generated fallback.
- Разделить detail на `Профиль`, `Инструкции`, `Runtime`, `Рабочее окружение`,
  `Возможности` без переполненного единого экрана.
- Provider/model/account pool/runtime выбираются явно.
- Добавить CodeMirror editors для Markdown instruction template и
  `config.toml` overlay.
- Overlay проходит TOML parse, allowlist/denylist параметров и preview
  эффективного config; provider credentials в нём запрещены.
- Каталог template variables показывает scopes и вставляет выбранную переменную
  или `range tools` в курсор.
- Environment selector использует общий rich async combobox.
- Публикации instructions immutable; draft/save/publish/rollback создают новые
  revisions.

### Блок 6. Kodex assistant

- Переименовать интерфейс в `Kodex`.
- В header постоянно показывать кнопку `+` нового разговора, history и close;
  новый диалог не прятать в dropdown.
- Сделать адаптивный drawer с нормальной шириной на desktop и bottom sheet на
  mobile; исправить режим на detail ИИ-сотрудника.
- Контекст экрана передавать как typed descriptor, а не pathname: entity kind,
  id, project, version, доступные commands и разрешения пользователя.
- Composer закрепить снизу; send - круглая icon-button внутри поля, microphone
  disabled с tooltip до STT.
- Composer поддерживает drag-and-drop и неограниченную очередь вложений;
  отправка ждёт `CLEAN` state или явно показывает заблокированный файл.
- Помощник генерирует короткое имя диалога и avatar для создаваемого сотрудника.
- Plan перечисляет каждую create/update/delete operation и все изменяемые
  параметры old/new. Скрытые изменения запрещены.
- План редактируется до apply; большие поля открываются в CodeMirror modal с
  syntax highlighting и validation.
- MCP/tool set помощника формируется из текущего context descriptor и RBAC.

### Блок 7. Live Run canvas и timeline

- Canvas занимает весь доступный viewport за вычетом shell; summary, legend,
  inspector и controls являются detached overlays.
- Использовать Vue Flow с pan, wheel zoom, fit, minimap/controls по необходимости
  и custom nodes/edges.
- Убрать текст с edges. Тип связи передавать цветом/штрихом/направлением и
  объяснять в закреплённой легенде.
- Node отображает avatar, сотрудника, Session name, state и activity animation.
  Будущие Process stages показываются ghost nodes.
- Click выбирает node и открывает компактный inspector. `Открыть подробно`
  открывает wide modal: identity, parent/root, parameters, RuntimeRevision,
  rendered prompts, turns, attempts, messages и tool calls.
- Timeline drawer показывает сообщения инициатора/parent agent, ответы агента,
  plan/status, tool start/progress/result и terminal/error events.
- Message/continuation composer в drawer поддерживает AttachmentSet, а timeline
  показывает безопасные file descriptors, preview/download/delete по RBAC.
- Tool output по умолчанию свёрнут, ограничен размером и очищен от секретов.
- Codex JSONL stream является источником tool events; hooks используются только
  как необязательное обогащение.
- Reconnect использует cursor catch-up и не очищает граф/выбранную node.

### Блок 8. Решения и Human Gates

- Заменить одинаковые большие карточки на decision inbox с группировкой по
  срочности, Проекту и Process.
- В строке/карточке показать предмет решения, кто запросил, контекст, влияние,
  срок и переход к точному run/node.
- Detail panel показывает полный вопрос, варианты, evidence, последствия и
  comment; primary action одна, reject/request changes вторичны.
- Решение может приниматься из inbox или Run inspector, но использует один API
  и OCC/version.
- Комментарий Human Gate принимает AttachmentSet через тот же общий composer.
- Закрытые решения остаются в истории и не смешиваются с ожидающими внимания.

### Блок 9. Интеграции

- Реализовать versioned YAML manifest: metadata, auth fields, capabilities,
  tools/MCP operations, approval policy, network destinations и health check.
- Добавить GitHub, GitLab, Jira, Confluence и email manifests без привязки к
  конкретной организации.
- Credential values создаются через secret-broker; manifest хранит descriptors.
- Опасные operations требуют Human Gate независимо от текста prompt.
- Локально поднять нейтральный fixture API для CRUD/approval/error/retry E2E.
- Создать отдельный тестовый GitHub repository и подключить bot credential для
  read/write integration E2E без использования рабочих репозиториев.

### Блок 10. Enterprise RBAC и OIDC

- Разделить platform role, Project membership и typed permissions.
- Поддержать scope: organization, project, entity kind и entity instance.
- Добавить grant conditions для конкретного ИИ-сотрудника, Процесса,
  окружения, интеграции и операции.
- OIDC groups map на platform roles и reusable permission sets; локальные
  bindings уточняют scope, но не повышают права выше policy.
- UI показывает человеческое описание каждого permission и effective access
  preview: `кто`, `что`, `в каком Проекте`, `над какими объектами`.
- Secret reveal, image build/promotion, privileged environment, integration
  mutation и full prompt view получают отдельные permissions.
- Backend проверяет точный resource scope; скрытие controls не является защитой.

### Блок 11. Автоматизации и знания

- Автоматизации получают edit, disable, delete, version history, next run и
  run history.
- Изменение опубликованной автоматизации создаёт revision; scheduler pins exact
  revision для каждого запуска.
- Knowledge UI пока показывает источники и indexing state только при наличии
  канонического backend API. Редактор chunks/vector search не имитируется
  статическими данными.
- После подтверждения текущей vector модели отдельным решением добавить chunk
  provenance, reindex и permission-aware retrieval diagnostics.

### Блок 12. Контракты, данные и эксплуатация

- Обновить OpenAPI/Proto/AsyncAPI до UI, не добавляя frontend-only mock paths.
- Добавить cursor pagination и stable sorting ко всем новым спискам.
- Добавить PostgreSQL migrations без migration legacy test data.
- S3 использовать для avatars, project files, run artifacts, session archives
  и backups с разными prefixes/policies.
- Добавить Artifact soft-delete/restore/purge state machine, `AttachmentSet`,
  message/turn/workflow bindings, workspace manifest и 30-дневную retention job.
- Delete немедленно закрывает новые download/materialization grants; purge
  удаляет exact S3 version с readback и не удаляет обязательный audit tombstone.
- Для production описать необходимые S3 env/secret names, buckets, lifecycle,
  encryption и backup restore, не добавляя значения в репозиторий.
- Обновить observability: websocket reconnect/gap, secret operations без value,
  image builds, environment readiness, assistant plans, run event lag и Human
  Gate latency.

### Блок 13. Локальная проверка и стабилизация

- Сначала исправить OIDC callback race и восстановить три незавершённых E2E.
- Выполнять E2E пакетами: накопить failures, исправить связанный блок, затем
  повторить пакет, а не пересобирать всё после каждого дефекта.
- Проверить desktop 1440/1920 и mobile; отсутствие overlap, horizontal page
  overflow и lost focus.
- Проверить create/edit/delete/version flows образа, окружения, секрета,
  сотрудника, Process, автоматизации и интеграции.
- Проверить два provider accounts, device auth и API key provider path.
- Проверить fixture integration и отдельный GitHub repository.
- Проверить одиночную Session, continuation, несколько Sessions одного агента,
  Process graph, tool events, cancel/retry, Human Gate и reconnect.
- Проверить drag-and-drop и большое число файлов во всех composer, chunk/batch
  upload, scan failure, duplicate names, manifest paths, continuation system
  notice и отсутствие недоступных attachments в workspace.
- Проверить delete/restore, 30-day clock, bulk purge, S3 readback, active Run
  warning и невозможность нового доступа к удалённому Artifact.
- Проверить отрицательные RBAC cases, secret non-disclosure и отсутствие файлов
  у сотрудника без capability.
- Снять Playwright screenshots ключевых экранов и проверить canvas pixels.
- Только после полного локального readback подготовить PR к owner acceptance.

## Порядок выполнения после owner OK

1. Решения и документация: блок 0.
2. Общая оболочка и API primitives: блоки 1 и cursor/realtime часть блока 12.
3. Параллельная волна A: файлы/новый запуск и образы/окружения/секреты.
4. Параллельная волна B: ИИ-сотрудники и RBAC/OIDC.
5. Параллельная волна C: assistant и integrations.
6. Live Run canvas/timeline после стабилизации event contracts.
7. Решения, автоматизации и project/home composition.
8. Локальный пакетный E2E, исправления, документация и owner acceptance.

В одном PR допустим большой согласованный MVP diff, но commits должны сохранять
границы блоков и позволять отдельно локализовать regression.

## Зафиксированные решения владельца

Приняты: `D1-A`, `D2-A`, `D3-A`, `D4-B`, `D5-A`, `D6-A`, `D7-A`, `D8-A`,
`D9-A`, `D10-A`, `D11-A`, `D12-A`, `D13-A`, `D14-A`, `D15-A`, `D16-A` и
добавленные владельцем `D17-A`-`D20-A`.

### D1. Формат образа

- **A, принято:** пользователь редактирует полный Dockerfile, а платформа
  валидирует и добавляет неизменяемый runtime contract/final wrapper Kodex.
- B: пользователь задаёт base image, packages и install script, Dockerfile
  генерирует платформа.
- C: произвольный Dockerfile выполняется полностью как есть. Максимальная
  гибкость, но можно удалить runner contract и усложнить поддержку.

### D2. Связь образа и окружения

- **A, принято:** окружение pin-ит exact promoted image revision/digest;
  обновление выполняется явно.
- B: окружение всегда следует за latest promoted revision.
- C: образ выбирается у сотрудника, окружение содержит только runtime settings.

### D3. Kubernetes policy окружения

- **A, принято:** typed resources/network/RBAC profiles; effective policy
  не превышает права пользователя и admission policy платформы.
- B: raw Kubernetes YAML с policy engine и отдельным preview/admission flow.
- C: только env/secrets/tools, а Kubernetes policy настраивает оператор вне UI.

### D4. Просмотр значения секрета

- A: write-only, доступны masked hint и rotation; plaintext нельзя повторно
  открыть. Это соответствует действующим security-документам.
- **B, принято:** отдельное право, свежая OIDC
  re-auth, одноразовый короткоживущий ответ напрямую из secret-broker, no-store,
  полный audit факта; plaintext не хранится в PostgreSQL и не кэшируется.
- C: хранить зашифрованную копию в PostgreSQL и раскрывать обычным API. Не
  рекомендуется из-за второй копии секрета и расширения boundary.

### D5. Маска секрета

- **A, принято:** типизированный `display_hint`, суммарно не более 15%,
  hard cap 12 символов; короткие и binary значения не показывать.
- B: всегда первые 4 и последние 4 символа.
- C: пользователь сам задаёт несекретную подпись, части значения не хранить.

### D6. Граница secret writer

- **A, принято:** отдельный `secret-broker` с минимальным namespace-scoped
  Kubernetes доступом. Control services размещаются в `kodex-system`, а один
  выделенный installation runtime namespace `kodex-runtime` содержит только
  agent Pods, PVC, execution tickets, provider projections и versioned runtime
  Secrets. Namespace-per-Project не создаётся: Project остаётся логической
  tenant-границей control-plane, а workload связывается с ним через
  server-owned refs, immutable RuntimeRevision, labels и admission policy.
- B: control-plane напрямую пишет Kubernetes Secrets.
- C: External Secrets Operator и внешний secret store обязательны для всех
  установок.

### D7. Модель доступных инструментов

- **A, принято:** image build обнаруживает/проверяет executable,
  environment выбирает поднабор и задаёт описания, readiness перепроверяет.
- B: окружение хранит произвольный список без проверки образа.
- C: полностью автоматическое обнаружение без пользовательских описаний.

### D8. Шаблонизатор инструкций

- **A, принято:** ограниченный Go `text/template`, typed namespaces,
  allowlisted функции, `range` для tools, validate/preview.
- B: Mustache/Handlebars без произвольных функций; проще, но слабее условия.
- C: Liquid-подобный язык с большим собственным runtime.

### D9. Graph node

- **A, принято:** node = логическая agent Session, turns/attempts/tools
  находятся в timeline; новая Session того же сотрудника = новая node.
- B: node = отдельный turn/attempt, Sessions только группируют nodes.
- C: переключатель представления Session/turn с двумя раскладками.

### D10. Источник tool-call timeline

- **A, принято:** нормализовать `codex exec --json` JSONL; hooks только
  опционально обогащают события.
- B: hooks являются основным источником.
- C: показывать только итоговый ответ агента без детальных tool calls.

### D11. Компоновка Live Run

- **A, принято:** full-bleed canvas, detached summary/legend/inspector и
  выезжающий activity drawer.
- B: постоянный split 60/40 между canvas и timeline.
- C: canvas и timeline как отдельные tabs.

### D12. Компоновка Kodex assistant

- **A, принято:** adaptive drawer 520-640 px, явные new/history controls,
  sticky composer; mobile bottom sheet.
- B: постоянная левая колонка диалогов внутри широкого drawer.
- C: полноэкранная modal/workspace вместо drawer.

### D13. Ширина страниц и модалок

- **A, принято:** full available page width и semantic modal sizes;
  preview/editor используют `xl/full`, простые подтверждения `sm/md`.
- B: увеличить один глобальный max-width всех модалок.
- C: все модалки сделать полноэкранными.

### D14. Представление решений

- **A, принято:** decision inbox + detail panel/modal с группировкой и
  одной primary action.
- B: kanban по состояниям `Ожидает / Изменения / Завершено`.
- C: master-detail split на весь экран.

### D15. Аватар сотрудника

- **A, принято:** S3 upload/crop/remove, immutable revision и generated
  fallback; URL руками не вводится.
- B: upload или внешний URL.
- C: только генерация помощником без ручной загрузки.

### D16. Realtime главной и других экранов

- **A, принято:** один WebSocket transport, store reducers, cursor
  catch-up по HTTP и обновление данных без route reload.
- B: SSE для server events и HTTP mutations.
- C: polling с visibility-aware interval.

### D17. Вложения во всех диалогах

- **A, принято:** общий `AttachmentSet` и `AttachmentComposer` для Kodex,
  Run chat, Session continuation, Process/Workflow input и Human Gate. Число
  файлов не ограничено продуктом; transport использует bounded chunks/batches.

### D18. Передача файлов агенту

- **A, принято:** read-only workspace directory + immutable manifest +
  типизированные `.input.files`, `.session.files`, `.run.files`,
  `.workflow.files`, `.project.files`. Continuation всегда получает
  platform-controlled notice о новых файлах.

### D19. Удаление и корзина

- **A, принято:** soft delete, восстановление в течение 30 дней, retention purge
  и явная необратимая очистка корзины с удалением exact S3 version.

### D20. Удаление входа активной Session

- **A, принято:** удаление отзывает будущий доступ, но не меняет immutable
  snapshot уже работающего Pod. Для немедленного отзыва UI предлагает отменить
  затронутые Runs и показывает последствия до подтверждения.

## Критерии готовности MVP

- Все решения D1-D20 утверждены и отражены в документах, contracts и UI.
- Все перечисленные сущности создаются, редактируются и проходят lifecycle без
  fake data и скрытых ручных операций.
- Секреты не раскрываются без выбранного отдельного flow и никогда не попадают
  в обычные read-модели.
- Live Run показывает Session graph, realtime timeline и tool calls после
  reconnect без перезагрузки страницы.
- UI пригоден на широком desktop и mobile, dropdown/modal/file/assistant cases
  воспроизводимо работают.
- Вложения работают во всех chat/composer surfaces; prompt variables,
  workspace manifest, удаление, восстановление и purge доказаны E2E.
- Полный локальный E2E пакет пройден на exact SHA; production не затронут.
- PR содержит ручную проверку владельца, список оставшихся осознанных
  ограничений и не содержит слоя совместимости с прототипом.
