---
id: OPS-DOC-TELEGRAM-2143
title: Ограниченные исходящие уведомления Telegram
type: operations
status: approved
owner: developer
version: 1.0.23
updated: 2026-11-15
---

# Уведомления проекта через Telegram

Новый producer install inputs выпускает только
`kodex.telegram-runtime-inputs/v2`: render обязан содержать четыре точных Telegram
env, включая отдельную revision. Сохранённый immutable receipt v1 проверяется
строго по своему прежнему render из трёх env и полному исходному snapshot; новый
pin в таком render отклоняется. Версия берётся из hash-bound install metadata,
а не угадывается по отсутствующему полю. Проверка не добавляет env, не меняет
версию/хеши/старый migration journal и не разрешает запуск нового приложения без
обязательного pin. Source, render, policy, target UID и original snapshot guards
сохраняются для обеих версий. `telegram-runtime-inputs.test.mjs` проверяет обе
формы и отрицательные current/retained сочетания.

Listener Telegram в egress требует полный отдельный набор
`EGRESS_GATEWAY_TELEGRAM_POLICY_FILE`,
`EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION` и
`EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST`. Канонический render закрепляет для
него revision `2026-09-05.1`; `EGRESS_GATEWAY_EXPECTED_POLICY_REVISION`
продолжает проверять только основную policy и может иметь другое значение.
Отсутствующий либо неверный Telegram pin не наследуется от основного профиля.
Проверка `TestTelegramRevisionIsIndependentFromReleasedBase` загружает оба
файла при разных ревизиях и подтверждает закрытый отказ отдельного профиля.
Доставка нового binary и его env в существующую установку требует отдельного
закреплённого плана; этот исходный контракт не переписывает ранее созданные
046/107 plans, journals или ConfigMap policy.

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

Kubernetes возвращает `serviceAccount` как deprecated alias `serviceAccountName`.
Dry-run admission принимает его только при точном совпадении с непустой
identity исходного Pod template; другая identity и отсутствующий source закрыты.
Проверено через Context7 `/websites/kubernetes_io` и официальный
[контракт PodSpec](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-v1/).

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

### Узкая API boundary и исправленный delivery executor

Для107 maintenance target может содержать только Deployment и Service
`control-api-gateway` либо прежний полный authority target из пяти ресурсов.
Во втором случае сохраняется прежний exact hash. В первом admission отдельно
проверяет совпадение всех installation/context/cluster/namespace seals и обеих
API UID с полным authority target. Произвольное подмножество, дополнительный
ресурс либо другой UID отказаны. Исходные close/isolate bytes, их hashes и
maintenance ID не переписываются. Terminal proof содержит API `targetSHA256`
и отдельный `authorityTargetSHA256`; final read/rejoin проверяет обе границы.
Исторический106 не получает этот альтернативный scope.

Если исправление release CLI понадобилось после остановки, не нужно менять
source уже опубликованных images или повторять DDL. Новый проверенный executor
должен содержать frozen application SHA как реального ancestor и не менять
application, config, schema, SQL или закреплённые authority policy. Допустимы
лишь release tools, документация и Makefile. Оба checkout должны быть чистыми.
Для новых `control-plane-policy-projection.mjs plan` и
`component-api-traffic.mjs plan --action restore --policy-transition
TELEGRAM_NOTIFICATIONS_106_107` явно передаются `--application-source` и
`--application-revision`. Новый immutable plan сохраняет оба source path/SHA;
apply/resume/status снова проверяют эти pins и отсутствие drift.

| Переход | Неизменная граница и результат |
| --- | --- |
| Уже выполненные close/isolate/DDL | Только исходные планы, журналы, UID и hashes; нет rebind, нового stop или DDL |
| Authority rotate/status | Новый executor читает explicit frozen `--source/--revision`; API admission сопоставляется с полным authority scope |
| CP projection | Новый план отдельно закрепляет executor; public migration inspect и fresh DELIVER обязательны до CAS; CP остаётся0 |
| API OPEN | Исполняется CLI исходного application checkout, поэтому source OPEN совпадает с migration/image source |
| Service restore | Новый план закрепляет executor и frozen application source; свежий RETIRE, точный API scope и ready inventory обязательны |
| Final migration inspect/resume | Новый release CLI читает прежний migration plan и новые projection/restore receipts; прежние journals не переименовываются |

