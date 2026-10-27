---
id: OPS-DOC-WORKFLOW-REWORK-1947
title: Возврат замечаний автору в серверном DAG
type: operation-contract
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-27
---

# Граница профиля

Подзадача #1947 эпика #1836. Этот документ задаёт контракт нового полного unit;
сам по себе он не подтверждает реализацию, native-проверку или поставку.
Исходный source checkpoint — `4cad806e7e375440034affcc1c06d720afc1592d`.
Полнота контекста manager относится отдельно к #1943 и forward012.

Новый переход включён только в явно опубликованной версии `SERVER_DAG_V1`
с `reworkPolicy: OWNER_APPROVED_UPSTREAM_V1`. Отсутствие policy сохраняет прежний
same-step recovery и прежние canonical published bytes. Старый episode/Gate
после rollout не получает новые последствия. Изменение policy — обычное новое
owner-публикуемое workflow version, а не перепривязка действующего Run.

## Отредактированный контракт и authority

| Путь | Полномочие, pins и результат |
| --- | --- |
| Owner UI либо assistant plan → workflow create/update | Прежние actor/project, specialized owner command, OCC и idempotency. Assistant payload не публикует себя и не задаёт root actor |
| OpenAPI → gateway caster → Proto → CP spec | Optional closed reworkPolicy и ARTIFACT_REVIEW сохраняются без default/repair; unknown поля и вид отклоняются до записи |
| ARTIFACT_REVIEW publication | subjectStepKey — точный published direct dependency, subjectResultKey — точный ARTIFACT predicate этого шага; reviewer независим от автора. Ссылки не выдают доступа |
| Runner final declaration | Старый schema kodex.workflow-result.v1; у ARTIFACT_REVIEW только key, source и verdict. source строго CAPTURED_ARTIFACT с artifactRef/revision/digest; verdict PASS либо CHANGES_REQUIRED |
| Owner evaluator | Exact completed lease/RR/attempt + captured input snapshot. Source обязан совпасть с принятым subject/result receipt; markdown, имя роли и URL не являются verdict |
| Recovery episode | Versioned UPSTREAM_REWORK, server-derived subject/targets и immutable source pins. Manager только объясняет; маршрут из произвольного текста запрещён |
| Owner Gate | Прежние resolve authority/OCC/idempotency; безопасный typed rework plan с авторами и affected перепроверками. APPROVE разрешает эту свежую попытку, не принимает результат |
| Dispatch/claim → provider | Только после owner transition; fresh run/node/turn/RuntimeRevision/grants. Root, agent и published steps разрешает сервер |
| Completion → journal/event → API/WS/PWA | Старый append-only receipt не переписывается. CURRENT/STALE_DEPENDENCY/HISTORICAL projection отделяет эффективную приёмку от исторического accepted |

Дополнительный `subjectResultKey` optional/omitempty; для прежних result kinds он
запрещён. Git REVIEW/QA сохраняют exact headSha и прежнюю форму. ARTIFACT_REVIEW
не подменяет ARTIFACT-отчёт: при необходимости отчёт — отдельный result predicate.

## Lifecycle

| Событие | Owner transition и потребители |
| --- | --- |
| REVIEW/QA/ARTIFACT_REVIEW CHANGES_REQUIRED | Закрытая причина плюс exact published subject. Append rejected reviewer attempt, один UPSTREAM_REWORK episode; существующие event/manager/Gate механизмы |
| Human REQUEST_CHANGES | Только exact result Gate и опубликованный rework profile; target выводится из subject predicate либо собственного author step. Обязателен сохранённый owner comment |
| Manager completion | Analysis — недоверенные данные; никакого self-approval, изменения target или acceptance |
| Owner APPROVE | Root lock, current exact episode/Gate, deadline/authority/caps/quiescence и pending UNKNOWN barrier. Один owner transition создаёт новые author attempts, сохраняет старые узлы и journal |
| Пока автор выполняется | Старые consumer PASS не эффективны. Никакого root SUCCEEDED или принятия stale Gate на новом output |
| Автор принят | В топологическом порядке создаются настоящие fresh attempts affected consumers с новыми exact accepted input snapshots. Независимая ветвь остаётся эффективной |
| Повторные reviewer PASS | Принимаются только exact новые subject revision/digest/head и новый Gate опубликованного physical producer |
| Replay/lost ACK | Тот же stored owner outcome; новый Run/attempt/event не создаётся |
| Late callback прежней attempt | Не восстанавливает effective acceptance и не меняет immutable inputs; прежние lease/generation/terminal guards |
| REJECT/CANCEL/expiry/root deadline | Прежнее атомарное закрытие графа/leases/grants/episodes. Причина человека не выдумывается для system expiry |
| Revoke/configuration drift | Fresh launch/claim отклоняется; состояние authority не восстанавливается из старого snapshot |
| UNKNOWN внешний WRITE | Продолжение заблокировано до authoritative read-only recovery; новый WRITE не подменяет неизвестный предыдущий |
| Известный SUCCESS | Прежний exact logical effect rejoin остаётся read-only. Новый intent/head/content требует собственного допуска. Уже выполненный affected MERGE/DEPLOY закрыто требует owner repair |

