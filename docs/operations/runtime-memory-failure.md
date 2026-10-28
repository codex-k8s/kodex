---
id: OPS-RUNTIME-MEMORY-001
title: Диагностика нехватки памяти исполнителя
type: operations
status: approved
owner: manager
version: 1.0.2
updated: 2026-10-25
---

# Граница изменения в bootstrap #1797

Подтверждённый kernel OOM Developer в Retry9 нельзя считать доказательством
отказа внешнего провайдера. Kubernetes-наблюдение различает текущий
`State.Terminated.Reason=OOMKilled`, общий выход контейнера и отсутствие Pod.
Сам по себе exit137, прежний `LastTerminationState`, текст ошибки и неизвестный
контейнер не доказывают текущую нехватку памяти. Проверенные источники:
Context7 `/websites/kubernetes_io`, Kubernetes resource limits и ContainerState.

Actor остаётся runtime-controller с прежним workload/RPC profile. Pod разрешается
из exact lease имени и immutable RuntimeRevision, не из нового клиентского поля.
Никаких новых endpoint, permission, credential, SQL, миграций или событий нет.

| Путь | Источник / authority | Результат и consumer |
| --- | --- | --- |
| Tracker без terminal callback | Прежняя exact lease/fence/generation; текущий Pod с matching revision | После прежнего callback grace CompleteExecution сохраняет FAILED, существующий RUNTIME_UNAVAILABLE и локализуемое резюме нехватки памяти |
| Ошибочный callback | Прежние mTLS/ticket/provenance и owner CompleteExecution | После подтверждённого нового commit до cleanup дополнительно читается cold Pod; закрытая диагностика пишет OBSERVED/UNAVAILABLE, не переписывает payload/usage/archive или owner verdict |
| Успешный callback / replay AlreadyExists | Прежний owner ответ | Нет новой диагностики отказа или подмены результата |
| Ошибка чтения / чужая revision / Pod отсутствует | Kubernetes read / существующий exact predicate | Не выдумывается OOM; прежний completion и cleanup сохраняются |
| Cancel / delete / expiry / stale fence | Прежняя owner-транзакция полного графа | Grants/leases отзываются штатно, поздняя диагностика не восстанавливает исполнение |
| Retry / continuation | Прежняя новая attempt и свежая RuntimeRevision | Новые pins; старые результаты и незакоммиченный временный checkout не считаются восстановленными |
| Warm callback | Нет доказанной привязки cold Pod | Не классифицируется по истории общего warm Pod; payload сохраняется |

Callback не переопределяет ошибку по изменяемому Pod snapshot: иначе повтор
одного callback мог бы иметь иной semantic intent при том же idempotency key.
Следовательно, отдельный callback, опередивший Kubernetes status, пока может
сохранить исходную общую ошибку. Этот остаток явно открыт; диагностический fix
не объявляется полной гарантией пользовательской OOM-классификации.

Резюме tracker проходит прежний CompleteExecution → PostgreSQL run/node/event →
HTTP/WebSocket snapshot → SafeSummary/чат. Используется уже разрешённый
RUNTIME_UNAVAILABLE; новый wire enum не вводится. Сам OOMKilled не различает
лимит контейнера и нехватку памяти узла, поэтому резюме не утверждает
превышение конкретного лимита. Логи содержат закрытые
коды и доверенные execution pins, не Kubernetes message, argv или payload.

Проверки: текущий OOM в известных main/init контейнерах; приоритет над обычным
выходом соседа; exit137 без OOM; прошлый OOM; неизвестный контейнер; callback
commit до cleanup; отсутствие новой диагностики для replay/success/owner error;
сохранение usage/payload и redaction; локализация ru/en. Живое провоцирование
OOM и новый Workflow остаются отдельными проверками, не подменяются fixtures.

## Закрытая диагностика warm reconciliation

Постоянный Aborted после hot reload проверяется через прежний
RuntimeWorkService.ReconcileWarmRuntime, workload principal и owner transaction.
Серверный enum показывает фактический rejecting stage: scope/transaction,
provider policy read/decode/catalog/publication, session binding/replacement,
runtime snapshot, overlay, session catalog, environment, lifecycle, prompt,
workspace, context, specification, digest и commit. Marker сохраняет cause
для errors.Is/As и более точный внутренний stage; unknown остаётся UNKNOWN.
Log содержит только stage и прежний gRPC code, не ошибку SDK, payload,
provider credential или SQL. Endpoint, ответ, scope, locks, rollback/commit,
OCC/fences, refresh policy, события и eligibility не изменяются. Успех не
приобретает failure marker. Это диагностика причины, не исправление конфликта.

