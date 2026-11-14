---
id: OPS-DOC-TELEGRAM-2143
title: Ограниченные исходящие уведомления Telegram
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-14
---

# Уведомления проекта через Telegram

Источники: #2143, #2033, #1830, `GUIDE-DOC-003`, `GUIDE-DOC-006` и явный
выбор владельца14.11.2026. Срез обслуживает `FINISHED`, `FAILED`, `HUMAN_GATE`
всех подходящих запусков выбранного проекта. Входящие сообщения, решения Gate,
произвольный MCP send, HTML, transcripts и платные broadcasts отсутствуют.

`control-plane` владеет настройкой подписки, grant, immutable delivery intent,
lease, Begin и receipt. `interaction-gateway` исполняет закрытые Bot API методы.
Прежние Mattermost EACH Gates, inbound, grants и receipts сохраняют свой путь.

## Полномочия и интерфейсы

| Инициатор и интерфейс | Авторитетный владелец и результат |
| --- | --- |
| Управляющий проектом → Definition/Connection | Прежний owner API разрешает проект до version/idempotency. Telegram использует только явно объявленный служебный bot и `PROJECT_SHARED`; personal credential/fallback запрещены |
| Проверка подключения → существующая очередь connection tests | Exact credential descriptor и private package/configuration pins передаются `interaction-gateway`. Свежие `getMe`, `getChat` и применимое чтение членства сверяют bot/chat. WRITE проверкой не совершается |
| `PUT /projects/{projectRef}/telegram-subscription` → `ConfigureTelegramSubscription` | Principal берётся из transport. Сервер разрешает проект и его connection, вычисляет bot/chat/topic/events/scopeDigest и назначает ref/version/expiry. Обычный payload ID не выдаёт права |
| `GET /projects/{projectRef}/telegram-subscription` → `GetTelegramSubscription` | Прежний project eligibility и permission на управление/чтение интеграций. Срок, версия и лимит принадлежат серверу; токен/descriptor не возвращаются |
| Терминальный Run либо открытие Gate → owner transaction | Создаётся один durable intent на source event и стабильную подписку. Telegram не переиспользует generic PROJECT grant помощника. Внутренний recipient `TELEGRAM_SUBSCRIPTION` назначается только специализированным owner command |
| Worker → `ClaimInteractionDeliveries` → `BeginInteractionDelivery` | Прежние workload credentials и fenced lease. Begin проверяет current project/root eligibility, subscription/grant/configuration/credential pins, expiry, quiet/rate/общий лимит и exact input digest перед внешним эффектом |
| Gateway → Bot API `sendMessage` → `CompleteInteractionDelivery` | Одна отправка безопасного server template. Проверяются точные chat/topic/message coordinates. Потерянный либо неверный ответ не разрешает повтор send |
| `GET /projects/{projectRef}/interaction-deliveries` → `ListInteractionDeliveries` | Один authoritative project/run eligibility rule; чужие проекты и скрытые Runs отсутствуют. UI читает status/version/timestamps/opaque receipt refs через bounded polling с отменой |

Новые RPC регистрируются закрыто в authority policy107. Schema046 следует
после observability045; ResourceKind остаётся `HTTPS_RESOURCE`, новый personal
permission profile не создаётся. Нужны точные bot/chat/topic и обязательный
Telegram canonical validator. Provider origin фиксирован
`https://api.telegram.org`; aliases, usernames вместо chat ID, local Bot API
server, неизвестные fields и перенаправления не расширяют scope.

Служебный bot ID и chat ID имеют каноническое десятичное написание в52-bit
диапазоне; bot ID положительный, chat ID ненулевой. `topic=NONE` означает
отсутствие thread parameter; иначе задаётся положительный существующий topic ID.
Автоматического создания topic либо перехода на `migrate_to_chat_id` нет.
Metadata lookup не доказывает существование либо право записи в topic: последнее
решает точный send endpoint и проверенный ответ. Неподдержанный тип chat закрыт.

## Явная подписка

Подписка относится ко всему выбранному проекту и выбранным типам событий.
Configure создаёт собственный owner receipt, внутренний grant и scope snapshot.
Он не создаёт фиктивное `OwnerGate.APPROVE`. UI предлагает24 часа, верхняя
граница7 дней; срок и лимит явно видны до действия пользователя. Автоматического
продления нет. Повтор того же idempotency key не переносит expiry вперёд.

Quiet hours по умолчанию выключены. Включение требует явных HH:MM и IANA time
zone. Расчёт продвигает реальные UTC минуты, учитывает переходы DST и не
выдумывает отсутствующий wall time. Очередь, частота, lifetime intent и
попытки ограничены. Expiry подписки ограничивает также lifetime её intents.

Текст выбирает сервер из закрытых templates по event/locale и содержит только
состояние и защищённую ссылку на Run/Gate. Project/Run names, prompt, transcript,
произвольные result summaries, filenames, credential metadata и HTML в шаблон
не попадают. Origin берётся из канонического
`kodex-platform-endpoints.data.publicOrigin`; HTTP Host и caller URL его не
задают. Эта же deployment projection проверяется CP и gateway.

## Жизненный цикл Telegram delivery

