---
id: OPS-PROMPT-CONTEXT-1046
title: Контекст preview и материализация инструкций
type: operations
status: approved
owner: manager
version: 2.2.0
updated: 2026-11-12
---

# Контекст preview и материализация инструкций

Источник: Issue #1046, Epic #1018, MVP-UI-30/34/35/36 и системный аналог
Schedule из MVP-UI-43. Этот документ описывает owner-контракт; проверка CP
сама по себе не подтверждает HTTP/PWA, развёртывание или приёмку владельцем.

## Сквозная матрица

| Инициатор / authority | RPC и owner | OCC / immutable pin | Результат, событие и consumer |
| --- | --- | --- | --- |
| Signed actor, exact Agent/Workflow view; system assistant organization.manage | ListTemplateVariables, ValidatePromptTemplate, PreviewPromptTemplate; одна read-only RepeatableRead owner-транзакция | Actor, Agent version, Workflow revision/stage, runtime/environment, файлы и grants входят в ContextPin; cursor привязан также query | Catalog/diagnostics/ordered sections; событий нет; PWA refetch при stale pin |
| Signed actor, specialized managed configuration command и exact target | Create/Save/Validate/PublishPromptTemplateDraft | Set OCC и idempotency; immutable PromptTemplateScope отдельно от revision; validate/publish заново разрешают текущий контекст | Receipt + existing managed lifecycle audit/event; protected history возвращает scope |
| Signed actor, exact consumer manage | RebindPromptTemplate: AGENT, AGENT_CONTINUATION, WORKFLOW, SCHEDULE | Published exact revision; impact digest и set OCC; Agent/Workflow повторно проверяют реальный контекст | Binding readback; generic payload не назначает owner или grant |
| Signed actor, existing Session turn authority | Preview SESSION_CONTINUATION → AddSessionTurn | Полный expected context digest перед созданием turn; server-derived dependency digest сохраняется отдельно и сверяется на claim/retry | Existing Run/turn lifecycle; preview не создаёт Run/Session/Turn |
| Runtime controller, exact tenant/node/lease/fence/generation | ClaimExecution → immutable RuntimeRevision | Свежие grants, runtime/config/catalog и input pins; previous/current revision notice | Notice атомарен с RuntimeRevision, отдельного события нет; exact worker revision read и existing Run events |
| Automation scheduler, exact occurrence attempt proof | ClaimDueSchedules → MaterializeOccurrence → первый root runtime claim | Schedule revision и bound template захвачены occurrence; marker и envelope digest; retry не читает mutable binding | Root task и AUTOMATION_TASK provenance до RuntimeRevision; existing Schedule/Run events, authoritative turn/revision read |
| Runtime reader, exact lease и выбранный input | ReadExecutionArtifact | Immutable per-item revision/content/manifest, tenant и текущие artifact.view/download | Exact content; read-only input не даёт result write или общий MCP catalog |

## Один renderer

`prompt-service-v2` использует canonical JSON envelope: пользовательский текст и
platform blocks находятся в отдельных escaped string values. PWA показывает
sections и provenance, а не имена служебных JSON-полей. Старые immutable
snapshots без service revision сохраняют прежний renderer; новые optional
metadata отсутствуют в JSON, когда не заданы, и не меняют прежние digests.

Явная вставка: `{{ slot "PURPOSE" }}`. Закрытый порядок:
WORKFLOW, STAGE, PURPOSE, EXPECTED_RESULT, INPUT, CONSTRAINTS,
EFFECTIVE_CAPABILITIES, FILES, TOOLS, INTEGRATIONS, RUNTIME_CHANGES.
Workflow/STAGE/EXPECTED_RESULT применимы к этапу, RUNTIME_CHANGES — к
continuation. Остальные обязательны. Locale en/ru.

Renderer учитывает фактически исполненные slots. False branch не подавляет
обязательный блок; повторный вызов не дублирует его. Condition, assignment,
pipeline, unknown slot и реально вызванный неприменимый slot закрыто
отклоняются. Пропущенные blocks добавляются после пользовательских sections.

