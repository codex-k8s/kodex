---
id: OPS-DOC-WORKFLOW-REVIEW-QA-1949
title: Native COMMENT и сохранённый QA-отчёт серверного процесса
type: runbook
status: approved
owner: architect
version: 1.5.0
updated: 2026-10-29
---

# Явный профиль результатов

## Контракт PDLC и карта передачи

`GITHUB_PDLC_V1` является отдельным явным профилем `SERVER_DAG_V1` и использует
публикацию wire4. Прежние wire1/2/3 и опубликованные версии не меняются.
Контракт не означает завершённую поставку: до native owner, schema/backup и
живой проверки всех потребителей профиль не считается доступным в staging.

Источник требований — решения владельца в #1830/#1949/#1967: сотрудники
сохраняют документы и код в ветке существующего PR, отвечают комментариями,
предоставляют файлы, после принятых результатов сервер допускает следующий
шаг. Промежуточные записи требуют действующего `NONE` grant; merge/deploy
сохраняют отдельные Human Gate. Публикация условий не выдаёт grants.

| Сценарий | Инициатор и полномочия | Передача и владелец состояния | Версии, результат и потребители |
| --- | --- | --- | --- |
| Создание процесса | Владелец доступного проекта из проверенной SSO-сессии; либо проектный помощник через подтверждённый owner plan | `POST /api/v1/projects/{projectRef}/workflows` → gateway → `CreateWorkflow` → control-plane domain/repository; проект разрешается по actor, не по наличию ref | Idempotency receipt и workflow назначает сервер; draft содержит profile/requirements/Branch. `WORKFLOW_CHANGED` и последующее `GetWorkflow` читают ту же owner boundary |
| Изменение, проверка, публикация | Текущий actor с разрешением на существующий workflow | `POST /api/v1/workflows/{workflowRef}/commands` → `UpdateWorkflowDraft` / `ValidateWorkflowDraft` / `PublishWorkflowDraft` → control-plane | Exact `If-Match`/idempotency; immutable published version и spec digest. Branch обязателен только у нового `REPOSITORY_FILE`, targetRef — существующий PR. Неполный либо неизвестный профиль отклоняется до публикации |
| Чтение процесса владельцем | Проверенный текущий actor и project eligibility | `GET /api/v1/workflows/{workflowRef}` → `GetWorkflow` → owner projection → Proto → gateway HTTP/PWA | Query не создаёт receipt/event; точные draft/published поля не смешиваются, чужой ресурс закрыто отклоняется |
| Чтение публикации сотрудником | Действующая server-owned lease/RR с закреплёнными каталогом и workflow pins | Managed MCP → runtime-controller callback → `GetExecutionWorkflowCatalog` → control-plane → wire4/shared decoder → сотрудник | Exact lease/fence/generation, publishedRef/version/specDigest и configurationSHA256; READ не выдаёт write authority. Branch/PR/fileName сохраняются без defaults либо downgrade |
| Принятие файла | Назначенный сотрудник выполняет существующие разрешённые MCP operations; штатный integration-gateway подтверждает native outcome | Запись файла → PR-read → immutable content-read; control-plane разрешает три разных receipt в exact owner/root lineage и проверяет порядок, branch/path/head и полный digest bytes | Декларация только key/write/head/content receipt refs. Accepted file хранит fileName/branch/number/headSha/три receipt refs/digest; модель не назначает accepted и не подставляет содержимое вместо proof |
| Чтение истории | Текущий владелец либо допущенный потребитель exact run | `GET /api/v1/runs/{runRef}/graph` → `GetRunGraph` → control-plane journal → Proto → gateway/PWA; model dependency читает server-owned accepted snapshot | Не меняет state/event. Новые file/comment поля проходят closed whitelist; private IDs, leases и grants не выдаются. RUN event и последующий graph read используют одинаковую типизированную проекцию |
| Terminal/cancel/retry | Прежний авторитетный owner lifecycle, без новой команды из-за полей API | Control-plane закрывает прежние lease/grant/invocation и удерживает uncertain native fact по действующему протоколу | Добавление полей не создаёт новых lifecycle events. История читается прежним authoritative graph path; retry не превращает известный WRITE в новый эффект. Изменение файла получает server-linked predecessor только после доказанного native success |

