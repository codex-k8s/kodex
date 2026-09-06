---
id: CP-AUTOMATION-PREVIEW-1076
title: Предпросмотр Автоматизации и готовность запуска Процесса
type: contract-map
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-06
---

# MVP43 и MVP33

Источники: исходные MVP-UI-43, MVP-UI-33, #1076, #1078, #1046.
Изменения расширяют существующие PreviewSchedule и Workflow. Нового RPC,
permission, scheduler PR или события нет; policy остаётся совместимой.

| Сценарий | Authority и lifecycle | Результат и потребитель |
| --- | --- | --- |
| Новый draft Automation | Проверенные actor/tenant/project; тот же project.manage, что CreateSchedule, и защищённый контекст выбранного Agent/Workflow | HTTP PreviewSchedule → owner RR snapshot → общий renderer → PWA; никакого создания Schedule/Session/Run/occurrence |
| Сохранённый draft | Точный schedule.manage до OCC; expectedScheduleVersion, target version, current template binding и owner-selected Session | DRAFT всегда описывает новую revision от текущего actor; CURRENT_REVISION требует exact saved spec/version и использует автора сохранённой revision |
| NEW_EACH_RUN | AGENT task либо WORKFLOW root coordinator; scheduledFor — первое occurrence общего parser/normalizer | В task входят введённый текст и Automation template; этапы не получают его неявно |
| CONTINUE_ONE | Без bound Session — первый запуск; с ней — тот же prospective continuation snapshot/notice и exact previous RuntimeRevision | Typed runtime diff, context digest и безопасные sections; retry/turn не создаются |
| Full materialization | Точный prompt.full.view выбранного Agent/Workflow/Session и свежая interactive authentication | IncludeFull включает полный текст; обычный ответ использует прежний SafePreview |
| Workflow single/list/result/replay | Текущие workflow.view/workflow.launch и signed project scope; пакетная проверка всех зависимостей | launchReadiness отделяет allowedToSubmit от operationalState; неизвестное состояние провайдера не превращается в READY |
| Workflow launch | Общий owner runtime-contract predicate для реально выбранных Agent dependencies | Неготовые hard dependencies запрещают submit; обычный запуск не требует file capability без вложений |

SchedulePromptPreviewContext содержит только входные данные и OCC, не authority.
Actor и tenant происходят из существующего полного Proto request proof.
`materializationPin` сообщает сохранённую Schedule version, доступную immutable
revision, рассчитанное время, timezone и выбранную Session. Context digest
связывает полный draft, actor и runtime dependencies. Он не выдаётся за receipt.

Переменные `.automation.name`, `.automation.task`, `.automation.scheduled_at`,
`.automation.timezone`, `.automation.revision` используют общий catalog и renderer
с runtime. Для любого DRAFT, включая неизменённую спецификацию, `.automation.revision` имеет
REVISION_NOT_SAVED; шаблон с обязательным обращением получает diagnostic,
а не выдуманную ссылку. `.automation.ref` также недоступна до создания Schedule.
Все пользовательские шаблоны проходят один проход интерполяции.

WorkflowLaunchReadiness.reason: READY, PERMISSION_REQUIRED, UNPUBLISHED,
DEPENDENCY_UNAVAILABLE. Значение READY здесь означает допуск к submit;
операционная доступность — отдельное поле. Ни transient capacity, ни неизвестный
provider health сами по себе не запрещают допустимую очередь. Response не
раскрывает имена скрытых зависимостей. Migration647 переносит прежний runtime
predicate в SECURITY INVOKER функцию с фиксированным search_path и закрытым
EXECUTE, сохраняя четыре существующих потребителя.

Workflow single/list и mutation/replay повторно получают текущую защищённую
версию, прежде чем добавить readiness. Опубликованный ref берётся из
`workflow_versions.ref`, а не из поля Ref исходного draft JSON. Immutable
spec/digest не меняются. Provider admission читает все root coordinator одним
батчем через общий helper #1077; READY в reason описывает только submit, не
отсутствие очереди и не provider SLA. Workflow-only launch не требует прямого
agent.launch или file capability при отсутствии вложений.

