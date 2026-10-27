---
id: OPS-LEGACY-WORKFLOW-REWORK-1924
title: Неизменяемая доработка результата прежнего workflow
type: operation
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-27
---

# Доработка результата прежнего workflow

Связанный Issue: [#1924](https://github.com/codex-k8s/kodex/issues/1924).
Документ описывает ограниченный профиль, а не подтверждение его доставки.
Schema011, локальная компиляция и штатный migration Job не заменяют native и
ручную приёмку на одном checkpoint. SERVER_DAG_V1 сохраняет отдельные business
attempts, journal и logical WRITE rejoin; этот профиль их не переиспользует.

Владелец разрешает `REQUEST_CHANGES` только опубликованным списком решений
результата конкретного делегированного этапа. Доверенные actor/project/root,
Gate, версия и опубликованный step разрешаются сервером до мутации. Fresh
проверка исходного инициатора использует `workflow.launch`, а не дополнительное
право прямого запуска агента. Provider grant не заменяет это разрешение.

Новая попытка получает отдельные Run, Session, Turn, node, lease и immutable
RuntimeRevision. Её `retry_of_run_id` ссылается на прежний физический child.
Неизменяемые run/session/root/retry/attempt поля защищены только у новой owned линии; обычные и SERVER runs сохраняют прежние transitions.
Два private pins связывают node с сохранённым CHANGES_REQUESTED Gate и первым
узлом новой attempt; technical CONTINUES наследует эти pins. Прежние Run,
node, RR, CALLBACK_TO и callback snapshot остаются неизменными. Public graph
проверяет child retry через private owner proof и не выдаёт private Gate IDs.

Координатор доставки разрешается по исходному callback и единственной точной
CONTINUES-цепи. QUEUED leaf без RR и active lease сервер отменяет перед новой
доставкой. Уже исполняющийся координатор даёт
`LEGACY_REWORK_PARENT_BUSY`; владелец ждёт завершения либо отменяет root.
Нельзя выбирать произвольный CANCELLED узел, переписывать старый receipt или
возвращать SUCCEEDED run в очередь ради повторной попытки.

| Переход | Авторитетный результат и граница |
| --- | --- |
| REQUEST_CHANGES | Exact OPEN result Gate становится CHANGES_REQUESTED; свежий child и новая callback boundary создаются одной owner-транзакцией. |
| Replay / stale / foreign | Exact receipt replay не создаёт ещё child; неверная версия и чужой project отклоняются до эффекта. |
| Claim / renew | Fresh published step, root initiator, agent capabilities и integration grants проверяются заново. Чужой Gate и terminal root не выдают lease. |
| Lease expiry | Старый lease закрывается; новый claim того же physical node получает свежие RR/generation/fence. Own OPEN Gate блокирует новый claim. |
| READ / NONE | Один незавершённый native READ slot на physical turn, под owner node lock; exact replay разрешён. Уже завершённые чтения не блокируют новое чтение. |
| Model раньше READ | Child сразу остаётся RUNNING; result Gate/callback не создаются до terminal READ. |
| READ раньше model | Model completion соединяет все terminal READ results того node в один CONTINUES. |
| READ ACK | Точный native ledger даёт все invocation refs, состояния, результаты и receipt pins. Fresh authority проверяется перед materialization; нового READ/WRITE не происходит. Gate не выдумывается. |
| READ UNKNOWN | Удерживает обязательство доставки и запрещает новый pending invocation под другим key. Владелец читает native ledger либо отменяет root. |
| READ task больше32768 codepoints | READ fact/receipt и уже выполненный capture сохраняются; root атомарно FAILED с `LEGACY_REWORK_READ_LIMIT`, leases/grants закрываются, CONTINUES не создаётся. Исходные данные доступны owner graph/native read. |
| Новая result Gate | Принимается точный final RR, включая результаты прежних own CONTINUES producers; до APPROVE новая доставка координатору не исполняется. |
| Несколько Gate | Разрешение одного Gate не снимает другие OPEN obligations. Последний подходящий owner event повторно проверяет общий delivery join. |
| APPROVE | Новая задача координатора содержит отдельно REQUEST_CHANGES feedback и новый accepted result Gate ref/version/decision/comment; каждое continuation создаётся не более одного раза. |
| REJECT / CANCEL | Один канонический FAILED либо CANCELLED full-graph closure. REJECT не создаёт промежуточного cancellation события. Старые успешные children и receipts сохраняются. |
| Terminal / project closure | Повтор не открывает root, grants, leases или callbacks. Физическое удаление проходит прежний exact project purge protocol и новые deferred FK. |

В новом child/technical профиле разрешены `platform.artifact.manage` и exact
READ capabilities. Artifact manage включает запись материалов, а не только
чтение. WRITE, delegate и launch закрыто отклоняются; обычный прежний workflow
и SERVER не получают этого ограничения. Если в root уже была non-READ native
invocation, новая доработка закрыта с
`LEGACY_REWORK_WRITE_REQUIRES_RECONCILIATION`: нужно сверить внешний результат
и использовать SERVER-процесс либо отменить этот запуск. Это ограничение
не следует объявлять полной приёмкой WRITE rework.

Callback сохраняет ранние собственные материалы только по доказанной цепи
того же child/agent/Gate/origin и единственному COMPLETED RR/lease каждого
producer. Минимальный профиль принимает исходные revision1/version1,
ACTIVE/CLEAN и exact revision content. Advance, delete, quarantine либо
отсутствующий member закрыто дают `LEGACY_REWORK_DELIVERY_BLOCKED`; файл не
пропускается молча и не заменяется новым HEAD. Его источник и текущая
доступность продолжают проверяться обычным protected file reader. Root actor
и старый receipt не создают новый grant на чтение скрытого материала.

Обход lineage ограничен фактическим графом с проверкой cycle/unique hop;
новый child учитывается в существующем пределе128 graph Run snapshots.
Полный assembled task использует canonical32768 codepoints. Произвольные
лимиты64 attempts или SERVER-предел4 к прежнему режиму не применяются.

## Проверка и доставка

Поддерживаемая disposable точка проверки:

```bash
bash scripts/tests/control-plane-postgres-test.sh '^(TestLegacyWorkflowReworkComponent|TestLegacyWorkflowReworkMigrationProofComponent|TestServerWorkflowMigrationProofComponent)$'
```

Реальные бизнес-состояния создаются командами Create/Validate/Publish/Launch,
Delegate/Claim/Complete/ResolveGate. SQL оснастки читает metadata; DDL drift
разрешён только в отдельных проверенных loopback clone databases. Прямой SQL
для постановки child/model/callback в готовое состояние недопустим.

Штатный `tools/release/control-plane-migration.mjs` получает отдельный
`--profile legacy-workflow-rework`: target20261026001100, predecessor010,
immutable source/image и собственные pre/postcondition SQL. CHECK → STOP →
apply → inspect → resume сохраняют прежний journal/CAS/Job UID protocol.
Функции verifier010 не принимают011. Новый postcondition сохраняет полный
SERVER/retention/visibility/effective ACL proof и проверяет композицию
прежнего284-графа с двумя exact deferred NO ACTION FK, а не угадывает hash.
Полный исходный purge body закреплён по `md5(prosrc)`; postcondition проверяет его обратной подстановкой единственного нового graph guard. Семантический drift вне graph clause не получает PASS.
Следующий неизвестный target отклоняется до эффектов.

Перед приложением нового CP требуется schema011. До первого rework совместимый
CP010 ещё читает обычные прежние/SERVER данные; после появления private rework
metadata откат на binary без этого reader/claim/callback path недопустим.
Миграции003–010 и их применённые тела не переписываются. Для возврата сервиса
после неизвестного DDL outcome используются inspect/resume того же плана;
Job и idempotency key не создаются заново.

Ручная приёмка: вернуть небольшой материал на доработку с замечанием, скачать
обе версии, принять исправленный результат с новым замечанием, проверить
одну новую доставку и окончательный результат координатора в UI/graph.
Отдельно выполнить REJECT/CANCEL и проверить отсутствие новых моделей после
terminal. Локальный PASS не заменяет эту авторизованную проверку.