Новые Proto tags добавляются без перенумерации прежних. `Branch` требования
и `fileName`, `branch`, `headReceiptRef`, `contentReceiptRef` принятого файла
сохраняются во всех направлениях. Неизвестные поля/виды и использование новых
исполняемых полей в прежнем профиле закрыто отклоняются. `PR_COMMENT` принимает
только фактический комментарий существующего PR, не Issue comment. Ready
разработчика должен относиться к тому же head, что принятый файл.

Browser session stream получает ту же публичную проекцию графа через
authoritative readback gateway: NATS остаётся только сигналом пробуждения.
Schema browser snapshot3.1.0 добавляет два вида и четыре file-proof поля;
нового доменного NATS event либо изменения его authority/lifecycle нет.
CP, gateway, runtime-controller и PWA поставляются совместно до включения
нового профиля. Совместимость старого browser validator с новым видом
результата не предполагается; локальный generator/SSR не заменяет проверку
настоящих HTTP/WS snapshots и reconnect в staging.

Managed MCP schema создания/изменения процесса и owner parser сохраняют те же
`branch/fileName/targetRef` при чтении и подготовке плана. Поля receipt и actor
не входят в редактируемые условия. Отсутствующий profile в sparse UPDATE
разрешается по owner readback; явный прежний profile не принимает новые file
selectors. Настоящая compiled JSON Schema и owner create/read/update проверяются
отдельно: успешный разбор схемы не заменяет native приёмку результата либо
активные grants. API `Schema.Resolve`/`Resolved.Validate` и условные схемы
проверены через Context7 `/google/jsonschema-go`; исполняемые tests используют
закреплённую версию модуля.

## Приёмка и продолжение PDLC

Серверная приёмка нового профиля повторяет native proof через общие owner SQL
readers: rejoin имеет прежний приоритет, неоднозначный результат не скрывается
fallback. Readers сохраняют текущие grants, frozen RuntimeRevision, lineage и
`FOR SHARE ... NOWAIT`; поэтому функции с row locks остаются `VOLATILE`,
`SECURITY INVOKER`. Новые функции принадлежат `control_plane_owner`, исполняются
runtime, не предоставляют `PUBLIC` либо backup reader право выполнения.

Файл подтверждается тремя разными receipts: WRITE → PR-read → immutable
content-read. Общая SQL-хронология используется и Go evaluator, и journal
guard. Проверяются опубликованные repository/branch/path/PR, полный digest
decoded bytes, bounded prefix/chunk digest и совпадение immutable head.
Допустим текст до256КиБ, включая стандартные CRLF-переносы base64; NUL,
невалидный UTF-8, неканонический padding, пробелы и tab в base64 отклоняются до
WRITE. Этот профиль текстовых файлов не заменяет загрузку бинарных артефактов.

Composite digests только новых FILE/PR_COMMENT используют SHA256 UTF8-полей с
NUL-разделителем и закрытой cardinality:12 и7 полей с domain tag. Исторические
digests не меняются. Фиксированные vectors включают кириллицу и `<&>` в пути,
чтобы Go JSON escaping и PostgreSQL JSON whitespace не влияли на commitment.
Comment дополнительно закрепляет digest exact native input в PostgreSQL JSONB
представлении, которое читает штатный pgx adapter.

`PULL_REQUEST` нового профиля принимается по native `github.pull_request.ready`
на exact expected head: output обязан сообщить открытый non-draft PR с тем же
SHA и repository URL. `already_ready=true` допустим; обычный PR-read либо
неизвестный исход Ready не заменяет этот receipt. Принятый Ready head совпадает
с принятым файлом того же этапа. Merge и deploy не входят в author delivery
registry и сохраняют собственные согласования.

