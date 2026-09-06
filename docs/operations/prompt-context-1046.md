---
id: OPS-PROMPT-CONTEXT-1046
title: Контекст preview и материализация инструкций
type: operations
status: approved
owner: manager
version: 2.0.0
updated: 2026-09-05
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
