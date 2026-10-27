---
id: OPS-DOC-WORKFLOW-REVIEW-QA-1949
title: Native COMMENT и сохранённый QA-отчёт серверного процесса
type: runbook
status: approved
owner: architect
version: 1.3.0
updated: 2026-10-27
---

# Явный профиль результатов

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
| Effect завершился до model completion | Final declaration проверяется в обычном completed capture; новый ход ради уже прочитанного результата не требуется. |
| Model завершился до native claim/completion | Completed lease/capture неизменяемы; physical run удерживается RUNNING, journal/downstream не создаются. Только exact native evidence invocation может быть claimed после producer completion. |
| Последний effect завершился после model | Owner сохраняет native receipt и полный terminal envelope; один fresh CONTINUES наследует logical origin/business attempt, исходную задачу и полные outcomes. Новый RR/grants создаются обычным claim. |
| Несколько effects | Join ждёт settlement всех invocations данного physical node; task не выбирает latest и не выбрасывает FAILED/REJECTED. |
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

- `make test-workflow-result-contract`:30s, JSON Schema portion общего serialized
  corpus. Go decoder использует тот же corpus в domain unit suite; contextual
  kind/authority проверяет owner decoder, schema доказывает только форму.
- `make test-workflow-review-qa-evidence-postgres`:300s, отдельные disposable clones,
  20 owner lifecycle graphs и14 canonical/drift schema cases: вместе с двумя
  parent suites ожидается36PASS/0FAIL/0SKIP. Без готовой
  согласованной012/013 composition этот target должен закрыто завершиться до suites.

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