Базовая пользовательская часть — опубликованные инструкции Agent. Отдельный
Workflow binding добавляет WORKFLOW_CONTEXT после неё. Назначение и ожидаемый
результат этапа проходят тот же renderer. Typed user provenance содержит
BASE_TEMPLATE / WORKFLOW_CONTEXT / AUTOMATION_TASK и exact template ref/digest.

Safe projection строится из того же исполнения: пользовательские sections
замаскированы целиком, порядок и slots сохранены. Повторный render на
redacted данных запрещён, поскольку он меняет условия ветвления.
Полный текст требует existing `prompt.full.view` и свежую authentication.

## Обход AST и ограничение исполнения

В #2108 общий renderer расширен именованными Go `define/template`, лексическими
переменными и точным происхождением `dot` внутри `with/range`. Анализ проходит обе
ветви `if`, параметры разрешённых функций и достижимые вложенные шаблоны. Вложенный
вызов получает собственный `$` и не наследует локальные переменные вызывающего.
Присваивание объединяет возможные зависимости внешней переменной; декларация
перекрывает её только в своей области. Недоступный runtime context блокирует полный
prompt также через псевдоним или вложенный вызов. Анализ не предоставляет полномочий.

Исполнение остаётся в штатном `text/template`; второго renderer нет. Перед ним
проверяются закрытые поля и функции, отсутствие рекурсии, глубина именованных
вызовов до16 и консервативный бюджет65536 посещений AST с учётом размера коллекций
и вложенных циклов. Integer range и коллекция с недоказуемым происхождением
отклоняются. Предел вычислений проверяется повторно по фактическому immutable
snapshot, поэтому пустой вывод не обходит ограничение вложенного range. Существующий
предел вывода256KiB сохраняется. Целый корень нельзя напечатать; передача корня
вложенному шаблону или локальному псевдониму разрешена только с последующим
проверяемым обращением к закрытым полям.

Preview и RuntimeRevision используют одну материализацию. Условия и элементы
коллекции действительно меняют исполняемый prompt; повторный render прежнего
снимка сохраняет его текст и digest. Порядок semantic sections, tool pairs и истории
не сортируется. Факт наличия AST-зависимостей сам по себе не является доказательством
стабильности prefix-cache или физической изоляции провайдера.

Через Context7 `/golang/go` проверены правила `text/template`; семантика `$`,
`PipeNode.IsAssign`, `walkRange`, `walkTemplate` сверена с исходниками закреплённого
Go1.26.10. Проверка renderer: `go test -race -timeout=120s
./internal/domain/service/prompt` из модуля control-plane. Матрица включает
вложенные templates, alias/assignment, функции, свежие значения, неизменный replay,
недоступный контекст и отказ рекурсивных/чрезмерных циклов; это локальное доказательство,
не приёмка model wire или установленного кластера.

## Каталог и объявленный scope

Agent/Workflow prelaunch возвращает фактические доступные pins, runtime tools,
integration grants, выбранные файлы и знания. Будущие Run/Turn refs не
выдумываются: referenced runtime-only variable даёт
`complete=false` и `RUNTIME_CONTEXT_REQUIRED`.

Весь файловый набор `files/files_count/files_dir/manifest_path` блокируется
при отсутствии assigned Agent files opt-in: `CAPABILITY_REQUIRED`.
Actor для выбранного чтения обязан иметь artifact.view/download;
upload/bind/delete не заменяют этот предикат. Новый claim после удаления
assigned opt-in закрыт. Immutable selected inputs и manifest дают отдельный
read context, поэтому отсутствие effective write bundle не запрещает их
RunnerInput.Validate. Result write, напротив, требует pinned effective
ArtifactCapability, текущий Agent opt-in и fresh actor artifact.upload
до object storage и повторно в terminal-транзакции.

Если Agent остаётся видимым, но actor потерял чтение выбранных файлов,
каталог сохраняет disabled variables с PERMISSION_REQUIRED без descriptors,
а preview возвращает complete=false. Этот частичный preview не разрешает
actual claim или чтение файла.

