---
id: OPS-DOC-WORKFLOW-EFFECT-REJOIN-1836
title: Повторное чтение успешного эффекта серверного workflow
type: runbook
status: approved
owner: architect
version: 1.0.2
updated: 2026-10-27
---

# Повторное чтение успешного эффекта серверного workflow

Политика относится только к `SERVER_DAG_V1` в #1836. Обычные integration flows
сохраняют прежнее поведение. Успешная native component-проверка не заменяет
GitHub/live PDLC: новый сценарий должен пройти отдельный disposable PG-контур
на точном интегрированном SHA. До такого прогона native-статус — `NOT RUN`.

## Опубликованные привязки

Сервер однозначно выводит `resultKey` из опубликованных обязательных результатов.
`PULL_REQUEST` в точном разрешённом repository допускает два отдельных слота:
`github.repository.bundle.publish` и `github.pull_request.create`.
`INTEGRATION_RECEIPT` допускает только `github.pull_request.merge` с точными
PR/head принятого опубликованного предшественника. Несколько подходящих
`resultKey` считаются неоднозначностью и отклоняются. У `REVIEW/QA` доказательство
имеет вид verdict/head; GitHub review WRITE для него не требуется.

Другие SERVER WRITE, включая комментарии и `github.pull_request.update`, в этом
профиле не имеют опубликованного слота и закрыто отклоняются, даже если grant
сам по себе включён. Модель не может назначить новый slot или generation.
Расширение этого набора требует отдельного точного binding-контракта;
обычный APPROVE recovery Gate его не создаёт.

## Владелец и идентичность

Порядок допуска: root lock → existing native node/connection/grant locks →
logical anchor. Проверяются активный root actor, текущие permissions, connection,
grant/version/definition/resource scope и immutable current execution. Первый
WRITE создаёт прежний native invocation/Gate и атомарно добавляет индекс
`workflow_effect_intents` к anchor. Существующий `integration_effect_receipts`
остаётся единственным источником внешнего факта. Нового registry нет.

Ключ объединяет root/anchor, `resultKey` и operation slot. Номер business attempt,
physical node/run и lease/callID не создают новое намерение. Его полное
canonical semantic input фиксируется отдельно от исходного native `input_digest`.
Последний не переписывается: старый receipt всегда проверяется по старому
invocation и его исходным pins. Изменение grant/version, connection version,
definition или scope не разрешается историческим успехом.

Для bundle перед semantic comparison обязательны точные source reads,
декодирование, policy и AV каждого member. Идентичность содержит полный manifest
с path/mode/size/member digest, source policy и base commit. Opaque source refs
могут отличаться, но target, содержимое и Git base не исключаются из сравнения.
Повтор возвращает прежние publication/commit/branch и не создаёт новый proposal.

## Матрица жизненного цикла

| Состояние и вход | Действие владельца |
| --- | --- |
| Новый exact опубликованный slot, fresh authority | Один native invocation; прежняя approval policy и Gate сохраняются |
| Exact RPC replay текущего исходного execution | Чтение того же native invocation, без нового эффекта |
| Тот же intent и known SUCCEEDED receipt, fresh execution и неизменённые текущие authority pins | Append-only `workflow_effect_rejoins`; возврат прежних receipt/result; без invocation/Gate/claim/Begin/POST |
| Новый physical call при pending либо UNKNOWN_OUTCOME | Закрытый отказ; reconciliation остаётся у прежнего native owner |
| Новая attempt при FAILED/CANCELLED native effect | Закрытый отказ: это не доказательство отсутствия внешнего эффекта |
| Изменённый input/target/base либо отсутствующий/неоднозначный slot | Закрытый отказ до allocation нового эффекта |
| Revoked/changed grant, connection или terminal root | Закрытый отказ; receipt не становится permission |
| Cancel/delete/purge root | Штатное закрытие графа; immutable факт не переписывается, физическое удаление только через canonical exact purge authority |

Предел —16 intents на logical step и16 rejoins на business attempt суммарно
по technical CONTINUES. Повторное чтение того же slot текущим execution
идемпотентно. При12 шагах и4 attempts это не более192 intents и768 rejoins;
technical continuation не выдаёт новую квоту. Forward migration009 защищает
неизменяемый prefix, точные native pins, unique slots и authorized deletion.
Применённые003–008 не изменяются, новых таблиц/FK нет.

Forward010 сохраняет009 и уточняет уникальность bridge по полному immutable
execution tuple: node/run/RR/lease/generation/runtime attempt/business attempt.
Штатный lease expiry переиспользует node, но новый RR/lease всё равно требует
bridge. Повторные exact bridges одного immutable intent/receipt в проверенной
CONTINUES-цепочке читаются как один факт через ближайший проверенный hop;
mutable timestamp либо общий `latest` для этого не используются.

Приёмка результата использует отдельный exact rejoin selector. Он связывает
текущие completed final RR/lease и business attempt с исходными invocation,
receipt и provenance, повторно проверяя актуальные grant/connection pins.
Проверенный exact bridge читается первым; при его отсутствии действует прежний
same-run selector, включая закрытую ambiguity при нескольких RR одного node.
Старый same-run selector не расширяется. `latest` либо совпадение одного URL
не являются доказательством. Ошибка SQL не превращается в отсутствие эффекта.
Неизвестный ответ COMMIT не разрешает новый ключ или внешний retry: повтор
читает тот же immutable logical slot в owner transaction.

