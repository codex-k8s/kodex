---
id: OPS-INTERACTION-PACKAGE-1046
title: Управляемый пакет системных подписок и доставки Mattermost
type: operational-contract
status: approved
owner: platform
version: 1.0.0
updated: 2026-09-05
---

# Источник и граница

MVP-UI-42 и CFG-03 эпика #1018 требуют исполнения опубликованной revision,
включая её ограничения и Human Gate. Состояние connection, grants, immutable
delivery intent и решений принадлежит control-plane (#1046). Worker #1030
получает private package с точными pins; public HTTP/SDK не публикует package
worker snapshot. Actor для команды решения приходит из проверенного transport,
внешний Mattermost actor разрешается через server-owned identity и текущие
права на конкретный gate.

| Сценарий | Владелец и authority | Snapshot и переход | Результат/consumer |
| --- | --- | --- | --- |
| Подписка | InteractionWork.ListSources, workload, активный exact grant | Один transaction read connection/version, credential, bound package; inbound READ без отдельного approval | Private source; worker сверяет package/pins, старое поколение cancel/join |
| Входящее сообщение | InteractionWork.AcceptMessage, проверенная identity и launch/gate.resolve | Повторно current package/grant/pins; gated inbound не принимается как автоматическая подписка | Receipt и Run/gate transition прежнего owner lifecycle |
| Gate request/ACK | Owner gate либо acceptance receipt | Отдельный серверный контекст точного gate/receipt, channel/root; без рекурсивного gate для сообщения о gate | Одна fenced delivery, точный readback |
| Terminal notification/mirror | Core terminal transition, точный connection grant | Immutable template/package/connection intent → WAITING_APPROVAL + отдельный owner gate | Gate доступен в общем owner list/get; core Run остаётся terminal |
| APPROVE | gate.resolve до OCC/idempotency | Один owner transaction: gate APPROVED, delivery DUE; exact intent неизменяем | OWNER_GATE_RESOLVED, authoritative delivery queue |
| REJECT/CANCEL | Тот же exact owner gate | Gate terminal, delivery CANCELLED; повтор не создаёт попытку | OWNER_GATE_RESOLVED, authoritative gate read; core Run без изменений |
| Claim | Exact workload/lease/fence/generation | Current grant/connection/package совпадают с intent; approval gate APPROVED для notification/mirror | Private claim с package/pins/approval receipt |
| Success | Та же leased attempt | SUCCEEDED + точные external post/channel/thread | Авторитетная delivery read/incident, без изменения core Run |
| Confirmed no effect | Та же leased attempt | Только доказанный отказ до эффекта допускает bounded retry в пределах выбранного package | Новая generation/lease; прежний effect intent и approval сохраняются |
| Timeout/expiry/unknown | Owner clock или worker report | UNKNOWN_OUTCOME, grant не выдаётся повторно | Авторитетный incident/read; скрытой повторной отправки нет |
| Revoke/rebind | Owner connection/grant command | Current eligibility прекращается; активный intent не переносится молча на новую revision | Cancel либо явный conflict по обязательному owner lifecycle |

Create gate публикует OWNER_GATE_OPENED, решение — OWNER_GATE_RESOLVED.
Для прочих delivery/source переходов отдельного доменного события нет:
авторитетны защищённые source/claim/read/incident RPC. Continuation и terminal
core Run не подменяются состоянием optional delivery. Проверки охватывают
обычный approval/reject/cancel, replay/OCC, tenant/grant/revision mismatch,
неизменность terminal Run и UNKNOWN без resend.

Документ задаёт контракт полного изменения. До завершения producer/consumer
и PostgreSQL проверок реализация этого вклада не считается готовой.

## Локальная проверка producer

Disposable PostgreSQL проверяет исходный identity/ACK/UNKNOWN path и пять
approval вариантов: APPROVE, REJECT, CANCEL, stale intent pin и revoked grant.
Проверяется отсутствие claim до решения, OCC, idempotency receipt, единичный
effect, private package/pins/approval readback и неизменность terminal Run.
Пакет задаёт число попыток; исчерпание этого бюджета показывается как terminal
incident, а не обещание очередного автоматического retry.

Сверены Context7 `/jackc/pgx`: закрытие Rows перед следующей командой того же
transaction, чтение ошибки cursor и обработка Commit/Rollback. Проверки
SQL boundary, Proto/policy codegen и race/vet/build выполняются отдельно от
browser и живого Mattermost. Последние остаются NOT RUN до общего gate.