| Переход | Проверяемое поведение |
| --- | --- |
| Create/publish/launch | Exact owner/project, закрытый профиль, immutable requirements; публикация не выдаёт grant |
| Claim/renew | Прежние lease/fence/generation и свежий RuntimeRevision, без восстановления закрытой аренды |
| Native WRITE | Только published author slot и действующий `NONE` grant; immutable intent до внешнего действия |
| Model complete до native outcome | Capture/lease закрываются, physical run удерживается; принятых результатов ещё нет |
| Native outcome до model complete | Сохраняется native факт; завершение модели проходит тот же owner join |
| Все outcomes завершены | Один `CONTINUES` с fresh lease/RR и полным bounded envelope; прежний producer не оживает |
| Final complete | Прежняя completed provenance плюс SQL-проверка каждого нового результата; только accepted journal допускает зависимость |
| Technical retry | Совпавший известный WRITE присоединяет receipt; новый ключ не разрешает повтор эффекта |
| Преднамеренная новая версия файла | Exact predecessor и другой digest bytes назначают новую file epoch; неизвестный предшественник закрывает запись |
| Revoke/cancel/delete | Прежние owner transitions закрывают grants/leases; сохранённый receipt не восстанавливает разрешение |
| Lease expiry/dead-letter | Прежние owner closure/recovery; expiry не доказывает успех внешнего эффекта |
| `WAITING_OWNER`/Gate expiry | Согласование не подменяется model outcome; merge/deploy обрабатываются своими owner-командами |
| `CHANGES_REQUESTED` | Проверенные отрицательные facts сохраняются отдельно от accepted inputs; новая business attempt требует действующего server rework protocol |
| Ошибка delivery либо overflow | Durable typed failure без усечения фактов и повторного WRITE; история читается штатным owner path |

`make test-control-plane-postgres` с фильтром `--pdlc-owner` использует отдельную
пустую loopback-БД, exact predecessor030 и дорабатываемую forward migration031.
Проверяются native file intent и owner graph для documentation/developer,
model-first/effect-first, комментария и Ready; gateway в этом контуре доверенный
fixture, внешние GitHub/модельные действия он не выполняет. Отдельный
`--pdlc-owner-legacy` выполняет шесть прежних rejoin/review сценариев на том же
successor. Фактические результаты привязываются к конкретным bytes/SHA отдельно.
Полное schema/backup proof, rollout потребителей и живой PDLC остаются отдельными
условиями включения нового профиля на staging.

## Прежний профиль ревью и QA

Issue #1949 вводит optional `version.resultEvidenceProfile=GITHUB_REVIEW_QA_V1`
только вместе с `SERVER_DAG_V1`. Отсутствие поля сохраняет прежний verdict/head
профиль. Новое поле требует strict publication wire3; старые v1/v2 bytes не
изменяются и не повышаются автоматически. Новую policy нельзя включить уже
запущенному snapshot задним числом. Сохранённый проект/процесс и действующие
integration grants по-прежнему разрешает владелец состояния. Явное пустое значение
при UPDATE снимает draft opt-in только после повторной проверки всего графа;
отсутствующее поле sparse update сохраняет прежнее значение, null запрещён.

В новом профиле subjectStepKey REVIEW/QA указывает на direct dependency с ровно
одним опубликованным PULL_REQUEST requirement. REVIEW/QA-only predecessor и
несколько PR predicates отклоняются до публикации; прежний профиль не меняется.
Owner выбирает именно этот accepted PR, не последний head из текста рецензента.

REVIEW объявляет только `{key,headSha,verdict,receiptRef}`. Обязателен успешный
native `github.pull_request.review.create`, input event `COMMENT`, точные PR/head
принятого `subjectStepKey`, output `COMMENTED/commit_id`, полный body и native
receipt digest. `PASS` — внутренний verdict независимого сотрудника, не GitHub
`APPROVE`. Same PAT не разрешает self-approval и не доказывает независимость
внешних аккаунтов. Owner проверяет независимое назначение REVIEW в опубликованном
графе; живой PDLC отдельно проверяет работу ролей.

QA requirement задаёт `evidenceKey`: ключ отдельного `ARTIFACT application/json`
того же этапа. Этот файл содержит `kodex.workflow-qa-report.v1` с exact repository,
PR number/head, verdict, tests и findings. Каждый test хранит name, command,
exitCode и exact `CAPTURED_ARTIFACT` selector полного вывода. Owner читает exact
immutable object version, проверяет size/digest, closed JSON и каждый собственный
closed-ancestor capture pin через frozen RR catalog и fresh ACL/effective CLEAN.
Report и logs подтверждают bytes и producing execution, **не аттестуют запуск**.
Реальные Go/SQLite/API/Chromium команды и их вывод сотрудник выполняет и сохраняет,
а владелец отдельно проверяет в live. Нельзя выдумывать PASS либо баг ради формы.

## Создание Issue и replay