Create/Save принимают optional PromptTemplateScopeInput с INSTRUCTIONS либо
CONTINUATION и exact Agent/Workflow context. Owner сохраняет scope в
immutable side table migration641: без task, input values, AttachmentSet или
будущих runtime refs. Validate/Publish повторно проверяют ContextPin.
Legacy revision без scope не считается context-ready: предупреждение
PROMPT_CONTEXT_NOT_DECLARED, а actual consumer binding проверяется отдельно.
Continuation scope не обещает доступность ещё не созданного turn.
Известные late variables task/run.ref/session.ref/turn.ref/node.ref с
owner reason RUNTIME_CONTEXT_REQUIRED допускаются при публикации с явным
предупреждением. Это не исключение для file/capability/scope errors:
неизвестное имя либо иной reason закрыто отклоняется. Actual runtime требует
полную материализацию без deferred diagnostics.

## Продолжение Session

Отдельный published PROMPT_TEMPLATE через consumer AGENT_CONTINUATION не
заменяет основной Agent template. Typed diff содержит safe changes:
инструкции, model/reasoning, image, environment, files, skills, memory,
tools, MCP, integrations, capabilities и policy.

Prospective preview не имеет current revision/Turn/attempt. Его полный digest
связан с предыдущим runtime и выбранной задачей. AddSessionTurn проверяет этот
pin; новый PWA flow передаёт его всегда. Отдельный server-derived dependency
digest не назначается caller и проверяется при actual claim/retry.
После claim notice получает настоящие refs и сохраняется migration639 вместе
с RuntimeRevision. Exact retry не дублирует turn или notice для той же attempt.

## Schedule

Occurrence захватывает original template revision/content digest/source/scope
в owner envelope вместе с original prompt inputs. Отдельная DB column
`prompt_input_format=1` назначается сервером и входит в digest domain.
Legacy marker0 всегда означает обычные пользовательские values, даже если
JSON похож на envelope. Marker/envelope immutable; user JSON не может
назначить template authority.

Первый root/direct либо initial coordinator claim один раз материализует
captured template с фактическими runtime refs. Итоговая task сохраняется в
root turn до RuntimeRevision; snapshot содержит original template и rendered
task digest. Повторная интерполяция rendered task запрещена. Retry использует
исходный occurrence, а не нынешний mutable binding. Workflow workers не
получают ambient AUTOMATION_TASK: задача доступна им через явный Workflow
context/template. Agent instructions и Workflow context сохраняются.

## Проверка и ручной сценарий

Локальные targeted PG: declared scope + prospective pin PASS 1.632s;
readonly actor claim/read, output denial и revoke-read PASS 1.716s;
Schedule capture/race/retry PASS 1.606s; captured-template drift → actual
root claim/turn task PASS 1.007s. Это проверки деревьев до финального checkpoint,
а не evidence staging. Полный Bootstrap на итоговой production-реализации
PASS 20.782s; последующее детерминирование порядка namespace diagnostics
проверено unit regression. Финальный targeted PG границ чтения и записи
PASS 2.168s, включая отзыв assigned opt-in, отдельный отзыв upload после
claim с pinned write capability и отзыв чтения при сохранённом agent.view.
One-pass Schedule task и tampered rendered digest проверены unit regression.
Итоговый exact SHA ledger публикуется отдельно.

Владелец проверяет: открыть Agent/Workflow editor; сравнить порядок sections,
insertion points и actual Run snapshot; изменить зависимость после preview
и получить stale rejection; проверить continuation diff/notice; сменить
Schedule binding после occurrence claim и убедиться, что старый occurrence
сохраняет captured revision. Для read-only actor проверить inputs без
возможности записать result artifact и отказ после отзыва чтения.

HTTP/PWA consumer новых DTO и общий integrated baseline отмечаются отдельно.
Merge/deploy и owner gate этим документом не разрешаются. Секреты не раскрыты.

