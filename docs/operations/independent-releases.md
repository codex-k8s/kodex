---
id: OPS-DOC-INDEPENDENT-RELEASES-001
title: Независимые релизы и переход worker grants
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Независимые релизы

Источник: решение владельца, #1204 и #1031. Реализация выполняется поэтапно;
описанный конечный режим не считается уже проверенным на staging/production.

Обычный релиз меняет только выбранный deployable. Security bootstrap, смена
ключей, общая policy и authority runtime имеют отдельные входы и revisions.
Совместимость относится к протоколу и возможностям, не к общему Git SHA.
Batch состоит из независимых обновлений с ограничением параллелизма и
поэлементным результатом. Ошибка одного участника не откатывает соседей.

## Подтверждённая причина и порядок внедрения

В cdefb3d3 hot-reload renderer назначает девяти grant workloads `Recreate`
и одну реплику. Это workaround для общего watermark: timestamp revision
одного Pod делает действующий grant другого Pod устаревшим. Production base
с двумя репликами сам по себе эту гонку не устраняет.

1. #1220: additive schema и control-plane reader принимают v1 и v2.
2. После готовности всех CP consumers включается v2 signer с Pod UID из
   Kubernetes Downward API. Instance не приходит из пользовательского request.
3. После проверки совместного обслуживания включаются RollingUpdate и scoped
   application release. Общий `up` остаётся bootstrap/reconcile операцией.
4. Проверяются повторные одиночные релизы, mixed versions, batch, отказ участника,
   откат, restart sidecar и outage/rotation authority под нагрузкой.
5. Завершаются прежние STT/runtime/workspace/QA64 и итоговый baseline #1031.

## Grant lifecycle и совместимость

| Переход | Проверка и результат |
| --- | --- |
| v1 issuance | Прежняя подпись и workload-wide revision без изменения wire |
| v2 issuance | Подписанный `v=2`, canonical nonzero UUID `instance_id`, revision=iat, exact workload/SPIFFE/key generation и срок |
| Первый instance | В одной PostgreSQL transaction блокируется общий generation floor и создаётся instance watermark |
| Refresh | Revision растёт только внутри instance; exact envelope digest при повторе обязателен |
| Соседний Pod | Другой подписанный instance того же поколения не двигает revision прежнего |
| Новое поколение | Общий floor исключает прежнее поколение также для неизвестного instance и старого v1 consumer |
| Истечение/повреждение | Закрытый отказ до proof; полномочия не продлеваются |
| Частичный сбой | Transaction rollback сохраняет оба watermark; restart читает устойчивое состояние |
| Откат приложения | Схема и watermarks сохраняются; старый CP нельзя вернуть при активных v2 writers |

Trust origin: отдельный workload signer утверждает instance; после проверки
подписи CP передаёт его typed owner port. Instance только разделяет поток
обновлений, не назначает actor/project/permission. Owner resolver отдельно
проверяет действующий граф, claim/lease и полномочия перед каждым proof.
Grant admission не публикует domain event: authoritative read path —
синхронный PostgreSQL AcceptWorkerGrant. Бизнесовые события не меняются.

Таблица v1 сохраняется как общий generation floor и прежний v1 revision.
V2 не двигает v1 revision внутри поколения. Первое v2 поколение резервирует
revision=1; реальные timestamp revisions v1 остаются выше. SQL блокирует
строку до commit, поэтому concurrent устаревший запрос не использует прежний
statement snapshot. Instance watermarks не очищаются при истечении токена.

Миграция forward-only; откат бинаря не выполняет goose down. Перед v2 rollout
проверяются все CP consumers, а не только один endpoint балансировщика.
Произвольный старый бинарь не обязан понимать новый контракт: независимость
сохраняется в явно поддерживаемом диапазоне контрактов.

## Оставшиеся части общего плана

- Стабильный sidecar digest и поэтапный trust publish/readback/switch/drain/revoke.
- Last-known-good в пределах срока/отзыва, atomic adoption, bounded reconnect.
- Scoped immutable release plan, application-only apply/rollback, batch budget.
- API/events/schema expand/contract и запрет разрушительного cleanup в обычном rollout.
- Проверки доступности запросов, сохранения задач и отсутствия повторных effects.

Context7: `/kubernetes/website` (Deployment/PDB), `/websites/postgresql_17`
(ON CONFLICT и блокировки транзакций). Значения секретов не документируются.
