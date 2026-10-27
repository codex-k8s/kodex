---
id: OPS-DOC-RUNTIME-INIT-1941
title: Диагностика подготовки рабочего каталога сотрудника
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-27
---

# Граница доказательства

Issue #1941. Исторический отказ workspace-init27.10.2026 остаётся с причиной
`UNKNOWN`: Pod уже удалён, точное исходное сообщение недоступно. Новый канал
диагностики не доказывает исправление доступности, DNS, диска либо провайдера.
Модель не запускается после неуспешного init; полномочия и retry policy не меняются.

Runner записывает только `kodex.workspace-initialization-failure.v1` в фиксированный
kubelet termination file `/dev/termination-log`. Максимум записи2KiB, один JSON
object без неизвестных/повторных ключей. Container использует policy `File`,
без fallback к stdout/stderr. Файл не создаётся runner, symlink/hardlink/FIFO не
принимаются, modes не расширяются. Ошибка записи не отменяет исходный отказ и
не делает неполную рабочую область готовой.

Record содержит три immutable digest, session/turn/attempt и закрытые stage/reason.
Run/node/root и lease уже принадлежат controller и owner. Pod UID берётся из
текущего Kubernetes object после проверки имени, namespace, owned metadata,
образа/init-команды и execution pins; runner не выбирает UID. Используется только
текущее `State.Terminated`, не `LastTerminationState` и не произвольные logs.
Это observation текущего Pod, не новая historical UID authority.

| Этап | Наблюдаемая причина |
| --- | --- |
| INPUT_DECODE, CONTEXT_SNAPSHOT, INPUT_MANIFEST | INPUT_INVALID |
| EGRESS_TRUST | UNKNOWN либо typed filesystem failure |
| CALLBACK_SETUP | UNKNOWN, без предположения об отказе серверной авторизации |
| WORKSPACE_LAYOUT, INPUT_PROTECTION, CONTEXT_FILES | Закрытые typed cancel/deadline/IO классы либо UNKNOWN |
| ARTIFACT_TRANSFER | Отдельные actual HTTP401/403/404/5xx, metadata pins, body digest, typed cancel/deadline/IO; неизвестное не объявляется сетью |
| UNKNOWN | Недоступная, повреждённая или несовпадающая запись |

Источником класса служат ветка, тип либо sentinel ошибки. Текст `Error()`, body
ответа, пути, имена материалов, credentials и содержимое файлов не используются
как classifier и не сохраняются в record. Пары stage/reason замкнуты общим
runtime-контрактом; новый произвольный suffix не становится кодом интерфейса.

# Owner commit и очистка

Controller фиксирует terminal candidate по Pod UID и закрытой записи. Смена
UID/записи сбрасывает grace этого candidate, но не продлевает execution deadline
либо lease. Callback completion сохраняет прежнее право выиграть гонку.

В existing `CompleteExecution` передаётся один immutable request: прежние
lease/fence/generation, failure, `RUNTIME_INITIALIZATION_FAILED` и закрытый
`i18n:RUNTIME_INIT_<stage>_<reason>`. Owner повторно проверяет locked lease и
атомарно сохраняет summary в `run_nodes.progress_summary`, `runs.result_summary`
и существующем событии. SERVER recovery продолжает собственный lifecycle;
диагностика не принимает business result и не создаёт effect receipt.

Новые поля RPC, таблицы, JSONB, DDL, permissions или diagnostic registry не нужны.
Public API/WS используют прежние safeErrorCode/progress summary с RU/EN переводом;
private execution pins и Pod UID в публичный граф не добавляются.

| Исход | Сохранение и cleanup |
| --- | --- |
| Новый owner ACK | Closed summary уже зафиксирован; затем safe structured log и прежний DeleteTurn |
| Transient delivery failure | Прежний bounded retry того же request/key; неизвестный ACK не объявляется новым успехом |
| AlreadyExists | Прежний replay/cleanup path, без нового diagnostic acknowledgment log |
| Owner deny/cancel/lease loss | Прежнее закрытие ресурсов; late diagnostic не обходит authority, новый owner record не выдумывается |
| Missing/malformed/foreign termination record | UNKNOWN init explanation, без raw bytes или конкретной выдуманной причины |

Structured log после ACK содержит только нормализованные stage/reason,
record_status и уже доверенные execution/UID metadata. Они не становятся metric
labels. Сохранённый owner summary переживает удаление Pod; stdout/stderr init
и24h idempotency receipt не являются его единственным хранилищем.

# Проверка и поставка

Локальные fixtures отдельно проверяют strict codec и formatter redaction,
HTTP/pin/digest классы настоящего reader, descriptor safety, fake-Kubernetes
provenance, смену UID, callback winner и immutable delivery до cleanup.
Disposable PostgreSQL fixture проверяет native owner commit, replay,
summary/event/receipt cardinality и защищённое чтение после удаления local caller
record. Это не live Pod cleanup и не реальная модель: эти границы не смешиваются.

Поставка: сначала API/PWA переводы и CP owner с closed code, затем RC reader,
после этого immutable runner binary в фактически используемом role image.
Старый runner даёт UNKNOWN init; новый controller сам не создаёт отсутствующую
запись. После записи нового кода возврат к owner, который его не принимает,
не является совместимым recovery. Требуется совместимый roll-forward; history
не очищают ради downgrade. До exact image/source readback новый путь не считают
доставленным. Нет credential/admission bypass, host fallback, remount или
увеличения квот.

Ручная проверка оператора после отдельных local/native/review/release checkpoint:
две разрешённые read-only роли получают четыре закреплённых входа; нарушенные
ownership/digest закрыто отклоняются. Проверяется безопасный owner summary после
cleanup и recovery, а не только Ready Pod. При отсутствии causal proof исходная
live причина остаётся UNKNOWN даже после успешного нового запуска.

Документация Kubernetes проверена через Context7:
https://kubernetes.io/docs/tasks/debug/debug-application/determine-reason-pod-failure/ .
