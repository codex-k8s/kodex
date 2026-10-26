---
id: OPS-DOC-WORKFLOW-EFFECT-REJOIN-1836
title: Повторное чтение успешного эффекта серверного workflow
type: runbook
status: approved
owner: architect
version: 1.0.0
updated: 2026-10-26
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

Приёмка результата использует отдельный exact rejoin selector. Он связывает
текущие completed final RR/lease и business attempt с исходными invocation,
receipt и provenance, повторно проверяя актуальные grant/connection pins.
Старый same-run selector не расширяется. `latest` либо совпадение одного URL
не являются доказательством. Ошибка SQL не превращается в отсутствие эффекта.
Неизвестный ответ COMMIT не разрешает новый ключ или внешний retry: повтор
читает тот же immutable logical slot в owner transaction.

## Проверки

`TestWorkflowEffectRejoinComponent` проходит native
Create→Validate→Publish→Launch→Claim→Complete, настоящий owner recovery Gate
и свежую business attempt. SQL читает counters; единственное прежнее исключение
bootstrap — synthetic credential metadata, без credentials реальной среды.
Сценарии: успешный PR receipt плюс отсутствующий ARTIFACT; отзыв grant; bundle
с новым набором CLEAN source refs тех же bytes. Проверяются changed/unbound
WRITE denial, отсутствие нового work/Gate/invocation, приёмка старого receipt
текущей attempt и terminal denial. Gateway synthetic, реального GitHub POST нет.

Native runner запускает только root после source freeze/проверки плана.
Heavy build, staging и модельные прогоны эта локальная оснастка не выполняет.