При действительном дефекте report может содержать одну aggregate Issue title/body.
Для этого требуются `CHANGES_REQUIRED` и непустые findings. Сначала сотрудник
сохраняет logs, затем report через штатный `CAPTURE_PROGRESS` и получает fresh RR.
До `github.issue.create` owner разрешает report по опубликованному filename,
требует единственный capture до eligibility filtering, читает bytes и проверяет
exact subject/head/logs/title/body. Repository выводится из принятого PR и current
scope grant. Ни report, ни payload не назначают target или authority.

QA final declaration содержит только key/headSha/verdict и optional receiptRef.
Receipt допустим лишь для реально созданного Issue, точно связанного с report pin
admission. При PASS Issue отсутствует. При CHANGES_REQUIRED без Issue receipt не
требуется. Изменённый report с прежними title/body не подменяет исходный report.
Файловое ARTIFACT declaration отдельно выбирает CURRENT_COMPLETION либо точный
CAPTURED_ARTIFACT; source kind/fileName/mediaType не переносятся в QA element.

Новые два logical slots имеют максимум один effect каждого вида на business
attempt. Semantic digest включает repository scope, canonical input и QA report
pin и canonical distinct log pins; COMMENT input уже содержит exact PR/head. Exact execution replay и fresh
same-attempt RR используют существующую invocation/receipt и immutable bridge,
не создают второй Gate/Begin/POST. Новый business attempt назначает только owner
recovery. Другой head/body/report в прежнем slot отклоняется. Остальные WRITE,
старые slots и legacy workflow остаются в прежних границах.

## Матрица жизненного цикла

| Переход | Owner state и продолжение |
| --- | --- |
| Effect завершился до model completion | Completed capture и lease сохраняются; physical run остаётся RUNNING до одного owner outcome CONTINUES. Бизнес-проверка выполняется на следующем fresh turn, даже если прежний NONE-вызов уже вернул receipt. |
| Model завершился до native claim/completion | Completed lease/capture неизменяемы; physical run удерживается RUNNING, journal/downstream не создаются. Только exact native evidence invocation может быть claimed после producer completion. |
| Последний effect завершился после model | Owner сохраняет native receipt и полный terminal envelope; один fresh CONTINUES наследует logical origin/business attempt, исходную задачу и полные outcomes. Новый RR/grants создаются обычным claim. |
| Несколько effects | Join ждёт settlement всех invocations данного physical node; task не выбирает latest и не выбрасывает FAILED/REJECTED. |
| HUMAN_EACH_EFFECT ACK и быстрый APPROVE | WAITING_APPROVAL мог быть возвращён до owner/worker completion. Порядок не выводится из текста модели: обе стороны соединяются тем же owner join перед evaluator. |
| Повторный join или новый turn | У parent уже есть CONTINUES — второго ребра нет. Новый physical node без собственных native invocations финализирует обычную декларацию; rejoin старого receipt не создаёт новый WRITE или бесконечную цепочку. |
| Pending/UNKNOWN | Нет acceptance и повторного POST. UNKNOWN не становится FAILED ради очереди; разрешено только authoritative readback/owner attention. Общий GitHub recovery сервис не заявляется. |
| Отзыв authority до join | Нового turn/WRITE нет; native факт сохраняется, canonical owner graph closure закрывает leases/grants/Gates. |
| Общий/step deadline | Нового join нет; существующий canonical expiry закрывает весь owner graph с прежним приоритетом execution clock над Gate. Срок не продлевается. |
| Outcome/task слишком велик либо исчерпаны существующие64 CONTINUES root | Durable WORKFLOW_EVIDENCE_DELIVERY_LIMIT закрывает граф, сохраняя факт/receipt/capture. Нет усечения и rollback известного внешнего факта ради нового task. |
| Cancel/delete | Прежний terminal envelope и native UNKNOWN retention остаются; исторические facts не переписываются, новый claim/replay с effect запрещён. |
| REVIEW/QA CHANGES_REQUIRED | Owner journal сохраняет results=[] и отдельные private reviewEvidence; public evidence — данные отклонённой попытки, не accepted dependency. Менеджер разбирает исключение. |
| Reload/history | Публичная whitelist выдаёт только immutable public pins; RR/lease IDs, credentials и object locators не публикуются. |