## Эффективная приёмка и хранение

Один bounded owner predicate проверяет latest accepted attempt, exact Gate и
свежесть всех dependency receipts. Проверка транзитивна только по валидированному
published DAG до 12 этапов и 8 зависимостей. Тот же predicate используется dispatch, final root
completion, runtime input eligibility и public full/delta projection. Timestamp,
первый/последний произвольный artifact или физический SUCCEEDED не заменяют pins.

Historical journal и physical inputs неизменяемы. Новый author/recheck получает
новый physical node с прежним logical anchor и business attempt+1. Same-input
retry остаётся отдельным прежним путём; его SQL guard не расширяется до
произвольного retarget. Старые artifact revisions сохраняются по действующему
retention, но не становятся current accepted input нового turn.

Прежние пределы: 12 этапов, 4 бизнес-попытки каждого этапа, 48 episodes, 2 одновременных
исполнения, общий deadline и durable capture budget. Rework не обнуляет счётчики.
Gate не предлагает APPROVE, если необходимые повторения исчерпали бюджет.
Forward013 добавляет только owner-proven transition и guards после frozen012;
применённые миграции и их proofs не переписываются. Exact pre/post/purge proof
определяется итоговой композицией, не предполагается по истории.

## Обязательное доказательство

Native RED старого same-review recovery, затем A1 → R1 CHANGES_REQUIRED →
manager/Gate → ровно A2 → реальные R2/Q2 на новых inputs. Отдельно: Q1 PASS
становится stale, независимая ветвь сохраняется, replay/late callback не создают
повторов, old Gate не принимает новый output, revoke/cancel/expiry/deadline,
attempt4/episode48/capture/concurrency2, UNKNOWN и известный SUCCESS rejoin.

Artifact review проверяет exact accepted revision/digest; одинаковые слова PASS
в markdown не влияют на typed verdict. Owner/public/WS/PWA формы, оба Gate views,
assistant configuration и editable controls должны сохранять одно значение.
Проверки codegen drift/wire bounds и browser выполняются отдельно на frozen SHA;
старое измерение wire не является proof нового поля. Real PDLC/live acceptance
не подменяется local unit либо source review.

Consumer-first rollout выполняет только оператор root после полного baseline и
независимого review. Новые durable episode формы могут требовать совместимого
roll-forward; old history не очищают ради downgrade. Здесь нет автоматического
merge, staging действия либо нового grant для сотрудника.

## Точные отложенные переходы и закрытая история

В approved plan каждый автор и каждый affected recheck закреплены logical anchor и
точными previousAttempt/nextAttempt. Монотонный floor начинает действовать в той же
owner-транзакции, до появления отложенного physical node. Старый PASS сразу перестаёт
быть current; его запись остаётся accepted как исторический факт. Следующий frontier
создаётся только после whole-root pending barrier и с полным свежим dependency snapshot.
Новая ошибка следующей попытки не потребляет прежнее approval.

Параллельный старый отказ, уже точно включённый в approved plan, не создаёт второй
конфликтующий episode. Это исключение ограничено origin и previousAttempt данного
плана; соседний отказ либо новый counter снова требуют manager/owner transition.

Только устаревшие OPEN ordinary result Gates точных old producers закрываются атомарно
с APPROVE как `state=CANCELLED`, `resolutionKind=SUPERSEDED`, без owner decision CANCEL.
Каждый такой Gate требует fresh gate.resolve. Соседние, integration/delivery Gates и
terminal history не меняются. Событие содержит действительное состояние root; клиент
не трактует supersession как отмену root или свежего автора.

Optional `effectiveState` публичной записи: CURRENT, HISTORICAL, STALE_DEPENDENCY,
REWORK_PENDING, WAITING_OWNER либо BLOCKED. При отсутствии поля действует legacy
проекция. Публичный reworkPlan содержит только schema, keys, counters и closed blocker;
приватные node/lease/RR/plan routing pins не выдаются. nextAttempt=0 в preview означает
недоступность пятой попытки при явном blocker, а не новую попытку с нулевым номером.

013 добавляет один private DEFERRABLE NO ACTION FK к exact approved Gate и один closed
closure discriminator. Authorized purge сохраняет прежнее canonical тело и проверяет
inverse graph: известный graph012 плюс только названный FK. Новый fingerprint вычисляется
из каталога; source-count expectation не является native metadata proof. Active-root
capture retention005 остаётся неизменным и удерживает прежние exact revisions до terminal.

Поставка требует отдельно подтверждённого predecessor012, operator pre/post proof013,
all-consumer rollout и только затем публикации нового profile. Пока #1943/012 остановлен,
доставка #1947 также не READY. Это не разрешение повторять остановленный native путь.