`make test-telegram-frozen-delivery` имеет отдельный бюджет900s: полный CLI
сценарий с узким API target, другим executor path/SHA и проверкой неизменных
close/isolate файлов. `make test-telegram-deployment` сохраняет полный target
и единый source как отдельную регрессию. Оба используют synthetic cluster/SQL
transport; это не installed E2E и не доказательство live readiness.

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


## Восстановление frozen046 после ошибки имени порта

Canonical имя Telegram listener — `tg-connect` (8086/TCP). Старое
`telegram-connect` имеет16 символов: это допустимый Service port name, но
недопустимый ContainerPort name и строковый Service targetPort. До нового
migration plan проверяется полный rendered source, включая init containers.
Правила проверены через Context7 `/kubernetes/kubernetes` и официальный
[IsValidPortName](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/apimachinery/pkg/util/validation/validation.go#L283-L309).
Существующий historical plan остаётся доступен для строгого readback.

Если046 уже успешно выполнена, повторять DDL либо редактировать старый plan
нельзя. `telegram-port-repair.mjs plan` вызывает полный original migration
inspect (Job/Pod, exact PostgreSQL identity и postcondition), затем свежий
Job/Pod join и exact Service-before. Допускаются только все5 stopped0/joined
до любого `PAIR_HANDOFF_INTENT`, Service PATCH intent, resume либо прежнего
repair bind. Выполненные ранее network/ConfigMap/RBAC inputs сохраняются.

```sh
node tools/release/telegram-port-repair.mjs plan \
  --context "$CONTEXT" --kubeconfig "$KUBECONFIG_FILE" \
  --migration-plan "$MIGRATION_PLAN" --migration-evidence "$MIGRATION_EVIDENCE" \
  --output "$PORT_REPAIR_PLAN"
```

Repair plan закрепляет original plan/input/handoff hashes, JobUID, installation,
PostgreSQL/postcondition pins, exact application и новый config/executor SHA,
а также hash/длину неизменяемого префикса журнала. Разрешены только три alias
поля в двух egress manifests; image, env, policy, RBAC, endpoint, port number
и DDL остаются исходными. Новый executor должен быть реальным потомком
application source, оба checkout — чистыми. Текущий config повторно проверяется
до runtime effects.

| Переход | Авторитетное доказательство и эффект |
| --- | --- |
| Repair plan | Полный fresh046 postcondition, exact terminal Job и все5 stopped/joined; Kubernetes writes отсутствуют |
| Handoff | `--telegram-port-repair-plan` → fresh owner/schema/join → append/fsync `TELEGRAM_PORT_REPAIR_BOUND` → штатные typed runtime inputs и staged specs |
| Resume/inspect/activate | Тот же repair pin; отдельные effective inputs/handoff не заменяют original planSHA; DDL/check/stop/retry с repair запрещены |
| Projection107 | Frozen application flags плюс repair pin разрешают только проверенные два manifest deltas; child migration inspect получает тот же repair path |
| API restore/final readback | Отдельный executor/source proof хранит repairPlanSHA; API readers и final migration inspection проверяют тот же pin |
| Потеря ACK | Durable bind/intent сохраняется; exact readback и штатный rejoin без повторного подтверждённого effect |

Для каждого postDDL migration вызова передаётся `--telegram-port-repair-plan`.
Projection/traffic планирование использует `--application-source`,
`--application-revision` и тот же `--telegram-port-repair-plan`; saved plans
закрепляют эти pins для apply/readback. Новый canonical API OPEN сохраняет
прежний application source: его spec данным исправлением не меняется.

`make test-telegram-port-repair` проверяет validators, negative lifecycle/source
и полный CLI046/107 с synthetic frozen application и отдельным новым executor.
Это disposable qualification; installed repair выполняет только уполномоченный
оператор в выбранном target. Прежний префикс журнала не меняется, новый bind и
обычные recovery receipts добавляются в append-only журнал. Rollback DDL и
возврат к недопустимому alias отсутствуют.


Для `telegram-notifications-backup` после final107 API restore создаётся новый
immutable backup resume plan штатным `backup-controller-maintenance-cli.mjs`.
Его `--stopped-source-sha` равен original application SHA (именно источнику
backup stop и migration), а отдельный executor остаётся новым. При plan
передаются original `--stopped-plan`, `--stopped-evidence`, `--migration-plan`,
`--migration-evidence` и полный набор final107 ссылок: `--cp-projection-plan`,
`--cp-projection-evidence`, `--authority-plan`, `--authority-evidence`,
`--authority-status-plan`, `--authority-status-evidence`, `--api-open-plan`,
`--api-open-evidence`, `--api-restore-plan`, `--api-restore-evidence`, а при
ремонте также `--telegram-port-repair-plan`. Они закрепляются в plan и
передаются spawned migration inspect; произвольные argv и source override
запрещены. Apply/status используют закреплённые ссылки; переданный иной путь
закрывается до journal lock и эффекта. SQL/owner/database pins не меняются.

`make test-telegram-port-repair-backup` — отдельный900s wholeCLI контур:
canonical backup stop → original046 → invalid port refusal → repair →107 →
API restore → real backup resume plan/apply/status через spawned final inspect.
Он не добавляет второй DDL и не изменяет original plan. Normal repair и backup
имеют отдельные конечные бюджеты; последовательный запуск обоих не укладывается
в один общий900s budget.


## Позднее owner чтение107 и отдельный executor ремонта

Проверка полного staged cohort может длиться дольше30 секунд. Job
`rotation-status` остаётся неизменной исторической квитанцией; его timestamp
нельзя обновлять по часам клиента. Для107 новый status plan отдельно закрепляет
`ownerScope`: точные context/kubeconfig, исходную application revision,
целевой authority scope, UID PostgreSQL Pod/StatefulSet, system identifier,
database OID и hash фиксированного READ ONLY SQL. Ранее созданный rotate plan,
его Job, registry/publisher provenance и журналы сохраняются.

| Consumer | Историческое доказательство | Непосредственно перед допуском |
| --- | --- | --- |
| Projection107 и запуск CP | Original rotate + status Job/Pod и полный staged046 inspect | Fixed local-socket `internal_rpc_authority` READ ONLY; та же operation/phase/registry/snapshot/required-reader область; затем повторный registry/publisher readback |
| API readers/activate | Те же immutable status/source/installation координаты | Новое actual owner чтение DELIVER, прежний30s TTL и монотонный readback count |
| Service restore/final inspect/backup resume | Original RETIRE Job и exact API2/fullauthority5 отношение | Actual RETIRE owner чтение с теми же immutable координатами; новый observedAt только из PostgreSQL |

Неизвестные поля, смена operation, phase, snapshot, PG system identifier/OID,
Pod/StatefulSet либо уменьшение readback count закрыто отвергаются. SQL JSON
`+00:00` и Go `time.Time` JSON `Z` сравниваются как один instant без потери
микросекунд. Прежний106 rotation/restore path не получает нового допуска.

Если ремонт уже закреплён за CONFIG revision, новый tools-only EXECUTOR
использует эту же immutable repair plan. Каждый migration handoff/resume/
inspect/activate получает прежний `--telegram-port-repair-plan` и явный
`--telegram-port-repair-executor-sha <EXECUTOR_SHA>`. При создании projection
и restore планов передаются также прежние `--application-source` и
`--application-revision`. Saved sourceCompatibility сохраняет три отдельных
pin: APPLICATION, CONFIG и EXECUTOR. Требуется настоящая ancestry
CONFIG→EXECUTOR, чистые checkouts и неизменность application, deploy, contracts,
SQL и107 policy. Отсутствующий либо чужой executor pin не разрешает продолжение.
Backup resume plan получает тот же дополнительный SHA вместе с полным набором
final107 references; child inspection передаёт их без изменения.

Публичные проверки:

```sh
make test-telegram-authority-owner
GOCACHE=<PRIVATE_TMPFS_CACHE> GOTMPDIR=<PRIVATE_TMPFS_TEMP> TMPDIR=<PRIVATE_TMPFS_TEMP> \
  KODEX_NATIVE_GO=<GO_1_26_10_BINARY> make test-telegram-authority-owner-native
make test-telegram-port-repair
make test-telegram-port-repair-backup
```

Native точка запускает pinned PostgreSQL18.3 только в RAM disposable container
без host volumes/network, с loopback ephemeral port, и применяет private Go
`-overlay` к exact-SHA существующей проверке ротации. Настоящие SQL status,
`decodeRotationStatus`, `json.Marshal` и повторный SQL сопоставляются общим
wire contract. Overlay/source pins, RAM filesystem и exact container join
проверяются; source Go и SQL не изменяются. Это representation qualification,
а не live107 delivery или новый authority grant. WholeCLI repair сценарии
добавляют реальную35s задержку до late read, сохраняя старые Job logs.

## Owner status до первых107 readbacks

`authority_rotation_status()` ранее читала snapshot coordinates только из
`authority_rotation_operation_publications`. Publisher создаёт эту строку
после `PROMOTED`, когда обязательные readers уже подтвердили snapshot. Во время
`DISTRIBUTING` snapshot уже записан в `authority_snapshot_history`, intent имеет
protocol2/`DELIVERED`, но status ещё не выдавал `sourceRevision` и
`sourceDigestSHA256`. Допуск projection требовал эти сведения для запуска CP,
а подтверждения CP требовались для `PROMOTED`: возникал цикл запуска.

Forward-only owner migration `20261115000100` сохраняет прежнюю функцию,
OID/signature/owner/ACL и исправляет только этот read path. При отсутствии
завершённой phase publication fallback допускается исключительно для
`DISTRIBUTING` + `DISTRIBUTE` + protocol2 `DELIVERED`. Полностью совпадают
operation, registry digest, phase intent, publication input digest, source,
predecessor и expected reader count. Snapshot publication предшествует
`deliveredAt`; отсутствуют promoted/retired/aborted markers. Coordinates
берутся из фактической immutable history; registry digest не используется
вместо snapshot digest. Функция считает фактические snapshot readbacks,
включая законный0, и не объявляет `PROMOTED` либо `RETIRED`.

Прежние SQL миграции не меняются. Delivery precondition принимает только canonical predecessor
Goose `20260912000200` и точную прежнюю функцию с owner/migrator EXECUTE ACL.
Изменённая function definition либо дополнительные EXECUTE grants закрыто
отклоняются. Новый schema head — `20261115000100`; создаётся только один Goose
record. Данные operation/phase/intent/history/readbacks не изменяются.
Существующие RETIRE и legacy ветви сохраняют прежний результат.

Доставка является отдельной migration-only операцией с immutable built CLI
image и прежней canonical migrator identity/TLS boundary. Она проверяет точные
PG Pod/StatefulSet UID, system identifier/database OID, старый function hash,
полную provenance уже доставленного107, исходный046 Job, API isolation и
остановленный cohort. Publisher deployment/image и старые rotation, migration,
projection plans/journals сохраняются. После DDL postcondition сверяет новую
функцию, прежние OID/attributes/ACL, Goose predecessor hash и неизменную ledger.
Потерянный Job create ACK допускает только exact read-only rejoin, не второй Job.

После успешного owner upgrade создаётся **новый** status Job через прежний
frozen application/executor. Ранее записанные status Job logs не переписываются.
Новый SQL результат проходит прежние Go DTO и DELIVER validators; исходный
projection plan с существующим ConfigMap ACK продолжается через `resume`.
Новая source compatibility либо ослабление30s TTL для этого не нужны. Далее
сохраняется обычная последовательность CP/readers, activate, RETIRE, Service
restore и backup resume. Откат owner migration запрещён; исправление возможных
ошибок — только новым квалифицированным forward successor.

`make test-authority-distribute-status-native` воспроизводит настоящий old
owner state с `DELIVERED` history до любых readbacks/завершённых publications,
применяет новую миграцию при активной operation и проверяет SQL raw/Go DTO
существующим107 validator. Тот же fixed observer SQL delivery CLI выполняется
до и после; проверяются same OID/owner/ACL/full ledger, repeat, отказ Down,
predecessor ACL/body drift и22 нарушенные provenance bindings с rollback.
`make test-telegram-authority-owner-native` отдельно сохраняет полный fresh/
repeat/RETIRE/legacy/workload/freshness native regression. Оба используют только
pinned disposable PostgreSQL18.3 в RAM и synthetic loopback credentials;
installed migration/rotation этими проверками не объявляются выполненными.


Для уже остановленного107 действует отдельный исполняемый оператор
`tools/release/authority-distribute-status-migration.mjs`:

1. Собрать `internal-rpc-authority` из проверенного immutable source штатным
   `build-stable-local.sh`, опубликовать через `stable-image-publication.mjs`.
   Новый image используется только Job миграции; image publisher не меняется.
2. Выполнить `plan --kubeconfig <absolute-path> --context <context>
   --projection-plan <private-plan> --projection-evidence <private-journal>
   --publication-plan <private-plan> --publication-evidence <private-journal>
   --plan <new-private-plan>`. Команда проверяет canonical stopped046 через
   прежний frozen executor, неизменные maintenance/isolation/107 plans,
   exact registry/publisher/cluster и PostgreSQL instance, предыдущие девять SQL,
   published image provenance, полный preimage функции и owner ledger.
3. Выполнить `apply --kubeconfig <absolute-path> --context <context>
   --plan <private-plan> --evidence <new-private-journal>
   --confirm APPLY-AUTHORITY-DISTRIBUTE-STATUS`. Единственный effect — создание
   Job `authority-status-20261115000100` с CLI `up`, `backoffLimit=0`, deadline300s.
   Durable intent записывается до create. Job не удаляется автоматически.
4. `observe` с теми же kubeconfig/context/plan/evidence не создаёт ресурсов.
   Потерянный ответ разрешается только exact Job UID/spec readback; отсутствие
   Job не разрешает повторный create. PASS требует exact succeeded Pod и
   init/app imageID, exit0 без restart, положительный join и поздний owner SQL
   с неизменными OID/ACL/атрибутами, history и ledger. Изменяются только definition
   функции и одна Goose row. Ошибка оставляет стенд в обслуживании.
5. После PASS создать новый107 status Job и продолжить прежние
   projection/resume/activate/restore/backup journals их frozen executor.
   Прежние plans и строки status Jobs не переписываются. Повторный запуск046,
   новая rotation107 и Goose Down не входят в этот путь.

`make test-authority-distribute-status-migration` проверяет создание, потерю
ACK, продолжение после перезапуска оператора, запрет повторного create,
подмену Job/Pod/image, незавершённый writer и несовпадение postcondition.
`make test-authority-distribute-status-native` отдельно доказывает настоящую
PostgreSQL миграцию и SQL→Go→защищённый107 consumer. Локальные проверки не
объявляются живой приёмкой или успешным CI.


## Запуск обязательных readers без цикла readiness

Для owner107 `DISTRIBUTE/DELIVERED` активация authority-снимка ожидает
`PROMOTED`, а promotion требует readback всех обязательных получателей.
Поэтому последовательность «дождаться Ready CP → запустить API → запустить
interaction» неработоспособна. Total readback count также может включать
необязательные image jobs и не доказывает полноту required membership.

После положительного schema046 join и immutable projection107 выполнить:

1. Штатные `component-api-maintenance.mjs plan/apply --action open` сохраняют
   изолированный Service и фиксируют собственный запуск API. `CONVERGING`
   означает только начатый запуск, не `OPEN` и не готовность.
2. `control-plane-migration.mjs start-readers` получает те же original migration,
   projection, authority/status, API-open и при необходимости port-repair pins,
   что `activate`, включая `--confirm APPLY-STAGING-CP-MIGRATION`. В backup-профиле
   сохраняется исходная остановка backup-controller. Полный read-only drain,
   exact Job/Pod, source/schema/image/spec/UID и свежий owner DELIVER обязательны.
3. Фаза запускает оставшиеся exact компоненты cohort до ожидания Ready. Каждый
   необходимый CAS получает отдельное свежее чтение owner после rejoin прежних
   readers, durable original intent, проверку TTL и повторное чтение изоляции.
   Новый proof сохраняет operation/registry/snapshot; TTL не увеличивается.
   Уже начатый CP с собственным intent не запускается повторно. Возвращается
   `READERS_STARTED_PENDING_READINESS`, `pairReady=false`; ACK готовности нет.
4. После реальных readback/promotion штатный API `status` записывает `OPEN`.
   Прежние `resume` и `activate` подтверждают full Pod/CRI readiness. Service
   возвращается только после полного owner107 RETIRE и прежних final guards.

`start-readers` не является режимом read-only для приложения: до promotion
startup barrier не допускает workers; после promotion действуют штатные
owner claims, Begin и approvals. Предварительный drain включает ожидающие,
назначенные и начатые interaction effects. При drift, активном эффекте либо
потере подтверждения продолжение закрывается; старые журналы не переписываются.
Повтор start подтверждает тот же spec/intent, не создаёт новую ротацию или DDL.

`make test-telegram-reader-bootstrap` проверяет настоящий CLI normal contracts
в backup/frozen/port-repair fixture с принудительным ожиданием обязательных
readers. Kubernetes/PostgreSQL transport в этой suite синтетический; это не
кластерная приёмка. `make test-telegram-authority-delivery` отдельно проверяет
pending/ready, потерю ответа, чужой UID/spec, TTL, operation и изоляцию Service.


## Восстановление пропущенного режима телеметрии

Creator target/v2 содержит явный `runtimeObservability` и hash публичной runtime
проекции publisher. Перед созданием Deployment применяется штатный
`tools/install/observability_profile.py`. Три long-running контейнера получают
`OTEL_SDK_DISABLED=true` в отключённом профиле; one-shot socket init не меняется.
Прежний target не переинтерпретируется новым creator.

Для уже доставленного receiver без этого env доступны отдельные фазы существующего
`control-plane-migration.mjs`: `plan-observability-repair` и
`apply-observability-repair`. Они используют original migration plan/evidence,
original107/status receipts, CP projection, API-open и ранее выбранный port repair
executor. `--telegram-observability-repair-plan` обозначает новый private output
при планировании и immutable input при apply. Apply требует обычного
`--confirm APPLY-STAGING-CP-MIGRATION`.

План допускает только отсутствующий env трёх контейнеров и подтверждённый
DISABLED у исходного publisher. UID/image publisher выводятся из hash-bound
original creator metadata. Original migration, creator, authority operation,
DDL и images не переписываются. Протокол: exact receiver3 → stop0 → положительный
Pod join → canonical env-only transform0 → resume3. API остаётся изолированным,
backup остановлен. `READERS_STARTED_PENDING_READINESS` не означает готовности
или завершения107. Потерянный ответ PATCH сверяется чтением; повреждённый успешный
ACK блокирует продолжение и требует отдельного расследования.

После первого repair intent этот pin обязателен для inspect/resume/activate,
start-readers и финального backup resume. Отсутствующий pin, незавершённая repair,
сменившийся UID/spec/publisher, активный owner effect и открытый API приводят к
отказу. Затем штатно проверяются full cohort readiness, terminal107, восстановление
API и backup. Повторный apply завершённой repair не создаёт PATCH.

`make test-telegram-observability-repair` проверяет immutable plan, canonical
renderer и lifecycle/ACK/negative paths. `make test-telegram-reader-bootstrap`
проверяет отдельные реальные CLI-процессы с synthetic Kubernetes/PostgreSQL
transport, включая counterfactual пропущенный renderer, repair и final backup.
Это локальная квалификация, а не live acceptance.

## Отдельная ревизия egress policy и позднее восстановление

Base egress policy и Telegram policy имеют самостоятельные revision pins.
Новый egress image читает `EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION`; он не
выводит эту ревизию из base policy. Для уже начатой установки046 используется
адресный `plan-egress-revision-repair` / `apply-egress-revision-repair`,
сохраняющий исходные DDL, authority107, creator и journal prefix.

Candidate descriptor связывает clean Git SHA, полный archive hash, receipt
канонической публикации и exact digest. CLI
`tools/release/telegram-egress-revision-candidate.mjs` принимает `plan`, затем
`qualify` с `--approved-plan-sha` либо `readback` без повторного pull. План
связывает cluster/namespace UID, UID узлов и соответствующие k3d containers.
Readback CRI проверяет exact repo digest каждого узла. Descriptor без node proof
не допускается к восстановлению; это не доказательство live E2E.

| Переход | Допуск | Результат и повтор |
| --- | --- | --- |
| План | Исходный046 PASS, immutable source/publication/node proof, API isolated и107 RETIRED | Новый отдельный plan; исходные bytes сохраняются |
| Stop | Fresh terminal owner proof, schema/drain, UID/RV/spec CAS | Replicas0 и положительный join всех прежних Pods |
| Render | Тот же terminal proof и stopped join | Только image + один закрытый Telegram revision env; replicas0 |
| Resume | Fresh terminal proof, exact staged spec, прежний replica count | Только исходное число replicas; readback effective spec |
| Повтор / потерянный ACK | Тот же repair pin, prefix и live CAS state | Readback исхода; прежний effect не повторяется |
| Повреждённый успешный ACK | Durable rejection marker | Последующие effects закрыты |
| Final / backup resume | Тот же repair pin во всех consumers, readiness, API restoration | Исходный backup lifecycle, без повторного DDL/rotation |

Обязательная локальная точка входа: `make test-telegram-egress-revision-repair`
(300s). Полный CLI-порядок после RETIRED проверяет
`make test-telegram-reader-bootstrap` (1800s); его Kubernetes/registry backend
синтетический. Реальные build/publication/node qualification, readiness и
сотрудник → Telegram подтверждаются отдельно на разрешённом target.
