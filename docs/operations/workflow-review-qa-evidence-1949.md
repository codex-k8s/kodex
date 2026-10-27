---
id: OPS-DOC-WORKFLOW-REVIEW-QA-1949
title: Native COMMENT и сохранённый QA-отчёт серверного процесса
type: runbook
status: approved
owner: architect
version: 1.1.0
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
pin; COMMENT input уже содержит exact PR/head. Exact execution replay и fresh
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
release pre/post/readback. Непроверенные012/013 и STOP1943 нельзя обходить новым
именем target. До их согласованной композиции поставка014 блокирована.

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
Goose013, отсутствие новых helpers014, три точных predecessor function bodies,
INVOKER/owner/search_path/return/EXECUTE и отсутствие активных executions/tasks.
Оператор не поставляет отсутствующие012/013 и не принимает их по имени файла.
Отдельное одобрение и доказательство полного predecessor013 остаётся обязательной
зависимостью поставки, а не результатом incremental verifier014.

Postcondition исполняет поставляемый READ ONLY SQL: шесть exact bodies и metadata,
два полных полных row triggers, runtime role boundary и неизменённый purge.
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
  девять owner lifecycle graphs и семь canonical/drift schema cases. Без готовой
  согласованной012/013 composition этот target должен закрыто завершиться до suites.

Нативные сценарии включают COMMENT effect-first/model-first, UNKNOWN без повторного
POST, отмену claimed WRITE, отзыв current grant, QA PASS, QA CHANGES_REQUIRED без
Issue и оба порядка Issue. Повтор completion сравнивает exact owner graph, прежний
COMMENT input не создаёт второй invocation, другие PR/head/body отвергаются.
Schema fault injection меняет только отдельные disposable clones: partial trigger,
PUBLIC EXECUTE, body, SECURITY DEFINER, purge body и отсутствие helper. Fixtures
передают provider-shaped native receipts и не заявляют настоящие GitHub writes.