| Переход | Авторитетное изменение, событие и чтение |
| --- | --- |
| Configure enable | Owner назначает monotonic version, immutable scope digest, server expiry и ограниченный grant. Прежний receipt/audit; отдельного domain event нет, authoritative read — GetTelegramSubscription |
| Configure update | Старый grant отзывается. Прежние queued/claimed intents до Begin закрываются, начатые получают UNKNOWN. Новая версия действует только на будущие source events; прежний event не переотправляется |
| Source event create/replay | Intent и origin/root/version фиксируются в той же Run/Gate owner transaction. Unique source key не зависит от изменяемой версии подписки; replay не создаёт новую доставку. Основной Run не зависит от доставки |
| DUE → claim | Одна bounded lease/fence/generation и новая attempt. Exact source, subscription и grant pins сохраняются. Чтение — delivery ledger |
| Claim expiry до Begin | Для Telegram доказано отсутствие send, поскольку adapter обязан получить Begin. Старый lease отзывается, bounded retry сохраняет тот же intent. Mattermost claim expiry остаётся прежним UNKNOWN path |
| Claim → Begin | Атомарно проверяются текущие полномочия и ограничения, резервируется quota, фиксируются effect-start и exact input digest. Повтор Begin не разрешает ещё один send |
| Quiet/rate/expiry/revoke до Begin | Внешнего эффекта нет. Задержка ограничена owner queue; expiry/revoke закрывают intent. Текущие project/connection/subscription/grant проверяются повторно, cached receipt не выдаёт authority |
| Provider success → complete | Сохраняются exact Telegram receipt и terminal success, lease/grant закрываются. Lost completion ACK восстанавливается owner readback/повтором completion, а не send |
| Проверенный429 | Только валидный Bot API envelope с bounded retry_after доказывает no-effect. Owner может назначить bounded следующую attempt с новыми lease/fence и всеми свежими проверками |
| Иной подтверждённый provider rejection | No-effect фиксируется закрытым кодом. Не применяется другой bot/chat/topic; migration/permission отказ требует явного исправления настройки |
| Transport failure/неверный success после dispatch | UNKNOWN_OUTCOME, без автоматического retry send. Telegram не предоставляет универсальную idempotency для sendMessage; exactly-once не обещается |
| Lease expiry после Begin | Возможный эффект сохраняется как UNKNOWN, старый lease отзывается. Новый credential либо idempotency key не разрешает повтор этого intent |
| Disable/expiry/credential rotate | Owner transaction отзывает соответствующие grants и закрывает только связанные intents. Начатый эффект не переписывается как известное отсутствие эффекта |
| Root retry/new terminal event | Новый реальный source event получает собственный intent, если подписка ещё действует. Повтор прежнего source event не получает новое право отправки |
| Gate resolve до Begin | Устаревшее уведомление об OPEN Gate закрывается. Уже отправленная защищённая ссылка показывает актуальное состояние Gate; Telegram не принимает решения Gate |
| Project archive/delete/purge | Новые effects закрыты. Leases/grants/intents закрываются вместе с owner lifecycle; history/purge graph и backup schema проверяются отдельно, основной Run не переходит в FAILED из-за Telegram |

Переходы delivery отражаются owner audit и authoritative ListInteractionDeliveries;
отдельный delivery domain event не нужен. Нового notification scheduler, broker
consumer либо независимого engine нет: используется прежняя owner очередь и
worker lifecycle `interaction-gateway`, включая startup barrier/cancel/join.

## Секрет и провайдер

Официальный Bot API требует token в HTTPS request path. Полный URL строится
только в памяти узкого неинструментированного transport; он не возвращается
в DTO, argv, logs, traces либо ошибки. Caller httptrace hooks не наследуются.
Raw provider error/description отбрасываются, operation/metric labels закрыты.
Credential читается по exact server-issued descriptor; token не становится
общим header, public configuration либо prompt. TLS использует точные
hostname/SNI/CA и штатный egress path без plaintext/insecure fallback.

## Квалификация

Обязательны native owner PostgreSQL переходы Configure/claim/Begin/complete,
конкурентные revoke/expiry, cross-project/tenant, stale pins, queue/rate/quiet,
429, UNKNOWN и no-repeat. Synthetic transport проверяет только in-process
boundaries, а UI/browser — пользовательский путь и stale responses. Полный
schema046 normal/backup/restore/purge proof и immutable render не заменяются
наличием деклараций в registry.

Live acceptance выполняет root после установленного authenticated owner API:
один настоящий synthetic FINISHED через Kodex в exact ранее выбранный chat.
Агент этого unit самостоятельно не читает live token и не отправляет сообщение.
Отсутствующий owner login не заменяется подделкой approval либо SQL-изменением.
Реальные endpoints, credential values и private acceptance evidence не
публикуются. NOT RUN остаётся NOT RUN до фактического исполнения.

Через Context7 `/websites/core_telegram_bots_api` проверены официальные
getMe/getChat/getChatMember/sendMessage, message_thread_id, ResponseParameters,
retry_after и ограничения sendMessage. Источник:
[Telegram Bot API](https://core.telegram.org/bots/api).
