---
id: OPS-RUNTIME-MEMORY-001
title: Диагностика нехватки памяти исполнителя
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-13
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