Ограничение claim/join относится только к явному профилю с двумя доказанными
slots. Оно не открывает generic READ/WRITE continuation, не снимает STOP #1848
либо #1943 и не расширяет capabilities координатора. Existing limits12steps,
4business attempts,2parallel,8KiB published text и32768-byte task сохраняются.
Обязательный outcome turn относится только к opt-in профилю и двум операциям.
Его дополнительный model turn расходует прежний бюджет и требует обычного fresh
claim с актуальными grants; обход лимита аккаунта либо повторный POST не допускается.

## Миграция, доставка и rollback

Forward migration `20261026001400` следует **после** exact012→013. Guard014
проверяет source preimages, сохраняет OID, INVOKER и закрытые EXECUTE ACL, native
origin checks и append-only prefix. Новых FK/таблиц нет; purge graph не расширяется.
Она не переписывает старые records и не исправляет недоставленные predecessor
профили. Source-derived fingerprints не являются native proof: перед release
нужны отдельные canonical/drift проверки на disposable PostgreSQL и exact
release pre/post/readback. Текущая исходная композиция включает root013
`b1748bfda3d0e9f50287bb84183d919d0aca4439`: его отдельная historical native
матрица завершилась50PASS/0FAIL/0SKIP. Старые1943 FAIL/STOP сохранены как история;
отдельные последующие root context probes не превращают новый014 в проверенный.
Полный014 native и будущая01150 остаются NOT RUN. Поставка014 блокирована до
проверки актуальной полной цепочки; новый target не заменяет predecessor proof.

До включения profile нужны новые CP и RC (wire3 codec), API и PWA (profile,
evidenceKey, negative evidence). IG operation wire/adapters не изменяются:
COMMENT/Issue используют прежние специализированные operations и current grants.
Совместимость runner/role images должна подтверждаться exact source/provenance,
а не предполагаться из Ready. Сначала consumer delivery и schema proof, затем
создание нового опубликованного workflow с opt-in. Профиль не включается
автоматически после rollout. После wire3 snapshots старый RC нельзя вернуть как
обычный rollback; DDL forward-only, остановка новых запусков и forward repair
сохраняют существующие snapshots/receipts.

## Проверка и границы доказательств

Поддерживаемая проверка должна доказать обе последовательности COMMENT и Issue,
реальные owner commands/capture/ledger, immutable report pin, empty rejected
results, replay без второго effect и закрытые negative paths. HTTP provider
fixtures проверяют фактические method/body/projection, disposable owner fixtures
не называются реальным GitHub/live PDLC. Положительный local/native результат
не заменяет независимое P/S/A и ручную проверку test-repo сотрудниками.

## Закрытая проверка014

`control-plane-migration.mjs --profile workflow-review-qa-evidence` использует тот же
immutable plan/check/stop/apply/inspect/resume протокол. Каждый phase сверяется с
одним закрытым registry. До DDL требуется clean exact source с полным рядом003..014,
Goose013, отсутствие новых helpers014, четыре точных predecessor function bodies,
INVOKER/SECURITY DEFINER/owner/search_path/return/EXECUTE и отсутствие активных executions/tasks.
Оператор не поставляет отсутствующие012/013 и не принимает их по имени файла.
Полный predecessor013 проверяется встроенным proof014 в той же read-only snapshot;
историческая квитанция отдельного запуска013 не заменяет текущую проверку.

Postcondition исполняет поставляемый READ ONLY SQL: восемь exact bodies и metadata,
два полных row triggers, runtime role boundary и неизменённый purge.
Фактический граф после013 содержит105 relations/287 FK:014 не меняет его. Fingerprint
читается из actual catalog и должен совпасть с записанным в canonical purge body;
inverse normalization дополнительно сверяет всё прежнее тело. Отдельный011/010
verifier намеренно отвергает014. Ни source-derived hashes, ни mock CLI не заменяют
реальное исполнение этой проверки на disposable PostgreSQL.

Публичные ограниченные точки входа:

- `make test-workflow-review-qa-upgrade-postgres`:300s, отдельный disposable
  PostgreSQL с canonical database `control_plane`. На пустом bootstrap Goose
  применяет миграции до013, затем настоящий полный pre014 SQL проверяется тем же
  exported verifier оператора; после forward014 проверяются post014 и неизменность
  proof при повторном up. Имена БД и результаты не подменяются. Модель, staging
  и business fixtures не используются. Этот новый контур до фактического запуска
  имеет статус NOT RUN; source и mock проверки не заменяют его результат.
