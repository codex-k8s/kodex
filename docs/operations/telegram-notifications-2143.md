---
id: OPS-DOC-TELEGRAM-2143
title: Ограниченные исходящие уведомления Telegram
type: operations
status: approved
owner: developer
version: 1.0.14
updated: 2026-11-14
---

# Уведомления проекта через Telegram

`make test-telegram-native-egress` с бюджетом360s запускает actual adapter
defaultTransport → отдельный CONNECT gateway → TLS provider `api.telegram.org`
с ephemeral CA. Только test DNS/literal dial отображаются в loopback; production
policy, CONNECT/SNI parser, TLS verification и token path сохраняются. Проверяются
health/send при deployed30s, wrongCA/SNI, запрет redirect и отсутствие второго
send после потерянного либо неверного ответа. Child binary не получает launcher
credentials. Это disposable qualification, не installed/live приёмка Telegram.

Ответ `sendMessage` связывается со свежими bot/chat observations и точными
chat/type/text/topic. В channel допустим отсутствующий `from`, только когда
`sender_chat` обозначает тот же configured channel; любой присутствующий
`from` проверяется по exact bot. Для остальных чатов `from` обязателен.
При topicNONE допускается отсутствие `message_thread_id`, но explicit
`null`/0 и неверные consumed scalar fields дают UNKNOWN без retry.
Эта форма проверена по официальному
[Message](https://core.telegram.org/bots/api#message), включая Context7.

Общий `INTERACTION_GATEWAY_OPERATION_TIMEOUT` сохраняет default30s и допустимый
диапазон1s–2m для worker/Mattermost. Telegram ограничивает каждый HTTP call
отдельно значением `min(operationTimeout,20s)`; более ранние operation,
capability, lease и subscription deadlines по-прежнему прекращают вызов.
Default ConfigMap обоих web profiles и его фактические health/send paths
проверяются вместе с границами допустимого operation budget.

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
| Provider success → complete | Сохраняются exact Telegram receipt и terminal success, lease закрывается. Подписка и её grant сохраняют прежние срок и оставшийся лимит; Lost completion ACK восстанавливается owner readback/повтором completion, а не send |
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

## Материализация и развёртывание

`interaction-gateway` входит в общий состав `web-only` и
`web-with-mattermost`. Mattermost остаётся необязательной настройкой;
пустой список его hosts не препятствует запуску общего gateway. Отсутствие
подписки либо credential отображается как отсутствие настройки и не является
успешной проверкой внешнего провайдера.

Telegram использует отдельный закрытый CONNECT listener8086 с фиксированным
`api.telegram.org:443` и точным ingress только от `interaction-gateway` в
`kodex-system`. Прежние core destinations8080 сохраняются. Runtime policy,
Service, readiness parsing и итоговые NetworkPolicy доставляются вместе;
caller header не выбирает профиль. Listener использует отдельный immutable
`egress-gateway-telegram-policy` с `EGRESS_GATEWAY_TELEGRAM_POLICY_FILE` и
`EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST`; runtime всегда выбирает лишь закрытый
Telegram profile из этого source. Базовый policy digest и ранее принятые
mail/integration/STT projections сохраняют свои прежние inputs. Отсутствие
optional Telegram input не препятствует прежнему egress startup; неполная пара
listener/file/digest закрыто отклоняется.

Публичный origin берётся из существующего
`kodex-platform-endpoints.data.publicOrigin` через `INTERACTION_PUBLIC_ORIGIN`
у CP и gateway. Caller Host, request URL и payload origin не используются.
Новый issuer добавляется к полному прежнему набору16 targets как17-й с новой
source revision `actual + 1`: после установленного106 это обычно2→3,
а не номер policy107 и не статический revision8 исходного render.
`startup_readback_required` определяет обязательный subset независимо от
общего размера registry. Для зафиксированных106/107 источников это14/15 required
при16/17 registered targets; два optional build issuer не становятся обязательными
из-за наличия receipts. Проверяются source-derived count, sorted tuple hash и
registered count вместе; caller не задаёт эти множества.
Замороженный переход105→106 сохраняет отдельную16-target
оснастку; он не переосмысливается как107.

В установленном web-only новый gateway отсутствует. Адресная доставка должна
доказать namespace ownership, отсутствие чужих ресурсов, exact specs и
подлинные image/materialization receipts, затем создать либо восстановить
только принадлежащие этому delivery ресурсы. Один render не является такой
установкой. Worker начинает работу после protected owner startup barrier;
shutdown отменяет и ожидает workers до закрытия зависимостей.

## Адресное создание отсутствующего gateway

`tools/release/telegram-resource-create.mjs` обслуживает только первоначальное
создание десяти ресурсов gateway в существующей установке `web-only`. Источник
получает сам helper через canonical Kustomize на чистом закреплённом Git SHA;
произвольные objects, patch, Secret, RBAC либо render overrides не принимаются.
Точный image gateway связывается с прежним publication receipt; общий image
issuer/worker — с UID и image текущего принадлежащего установке publisher.
Оба образа закреплены digest. Registry publication здесь не доказывает node pull.

Target содержит точные context, cluster/namespace/installation/publisher UID,
installation data digest и source SHA. Каждый kubectl получает явный
`--kubeconfig` и `--context`; terminating namespace, другой publisher или изменение
installation data отклоняются. Plan/target/publication — canonical JSON0600 в
принадлежащем текущему пользователю каталоге0700. В target не помещают credentials.

Plan фиксирует исходные три replicas, но создаваемый Deployment имеет строго
ноль. Server-side dry-run допускает лишь закрытый набор штатных defaults:
добавленные sidecar, env, volume, network destination или изменённое исходное
поле отклоняются. Нет автоматического усыновления существующего ресурса.
Namespace inventory без selector исключает orphan Pod/ReplicaSet; собственный
ReplicaSet допустим только с точным новым owner UID и подтверждёнными нулевыми
replicas. Наличие любого Pod этого gateway запрещено на данном этапе.

План закрепляет единственные абсолютные пути своего файла и evidence journal;
копия плана либо другой journal закрыто отклоняются. До первого create
сохраняются и fsync файла, и directory entry плана/журнала.
Перед каждым create журнал устойчиво сохраняет INTENT. Readback связывает
назначенный API UID, точный spec, installation owner reference и уникальный
marker плана. После потерянного ACK `status` восстанавливает только совпадающий
объект; повторного create нет. Отсутствие после INTENT, удалённый объект, смена
UID или Service allocation требуют отдельной диагностики и не создают новый UID.
История UNKNOWN сохраняется. Этот протокол не выполняет patch, delete, rollout
либо запуск worker.

Публичная проверка модели и настоящего CLI с synthetic Kubernetes backend —
`make test-telegram-resource-create` с бюджетом180 секунд. Только результат
`CREATED_DORMANT` с `NOT_ACTIVATED_NOT_INSTALLED` допустим для этого этапа.
Следующая materialization/schema046/policy107 волна отдельно доказывает все
семнадцать registered targets и обязательный subset readers, node pull, startup/readiness, bounded shutdown и сохранение
изоляции API. Сам create receipt не разрешает обход этого barrier.

## Адресная доставка runtime inputs

Schema046 planner сам получает strict source Kustomize и сохраняет в приватном
плане закрытый набор inputs. Handoff после положительного join всех старых
writers создаёт отдельный immutable Telegram policy ConfigMap, добавляет8086
в существующий egress Service, точные peers в пять NetworkPolicy и семь
именованных Secret в publisher Role с прежними `get/update`. Прежние
ports/IP, base policy, mail/integration projections, RBAC verbs и прочие поля
сохраняются. CP получает только публичный origin reference и существующий
worker grant trust mount; egress — отдельный policy mount/digest/listener.
Никакие Secret values не читаются и не записываются этим протоколом.

Каждый create/patch имеет UID/version/spec pins и свой fsync INTENT общего
migration journal. Успешный create ACK immutable ConfigMap и dormant
objects отдельно сохраняет assigned UID/body/allocation до первого GET.
Замена после ACK закрывает и текущий шаг, и последующий status; неизвестный
либо расширенный ACK не становится разрешением lost-ACK adoption.
Foreign/replaced/missing ConfigMap, расширенный source,
конфликтующий input и изменённый target закрыты. Lost ACK после выполненного
PATCH восстанавливается чтением exact body без второго эффекта; неизвестный
исход с прежним BEFORE допускает повтор только после отдельного inspect с тем
же resourceVersion. Отсутствие ConfigMap после create intent не разрешает
автоматическое повторное создание. Read-only partial status не объявляет
inputs установленными. Перед каждым новым effect снова подтверждается join;
после handoff inputs и все переданные references перечитываются до допуска
resume/activation. Terminal UNKNOWN Telegram history не считается активным
writer, незавершённая interaction delivery блокирует schema/activation drain.

Проверены Context7 `/websites/kubernetes_io`,
[immutable ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)
и [обновление ссылки Pod template](https://kubernetes.io/docs/tutorials/configuration/updating-configuration-via-a-configmap/).
Старый immutable ConfigMap не изменяется; отдельная ссылка участвует в новом
Pod template полного cohort.

Admission перед create и первым107 CAS сканирует все восемь встроенных
Kubernetes producer kinds, включая controllers с ещё не созданным Pod. Только
assigned Deployment0 и собственный ReplicaSet0 допускаются по exact UID и
положительному observedGeneration. Чужие SA/template labels либо прямые refs
десяти identity Secrets, даже у suspended CronJob либо нулевого ReplicationController, не дают право усыновить workload.
Учитывается также typed inline CSI `nodePublishSecretRef.name`.
Metadata-only TSV содержит только имена refs, не env/commands/Secret values;
пустые trailing columns сохраняются. Негативные fixtures фильтруют именно запрошенные kinds,
чтобы старый неполный scan не получил лишний ответ fake API.
Проверены Context7 `/websites/kubernetes_io`,
[Pod templates](https://kubernetes.io/docs/concepts/workloads/pods/) и
[service account Pod template](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_set/kubectl_set_serviceaccount/) и
[CSIVolumeSource](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-v1/).

## Порядок authority107

`TELEGRAM_NOTIFICATIONS_106_107` — отдельный закрытый профиль. Его source
policy107 добавляет ровно четыре operation profiles к106; registry сохраняет
все прежние16 entries и добавляет только issuer нового gateway. Старые106
receipts (required14/registered16) и terminal proof106 не разрешают открыть Service, изолированный для107.

| Этап | Авторитетное доказательство и допустимый результат |
| --- | --- |
| Dormant create | Свежие source/publication receipt и10 actual objects с назначенными UID/spec; Deployment0, плановые replicas3. Только `PENDING_AUTHORITY` |
| Schema046 и cohort | Закрытый predecessor045, полный normal/backup catalog, node proof и неизменные UID старых компонентов. Новый receiver сохраняется0; весь cohort ещё не READY |
| Registry/key DELIVER | Fresh isolated Service и прежний API join; перед первым CAS снова читаются все dormant objects и private create lineage. Registry revision увеличивается от actual, publisher остаётся exact owned image |
| CP projection107 | Public046 inspect подтверждает полный schema postcondition, terminal Job/Pod и положительный join всех5 writers. `control-plane-policy-projection.mjs` принимает `--migration-plan/evidence` только для staged107; pointer CAS сохраняет CP0 и требует свежий owner DELIVER |
| Запуск readers | Fresh owner status Job подтверждает exact107 operation/registry, `DISTRIBUTE`, protocol2, опубликованный snapshot `base+1` и `deliveredAt`. Это `PENDING_AUTHORITY`, без требования выдуманных17 readbacks до запуска |
| Readbacks и RETIRE | Новый issuer реально запускается после barrier; CP уже обслуживает107 projection, точный startup-required subset15 из17 registered targets даёт свежие readbacks. Exact owner RETIRE с required-set hash относится к `base+3` и той же operation |
| Service restore | Только отдельный fresh terminal107 proof с точным required subset и registered17 и полный ready serving inventory открывают прежний selector. Dormant/DELIVER receipt не является terminal admission |

Для нового receiver schema handoff является устойчивым принятием уже
созданного exact spec0 без patch: исходный creator proof должен остаться
проверяемым перед107 CAS. Отдельный start меняет replicas0→3 только после
DELIVER; lostACK восстанавливается по intent и полному ready inventory,
не повторяя patch. Перед `activate` штатный API maintenance OPEN должен
подтвердить exact новый spec, собственный journal и готовность всех Pods; Service
остаётся изолированным. Результат `READERS_STARTED_PENDING_RETIRE` подтверждает
лишь запуск readers за этой boundary, без открытия пользовательского трафика.

Первый schema `resume` возвращает `STAGED_PENDING_AUTHORITY`: CP, PWA и egress
также остаются0, без ложной readiness нового image со старой106projection.
Handoff фиксирует протокол `STAGED_POLICY_107_V1`, несовместимый со старым executor,
который запускал CP раньше projection. После DELIVER и успешного pointer CAS
`resume` получает `--cp-projection-plan`, `--cp-projection-evidence` и свежие
authority status paths. Проверяются exact own ACK UID/body, source policy107,
исходный migration plan и единственное разрешённое изменение policy pointer.
Только после полного Pod ready CP возобновляются PWA/egress; API и новый issuer
остаются0. Привязка projection передаётся во все последующие inspect/resume/activate.

Публичная фаза `control-plane-migration.mjs activate` принимает приватные
`--authority-plan`, `--authority-evidence`, `--authority-status-plan`,
`--authority-status-evidence`, `--api-open-plan`, `--api-open-evidence`,
`--cp-projection-plan`, `--cp-projection-evidence` только
для046. GET/logs перечитывают исходный owner operation, свежий status Job,
OPEN journal и Service/EndpointSlices. Чужой source/UID, незавершённый OPEN,
неполный Pod inventory либо потерянная изоляция не дают start. Та же привязка
передаётся в последующие inspect/resume; простые replicas3 не разрешаются.
Normal и backup команды046 отдельно зарегистрированы в terminal inventory
только для Job `control-plane-migrate-20261114140000` и bounded `-attempt2`.
Это исключение не доказывает DDL PASS и не разрешает его повтор.

`make test-telegram-authority-delivery` воспроизводит закрытые policy/registry
границы107 и отдельные total/required counts, промежуточный DELIVER, creator lineage и старые106 regressions с
бюджетом180 секунд без live effects. Полный install/activation unit и actual
readbacks квалифицируются отдельно; один этот target не доказывает установку.

Terminal UNKNOWN с `completed_at` и всеми закрытыми lease fields не блокирует
явное изменение metadata Telegram connection. Только этот путь допускает
исключение; удаление, credential policy, package binding и Mattermost сохраняют
прежний запрет при неопределённом исходе. UI предупреждает, что доставка могла
произойти и изменение настройки не отменяет прежнюю отправку. Новый scope
требует свежей проверки credentials и явного Configure; прежний source event
не отправляется повторно, stale completion не превращает UNKNOWN в SUCCESS.

`make test-telegram-deployment` — отдельный public real-CLI transport contour
с бюджетом900 секунд: source-only Git checkout, настоящий creator journal,
полный045→046 metadata proof, check/stop/join/DDL, адресные input effects,
lost ACK, image/config cohort и повторный resume без второго DDL. Синтетические
Kubernetes/PostgreSQL transports не заменяют native SQL либо installed proof;
После DDL результат остаётся `STAGED_PENDING_AUTHORITY`, пока CP не получил
собственную projection107; mock не подтверждает ready нового CP со106policy.
Далее тот же contour выполняет реальные отдельные CLI-процессы
rotation/status→projection107→CP resume→API OPEN за изоляцией→issuer activation→
RETIRE/status→Service restore→повторный final inspect/resume без effects.
История RESTORED не разрешает первый start нового issuer: activation требует
ещё изолированного Service. Для последующего read/rejoin требуются exact own
`--api-restore-plan/evidence`, terminal107 той же operation и текущий полный
serving inventory; это не выдаёт нового права открыть Service.

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

Для адресного create через Context7 `/websites/kubernetes_io` проверены
[API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/)
и [garbage collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/):
server-side dry-run не сохраняет объект, назначаемые UID/resourceVersion/Service
адреса могут отличаться, owner reference относится к той же namespace boundary.
