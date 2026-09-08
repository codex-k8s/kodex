---
id: OPS-DOC-1260
title: Карта выполнения полной MVP-приёмки и две вкладки WS v2
type: operation-plan
status: approved
owner: developer
version: 1.1.5
updated: 2026-09-08
---

# Назначение и границы доказательства

Issue [#1260](https://github.com/codex-k8s/kodex/issues/1260), итоговая приёмка
[#1031](https://github.com/codex-k8s/kodex/issues/1031), независимые релизы
[#1223](https://github.com/codex-k8s/kodex/issues/1223).
Карта составлена по [каноническому плану](mvp-1031-acceptance.md),
[авторизации](mvp-1241-auth-proof.md), [интерфейсу](mvp-1240-ui-proof.md),
[lifecycle](mvp-1239-lifecycle-proof.md), [runtime](mvp-1198-runtime-proof.md),
[интеграциям](mvp-1242-integrations-proof.md) и [голосу](mvp-1243-voice-proof.md).
Она сохраняет 61 MVP-UI и три CFG строки. Несколько аспектов MVP-UI-19
закрываются совместно. Отсутствие driver обозначено явно; утверждение о
наличии реализации берётся из профильного proof, а не выводится из NOT RUN.

Все live-ячейки ниже первоначально **NOT RUN** для нового финального кандидата.
Исторический PASS не переносится на эту карту автоматически. Колонка
«Остаток оснастки» означает частичное покрытие указанным entrypoint, а не
доказанный дефект приложения. Полный ID получает PASS только после всех
вариантов и общих измерений. Точный SHA проверок оснастки закрепляется в PR;
эта документация сама по себе не является тестовым evidence.

# Входы и prerequisites

Все browser-команды запускаются из `services/staff/control-center`.
Обозначения в таблице разворачиваются в следующие публичные входы:

| Код | Команда / сценарий | Граница |
| --- | --- | --- |
| S:имя | `npm run test:e2e:synthetic -- имя.synthetic.spec.ts` | Локальный изолированный synthetic HTTP/UI; только поведение компонента. Для S:synthetic — `npm run test:e2e:synthetic -- synthetic.spec.ts`. |
| U:путь | `npm run test:unit -- src/путь` | Герметичный frontend unit, не browser/live. Каталог означает всю профильную группу. |
| L:название | `npm run test:e2e -- --project web-only-desktop-chromium` | `e2e/web-only.spec.ts`: названный test служит указателем. Suite serial и зависит от ранее созданных fixtures; одиночный grep не заменяет prerequisites. |
| R | `npm run test:e2e -- --project web-only-mobile-chromium` | `e2e/responsive.spec.ts`, зависит от полного desktop проекта. |
| I:название | `npx playwright test --config playwright.integration.config.ts` | `e2e/integration-path.spec.ts`: synthetic установленный adapter и optional GitHub/API-key профили различаются; credentials задаются только по README оснастки. |
| M | `KODEX_E2E_PROFILE=mattermost npm run test:e2e` | Отдельный optional Mattermost deployment и ограниченная учётная запись. |
| N | `npx playwright test --config e2e/session-renewal.config.ts` | Новый ограниченный реальный session-only сценарий, описан ниже. |
| BUI | `npx playwright test --config e2e/ui-acceptance.config.ts` | Независимые browser variants чтения/геометрии; opt-in создаёт только два собственных проекта. Полные IDs остаются NOT RUN до остальных вариантов. |
| RI | `tools/dev/local-role-image-supply-chain-e2e.mjs` из корня | Операторский профиль полного image lifecycle по [плану](mvp-1031-acceptance.md); QA не получает Kubernetes/root credentials. |
| W | `tools/dev/runtime-workspace-acceptance.mjs` из корня | Операторский runtime/workspace профиль; exact сценарии/аргументы из текущего `--help`, не writable canary вместо агента. |
| ST | `tools/dev/stt-http-acceptance.mjs` и direct adapter профиль из [STT-плана](mvp-1031-acceptance.md#защищённый-http-smoke) | Новый платный POST только после допуска, durable intent, без автоматического повтора UNKNOWN. Browser hardware отдельный. |

| Fixture | Необходимое состояние |
| --- | --- |
| B | Разрешённый disposable URL после maintenance, exact serving manifest/readback, отдельная legitimate OIDC/API session, роли owner/operator/reader/no-STT/no-delegation; QA получает только ограниченный private storageState. |
| V | ru/en; 1280×900, 1440×900, 1920×1080, 2560×1440, 2900×1600, 390×844, 768×1024; long labels, loading/empty/error/forbidden/ready. Chromium, Firefox, WebKit — отдельные browser результаты. |
| C | Две организации, три проекта (полный/ограниченный/недоступный), больше двух страниц 20–50 элементов, длинные названия, пустой каталог; Kanban больше шести в каждой колонке. |
| A | Реальный provider account с доступной model/effort, admitted runner, Agent/Workflow/assistant, gate/continuation/retry/cancel; выполнение provider может требовать отдельного платного допуска. |
| E | Опубликованное окружение, draft и старая revision, permissions/re-auth, минимум два потребителя для impact, active pinned attempt, encrypted staged Secret всех трёх типов. |
| F | Синтетические файлы/JSON/TXT, upload/scan/read/trash, valid/invalid SkillBundle, память с provenance/retention, foreign/readonly/quota fixtures без реальных пользовательских данных. |
| G | Typed definition/connection/grant, exact resource scope, actor/recipient и Human Gate; отдельные разрешённые реальные vendor fixtures. Новая definition не расширяет старые grants. |
| MAIL | Разрешённые SMTP/IMAP и отдельный POP3 test mailbox, TLS/STARTTLS и bounded external effects; полный перечень операций ниже. |
| STT | Fresh admin config/probe/model/credential binding + exact permission; отдельный hardware microphone и девять containers; test credential остаётся server-side. |
| IMG | BuildKit/scan/SBOM/provenance/admission/promotion/node-pull readiness, exact immutable digest, новый runner в RoleImage; старые pins сохраняются. |
| GIT | Выделенный test repo/branch/path, read и отдельно write-back permission; root выполняет разрешённый merge, QA без owner PAT. |
| SRE | Root выполняет repo-owned readback/fault profile после code-first допуска; QA не получает SSH/kubeconfig. |

# Полная карта 64 строк

В каждой строке `[ ]` относится ко всем вариантам. Выполнять независимые
неплатные UI/read сценарии первым пакетом; общий blocker исправить до зависимых
runtime/vendor запусков. Для каждого нового эффекта сохраняется исходный
intent и authoritative receipt; смена prefix не разрешает повтор UNKNOWN.

| Требование | Обязательные варианты и ожидаемый результат | Публичный вход | Fixtures | Остаток оснастки / ручное выполнение | Live |
| --- | --- | --- | --- | --- | --- |
| [ ] MVP-UI-01 | Проверить ru/en, literal и зарегистрированные динамические/server keys. `SYSTEM_BASE_ROLE_IMAGE`, `CONFIG_OVERLAY_INVALID_OR_PROTECTED`, `workboard.sourceHint` локализованы; отсутствующий ключ закрыто ломает статическую проверку. Сырой `i18n:` не виден. | S:ui-proof,synthetic; U:app/i18n,shared/ui/server-message-catalog.test.ts | B,V | частичное UI + статическая полнота; все экраны вручную | NOT RUN |
| [ ] MVP-UI-02 | Переключить loading/connected/reconnecting и длинную подпись статуса. Центры badge, решений и пользователя совпадают; высота header не меняется. | S:synthetic; L:финальная визуальная приёмка: badges | B,V | реальная смена connected/reconnecting вручную | NOT RUN |
| [ ] MVP-UI-03 | На всех контрольных viewport открыть PageFrame, двухколоночные детали, Template variables, RoleImage и FAB. Симметричные отступы, панели и текст в границах экрана; горизонтально не прокручивается вся страница. | S:ui-proof,cards; L:финальная визуальная приёмка: desktop 1440; R | B,V | семь ширин общих fixtures; каждый live detail вручную | NOT RUN |
| [ ] MVP-UI-04 | На главной оставить один содержательный блок и несколько блоков. Нет пустой половины layout; решения/ошибки остаются раньше справочных данных. | S:synthetic; U:pages/HomePage.layout.test.ts | B,V | live empty/один/несколько блоков вручную | NOT RUN |
| [ ] MVP-UI-05 | В компактном списке видно не более шести строк; разворачивание открывает модалку с тем же порядком, серверным поиском и total. В обоих видах страницы 20–50, внутренняя догрузка без дублей и полного выгружания каталога. | S:synthetic,ui-proof; U:features/home/result-catalog.test.ts | B,V,C | server total, concurrent changes, обе страницы вручную | NOT RUN |
| [ ] MVP-UI-06 | Быстрые действия находятся сверху рядом с запуском. Проверить создание сотрудника/процесса и загрузку с выбранным проектом и без него; на узком экране доступно компактное меню. | S:synthetic; L:первый вход, горячий помощник и первый Проект | B,V | каждый quick action с project/без project вручную | NOT RUN |
| [ ] MVP-UI-07 | В новой и авторизованной сессии manifest/icons/service worker возвращаются с корректным типом и origin без Keycloak redirect/CORS. Публичное исключение не открывает API/данные. Сверить render exact paths. | U:shared/config/pwa-contract.test.ts; L:административные экраны и security boundary дают ожидаемый readback | B,SRE | публичные assets до/после login и render вручную | NOT RUN |
| [ ] MVP-UI-08 | Workboard без warning; readiness/agents для живого окружения работают сквозь generated route/RPC/owner. Stale/hidden ref не запускает бесконечный polling; route skew не маскируется пустым результатом. | L:runtime сотрудника публикует policy, config.toml overlay и окружение; U:features/runtime/store.test.ts | B,E | hidden/deleted/dependency failure и stale polling вручную | NOT RUN |
| [ ] MVP-UI-09 | Чат открывается большой модалкой: слева история с серверным поиском/пагинацией, создание/переименование/архивирование; по центру сообщения и composer. Проверить контекст разных сущностей, inspector, вложения/DnD, подтверждения, Escape и предупреждение о несохранённом вводе. Mobile полноэкранный, tablet history overlay. | S:ui-proof,synthetic; L:глобальный и проектный Kodex принимают вложения через полный composer lifecycle; L:контекстный Kodex показывает полный редактируемый план без скрытых изменений | B,V,C,A | rename/archive/history search, все scope и unsaved close вручную | NOT RUN |
| [ ] MVP-UI-10 | Новый подзаголовок проекта, не более двух читаемых карточек в строке. Назначение, статус, агрегаты и свежесть получены с сервера; icon actions открывают нужные сущности/формы запуска без случайного клика всей карточки. | S:cards; L:первый вход, горячий помощник и первый Проект | B,V,C | actual aggregate freshness и все actions вручную | NOT RUN |
| [ ] MVP-UI-11 | Прожить серверный expiry в нескольких вкладках: single-flight refresh с jitter, новый одноразовый WS ticket, сохранённый cursor без дублей. 401/403 завершает bounded retry одним re-auth. Нет tokens в JS storage/WS URL; provider expiry отличим от SSO. | N; L:вложенный Процесс показывает live graph и Human Gate переживает reconnect; U:features/session,features/realtime | B,A | N покрывает natural refresh двух вкладок; expiry/401/403/suspend/потери новых events отдельно | NOT RUN |
| [ ] MVP-UI-12 | Селектор проекта расположен между логотипом и поиском; дублирующего control слева нет. Поиск занимает остаток, пользовательские действия сохраняют своё место. | S:synthetic; U:app/navigation-context.test.ts | B,V | live header ru/en вручную | NOT RUN |
| [ ] MVP-UI-13 | Rich selector проекта показывает назначение/статус/агрегаты, серверный поиск, страницы 20–50 и infinite scroll. Проверить повтор cursor, duplicate rows, loading/error/empty, длинный текст, клавиатуру, clear, outside click, route change и focus return. | S:ui-proof; U:shared/ui/AsyncEntityPicker.test.ts | B,V,C | project-specific search, duplicate token, scope/route change вручную | NOT RUN |
| [ ] MVP-UI-14 | Во всех редактируемых textarea и Markdown/YAML/TOML/JSON/Dockerfile редакторах общий STT control; нет перекрытия текста/resize/send. В Secret, password, credential, private key, readonly и disabled он отсутствует даже у STT-enabled пользователя. | S:voice; U:shared/ui/voice-input.test.ts | B,V,STT | hardware и все типы редакторов live вручную; чувствительные поля без микрофона | NOT RUN |
| [ ] MVP-UI-15 | Editor, preview и materialized preview сохраняют общую высоту с каталогом переменных. Скролл внутренний, соседние секции не смещаются. | S:runtime-detail; L:история инструкций позволяет откатиться к опубликованной версии | B,V,C | все три preview и длинные переменные вручную | NOT RUN |
| [ ] MVP-UI-16 | `POST /api/v1/prompt-templates/preview`: plain text, все scopes, file/tool ranges, неизвестная/недоступная переменная, неверный тип, смена revision. Валидный preview без 500; пользовательская ошибка даёт line/column 4xx; secrets не материализуются. | L:runtime сотрудника публикует policy, config.toml overlay и окружение; S:automation-preview | B,E,A | все scopes/range/errors и exact preview/provider digest вручную | NOT RUN |
| [ ] MVP-UI-17 | Account-specific versioned model catalog: Astra, Sol/Terra/Luna, 5.5/5.4/mini и Spark только при реальной доступности. Spark не предлагается обычному API key; исчезнувшая выбранная модель не заменяется молча. | S:models; I:опциональный API key account выполняет run с exact affinity | B,A | каждый реальный account/model и исчезнувший selection отдельно | NOT RUN |
| [ ] MVP-UI-18 | Effort/default получены из model capabilities. Поддерживаемая пара сохраняется в immutable runtime revision; несовместимая отклоняется перед turn. Проверить смену account/model и недоступный прежний effort. | S:models; I:опциональный API key account выполняет run с exact affinity | B,A | все допустимые model/effort и отрицательные пары live отдельно | NOT RUN |
| [ ] MVP-UI-19 | Rich provider selector: однострочный badge, стабильная ширина, server search/cursor, keyboard flow. Отдельно воспроизвести lifecycle, credential, health, model, capacity и permission причины; есть доступный переход исправления и возврат. Health подтверждает только свежую credentialed catalog reachability данной учётной записи, не provider-wide SLA: пустой verified catalog может иметь успешный health при отсутствии совместимой модели; stale/failed observer даёт UNKNOWN. Проверить current account/credential и candidate runtime profile pins, смену профиля одного provider, stale response/cursor. До выбора модели selectable не означает allowedToSubmit; исчерпанная capacity и UNKNOWN health не запрещают допустимый queued launch. Admin без контекста не выдаёт model/actor NOT_EVALUATED за готовность. | S:models; U:features/providers | B,V,C,A | обе поверхности ID19: selector и authoritative readiness; все причины/queued launch вручную | NOT RUN |
| [ ] MVP-UI-20 | Tab/Shift+Tab выполняют indent/outdent в каждом code editor, включая выделение и undo. Доступная команда выхода фокуса работает; обычные textarea сохраняют стандартный Tab. | S:voice,runtime-detail; U:features/assistant/code-editor.test.ts | B,V | Tab/ShiftTab/undo/focus escape каждого editor вручную | NOT RUN |
| [ ] MVP-UI-21 | Overlay allowlist: `model_reasoning_effort`, `personality`, `allow_login_shell=false`, `history.persistence`. Проверить диагностику, completion/hover и versioned schema. Provider/model/credentials/policy/MCP/environment overrides отклонены; draft/validate/publish/rollback сохраняют canonical digest. | S:runtime-detail; L:runtime сотрудника публикует policy, config.toml overlay и окружение | B,E,A | protected TOML, completion/hover, все revision transitions вручную | NOT RUN |
| [ ] MVP-UI-22 | Один общий async selector поддерживает single/multi, search, loading/error/empty, pagination, clear, disabled reason, Escape, outside click и возврат фокуса без геометрических скачков. | S:ui-proof,resource-selection; U:shared/ui/AsyncEntityPicker.test.ts | B,V,C | все single/multi selectors с реальными cursors вручную | NOT RUN |
| [ ] MVP-UI-23 | Exact environment edit action виден при достаточном праве и возвращает к агенту. Без права кнопки нет, а прямой HTTP update отклоняется владельцем. | L:runtime сотрудника публикует policy, config.toml overlay и окружение | B,E | точный return URL и прямой backend deny вручную | NOT RUN |
| [ ] MVP-UI-24 | Runs имеет только Kanban. Не более шести карточек по высоте колонки, независимые cursors/scroll, строки названия/исполнителя не превращаются в один символ; timestamp/badge не растягивают карточку. Горизонтальный scroll только внутри board. | L:workboard сохраняет контекст Проекта, а запуск выбирает файлы и сессию; U:features/workboard/run-board.test.ts | B,V,C,A | четыре большие независимые live колонки и переходы вручную | NOT RUN |
| [ ] MVP-UI-25 | `trash-toolbar` есть только в корзине. Details появляется только при выборе файла; обычный режим без выбора занимает всю ширину, пустой столбец/зазор отсутствует. Selection и смена режима не ломают layout. | S:resource-selection; L:финальная визуальная приёмка: files workspace | B,V,F | active/trash/details/selection live вручную | NOT RUN |
| [ ] MVP-UI-26 | Empty files workspace занимает рабочую высоту с отступами со всех сторон. Upload и DnD работают во всей безопасной области; loading/empty/content не скачут. | L:файл загружается, просматривается, привязывается и скачивается; R | B,V,F | пустое состояние и полный DnD lifecycle на всех ширинах вручную | NOT RUN |
| [ ] MVP-UI-27 | Все восемь проектных разделов всегда доступны в верхнем меню. Режим всех проектов группирует только доступные сущности; файлы начинают с папок проектов. Выбор проекта меняет server filter и сбрасывает cursors/selection/realtime, чужие данные не выдаются. | L:workboard сохраняет контекст Проекта, а запуск выбирает файлы и сессию; U:features/catalogs | B,C,F | восемь каталогов, все проекты, tenant/revoke/cursors вручную | NOT RUN |
| [ ] MVP-UI-28 | У готового агента один итоговый badge, центрального «Сейчас / Готов» и зарезервированной пустоты нет. Реальная активная работа показывает компактную ссылку на запуск. | S:cards; L:ИИ-сотрудник выполняет первый запуск и отдаёт файл | B,V,A | готовность/активный run и live invalidation вручную | NOT RUN |
| [ ] MVP-UI-29 | Avatar upload: success, замена, неверный тип/размер, scan rejection, отказ после upload до binding, duplicate retry. До успешной атомарной привязки объект не виден ни в active, ни в trash; прежний avatar остаётся до успеха, cleanup bounded. | U:features/agents/detail/avatar.test.ts; L:административные экраны и security boundary дают ожидаемый readback | B,F,A | полная scan/failure/duplicate/cleanup матрица не имеет отдельного live driver | NOT RUN |
| [ ] MVP-UI-30 | Variable catalog даёт authoritative available/disabled reason. Без files capability недоступны коллекция, count, dir и manifest. Смена Agent/role/runtime инвалидирует старую страницу и preview; validate/publish также отклоняют недоступную переменную. | S:runtime-detail; L:сотрудник без capability Файлы не получает файл | B,E,A,F | все descriptors, stale catalog и publish denial вручную | NOT RUN |
| [ ] MVP-UI-31 | Actor без project.manage не выдаёт это право агенту/процессу/интеграции. Проверить requested/effective/unavailable как пересечение actor, delegation ceiling, organization/project, agent grants, step requirements, runtime readiness и exact integration scope; свежая revision перед turn/retry/continuation. | L:enterprise RBAC объясняет точечный allow и отказывает другому сотруднику; I:synthetic READ/WRITE, Human Gate и exact retry | B,A,G | полное пересечение и fresh turn/retry/continuation вручную | NOT RUN |
| [ ] MVP-UI-32 | Карточки процессов не шире двух колонок: этапы, уникальные агенты, parallel groups, Human Gate, активные запуски/решения и свежесть. Нет N+1 чтения этапов/запусков; actions соблюдают permission. | S:cards; L:вложенный Процесс показывает live graph и Human Gate переживает reconnect | B,V,A | projection/N+1, permission и все aggregates вручную | NOT RUN |
| [ ] MVP-UI-33 | Кнопка запуска справа в заголовке, центр совпадает с badge. Панели во всю высоту нет. Открывается полная форма, а не немедленный запуск; disabled reason различает неопубликованность, права и readiness. Действие сохраняет геометрию при dirty/busy/denied; owner aggregate совпадает с actual launch admission. Проверить Workflow-only launch permission без прямого agent.launch или files capability; transient provider/capacity состояние отображается отдельно от допустимости постановки в очередь. | S:cards; L:вложенный Процесс показывает live graph и Human Gate переживает reconnect | B,V,A | workflow-only permission, queueability, dirty/busy/denied вручную | NOT RUN |
| [ ] MVP-UI-34 | Назначение и результат этапа используют общий template editor. Preview координатора/исполнителя показывает insertion points, порядок и provenance Agent/workflow/step/input/files/tools/integrations. Preview и реальный run имеют один renderer и exact revisions. | S:runtime-detail,automation-preview; L:вложенный Процесс показывает live graph и Human Gate переживает reconnect | B,E,A,F,G | все slots и exact actual provider input вручную | NOT RUN |
| [ ] MVP-UI-35 | Шаблон со всеми semantic slots не получает дублей; пропущенные slots добавляются детерминированными versioned service blocks. Вложенный пользовательский текст не подменяет границы блока; digests шаблона/snapshot/итога совпадают с runtime. | L:ИИ-сотрудник выполняет первый запуск и отдаёт файл; U:features/agents/detail | B,A,F,G | semantic slot cardinality, escaping и runtime digest вручную | NOT RUN |
| [ ] MVP-UI-36 | Изменить инструкции/model/effort/image/environment/files/skills/memory/tools/MCP/grants/policy и продолжить ту же session. Preview и одно user notice описывают безопасный typed diff previous/current revision; retry не создаёт второй turn/message, секретов/locator нет. | L:отмена закрывает граф, а retry создаёт новую попытку с lineage; S:runtime-detail | B,A,E,F,G | cold/resume history и каждый тип diff, exact one notice/turn вручную | NOT RUN |
| [ ] MVP-UI-37 | Типизированный VFS: проекты, применимые сущности, avatar/inputs/skills/memory/run results; breadcrumbs/search/pagination/bulk selection с exact refs/revisions. SkillBundle с SKILL.md проходит structure/scan/provenance/policy, память отдельная versioned сущность. MCP search/metadata/bounded preview/manifest ограничены pinned grant, не принимают filesystem authority и не выдают quarantined/deleted/foreign данные. | S:resource-selection; L:привязанный knowledge-файл доступен ИИ-сотруднику; L:корзина восстанавливает файл и необратимо удаляет точную S3-версию | B,F,A | skills/memory provenance/retention и MCP eligibility всех lifecycle вручную | NOT RUN |
| [ ] MVP-UI-38 | Вкладка «Ожидают» и счётчик помещаются в одну строку, доступное полное имя сохранено. | S:gate-navigation; U:pages/DecisionsPage.test.ts | B,V,A | live счётчик/aria/tooltip вручную | NOT RUN |
| [ ] MVP-UI-39 | Селектор подключений стилизован и ищет на сервере; scope/credential type/readiness/reason читаемы. Смена проекта очищает несовместимый selection и страницы. | S:synthetic; I:synthetic READ/WRITE, Human Gate и exact retry | B,V,C,G | все scopes/search/cursors/readiness и route reset вручную | NOT RUN |
| [ ] MVP-UI-40 | Project/recipient/capability используют зависимые rich selectors с серверным поиском; короткий recipient-kind enum без поиска. Проверить очистку stale selection и повторную backend-проверку grant. | I:synthetic READ/WRITE, Human Gate и exact retry; U:features/integrations | B,V,C,G | повтор same ref с новой version/pins и server deny вручную | NOT RUN |
| [ ] MVP-UI-41 | в UI и YAML задать SMTP/IMAP и отдельно ограниченный POP3. Проверить exact host/port/SNI, TLS/STARTTLS, auth, from/reply-to, mailbox, timeout и Secret refs; неизвестные/protected поля отклонены. Readiness каждого протокола использует рабочий credential/network path. POP3 не симулирует search/folders/threads/flags. Выполнить mailbox list, message list/search/read, thread read, attachment list/read, mark read/unread, move/archive/delete, draft create/update/delete, send/reply/reply-all/forward. Три connection policies: read без gate/write с gate, всё с gate, допустимое без добавочного gate; minimum package/platform policy не ослабляется. Проверить unknown write outcome, lost reply, receipt, audit, revoke и запрещённый mailbox. | S:mailbox,synthetic; I:synthetic READ/WRITE, Human Gate и exact retry | B,MAIL,G | локальные fake fixtures не vendor; полный SMTP/IMAP/POP3 operation matrix live вручную | NOT RUN |
| [ ] MVP-UI-42 | «Подробнее» открывает modal без роста карточки, zero-connection-notice отсутствует. Все операции ниже реально вызываются через grant -> MCP -> owner claim -> adapter -> provider -> receipt, а не только присутствуют в YAML. Для каждой есть positive fake-provider scenario, отрицательный exact scope, bounded pagination/output и failure path. | I:synthetic READ/WRITE, Human Gate и exact retry; I:опциональный GitHub READ и обратимый WRITE проходят через MCP; M | B,G | 157 capabilities не 157 PASS; каждый vendor/operation отдельно, полная карта OPS-DOC-1242 | NOT RUN |
| [ ] MVP-UI-43 | Preset и custom cron имеют один ScheduleRevision. Preview пяти occurrence совпадает с scheduler для timezone/DST/minimum interval; неверный cron диагностируется. Task идёт агенту или root coordinator, scopes automation доступны по правилам. CONTINUE_ONE добавляет task+notice один раз. Materialized preview использует тот же renderer для AGENT/WORKFLOW и first/bound Session. DRAFT описывает будущую ревизию от текущего пользователя; automation.revision недоступна до сохранения даже при прежних значениях полей. CURRENT_REVISION связан с exact saved spec/revision и автором исполнения, при отдельных текущих правах viewer. Проверить другого manager, отозванные права автора, stale spec/version, отсутствие preview effects и неусиление capabilities viewer-полномочиями. | S:automation-preview; L:автоматизация создаётся, редактируется, запускается и архивируется | B,E,A | AGENT/WORKFLOW, CONTINUE_ONE, пять occurrence DST и author/viewer revoke вручную | NOT RUN |
| [ ] MVP-UI-44 | Каталог окружений не выбирает первую строку автоматически и не вызывает detail endpoints заранее. Click выбирает; повторный click/Escape/outside снимают выбор; double click и клавиатурное действие открывают редактор. Action click изолирован; незавершённые запросы отменяются. | S:resource-selection; U:features/runtime | B,V,E | live click/cancel/Escape/outside/double click, late response вручную | NOT RUN |
| [ ] MVP-UI-45 | Tabs не дублируются command bar. Добавить переменную/secret можно только в соответствующей вкладке; глобальные draft/validate/publish остаются на стабильном месте с точными disabled reasons. | S:runtime-detail; L:runtime сотрудника публикует policy, config.toml overlay и окружение | B,V,E | контекст tabs, disabled reasons, stale image selection вручную | NOT RUN |
| [ ] MVP-UI-46 | Несохранённый ввод, server draft, validated revision и published различимы. Save не меняет active binding; publish только exact validated revision с fresh permission. Проверить уход со страницы/re-auth. Secret staged encrypted revision без plaintext readback и с bounded cleanup. | S:runtime-detail,rotation; L:runtime сотрудника публикует policy, config.toml overlay и окружение | B,E,F | dirty/save/validate/publish/discard/reauth каждого kind и encrypted cleanup вручную | NOT RUN |
| [ ] MVP-UI-47 | До publish открыть impact modal всех доступных потребителей с поиском/пагинацией, допустимые отмечены. Проверить отмену, без замены, выборочную замену, истёкший plan, OCC/permission conflict и чужой binding. Есть поэлементные receipts; running attempt остаётся pinned, следующий turn получает новую revision. | S:impact; L:runtime сотрудника публикует policy, config.toml overlay и окружение | B,E,C,A | два consumers, все receipts, expired plan и pinned attempt вручную | NOT RUN |
| [ ] MVP-UI-48 | `GET /api/v1/runtime-environments/{environmentRef}/readiness` и `/agents`: ready/not-ready, пустая/следующая страница, скрытый/удалённый ref. 404 только для отсутствующего/скрытого ресурса; ошибка inspector не роняет каталог. Проверить фактический deploy route. | L:runtime сотрудника публикует policy, config.toml overlay и окружение; U:features/runtime/store.test.ts | B,E,C | оба endpoint, следующая page, deleted/hidden/dependency вручную | NOT RUN |
| [ ] MVP-UI-49 | STRING и Base64 вводятся в textarea, JSON в общем code editor. Проверить padding/whitespace/размер Base64, JSON diagnostics/format/Tab, подтверждение очистки при смене типа. Plaintext исчезает после успеха/close/unmount и не сохраняется в storage/telemetry. | S:rotation; U:features/runtime-secrets | B,E | STRING/JSON/Base64, malformed, plaintext cleanup live вручную | NOT RUN |
| [ ] MVP-UI-50 | Secret create/rotate/reload и потерянный ответ дают одну committed revision. Malformed page (`items` null/undefined), projection lag и readback error дают локальный problem/retry, не стирают строки и не вызывают length/emitsOptions/subTree исключений. | S:rotation; U:features/runtime-secrets | B,E | create/rotate/reload, lag/lost ACK и malformed page live вручную | NOT RUN |
| [ ] MVP-UI-51 | Disable/revoke/delete различимы. Active turn, warm consumer, agent/pool/automation binding видны как blockers; queued cleanup не удаляет credential до освобождения. После cleanup tombstone убран из обычного каталога, audit/pinned history сохранены, stale page не оживляет account. | S:provider-lifecycle; I:опциональный API key account выполняет run с exact affinity | B,A | disable/revoke/delete, все blockers и queued durable cleanup вручную | NOT RUN |
| [ ] MVP-UI-52 | Device auth: первоначальный code, проверка реального статуса, re-auth той же account, expiry/cancel/retryable503/потерянный ответ/active-turn blocker. Bounded polling и exact retry не создают новый challenge/account; новая credential revision атомарно active без замены pinned turn. | S:provider-lifecycle; U:features/providers | B,A | реальный device first/check/reauth/expiry/cancel/503/UNKNOWN/pinned turn вручную | NOT RUN |
| [ ] MVP-UI-53 | На 499 мс запрос ещё не отправлен, на 500 мс отправлен без Enter. Проверить reset, Enter flush, trim/minimum length, clear, unmount, abort и stale response. HTTP kind только PROJECT/AGENT/WORKFLOW/RUN, ru/en локализованы. | U:features/search/model.test.ts; L:первый вход, горячий помощник и первый Проект | B,C | 499/500, Enter/abort/stale/clear/unmount browser вручную | NOT RUN |
| [ ] MVP-UI-54 | Найти и открыть каждый из четырёх типов: exact URL и detail endpoint совпадают с kind/ref/projectRef. AGENT не вызывает `/workflows/agt_*`. Proto enum, неизвестный kind и malformed projectRef закрыто блокируют переход. | U:features/search/model.test.ts; L:первый вход, горячий помощник и первый Проект | B,C,A | четыре exact routes/request kinds и malformed enum browser вручную | NOT RUN |
| [ ] MVP-UI-55 | самостоятельный stt-tts-service присутствует в фактическом render и registry; первый adapter только OpenAI transcription. TTS-заглушки нет. Audio/transcript не сохраняются, egress проходит exact gateway/TLS. | ST; S:stt-activation | B,SRE,STT | service/render/egress факты отдельным SRE readback; TTS не входит | NOT RUN |
| [ ] MVP-UI-56 | администратор создаёт/меняет versioned configuration через UI, проверяет OCC/retry/readback, credential binding и доступность. Проверить model-specific language/languages, keywords, prompt, temperature, chunking, stream policy и совместимость. Default берётся из versioned каталога; каталог adapter с `version/observedAt` доступен по праву управления системой до первой enabled configuration и credential. Это чтение не выдаёт microphone eligibility или provider READY. Неподдерживаемые параметры не уходят провайдеру, browser не выбирает key. | S:stt-activation,stt-catalog; ST | B,STT | admin configuration/catalog before first enable, actual binding/probe/OCC вручную | NOT RUN |
| [ ] MVP-UI-57 | org-scoped permission с выбранным проектом и без него; session/CSRF/Origin -> bounded multipart -> version-bound grant -> STT -> provider. Проверить все девять поддерживаемых контейнеров, 10 MiB/120 s limits, unsupported/truncated audio, отсутствие permission, revoked key, недоступную модель, timeout/rate limit/cancel. После возможного billable POST нет автоматического retry. | ST; S:voice | B,STT | org/project, девять контейнеров, все security/limit/error варианты отдельно | NOT RUN |
| [ ] MVP-UI-58 | реальные MediaRecorder capture/stop/cancel и вставка в textarea/CodeMirror в selection, одной undoable transaction с сохранением focus/scroll. Navigation/logout закрывают microphone tracks и запрос; browser deny/unsupported codec не вызывают console errors. Availability обновляется после отзыва права/config, sensitive/readonly поля исключены. Permissions-Policy допускает microphone только same-origin, не camera. | S:voice; ST | B,V,STT | реальный hardware MediaRecorder и browser matrix не покрываются synthetic | NOT RUN |
| [ ] MVP-UI-59 | фактические mTLS/application identity, exact ingress/egress, credential/model probe без billable POST, свежая availability, startup/join, cancel cleanup, organization/subject quota, bounded metrics и alerts с HTTPS runbook. Логи/trace не содержат filename, audio, prompt/keywords, transcript или credentials, в том числе после ошибочного запроса. | ST; U:shared/ui/voice-input.test.ts | B,SRE,STT | live identity/probe/quota/cancel/cleanup/metrics/log redaction отдельно | NOT RUN |
| [ ] MVP-UI-60 | реальный OpenAI smoke и сквозной HTTP/UI запрос распознают tracked MP3 fixture `services/internal/stt-tts-service/testdata/1-2-3-4-5.mp3` (46 364 bytes, SHA-256 `56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`). Нормализация допускает только регистр, пробелы и конечную пунктуацию; ожидается «раз два три четыре пять», не перестановка или пропуск слов. В отчёт записываются match/digest, не transcript. Тестовый credential задаётся отдельно; без него NOT RUN блокирует приёмку STT. | ST; S:voice | B,STT | direct adapter, protected HTTP, hardware capture/insertion — отдельные evidence | NOT RUN |
| [ ] MVP-UI-61 | Реальный agent-runner создаёт вложенный файл, читает, атомарно заменяет, удаляет и публикует другой результат с attempt provenance. Отдельно read-only inputs/skills/memory/credentials, quota, traversal/symlink и foreign session/project дают READ_ONLY/QUOTA_EXCEEDED/PATH_OUTSIDE_WORKSPACE/RUNTIME_IO_ERROR. Readiness canary убран, пользовательские файлы не тронуты. | W; L:ИИ-сотрудник выполняет первый запуск и отдаёт файл | B,A,F | реальный CODEX_SHELL, JSON/TXT provenance и все read-only/escape/quota варианты отдельно | NOT RUN |
| [ ] CFG-01 | «Образы ИИ-сотрудников» в навигации, active catalog/new/detail routes; полный Dockerfile editor с diagnostics/diff/history. Без Git создать UI recipe, validate/publish, выполнить изолированный build/scan/ SBOM/provenance/promotion и назначить окружению exact admitted image digest. Source требует отдельного права, secrets не проходят ARG/ENV/context/log. | RI; L:административные экраны и security boundary дают ожидаемый readback; S:writeback | B,E,IMG | UI recipe полный build/scan/SBOM/provenance/admission/promotion/node-pull и новый build после restore | NOT RUN |
| [ ] CFG-02 | создать UI IntegrationDefinition через синхронные форму/YAML, прочитать полный source по праву, edit/validate/publish/archive/history/copy. Проверить schema/semantic registry, risk/Gate/network/credential slots, immutable revisions, OCC/retry, old connection pins и compatibility preview. | S:synthetic,writeback; I:synthetic READ/WRITE, Human Gate и exact retry | B,G | UI Form/YAML полный lifecycle, source permission, restore/new revision и old pins вручную | NOT RUN |
| [ ] CFG-03 | SHIPPED загружается в пустую БД без Git и не редактируется на месте; UI copy разрешена. GIT импортируется с exact repo/ref/path/commit, loss/revoke credential даёт SYNC_BLOCKED без смены owner и прежних pins. Проверить explicit detach/copy, отдельный write-back plan/PR permission, sync после merge, audit и rollback как новую revision, не движение назад. | S:git-source,writeback; RI | B,IMG,G,GIT | SHIPPED/UI/GIT, revoke SYNC_BLOCKED, detach/copy/write-back/merge/sync, typed412/UNKNOWN/reload вручную | NOT RUN |

# Дополнительные обязательные разветвления

Эти варианты добавляются к строкам выше, не заменяют их:

- MVP-UI-36/61: cold/new thread получает bounded историю, provider resume —
  ровно одну текущую continuation notice. Каждый diff kind проверяется отдельно:
  instructions, model, effort, image, environment, inputs, skills, memory,
  tools, MCP, integrations, capabilities, policy. Реальный CODEX_SHELL делает
  mkdir/create/read/atomic-replace/delete; JSON и TXT results имеют exact attempt
  provenance. Read-only inputs/skills/memory/credentials/system, foreign
  session/project, traversal, symlink и отдельная quota failure обязательны.
- MVP-UI-47: минимум два непустых consumers; cancel, publish без замены и
  выборочная замена; APPLIED, NOT_SELECTED, CONFLICT, FORBIDDEN — отдельные
  outcomes. Search/cursor и старый pinned attempt проверяются независимо.
- CFG-01/02/03: restore старой revision без искусственной правки source создаёт
  новую forward draft revision; старые published pointer/pins сохраняются до
  явной публикации/rebind. RoleImage проходит новый build после restore.
  Source permission, поздняя history page после revoke/route change,
  initial typed412, UNKNOWN/reload — отдельные варианты. SHIPPED установка
  проверяется локальным disposable harness; существующий staging не очищается.
- MVP-UI-41: SMTP/IMAP mailbox.list, message.list/search/read, thread.read,
  attachment.list/read, mark_read/mark_unread, move/archive/delete,
  draft.create/update/delete, send/reply/reply_all/forward. POP3 — один maildrop,
  UIDL/header client filtering, без IMAP folders/server search/flags/threads.
  Три Gate policy, approve/reject/replay/revoke/restart, denied scope,
  UNKNOWN_OUTCOME/readback и отсутствие повторного effect проверяются отдельно.
- MVP-UI-42: по [операционной матрице](mvp-1242-integrations-proof.md) отдельно
  GitHub 41, GitLab 37, Jira 22, Confluence 16, Email 21, Mattermost 18,
  Synthetic 2 capabilities. Две Mattermost subscriptions system-only, не MCP.
  У каждого vendor отдельный live PASS/FAIL/NOT RUN; fake-provider не vendor PASS.
- MVP-UI-57/60: mp3, webm, ogg, wav, m4a, mp4, mpeg, mpga, flac;
  size/duration/timeout/quota/rate-limit/model/credential/revoke negatives.
  Tracked MP3 46364 bytes, SHA256
  `56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`.
  Match сохраняется как boolean/digest, audio/transcript не сохраняются.
- MVP-UI-11: N не покрывает logout, terminal401/403, absolute expiry,
  focus/suspend, ticket replay/expiry/uncertain CAS или потерю/дубли новых
  бизнес-событий. Для stream delivery нужен controlled producer + ожидаемый
  набор event IDs/cursors; пустой поток не является доказательством полноты.

# Две вкладки: ограниченный воспроизводимый сценарий

Вход N открывает две настоящие страницы одного BrowserContext. Исходный state
создаётся штатным OIDC/API-session helper оператором. Специализированный
`loadE2ESessionRenewalEnvironment` читает API state через private descriptor
guards и канонический `selectedSessionCookies`: exact control-origin BFF pair
и, если ingress его использует, полный OAuth2 Proxy single/chunks. IdP cookies,
чужие cookies и любые origins/localStorage закрыто отклоняются. Общий bootstrap
reader по-прежнему запрещает BFF cookies. Исправление #1268 устраняет отказ
оснастки до браузера, обнаруженный при первом live preflight; это не PASS
серверной авторизации. Тест не создаёт JWT,
не выполняет login, не меняет срок и не отправляет PUT вручную. Он читает
metadata и отклоняет preflight, если natural renewAfter уже прошёл, до него
меньше 15 секунд, он не помещается в бюджет либо законный срок не оставляет
45 секунд на readback. Требуется отдельная session family без конкурентного
Node-клиента/монитора. State после теста считается использованным и заново
не передаётся другому процессу; для следующего браузера оператор выдаёт новый.

Из точного чистого checkout после отдельного serving readback оператор задаёт:

```sh
export KODEX_E2E_BASE_URL=https://control.kodex.works
export KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
export KODEX_E2E_PROFILE=web-only
export KODEX_E2E_RESOURCE_PREFIX=<новый-уникальный-prefix>
export KODEX_E2E_STORAGE_STATE=<private-0600-storage-в-каталоге-0700>
export KODEX_E2E_RUN_TIMEOUT_MS=1200000
export KODEX_E2E_SOURCE_REVISION=<40-символьный-SHA-оснастки>
export KODEX_E2E_API_REVISION=<40-символьный-обслуживаемый-SHA-API>
export KODEX_E2E_PWA_REVISION=<40-символьный-обслуживаемый-SHA-PWA>
export KODEX_E2E_BROWSER=chromium
npx playwright test --config e2e/session-renewal.config.ts
```

SHA в metadata указывает оператор; N сам не доказывает serving provenance.
Необязательный `KODEX_E2E_SERVING_MANIFEST_SHA256` связывает окно с полным
component manifest root; source SHA в evidence называется `harness`.
Root прикладывает отдельный exact manifest/readback к тому же UTC окну.
`KODEX_E2E_BROWSER=firefox|webkit` выбирает следующий отдельный профиль;
отсутствующий browser/runtime capability остаётся NOT RUN с причиной.
Глобальный timeout ограничен общим environment loader до 1800000 мс,
workers=1/retries=0, trace/video/screenshots выключены. Error message содержит
только закрытое имя фазы; страницы закрываются до teardown, чтобы автоматический
error-context не снимал DOM. Output содержит только безопасные counters,
численные cursors, версии, UTC timestamps и digest. Исправление #1276 явно
создаёт через `testInfo.outputPath` файлы `session-renewal-safe-evidence.json`
и `session-renewal-safe-evidence.sha256` с mode0600 и эксклюзивным `wx`; прежний
artifact не перезаписывается. SHA-256 относится к точным байтам JSON.
`attach(path)` передаёт уже сохранённый файл reporter, поэтому JSON остаётся
доступен и при штатном `reporter=list`, включая FAIL после начала наблюдения.
Ошибка сохранения блокирует успешное завершение теста. Ticket/cookies/subprotocol
со значением ticket, response body, auth URL и содержимое сообщений не пишутся.

PASS требует одного штатного PUT200, ticket200 для обоих первоначальных и новых
WS, negotiated `kodex.session.v2`, закрытых старых WS, двух новых SESSION_READY,
сохранённого platform cursor и неизменного absoluteExpiresAt. Native WebSocket
наблюдается обёрткой конструктора без изменения аргументов или обработчиков
приложения; выбранный protocol сводится к v2/other. Бизнес payload из фрейма
не сохраняется. После успеха оставляется ещё 5 секунд для позднего второго PUT.
Неожиданный 4xx/5xx, requestfailed, pageerror, malformed metadata/frame или
SESSION_PROBLEM дают FAIL. Тест не кликает Stop и не запускает STT/provider.

# Локальная проверка оснастки

```sh
npm run test:unit -- e2e/session-renewal-proof.test.ts e2e/api-session-storage.test.ts e2e/session-renewal-evidence.test.ts
npm run test:e2e:synthetic -- session-renewal.synthetic.spec.ts
npx tsc --noEmit -p tsconfig.e2e.json
npx eslint e2e/session-renewal* --max-warnings 0
KODEX_E2E_CHECK_ONLY=1 npx playwright test --config e2e/session-renewal.config.ts --list
```

Synthetic unit проверяет достаточный законный budget, expired/malformed
metadata, отсутствие identity/secret в projection, требование старого close и
нового READY, cursor rollback, SESSION_PROBLEM, oversized/malformed frames.
Отдельный Chromium fixture использует две настоящие страницы и локальный
loopback WebSocket server: проверяет native frame/close events и неизменённый
protocol observer. Он не моделирует OIDC, refresh coordinator или ticket
authorization и не подменяет live N. Context7 `/microsoft/playwright/v1.61.0`: проверены
BrowserContext shared pages/storageState, page WebSocket frame/close events,
trace/video/screenshot configuration. Локальная ошибка проверки остаётся FAIL
до исправления и повторного запуска; точные команды/результаты — в PR.
Регрессия persistence запускает настоящий Playwright CLI с `reporter=list`
на безопасных локальных PASS/FAIL fixtures без browser/login/network;
проверяет наличие JSON, mode0600, exact digest, отсутствие отброшенного
содержимого фрейма, отказ повторной записи и ограничение размера. Context7
`/websites/nodejs_latest-v24_x_api`: проверены `fsPromises.open` с `wx`/mode,
`FileHandle.writeFile`, `sync` и `close`. Прежнее живое окно до #1276
с успешными assertions и потерянным подробным artifact сохраняет отдельные
статусы: assertions PASS, detailed evidence FAIL; файл не восстанавливается
из предположений, а новое длительное окно выполняется на финальном кандидате.

# Широкий независимый browser-профиль BUI

BUI расширяет приёмку после N и не запускает длительное ожидание renewal повторно.
Вместо последовательного общего `web-only.spec.ts`, который создаёт runtime и
вызывает provider, используется отдельный публичный `ui-acceptance.config.ts`.
Нужны отдельный GO, свежая legitimate API-only session по canonical loader,
точные harness/API/PWA revisions и обязательный serving manifest SHA-256.
Workers=1, retries=0, общий budget ограничен 1800000 мс; зависимые шаги после
общего session/shell blocker не выполняются, локальный FAIL не отменяет
независимые route groups. Новый шаг не начинается за 30 секунд до общего предела.

Оператор дополнительно к env N задаёт:

```sh
export KODEX_E2E_RUN_TIMEOUT_MS=1800000
export KODEX_E2E_SERVING_MANIFEST_SHA256=<64-символьный-digest-readback>
export KODEX_E2E_UI_EVIDENCE_DIR=<новый-отсутствующий-каталог-в-private-0700-parent>
export KODEX_E2E_UI_CREATE_PROJECTS=0
npx playwright test --config e2e/ui-acceptance.config.ts
```

`KODEX_E2E_RESOURCE_PREFIX` имеет 4–61 символ: начальная латинская строчная
буква, далее строчные буквы/цифры/дефис. `KODEX_E2E_UI_CREATE_PROJECTS=1`
разрешает только два новых проекта с собственным prefix. Перед каждым POST
записывается и синхронизируется durable intent; разрешение потребляется одним
POST `/api/v1/projects`. Код не повторяет неопределённый результат и не
изменяет старые проекты. Fixture остаётся для дальнейшей приёмки; отсутствие
созданного проекта после lost response сначала выясняется authoritative
readback, а не повтором под другим prefix. Существующий evidence directory
отклоняется до нового intent и не удаляется Playwright.

По умолчанию профиль читает до двух доступных проектов. Если создан свой
fixture, project variants используют только новые refs. Для отображаемых
конфигураций допустимо чтение существующей истории через UI; source/значения
не выгружаются в evidence. Request guard разрешает GET/HEAD/OPTIONS и штатные
session ticket POST/renewal PUT; остальные same-origin writes блокируются,
кроме единственного opt-in project POST. Provider Run, device start, STT,
publish, grants, delete и paid effects не входят в BUI. Чтобы service worker
не обходил browser request interception, BUI использует `serviceWorkers=block`.
Это ограничение профиля: install/update/offline и полнота MVP-UI-07 остаются
отдельными обязательными проверками.

План BUI:

- [ ] Session metadata200, authenticated shell и наблюдаемый native WS v2.
  Этот шаг не доказывает повторное потребление ticket, natural renewal или
  отсутствие потерь business events.
- [ ] 17 global route groups: home/projects/agents/workflows/automations/
  environments/secrets/members/files/runs/integrations/decisions/providers,
  PROMPT_TEMPLATE/ROLE_IMAGE/INTEGRATION_DEFINITION/SYSTEM_STT catalogs.
  Все группы на ru/en, desktop 1440 и mobile 390; route сохранён, h1 и header
  видимы, document overflow<=1px, нет видимых alert/i18n markers и новых
  HTTP 4xx/5xx, page/console/network errors. Причины намеренных abort считаются
  отдельно; после действия ожидается завершение текущих API requests.
- [ ] Home/projects/runs на всех семи контрольных ширинах 1280/1440/1920/
  2560/2900/390/768 и обеих локалях. Это geometry конкретных страниц, не
  доказательство каждого редактора, длинного label или populated состояния.
- [ ] Восемь разделов каждого выбранного проекта и переходы scope;
  существующие menu links, files/trash и environment inspector
  без автоматического выбора, click/Escape. Пустой fixture не доказывает
  populated selection, multiple pages или controlled delayed response.
- [ ] Rich project selector focus/ArrowDown/Escape/возврат фокуса и настоящий
  async GET search с empty readback; global search Enter/clear. Точные 499/500 мс,
  controlled stale response и все четыре маршрута результатов остаются
  отдельными вариантами исходной 64-матрицы.
- [ ] Kanban из четырёх колонок: при наличии прокручиваемой колонки настоящий
  mouse wheel меняет только её scrollTop. Если fixture не прокручивается,
  этот вариант NOT RUN; независимые server cursors и task delivery не выводятся
  из одной проверки прокрутки.
- [ ] Existing configuration detail/history open/Escape для PROMPT_TEMPLATE,
  ROLE_IMAGE и INTEGRATION_DEFINITION без write/restore; assistant modal
  open/read/close на desktop/tablet/mobile без нового чата, ввода или отправки.

Каждый шаг пишет отдельный PASS/FAIL/NOT RUN variant в
`ui-acceptance-safe.jsonl` (0600) и синхронизирует файл. В конце добавляется
applicability всех 64 требований: partial variants не повышают
`fullRequirementStatus` из NOT RUN. Рядом сохраняется SHA-256 точных байтов;
`attach(path)` использует уже записанный файл. Неожиданный interrupt может
оставить незавершённый JSONL без финального digest: это неполное evidence,
а не разрешение повторить UNKNOWN fixture intent. Файл содержит закрытые
case IDs/reasons, numeric/boolean metrics, locale/width, UTC и component
versions. Произвольные DOM/URL/body поля отбрасываются, string payload в
metrics отвергается. Нет screenshot/HAR/trace/video, реальных названий,
source, prompt, cookies, ticket, auth URL или произвольного error text.

Локальные входы без staging/credentials:

```sh
npm run test:unit -- e2e/ui-acceptance-proof.test.ts
npx playwright test --config e2e/ui-acceptance.fixture.config.ts
npx tsc --noEmit -p tsconfig.e2e.json
npx eslint e2e/ui-acceptance*.ts --max-warnings 0
KODEX_E2E_CHECK_ONLY=1 npx playwright test --config e2e/ui-acceptance.config.ts --list
```

Node suite проверяет applicability 64/partial status, safe projection,
version/manifest guards, refs/traversal/malformed page, закрытый набор
разрешённых запросов, intent/receipt ordering, durable bytes/digest/mode и
отказ повторного/публичного/symlink каталога. Настоящий Chromium fixture
проверяет общий helper чтения геометрии и блокировку POST Run на полностью
synthetic странице. Отдельная browser-регрессия доказывает, что ранний отказ
UI-действия не оставляет поздний необработанный timeout ожидаемого ответа. Он не подменяет live UI и provider acceptance.
Context7 `/microsoft/playwright/v1.61.0`: проверены BrowserContext routing,
service worker limitations, response observation и `testInfo.attach(path)`.

# Итоговое evidence

Одна запись: ID + variant + environment/browser/viewport/locale + source и
serving component versions + команда/UI sequence + expected/actual +
PASS/FAIL/NOT RUN + UTC + artifact/digest + отдельный bug Issue при дефекте.
Сохранять первоначальный FAIL и UNKNOWN. Полный baseline запускается на последнем
кандидате; 64 флажка и обязательные варианты сверяются отдельно от зелёных Pods.
Release resilience, worker grants, trust rotation/LKG и mixed rollback ведутся
в #1223 и не закрываются картой браузера.

## Целевой повтор BUI после диагностического отказа (#1300)

`KODEX_E2E_UI_VARIANTS` задаёт непустой список точных IDs через запятую.
Поддержаны только восемь read-only вариантов:
`route-integrations-{ru|en}-{1440|390}`,
`project-picker-keyboard-escape`,
`assistant-history-shell-{1440|768|390}` (скобки здесь описывают варианты,
CLI принимает только полностью раскрытые ID). Пример:

```bash
export KODEX_E2E_UI_CREATE_PROJECTS=0
export KODEX_E2E_UI_VARIANTS=route-integrations-ru-1440,project-picker-keyboard-escape,assistant-history-shell-390
npx playwright test --config e2e/ui-acceptance.config.ts
```

При selection профиль выполняет только выбранные действия и prerequisites:
initial session/shell, смену ru/en и viewport. Discovery, create и остальные
действия не вызываются. Неизвестный/повторный/пустой ID и сочетание selection с
`CREATE_PROJECTS=1` отклоняются до создания журнала и browser действий.
Обязательны новая отдельно выданная legitimate API-session и новый private
evidence каталог; это не разрешение повторять прежний UNKNOWN effect.

FAIL сохраняет `condition` из закрытого списка: геометрия, наличие shell/heading,
маршрут (только boolean совпадения), API readiness, счётчики ошибок, отдельные
шаги фокуса/открытия/Escape селектора и assistant. `actual`/`expected` содержат
только число или boolean; `measurementAvailable=false` означает отсутствие
измерения. Код `DOCUMENT_OVERFLOW` сравнивает actual с максимальным expected,
`HEADER_HEIGHT` — с нижней исключительной границей; остальные — равенство.
Неизвестная ошибка получает `UI_ACTION` без извлечения её текста. DOM, URL,
locator error, cookie, ticket, имена ресурсов и transcript не попадают в журнал.

Assistant ждёт штатного фокуса после загрузки перед Escape. После ошибки
оснастка закрывает оставшееся окно штатной кнопкой; если очистка не удалась,
зависимые шаги получают NOT RUN. Ошибка варианта при этом сохраняется.
Сохранённые старые FAIL не заменяются новым PASS. Полные 64 requirement ID
по-прежнему не закрываются частичным browser-профилем.


## Пакет причин #1305/#1306/#1307

Карта read-only сценария #1306: пользователь с действующей BFF session и
transport/signed organization scope → GET `/api/v1/integration-connections`
→ `ListIntegrationConnections` → generated PlatformQueryService RPC →
control-plane authoritative eligibility/query → `ListIntegrationConnectionsResponse`
→ BFF public page → `IntegrationsPage.loadConnections`. Query/page token не
задают authority. Version/idempotency mutation неприменимы: состояние, audit
mutation и domain events не создаются. Denied/неполный owner response сохраняет
существующую закрытую ошибку; исправляется только обязательное public поле.
OpenAPI требует `items,nextPageToken`; terminal cursor — пустая строка.

Системные аналоги общего `writeMessage` проверены по конкретным owner response:

| Owner response / public page | Обязательный terminal cursor |
| --- | --- |
| ListIntegrationConnectionsResponse / подключения | Возвращается пустой строкой |
| ListSchedulesResponse / global и project schedules | Возвращается пустой строкой |
| ListAuditEventsResponse / audit events | Возвращается пустой строкой |
| ListScheduleRevisionsResponse / история schedules | Возвращается пустой строкой |
| ListScheduleRunsResponse / occurrences | Возвращается пустой строкой |
| ListRoleImageRecipeRevisionsResponse / история рецепта образа | Возвращается пустой строкой |
| ListProviderDefinitionsResponse / каталог providers | Возвращается пустой строкой |
| InteractionIdentityPage / RuntimeSecretPage | Уже отдельные typed writers с required string; не изменены |
| Остальные optional pages | Прежнее omission сохранено |

Реестр использует конкретный Proto response type; общий field `revisions`
или `definitions` не достаточен для выбора формы. Непустые cursors не меняются.
Исходники контрактов, owner authority, PostgreSQL и схемы не изменяются.

#1307: DismissiblePopover дожидается DOM flush `positioned=true` перед focus;
watch cleanup, open и connected checks отсекают закрытое/устаревшее окно.
ModalDialog из #1303 не переписывается. #1305: assistant проверяется по ru/en
accessible name, а независимый cleanup использует устойчивый ID компонента.

BUI дополнительно сохраняет только safe boolean shape фактического ответа
подключений (items array, cursor present/string/empty), совпадение alert с
canonical `errors.default` ru/en и закрытую классификацию activeElement
(picker input/trigger/popover/assistant/body). Неизвестный alert остаётся
unclassified; его текст и arbitrary DOM/response не выводятся. Отсутствие
наблюдения shape не заменяется выдуманным ответом. Всё live evidence пакета
остаётся NOT RUN до нового GO на фактически выложенных компонентах.


## Следующий ограниченный пакет форм и populated collections

Issue #1260. Профиль остаётся BUI, `KODEX_E2E_UI_CREATE_PROJECTS=0`.
Новые действия включаются только явным `KODEX_E2E_UI_VARIANTS`, поэтому
прежний широкий профиль не начинает дополнительных сценариев скрыто.
Реестр `e2e/ui-readonly-variant-ids.ts` содержит 24 IDs: для каждой ru/en и
ширины1440/390 — шесть вариантов:

- `project-form-cancel-<locale>-<width>`: открыть New Project, начальный focus,
  ввод локальных name/purpose, Tab, native required validity, Cancel; Submit
  не вызывается, ACK fixtures прошлого окна не создаются повторно.
- `project-collection-expand-<locale>-<width>`: непустой существующий список,
  не более6 строк в свёрнутом виде, раскрытие populated списка, закрытие.
  Пустая коллекция — NOT RUN, а не проверка populated состояния.
- `configuration-create-editor-<kind>-<locale>-<width>` для
  `prompt-template`, `role-image`, `integration-definition`: из каталога открыть
  новый редактор, проверить поля/геометрию. Save/validate/publish/build не
  вызываются; этот вариант не закрывает lifecycle CFG.
- `assistant-history-draft-<locale>-<width>`: выбрать существующий диалог,
  ввести только локальный unsent текст, отклонить native unsaved-close,
  очистить composer, выполнить GET search истории и закрыть. Пустая history
  либо disabled composer — NOT RUN. New chat/send/rename/archive/upload
  не вызываются; conversation refs/text в evidence не возвращаются.

Все действия проходят прежний request guard. Неизвестный ID, выбор с
CREATE_PROJECTS=1 и duplicate IDs закрыто отклоняются. Для запуска всех24
оператор формирует список из опубликованного реестра, а не задаёт wildcard.
Нужны новый private evidence каталог, отдельный GO/fresh session и точный
serving inventory; full requirement statuses остаются NOT RUN до остальных
обязательных вариантов. API responses с credentials/source contents и DOM
не сохраняются. Нативный confirm учитывается только boolean фактом отказа.

Локальный public regression вход:
`npx playwright test --config e2e/ui-readonly.fixture.config.ts`.
Он проверяет native form/keyboard/Cancel без backend POST на synthetic HTML;
не является доказательством live ProjectsPage или пользовательской приёмки.


## Уточнение read-only профиля после forms24 (#1323/#1324)

Первый live forms24 на harness `2103be1821ff10f4d4d3a95775ab9cbebac2d0dd`,
API/PWA `1303f0c186f2d02b9ac149451f16df35b8b5b521` сохранил
17 PASS / 4 FAIL / 7 NOT RUN (24 выбранных варианта, три prerequisites и
общий remaining). Четыре RoleImage FAIL возникли в guard до сервера;
четыре assistant и две en collections остались NOT RUN. Journal SHA256:
`cf27ca6dd63305c6450465828261acbb8aa13ee74f7254e29fc138861ee2dfbb`.
Новый запуск не переписывает эти результаты.

Новый RoleImage editor читает eligibility через POST
`/api/v1/administration/access/effective-access/query`. Guard разрешает только
этот метод/path и точное тело: target ORGANIZATION без дополнительных полей
либо PROJECT с одним projectRef; ровно три уникальных permissionKeys
`image.build`, `image.source.view`, `image.source.manage`. Subject/actor,
дополнительные поля, другие permissions и соседние administration endpoints
закрыто отклоняются. Новых бизнесовых записей этот допуск не разрешает.

Карта существующего read: authenticated browser cookie/CSRF → generated
queryEffectiveAccess → BFF QueryEffectiveAccess → CP Access RPC → principal
из проверенного transport → доменный service → PostgreSQL effective-access
read → EffectiveAccessPage → source eligibility редактора. Subject в этом
профиле не передаётся, сервер выбирает текущего actor. Это чтение рассчитанных
allow-only решений, не изменение grant; idempotency/If-Match, mutation event и
потребитель эффекта к нему неприменимы. Ошибки CP остаются Problem.

Project collection ждёт ответ именно GET projects pageSize30 без query/cursor
и окончания видимого loading текущей страницы; alert или HTTP failure остаются
FAIL. Только settled пустой список становится NOT RUN. Assistant ждёт
aria-busy=false до чтения истории и после выбора. Evidence сохраняет закрытые
boolean стадии отсутствия fixtures: projects/history empty либо composer
unavailable; текст/refs/DOM не сохраняются. Это не доказывает, что все прежние
NOT RUN были вызваны гонкой, и не создаёт недостающие данные.

Локальные delayed fixtures проверяют populated/empty/HTTP failure, exact
permission read и блокировку соседней mutation. Точный query search из #1318
сохраняется. После merge требуется новый ограниченный live readback, остальные
варианты полного MVP по-прежнему не закрыты.


## Безопасная диагностика запроса в session-only профиле (#1327)

Исторический WebKit N на harness9e6/app1303 завершился FAIL READBACK при
одном failedRequests, несмотря на coordinated refresh и v2/SESSION_READY.
Исходный artifact не связывал счётчик с конкретным запросом; причину нельзя
приписывать intentional abort, asset либо продукту без новых данных.

N теперь сохраняет максимум32 failure записей и счётчик overflow. WeakMap
сопоставляет START и requestfailed одного объекта Playwright Request, без
нового wire header или изменения fetch. Evidence включает локальный sequence,
номер вкладки, закрытые route/method/resource категории, stage начала/отказа,
elapsedMs, closed error enum и SHA256 исходного error. URL/query/headers,
ticket, DOM, response/body и raw error не сохраняются. Неизвестная категория
остаётся OTHER/UNKNOWN; отсутствие START явно identityKnown=false. Этот helper
не подтверждает отмену и не подавляет ни одного failedRequests.

Snapshot фиксируется до закрытия вкладок, чтобы teardown не менял измеренное
окно. Unit проверяет exact identity, redaction и bounded overflow; synthetic
browser fixture принудительно разрывает один read и сохраняет его как failure.
Нужен новый WebKit N live readback после merge. Сохранённый cursor не является
проверкой доставки новых событий: businessEventDelivery остаётся NOT RUN без
контролируемого producer и ожидаемого набора событий.

Повтор N на harness `affcc7a1` уточнил отказ: WebKit, GET BOOTSTRAP,
INITIAL_READY до продления, `WEBKIT_CANCELLED`. Это ещё не доказывает
принадлежность запроса штатному AbortSignal. В приложении существует
конкретный путь: `useSpeechInput` запускает отдельную проверку допуска;
завершение общего bootstrap вызывает `SpeechAvailabilityLease.synchronize`,
который отменяет предыдущую проверку и начинает свежую. Допуск при этом не
продлевается из позднего ответа.

Публичный `S:bootstrap-cancel` воспроизводит этот путь настоящим
`SpeechAvailabilityLease` и native browser fetch. Fixture удерживает первый
ответ, вызывает synchronize и связывает отмену с exact Request через
существующий fixture-only observer; ожидается закрытый допуск. Проверка
выполняется в Chromium/Firefox/WebKit. Она подтверждает механизм, но не
атрибутирует прежний live Request по совпадению URL, стадии или тексту ошибки.
По согласованному opt-in `KODEX_E2E_BOOTSTRAP_SIGNAL_DIAGNOSTICS=1` N добавляет
случайный неавторитетный `X-Kodex-E2E-Fetch-ID` только в same-origin GET
`/api/v1/bootstrap` без query. Заголовки auth, credentials и native signal
сохраняются; прочие routes/methods не инструментируются. Identity связывает
native START, AbortSignal с причиной AbortError и rejection самого fetch с
AbortError с exact Playwright Request. TimeoutError, network rejection,
поздний abort после requestfailed, дублированная identity и overflow не
подтверждают отмену. Сопоставление по URL/порядку не используется.

Сырой failedRequests и все bounded failure записи сохраняются. Отдельно
фиксируются confirmedBootstrapAborts, безопасные local request sequences и
unexplainedFailures. PASS требует отсутствия необъяснённых отказов и overflow;
код WEBKIT_CANCELLED сам по себе ничего не разрешает. Без opt-in прежний
критерий zero failedRequests сохраняется. Старый FAIL не переклассифицируется:
нужен новый разрешённый live readback после exact merge.