## Typed response и поставка

У существующего `PrepareRepositoryBundlePublicationResponse` сохраняется поле1
`proposal` с исходными origin/source/commit/branch. Optional message field2
`rejoin` присутствует только при owner-proven terminal read-only исходе и несёт
текущие execution/source pins отдельно от исходных. Доменное response-only
поле исключено из JSON frozen proposal и его approval digest.

RC проверяет current pins по trusted immutable RunnerInput/request и ещё раз
читает прежний native receipt через существующий `GetIntegrationInvocation`.
Receipt ref/digest и точные commit/tree/branch/manifest/count/bytes сверяются
с прежним publication. Новый MCP outcome — terminal `READ_ONLY_REJOIN` с этим
receipt/commit, без ожидания нового Gate/callback. Исходная normal branch
сохраняет все strict equality и unknown-field проверки.

Нужен новый RC consumer до допуска CP к выдаче нового envelope. У старого RC
новое поле вызывает закрытый отказ; старый response без optional field2
сохраняет wire bytes. До поставки обоих компонентов SERVER retry не запускается.
Новых RPC/method permissions, MCP tools либо расширения runtime grants нет.

## Модель и Human Gate

В SERVER-профиле открытие Gate переводит в `WAITING_HUMAN` только root.
Уже назначенные physical executions остаются `RUNNING`: Gate удерживает
внешний WRITE, но не отзывает живой model lease. Renew и завершение по-прежнему
проходят owner deadline checks; `execution_started_at`/`execution_deadline_at`
не сдвигаются при открытии, решении Gate или восстановлении physical state.

| Переход | Закрытое правило SERVER |
| --- | --- |
| Lease истёк при sibling OPEN Gate | Exact опубликованный business node либо текущий exception manager ставится в `QUEUED`; root WAIT не мешает новому claim независимого шага |
| Lease истёк при собственном OPEN Gate | Узел также `QUEUED`, но новый claim запрещён через exact `gate_node.run_id`; старый lease остаётся `EXPIRED` |
| Один из нескольких собственных Gate закрыт | Пока другой собственный Gate OPEN, нового RR/lease нет |
| Последний собственный Gate закрыт | Сохранённый queued node может получить fresh execution без повторного expiry; historical physical WAIT восстанавливается только по exact assigned proof |
| Terminal root, неактивная/неизвестная lineage, чужой manager | Нового claim/resume нет; namespace/prefix не заменяет published/episode identity |
| Legacy workflow | Прежние all-runs wait и условия expiry сохраняются |

Собственный Gate определяется через его проверенный node и physical run, а не
по общему root. Новая eligibility связывает version/org/project, опубликованный
step/agent либо exact active manager episode; предел моделей остаётся прежним.
Expiry сохраняет очередь даже при OPEN Gate, иначе уже EXPIRED lease невозможно
было бы обнаружить повторно после решения владельца.

## Проверки

`TestWorkflowEffectRejoinComponent` проходит native
Create→Validate→Publish→Launch→Claim→Complete, настоящий owner recovery Gate
и свежую business attempt. SQL читает counters; единственное прежнее исключение
bootstrap — synthetic credential metadata, без credentials реальной среды.
Сценарии: успешный PR receipt плюс отсутствующий ARTIFACT; отзыв grant; bundle
с новым набором CLEAN source refs тех же bytes. Проверяются changed/unbound
WRITE denial, отсутствие нового work/Gate/invocation, приёмка старого receipt
текущей attempt и terminal denial. Gateway synthetic, реального GitHub POST нет.
Дополнительно выполняются настоящий lease expiry и same-node reclaim с новым
RR, а также repeated bridge через CAPTURE_PROGRESS и новый CONTINUES hop той же
business attempt. Оснастка не подменяет expiry готовым SQL state или clock.
Отдельный RC unit проходит actual MCP dispatcher, text/structured result,
safe audit и replay, включая malformed/foreign/pending/unknown proof negatives.

В bundle-сценарии original completion сначала оставляет business journal пустым:
штатный owner ACK создаёт обязательный CONTINUES. Оснастка получает его fresh
node/RR/lease, проверяет переданные immutable bundle receipt/commit и повторно
читает прежний PR receipt. Только completion этого ACK с тем же полным
declaration без обязательного файла должен дать `ARTIFACT_REQUIRED`.

`TestWorkflowServerGateIsolationComponent` держит две модели и два OPEN Gate
одного physical run. Он проверяет renew живых leases, настоящий expiry, claim
независимого шага, сохранение queued узла с own OPEN Gate, два последовательных
owner decisions и последующий fresh claim без второго expiry. Provider work
до approvals отсутствует; после проверки Cancel закрывает leases/claims без
Git WRITE. Clock anchors и absolute deadlines остаются прежними.

Native runner запускает только root после source freeze/проверки плана.
Heavy build, staging и модельные прогоны эта локальная оснастка не выполняет.