- `make test-workflow-result-contract`:30s, JSON Schema portion общего serialized
  corpus. Go decoder использует тот же corpus в domain unit suite; contextual
  kind/authority проверяет owner decoder, schema доказывает только форму.
- `make test-workflow-review-qa-evidence-postgres`:360s, отдельные disposable clones,
  20 owner lifecycle graphs и14 canonical/drift schema cases: вместе с двумя
  parent suites ожидается36PASS/0FAIL/0SKIP. Без готовой
  согласованной012/013 composition этот target должен закрыто завершиться до suites.

Предел 360s относится только к полному фиксированному профилю1949; его внешний
wrapper ограничивается 380s с завершением и проверкой очистки своих ресурсов.
В измеренном запуске на `c3e167b80` все 20 бизнес-сценариев прошли за 275.68s,
включая 50.10s на ожидание настоящего deadline в expiry-case; прежние 300s истекли
во время проверки схемы. Отдельная ранее завершённая проверка схемы занимала 8.16s;
сверх этого требуется время bootstrap и миграций. Поэтому 300s не оставляли запаса
на весь профиль. Timeout остаётся FAIL всего target, даже если часть cases получила
PASS. Все 20 графов, 14 вариантов схемы и guards сохраняются; runtime deadlines,
CPU2, resource envelope 1GiB с резервом 5GiB и отдельный upgrade-target 300s не меняются.

Нативные сценарии включают COMMENT effect-first/model-first, UNKNOWN без повторного
POST, отмену claimed WRITE, отзыв current grant, QA PASS, QA CHANGES_REQUIRED без
Issue и оба порядка Issue. Повтор completion сравнивает exact owner graph, прежний
COMMENT input не создаёт второй invocation, другие PR/head/body отвергаются.
Четыре смешанных сценария проверяют negative ARTIFACT_REVIEW вместе с native
COMMENT, отсутствие обязательного receipt, смену revision и digest. Отдельные
predecessor-negative clones проверяют неизменённый context helper/его ACL, возврат
reader к012, SUPERSEDED CHECK grouping и лишний overload. Retention cases включают
обе стороны HEAD lock/NOWAIT race; они не заменяются statically inferred PASS.
Schema fault injection меняет только отдельные disposable clones: partial trigger,
PUBLIC EXECUTE, body, SECURITY DEFINER, purge body и отсутствие helper. Fixtures
передают provider-shaped native receipts и не заявляют настоящие GitHub writes.

## Удержание незавершённого Issue

Owner admission сохраняет приватные `qaReport` и `qaLogs`: exact artifact ref,
revision ref/number/digest каждого отдельного вывода. Повторный selector одного
файла допустим только при совпадении всех pins; список ограничен восемью и имеет
канонический порядок. Эти поля входят в semantic identity и не входят в public
wire либо reader authority.

Новый owner-only helper `workflow_evidence_retains_revision` удерживает только
указанные revisions, когда exact native Issue ещё WAITING_APPROVAL, READY,
RUNNING либо UNKNOWN_OUTCOME. Исторические root/version/origin/RR/lease/generation,
invocation/effect/input и closed capture ancestry проверяются без зависимости от
живого root, grant, agent readiness или active model lease. У producing capture
по-прежнему обязателен COMPLETED lease. Origin, вызвавший Issue, может быть
CANCELLED после owner cancellation до model completion: это не уничтожает прежние
captured bytes. Текущий CLEAN/ACL остаётся обязательным при admission/чтении,
но последующий quarantine/revoke не даёт retention удалять unresolved evidence.

| Состояние | Дополнительное удержание report/logs |
| --- | --- |
| Pending/UNKNOWN и работающий root | Exact revisions удерживаются |
| UNKNOWN после cancel, execution expiry или grant revoke | Удержание сохраняется; новый POST не разрешён |
| Известный CANCELLED/FAILED/SUCCEEDED native outcome | Эта причина удержания снимается; прежние bindings/RR/skill/bundle правила сохраняются |
| Другой capture/revision того же root | Новая причина удержания не возникает |
| Явный авторизованный project purge | Действует прежний sealed project purge protocol; новый reader доступ не выдаётся |