Owner641 объединён с текущими RoleImage643, Environment644, Files targets67
и runtime file catalog642. Полный Bootstrap объединённого дерева PASS27.109s;
prompt/repository/grpc race PASS1.104/1.729/1.087s. Свежие actor read/download
guards и readonly input eligibility сохранены вместе с immutable catalog
capture до RuntimeRevision digest. Это evidence owner composition;
HTTP/PWA и общий owner gate проверяются отдельно.

Context7: Go standard library `/websites/pkg_go_dev_go1_25_3`,
text/template FuncMap и encoding/json Marshal; execution error закрывает
render, JSON Marshal экранирует значения.

## Консервативная классификация зависимостей #2108

Источник требования — #2108: стабильность model input не может выводиться из
одного имени шаблона. Тот же AST renderer обходит условия, присваивания,
`with`, `range`, чистые функции и вложенные templates. Отчёт `dependencies`
относится к исходникам шаблонов, а не обещает cacheability полного prompt.
Автоматически добавляемые служебные блоки по-прежнему разрешаются для хода.

| Путь | Авторитетный владелец и результат |
| --- | --- |
| Actor → POST `/api/v1/prompt-templates/preview` | Проверенный transport principal и прежние permissions на target/full preview; request refs не назначают actor/project |
| Gateway → `PlatformQueryService.PreviewPromptTemplate` | Generated adapter сохраняет exact target/context/version/digest; CP разрешает owner snapshot до исполнения AST |
| CP → общий `prompt.Materialize` | Preview и runtime используют одинаковый анализ exact snapshot. Исходный template digest и path связывают каждую строку; значения не выдаются |
| CP Proto → gateway → UI | Закрытая классификация, bounded 512 строк, проверка сочетаний digest/trust; UI показывает приватность и отсутствие cache guarantee |
| Retry/continuation | Exact retry использует прежний immutable snapshot; continuation заново разрешает данные и pins, не меняет оригинал |
| Ошибка/повторное чтение | Неизвестная конструкция отклоняется до исполнения; недоказанная стабильность UNKNOWN. Query не меняет состояние, не требует idempotency receipt и не создаёт domain event; авторитетный read path — прежний preview RPC |

`STATIC_LITERAL` означает только буквальный текст конкретного исходника.
`VERSION_PINNED` требует проверенный ContextPin и точную версию агента,
workflow либо immutable environment digest. Динамические purpose/result
шаблоны стадии не объявляются статическими. Остальные разрешаемые значения и
явные slots считаются `TURN_LIVE`; отсутствие pin или доступности даёт UNKNOWN.
Все значения консервативно PRIVATE. `OWNER_TEMPLATE` описывает происхождение
текста, а `DATA_ONLY` не даёт данным стать разрешением на инструменты.
Ни одна классификация не разрешает кешировать готовый ответ или пропускать
актуальное чтение данных. Совпавшее свежее live-значение не запрещает prefix reuse.
Существующие RPC registration, readiness, deploy ownership и transport сохранены;
новый consumer — защищённый просмотр зависимостей в карточке preview.

Для UI проверены через Context7 [ключи списков Vue](https://vuejs.org/guide/essentials/list):
identity строки составляют source digest и path; данные не помещаются в ключ.

Локальная квалификация изменения зависимостей: renderer race, gateway prompt
race, Proto codegen/lint, frontend typecheck/build и15 UI tests прошли.
PostgreSQL preview/core сначала FAIL из-за оставшегося тестового вызова старого
Codex compatibility helper. Неиспользуемый helper удалён; native-проверка теперь
сохраняет transcript при свежих инструкциях, но отвергает чужого агента. MCP
негативные проверки переведены на действующий `managedMCPInputFromOwnerSnapshot`.
Повторный disposable PG18.3 прогон `TestAssistantCoreInheritanceComponent`
(SYSTEM, SYSTEM в project context, два PROJECT) и
`TestProspectiveWorkflowPromptComponent` прошёл за8.396s. Это не установленная
browser/runtime приёмка и не результат CI.