## Прерывание исполнения и выселение окружения (#1856)

В callback producer `agent-runner/internal/callback.Client.Complete` только
проверенная exact diagnostic `REQUEST_FAILURE / TERMINAL_WAIT / PROVIDER /
CONTEXT_CANCELLED` с исходным машинным `PROVIDER_UNAVAILABLE` меняется на
существующий `RUNTIME_UNAVAILABLE` и резюме `i18n:RUNTIME_INTERRUPTED`.
Префиксированный `RUNTIME_PROVIDER_UNAVAILABLE` является прежним ключом
пользовательского сообщения, а не машинным кодом этого predicate.
Проверяются schema/closed enums и RuntimeRevision/input/execution digests,
session/turn/attempt. Mapping выполняется после validation, до единственной
serialization; повтор доставки использует прежние зафиксированные bytes.
Usage (включая completeness), artifacts, archive и diagnostic не меняются.
Успех, auth, cancel, timeout, workspace failure и прочие diagnostic не remap.
Уже принятые старые RC intents не переинтерпретируются.

| Сценарий | Переход и authority | Результат / read path |
| --- | --- | --- |
| Отменённое ожидание с exact diagnostic | Прежний callback ticket и owner lease/fence/generation; mapping в runner перед serialization | FAILED с generic code и локализованным прерыванием, прежние usage/archive/artifact receipts; HTTP/WS → SafeSummary |
| Потерянный callback ACK / retry | Тот же immutable intent и idempotency key; Pod не входит в predicate | Прежний owner replay; изменённый Pod не меняет bytes либо authority |
| Tracker первым видит текущий `Pod.Status.Reason=Evicted` | Один GET cold Pod, exact metadata и непустой observed UID; прежний callback grace и owner completion | Generic `RUNTIME_UNAVAILABLE`, резюме `RUNTIME_ENVIRONMENT_EVICTED`; без нового event/enum |
| Текущий OOM известного контейнера | Прежний typed State.Terminated, приоритет над Evicted/обычным выходом | Прежнее `RUNTIME_MEMORY_EXHAUSTED`; exit137/history/чужой контейнер OOM не доказывают |
| Callback первым завершил ошибку | Только новый успешный owner commit, до cleanup; один bounded cold GET (1s) | Операторские closed `pod_observation/pod_reason/observed_pod_uid`; callback не меняется. Старые memory log keys сохранены |
| Success / owner error / AlreadyExists | Прежний ответ owner | Без нового probe/лога ошибки; cleanup и cardinality не расширяются |
| Чужие metadata, revision/lease/attempt, пустой UID / read error / missing Pod | Закрытый конфликт либо недоступность | Нет Evicted/OOM; raw message не используется и не выводится |
| Warm | Сохраняется существующий compatibility check; probe не читает общий warm Pod | История его ресурсов не приписывается текущему cold исполнению |
| Cancel/delete/expiry/late callback | Прежние owner terminal guards и отзыв grants | Наблюдение не открывает root и не восстанавливает lease |

Cold metadata берутся из общего producer `runtimePodMetadata`: namespace/name,
managed/mode/execution hash, revision/config/environment/workspace/execution/MCP
и policy digests, controller, organization/project/session/turn/attempt, lease.
Полный `runtimePodMatches` остаётся в Create/adopt. UID — **текущий наблюдённый
UID одного GET**, а не historical expected UID и не источник полномочий.
Историческое UID pinning данным unit не вводится; новых реестров/кэшей/прав нет.
Root/run полномочия и действительность lease по-прежнему проверяет owner.

Typed `Evicted` не различает storage pressure, лимит emptyDir, memory pressure
и другие причины. Kubernetes message не парсится. Точная storage-specific
причина в UI позднего callback **не исправлена**: он получает только нейтральное
прерывание, а доступный текущий Pod даёт отдельную операторскую диагностику.
Живое воспроизведение эвикции, новые лимиты, cache/TMPDIR tuning и восстановление
scratch не входят в проверку. Опубликованные CLEAN artifacts и Git остаются
штатным путём восстановления; временный checkout не является durable state.

Доставка: новый RC с диагностикой принимает прежние callback payloads;
локализация PWA устанавливается до активации нового runner. Proto/DB/RuntimeRevision hash/права/лимиты не
меняются. Старый runner сохраняет прежнее поведение; это не remap исторических
completion receipts. Rollback кода не меняет уже сохранённые сообщения.