Automation task добавляется в coordinator purpose общим runtime/preview
helper только если его точное значение отсутствует в результате одного прохода
шаблона. Существующий task slot не дублируется, условная пропущенная вставка не
теряет обязательное задание, а `{{...}}` внутри task остаётся данными. Другие
этапы не получают задание Automation неявно. Это изменение будущего snapshot,
не reinterpretation уже сохранённых snapshots.

## Исторические проверки разработки

- Ранние source-only dc55a043 и 0bf95a946: canonical generation/compile; не
  объявлялись законченным owner или browser acceptance.
- PG1 FAIL29.190s: новый preview ошибочно проверял schedule.manage на Project.
  Исправлено через тот же commandAccessTarget, что Create/UpdateSchedule.
- PG2 Bootstrap PASS25.224s + Avatar0.290s: промежуточный owner до окончательного
  решения CURRENT_REVISION/DRAFT и добавленных negative fixtures.
- Unit3 FAIL: старый catalog fixture не содержал пять новых Automation fields;
  fixture дополнен полной canonical shape.
- PG3 FAIL29.210s: новый full-preview fixture имел auth_time без обязательных
  ACR/AMR; исправлен fixture, freshness guard сохранён.
- PG4 FAIL31.974s: permission-negative fixture ожидал Forbidden вместо
  канонического NotFound; owner authorization не ослаблялась.
- PG5/PG6 FAIL28.528s/33.514s: прямой repository fixture пропускал ResolvePrincipal
  для proof actor/owner; исправлен канонический переход к owner scope.
- PG7 FAIL31.230s: обнаружена и исправлена старая выдача raw draft Ref вместо
  authoritative published Workflow ref в readback.
- PG8 FAIL32.585s: обнаружена потеря Automation task из coordinator purpose;
  исправлено общим runtime/preview helper с проверкой отсутствия дублей.

Объединённый owner #1076/#1077/#1078: полный публичный
`make test-control-plane-postgres` PASS — Bootstrap 31.429s, Avatar 0.392s.
Проверены AGENT/WORKFLOW, NEW_EACH_RUN/CONTINUE_ONE first и bound Session,
DRAFT/CURRENT_REVISION, разные viewer/execution actor и отзыв автора,
immutable revision/spec mismatch, свежая full-preview authentication,
Workflow-only launch без agent.launch/file capability, dependency denial,
protected single/list и replay текущей публикации, Provider health/profile и
capacity=1/max=1 с разрешённой queued submission. Unit проверяет единственный
проход task, отсутствие дубля существующего slot и условно пропущенный task.

После объединения #1077 полный CP race/vet/build, SQL boundary,
Proto lint/build/canonical replay, policy72 replay, authority ABI render и
web-only/optional Mattermost release render — PASS до заключительного SQL
исправления повторной проверки ProviderAccount после ожидания row lock.
После него targeted PostgreSQL regression PASS 0.292 s: ожидание подтверждено
через pg_blocking_pids, отключённый аккаунт не выбран. Первый запуск этого
regression FAIL 0.650 s нарушал state/enabled constraint в fixture; fixture
исправлен на DISABLED, production constraint сохранён.
После исправления repository race PASS 1.894 s и SQL boundary PASS.
Buf remote rate limit обойдён штатным exact local plugin fallback;
generated readback не изменился. HTTP/PWA, live providers и staging/production не подтверждаются
этими PostgreSQL fixtures; общий baseline и review принадлежат #1031.

Режим по умолчанию — DRAFT. CURRENT_REVISION не принимает actor из запроса: автор разрешается из immutable revision. Caller независимо проходит свежие права Schedule/target/prompt context/full preview. Current mode повторяет актуальную authority LaunchRun автора, а capability intersection и continuation diff используют его контекст. Pin содержит mode, executionActorRef, baseRevisionRef/baseRevisionDigest; revisionRef доступен только для CURRENT_REVISION. Несовпадение текущей версии или спецификации закрыто отклоняется.
