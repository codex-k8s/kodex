---
id: OPS-SERVER-WORKFLOW-RETRY-SNAPSHOT-1939
title: Полный снимок SERVER business retry
type: operation
status: approved
owner: architect
version: 1.0.0
updated: 2026-10-27
---

# Полный снимок SERVER business retry

Issue [#1939](https://github.com/codex-k8s/kodex/issues/1939). Этот документ
описывает контракт; native, browser и delivery отмечаются отдельно на точном SHA.

SERVER recovery создаёт новый дочерний run логического этапа. `RETRY_OF`
соединяет фактический final node предыдущей business attempt с первым physical
node новой. Оба run принадлежат одному workflow root, имеют отдельные sessions
и обычный `Run.attempt=1`; их `retryOfRunRef` пуст. Техническое `CONTINUES`
остаётся внутри своей business attempt и может сделать source runtime attempt
больше1. Нельзя переписывать эти данные под прежний root-to-root retry reader.

У technical final `parentNodeRef` указывает на предыдущий physical node.
Owner читает единственную обратную `CONTINUES`-цепь с точным parent, теми же
run/agent/origin/business/inputs и историческими COMPLETED capture/RR/lease
координатами каждого производителя. Она заканчивается первым physical node
с runtime attempt1 и root parent; для прежней business attempt2/3 закреплены
также предыдущее одобренное recovery и принадлежащий ему `RETRY_OF`.
Публичные consumers проверяют ту же структурную цепь и прежний typed proof;
приватный journal или active grant они из неё не выводят. Обход ограничен
числом уже существующих узлов и исключает cycles, а не вводит новый retry budget.

## Владелец и транспорт

| Этап | Источник и граница |
| --- | --- |
| Owner query | Проверенные transport actor/org → прежний domain GetRunGraph/GetRunGraphSnapshot → canonical run/project eligibility → одна `REPEATABLE READ READ ONLY` transaction. |
| Owner history | Exact root/published SERVER version/step/agent; source immutable journal и COMPLETED RR/lease/capture tuple; same origin и sealed inputs; fresh anchor business attempt+1 от2 до4. |
| Разрешение повтора | Сохранённый RECOVERED episode с exact recoveryNodeId и APPROVED recovery Gate. Это историческая связь; новая authority при чтении не появляется. |
| Публичное поле | Optional `RunEdge.serverWorkflowRetry` (Proto field7): workflowVersionRef, workflowVersionDigest, originNodeRef, stepKey, previousBusinessAttempt, businessAttempt. Private RR/lease/generation/input/Gate comment не публикуются. |
| CP consumer | Проверка типизированного proof и его связи с graph endpoints/runs/anchor public history; только затем исключение из прежнего root-to-root validator. |
| API consumer | Generated Proto → строгая полная WebSocket projection с exact node/run/project/parent/agent/version/attempt/history и отсутствием повторного slot. Прежние cardinality/frame/readiness guards сохраняются. |
| PWA consumer | Закрытые шесть полей и те же структурные связи проверяются до изменения cache. RUN_GRAPH_SNAPSHOT → RUN_READY/heartbeat подтверждает поток; общий PLATFORM_READY не заменяет RUN. |

Consumer доверяет owner attestation приватной истории; он не пытается проверить
Gate или получить grant из шести публичных полей. Ошибка чтения либо неполная
проекция остаётся `RUN_UNAVAILABLE`; не допускаются любые похожие same-root edges.
Обычный HTTP graph и command graph используют тот же owner proof. Command graph
не является полным readiness snapshot; сохранённые event deltas остаются прежними
bounded wake/history и не дополняются задним числом. Rejoin перечитывает полный
owner snapshot, а не восстанавливает приватное доказательство из старого event.

## Матрица жизненного цикла

| Случай | Результат |
| --- | --- |
| Initial, ordinary или assistant retry | Новое поле отсутствует; прежний validator сохраняется. |
| SERVER attempts2–4 | Exact предыдущий final, исходный logical anchor и новая first physical attempt; без выбора latest или timestamp. |
| Predicate rejection | Предыдущая immutable journal entry accepted=false и exact approved recovery episode. |
| Принятый результат → REQUEST_CHANGES | Journal acceptance сохраняется; exact result Gate CHANGES_REQUESTED объясняет episode OWNER_CHANGES_REQUESTED, отдельный recovery Gate APPROVE создаёт новую попытку. |
| Technical CONTINUES до business rejection | Source соответствует terminal journal provenance, не обязательно первому physical node. |
| Cancel, terminal, session archive, reload | Историческое доказательство сохраняется без active model lease/grant/agent readiness. Свежая canonical reader eligibility всё равно обязательна. |
| Неверные step/input/origin/attempt/history/Gate/project/root | Owner proof отсутствует либо malformed projection закрыто отклоняется; read не создаёт допустимого ребра. |
| Повтор чтения/rejoin | Нет mutation, idempotency receipt, audit, domain event, нового lease или effect. |

## Проверка и поставка

Нативная существующая `TestWorkflowExceptionRecoveryComponent` проверяет
попытки2–4, включая capture-progress/technical final второй попытки, полный
owner snapshot до следующего claim и после terminal rejection. Новая
`TestServerWorkflowRetrySnapshotComponent` проверяет accepted→REQUEST_CHANGES→
APPROVE и сохранённое доказательство после owner Cancel. Они используют обычные
owner commands и scanner; SQL не вставляет готовые business states. PostgreSQL
запускается лишь штатным disposable entrypoint с явным фильтром и frozen plan.
API tests проводят generated projection через настоящий recoverRun→RUN_READY,
PWA tests — через JSON frames/reconnect без полного HTTP resync. Browser на
реальном доставленном SHA нужен отдельно.

Требуется consumer-first поставка совместимых API и PWA, затем CP producer.
Это additive Proto message field, но прежний строгий JSON decoder не обязан
понимать новое поле. После первого proof-снимка восстановление использует
совместимые consumers; старые CP вновь отвергнут SERVER retry snapshot.
Новой DDL/RPC/permission/authority policy нет. RC, IG и runner не валидируют этот
full browser read profile; их runtime/retry/lease контракты не меняются.

Проверены Context7: Protocol Buffers proto3 message presence
(`/websites/protobuf_dev`) и pgx StrictNamedArgs (`/jackc/pgx`). Existing RPC
метод и actor authorization остаются прежними.