Unresolved UNKNOWN нельзя объявить известным результатом ради очистки. Если
авторитетное reconciliation ещё не выполнено, pins сохраняются. Helper не
выдаётся runtime/retention worker напрямую: его вызывает прежний SECURITY DEFINER
`artifact_has_retained_revisions`, чьи OID/owner/ACL/search_path и все прежние
ветки сохраняются. Forward014 не создаёт таблиц или FK.

### Порядок HEAD lock и purge

Admission сначала берёт SHARE NOWAIT на ACTIVE HEAD report и каждого log и
удерживает их до commit exact intent. Concurrent UPDATE/DELETE/purge не может
пересечь эту границу. Если HEAD уже DELETED/PURGE_PENDING/PURGED, admission
отвергается до назначения effect. PurgeArtifact сначала берёт отдельный HEAD
FOR UPDATE, затем новым READ COMMITTED statement проверяет actual impact и
retention перед PURGE_PENDING; снимок до ожидания HEAD не используется для
разрешения удаления. Уже назначенный purge переводит HEAD в PURGE_PENDING,
поэтому последующее Issue admission закрыто. Worker retention также работает
только с DELETED/PURGE_PENDING и не может claim-ить ACTIVE report из admission.

Native source cases (пока не доказательство исполнения): UPDATE-lock→NOWAIT deny
без intent→rollback→успешный admission; owner DELETE до admission→deny; UNKNOWN
после cancel/expiry/revoke→owner DELETE разрешён, actual purge закрыт и bytes
удержаны; известный CANCELLED после root cancel→обычный purge освобождается.
Отдельно проверяются exact unrelated capture и foreign revision ID. Schema
clones включают повреждённый retention body и выдачу EXECUTE private helper.

Полная поставка014 остаётся заблокирована до исполнения нового полного proof
и явной композиции01150→012→013→014. Текущие
source-derived MD5 и105/287 описывают только схему без будущей01150 и не заменяют
её actual catalog/fingerprint или разрешение на DDL.


Полный proof014 встраивает замороженный postcondition013 в **тот же** REPEATABLE
READ READ ONLY snapshot. Собственные metadata-поля014 имеют префикс
`reviewQAEvidence` и не перезаписывают `functionSources`, ACL и прочие результаты
предшественника. До DDL проверяются все функции/ACL/constraints013 и отсутствие
объектов014; после DDL сохраняются те же проверки с четырьмя точными новыми
телами effect/journal/public projection/retention. Генератор
`tools/release/generate-workflow-review-qa-proof.py --check` проверяет воспроизводимость
обоих standalone SQL; runtime не читает внешний predecessor и не наследует старую
квитанцию. Старый verifier013 не принимает014. Source013 не изменяется.

Смешанный результат ARTIFACT_REVIEW/REVIEW проверяется одним owner evaluator.
Отрицательный ARTIFACT_REVIEW сохраняет exact reviewed revision/digest как data-only
`evidence` и отдельный typed маршрут upstream rework. Положительный REVIEW всё равно
требует native COMMENT receipt exact PR/head. При отсутствии receipt либо смене
revision/digest ни negative evidence, ни reworkSubjects не принимаются. Отсутствие
нового профиля сохраняет прежний публичный history без поля evidence. Все проверки
новой композиции, включая новые negative schema cases, до фактического исполнения
имеют статус NOT RUN. Будущая01150 потребует новой явной композиции metadata;
проверенные013 fingerprints не объявляют её применённой.


Составные JSON expressions с удалением ключей группируются явно:
`(record->'member') - ARRAY[...]`. Binary `-` имеет более высокий приоритет,
чем JSON `->`; без скобок PostgreSQL пытается преобразовать имя member в JSON.
Это проверяется реальным Goose013→014 upgrade, а не mock boolean. После такого
исправления меняется exact function source pin и заново формируется полный014
postcondition; прежняя неуспешная миграция не объявляется PASS. Правило одинаково
для intent/report/log guards и других JSON arithmetic expressions.
Официальные источники PostgreSQL18 проверены root через Context7:
[operator precedence §4.1.6](https://www.postgresql.org/docs/18/sql-syntax-lexical.html#SQL-PRECEDENCE),
[JSON operators](https://www.postgresql.org/docs/18/functions-json.html).
