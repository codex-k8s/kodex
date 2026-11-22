---
id: ARCH-SSH-TARGET-2156
title: Общая SSH-интеграция с broker-owned identity
type: architecture
status: approved
owner: architect
version: 1.0.42
updated: 2026-11-21
---

# Граница реализации

Issue #2156, эпик #1830. Документ фиксирует согласованный контракт полного
unit; наличие документа, отдельных библиотек и native fixtures не означает
готовность owner setup, схемы048, policy109 или выбранного живого host.
Существующая SSH Compose-интеграция #1838 сохраняет самостоятельный протокол.
Её credential projection не становится общей выдачей private key сотруднику.

Владелец выбирает exact literal endpoint/port, user и полный host public key.
Broker держит private key и проверяет pin до authentication. Адрес не берётся
из operation input. Endpoint исключает loopback, link-local, mapped IPv6,
zone, hostname resolution и неоднозначные написания. Private routed address
требует выбранного оператором exact egress. Само имя user, отличное от root,
не доказывает непривилегированность: фактические UID/GID/groups и privilege
inventory входят в отдельную operator qualification.

# Режимы и полномочия

| Режим/операция | Scope и gate | Исполняемая граница |
| --- | --- | --- |
| Managed inspect/system/process | exact target/user/profile, `READ/NONE` | Закрытый helper recipe; без shell/argv/env от сотрудника |
| Managed file list/stat/read | выбранный root alias и bounded relative path, `READ/NONE` | Kernel `openat2`, private regular single-link file |
| Managed file create/replace | exact recipe/arguments digest, content version, expiry/effect limit, `HUMAN_SCOPED` | Durable UNKNOWN intent перед effect; CAS и fsync |
| Managed file delete | exact file version, `HUMAN_EACH_EFFECT` | Тот же durable intent и отдельный receipt |
| Managed service read/journal | выбранный root-managed system unit, `READ/NONE` | Digest-pinned fixed tools и bounded typed projection |
| Managed service restart/reload | exact выбранный unit, `HUMAN_EACH_EFFECT` | Отдельная делегированная привилегия; service UID отличается от helper UID |
| Arbitrary execute | exact target/user/command/input digest, `SENSITIVE/HUMAN_EACH_EFFECT` | Отдельная execute identity; строка не классифицируется как READ |
| Recovery | exact original attempt/operation/input digest | Только чтение receipt; исходная команда не повторяется |

Managed identity предназначена только для root-controlled forced command.
Её root-owned account shell принимает только fixed-helper argv и проверяет
собственный/целевой executable digest. Это закрывает ordinary command/shell
route, но не заменяет полный operator readback: OpenSSH internal-sftp может
обойти shell, а user rc выполняется раньше него. Эти пути и alternate keys
должны быть исключены root-controlled account/sshd inventory.
Другой shell, сервис либо writer под тем же UID мог бы подделать файл или
receipt, поэтому такой deployment не квалифицируется. Для произвольного
execute требуется отдельный явно выбранный режим/connection. Закрытый helper
не заменяет всю интеграцию универсальной shell-командой и не ослабляет
`HUMAN_SCOPED` до молчаливого обязательного EACH для всех managed изменений.

# Карта от инициатора до результата

Общий owner credential selector и metadata-only variant принадлежат047.
SSH048 использует тот же selector и собственный purpose-specific setup и
private material lane; второй selector либо generic TOKEN fallback запрещён.
Точные RPC/API объявления добавляются вместе с consumers полного unit.

| Сценарий | Инициатор и authority | Owner, версия и результат |
| --- | --- | --- |
| Создать/изменить выбранный target | OIDC/CSRF, `integration.manage`, штатный owner connection API | CP назначает org/connection/target refs; OCC/idempotency и pinned definition; UI читает безопасную metadata |
| Личная identity | Текущий authenticated subject и `PERSONAL_REQUIRED`; key передаётся один раз через защищённый purpose-specific setup | CP создаёт immutable setup intent с binding generation; broker claim выводит subject/purpose/target из owner state; браузер не получает key readback |
| Shared identity | Отдельный явный `PROJECT_SHARED`, service identity и owner permission | Тот же purpose-specific setup; личный key не повышается до shared автоматически |
| Materialize | mTLS + bearer + exact registered method, setup ref/claim fence/generation/expiry | Broker пишет отдельный immutable SSH Secret; owner Complete проверяет exact tuple и в одной Tx присоединяет common BI metadata revision |
| Проверить identity | Fresh owner verification generation и текущий operator artifact | Broker читает exact private material, проверяет оба public keys и non-root profile; managed Inspect использует тот же transport, что рабочая операция |
| Invoke | Runtime immutable revision и серверное root lineage; поля payload не назначают actor/org/connection | Existing CP invocation owner разрешает current grant/credential source/gate и immutable input; gateway получает только broker metadata ref/revision |
| Broker dispatch | Owner exact invocation/attempt/fence/generation/input digest и fresh admission | Broker повторно разрешает purpose/target/material и operator proof; private key остаётся внутри broker |
| Complete | Exact broker outcome и тот же immutable claim | Existing invocation owner атомарно фиксирует terminal/receipt и закрывает grant; штатный protected read предоставляет безопасную проекцию |

В политике полномочий109 маршруты SSH разделены по вызывающей нагрузке.
Control API gateway передаёт операции подготовки, чтения и отмены setup
с текущей OIDC-сессией. Secret broker использует общий с генератором
реестр из восьми методов владельца для загрузки, проверки и исполнения.
Integration gateway вызывает только `ProbeSSHIdentity`, `ExecuteSSHIdentity`
и `ReconcileSSHIdentity` через профиль `secret-broker.managed-ssh`.
Чтение результата исходного исполнения принадлежит runtime controller.
Дайджест полного Proto-запроса закрепляет вложенные OCC, ключ идемпотентности
и исходный claim. Полномочия на конкретный ресурс проверяет владелец по
текущему состоянию. Проверка генератора подтверждает регистрацию этих
маршрутов и сохраняет отдельную политику SSH Compose.

Результат helper и любой remote stdout являются недоверенным содержимым.
Вывод ограничен32KiB, private key echo отклоняется, request/command/key не
добавляются в runtime diagnostics. Наблюдённый exit status означает только
завершение SSH session, а не успех произвольного бизнес-эффекта или join всех
remote descendants.

# Private material и operator qualification

Common `integration_broker_identity_revisions` содержит только immutable
ref/target/revision/purpose/owner metadata. Отдельная SSH material revision
закрепляет namespace/name/UID/resourceVersion, private content digest и
authentication public-key digest. Fixed purpose — `SSH_TARGET`, единственный
data key — `private_key`, значение не более16KiB. Secret immutable, Opaque,
с одним data entry, точной owner revision и target metadata. Kubeconfig,
generic runtime Secret и другой namespace не принимаются. Ни descriptor,
ни content hash, ни private key не попадают в gateway/public credential view.
Material key закрепляет immutable BI ref и revision; mutable lease/fence не
входят в Secret. Create отправляет одну SDK application attempt с отключённым
retry; `429/5xx`, AlreadyExists и потерянный ACK остаются UNKNOWN без implicit
GET. Повреждённый завершённый успешный ответ имеет отдельный
`ACK_REJECTED`: adapter возвращает bounded SHA ответа и доступный однозначный
UID без usable locator, key bytes либо SDK diagnostics. Owner обязан сохранить
этот receipt как terminal до нового admission; последующие Create и Observe
исходной попытки запрещены, даже если API позже возвращает исправленный объект.
Оборванный transport/read остаётся UNKNOWN, а не повреждённым завершённым ACK.
Body успешного ответа ограничен64KiB; превышение даёт terminal rejected receipt
с hash ограниченного префикса и явным `Truncated`. Private requests используют SDK-built fixed CoreV1 URLs и отдельную копию
trusted HTTP TLS/auth client; SDK `Do/Stream` не вызываются, поскольку их error
path может логировать отражённое тело Secret при verbose logging. HTTP body
имеет единственного владельца: synchronized Read/Close очищает buffer без
гонки с asynchronous transport cleanup, `GetBody=nil` запрещает replay closure.
Rate-limit wait и запрос разделяют bounded5s context; redirects/Jar/retries
не получают обхода. Successful response разбирается отдельно в строгом official
Kubernetes decoder, остальные тела ограничены и очищаются без SDK formatting.
Этот boundary проверен через Context7 `/kubernetes/client-go` (Result/Stream)
и exact source `client-go@v0.36.3`; Stream сознательно исключён из private path.
Через Context7 `/websites/pkg_go_dev_net_http` сверены
[Client.Do](https://pkg.go.dev/net/http#Client.Do),
[RoundTripper](https://pkg.go.dev/net/http#RoundTripper) и
[NewRequestWithContext](https://pkg.go.dev/net/http#NewRequestWithContext):
асинхронное закрытие request body и автоматический GetBody стандартных readers
не считаются гарантией secret lifetime либо one-attempt поведения.
Durable owner048 consumer ещё не подключён; отдельный error type не объявляет
setup lifecycle готовым.
Отдельный Observe разрешён owner только до первого binding commit и не
даёт права создать заново либо усыновить replacement UID после binding.
API create/delete defaults client-go не считаются one-attempt контрактом.
Purpose-local TLS client дополнительно запрещает redirects для GET/POST/DELETE;
он не копирует atomic SDK state и не меняет generic credential transport.
Kubernetes UID/resourceVersion сохраняются opaque и сравниваются byte-exact;
из них не выводятся UUID, целочисленное поколение либо latest semantics.
Этот контракт сверён через Context7 с Kubernetes ObjectMeta и непосредственно
с комментариями pinned `k8s.io/apimachinery@v0.36.3`.

Operator artifact `kodex-ssh-qualifications/v2` доставляется отдельно от
product configuration в фиксированный read-only broker mount с полным
expected SHA. Broker перечитывает его перед каждым dispatch и проверяет
target ref, endpoint/user, оба public-key wire digests, наблюдённые UID/groups,
mode и срок не более24h. MANAGED закрепляет полный helper profile и требует
root-controlled deployment/exclusive-writer evidence. EXECUTE не принимает
helper profile. Hashes обозначают provenance доверенной operator capture;
самостоятельный checkbox, JSON либо helper self-report не являются capture.
EXECUTE использует фиксированные numeric `/usr/bin/id` probes real/effective
UID/GID и groups в том же authenticated connection до business command;
root/changed identity, stderr, nonzero/missing exit либо oversized response
закрыто запрещают effect. Group set включает primary group. Operator artifact
повторно читается после channel-opening I/O непосредственно перед Start,
поэтому expiry/withdrawal во время handshake не продолжают старый admission.
Input bytes фиксируются до network preflight и не меняются при reuse buffer.

Перед созданием SSH invocation owner `ValidateInput` и private broker используют
единый `managedssh.DecodeOperationInput`. Принимается canonical JSON object,
только exact scalar types/keys и конечные arguments выбранного recipe.
Command ограничен8192 байтами; decoded stdin/file content —16384 байтами,
независимо от длины base64. Null content, duplicate/aliases, exponent/fraction,
неcanonical base64 и caller helper pins закрыто отклоняются до semantic use.
Произвольный command остаётся EACH. Для managed request actual UUID и profile
digest добавляет trusted owner/broker; receipt input несёт только original
invocation reference, а исходные operation/UUID/digest разрешает owner.
Наличие decoder и local tests не означает готовность execution graph048/109.

Private transport принимает exact public host key либо SHA256 от его SSH
wire representation с exact raw-key algorithm как закрытый oneof: смешение
этих форм запрещено. Новый artifact v2 требует algorithm; v1 не угадывается и
не повышается автоматически. Это исключает ложный отказ при server, который
предлагает одновременно несколько host keys: RSA pin согласует только RSA SHA2.
В runtime endpoint/account и digest выбираются лишь из доверенного operator
artifact для server-selected `target_ref`, режима exact package и actual
broker-held authentication key. Отдельный caller endpoint registry не нужен;
peer key никогда не становится pin по TOFU. Digest не отменяет закрытый набор
modern raw algorithms, RSA3072+ и запрет SHA1/certificates. Mismatch закрывает
handshake до user authentication. Полный artifact повторно читается перед
Start, поэтому первоначальная материализация target не сохраняет старую
qualification после отзыва/expiry. Через Context7 проверены `x/crypto/ssh`
`HostKeyCallback` и `HostKeyAlgorithms`; pinned host-key mismatch и отсутствие
user auth дополнительно проверяются настоящим synthetic SSH server.

Operator producer, deployment, revocation и readiness должны проходить общий
проверенный lifecycle. До их подключения отсутствующая qualification закрыто
оставляет capability недоступной; формат DTO не является подтверждением
выполненной установки. Изменение account/sshd/helper/namespace/service/key
требует отзыва старой qualification и drain до установки нового профиля.

# Broker workflow setup

Broker передаёт private key только своему parser и purpose store. Resolve
получает hash одноразового32-byte permit, но не сам permit либо key. PREPARED
snapshot содержит owner coordinates и content hash/bytes; authentication digest
ещё не существует. Broker выводит его из actual supported key, затем Claim
атомарно закрепляет это значение вместе с immutable snapshot и выдаёт одну
WRITE attempt с fence/generation. Identity revision, namespace, target, subject
и credential policy не выбираются upload request. До Claim и при malformed
либо lost Claim response private store не вызывается.

Owner принимает completion как oneof known exact pins, explicit UNKNOWN либо
terminal ACK_REJECTED. Broker выполняет одну completion delivery с независимым
bounded context после local I/O; потерянный completion не повторяет CREATE.
Истёкшая lease без completion остаётся UNREPORTED/закрытой: она не доказывает
потерянный transport ACK и не выдаёт OBSERVE. Только durable explicit UNKNOWN
исходной WRITE открывает bounded observation generations того же intent.
После ACK_REJECTED commit либо known attachment новый broker handler также не
получает observation credit. Late material evidence после cancel/expiry может
пополнить orphan history, но не восстановить grant. Final attach обязан в той
же owner Tx проверить DB-derived authorizedUntil после закрытия зависимостей.

`make test-ssh-identity-setup` проверяет настоящий Ed25519 parser → domain
workflow → client-go-built fixed HTTP request → локальный TLS1.3 API → exact
private material reader. Проверяются сетевой lost ACK, повреждённый successful
ACK с известным UID, потеря completion до owner commit, новый handler без
прежней памяти, отказ второго CREATE, verbose error-body echo и смена fenced
admission. State model owner в этой оснастке синтетический: это не PostgreSQL
owner048, не issuer109 и не готовый public setup/API. Их подключение остаётся
обязательной частью полного unit. Оснастка не обращается к кластеру.

# Отдельный upload listener broker

Optional composition включается только полным набором env:
`SECRET_BROKER_SSH_IDENTITY_NAMESPACE`, `SECRET_BROKER_SSH_UPLOAD_LISTEN`,
`SECRET_BROKER_SSH_UPLOAD_ORIGIN`, `SECRET_BROKER_SSH_UPLOAD_BROWSER_ORIGIN`,
`SECRET_BROKER_SSH_UPLOAD_CERTIFICATE_FILE`, `SECRET_BROKER_SSH_UPLOAD_PRIVATE_KEY_FILE`.
Namespace фиксирован как `kodex-ssh-identities`, listen — `:8446`;
origin и browser origin задаются явно как exact HTTPS origins. Отсутствие всех
полей выключает listener, partial configuration закрыто отклоняется.
Ключ загружаемой SSH identity остаётся в отдельном purpose handler, а не в
config broker или owner RPC. TLS certificate/key listener являются отдельным
deployment input; изменение сертификата требует явного rollout. Readiness
проверяет lifetime всей загруженной цепочки и DNS leaf относительно public origin.

До readiness composition выполняет защищённый owner Resolve с заведомо
неподходящим permit digest: только авторитетный `NotFound` подтверждает
доступность exact метода и owner SQL. Это не проверка полномочия CREATE либо
SSH authentication. Второй read-only probe тем же private API transport
проверяет Active namespace и Secret GET: probe object не создаётся.
RBAC полного installer должен давать namespace GET только для fixed name и
purpose Secret operations только в этом namespace. Пока installer/109 не
подключены, app composition не означает готовность deployment.

HTTP обслуживает TLS1.3/HTTP1.1 с конечными read/write/header budgets;
отдельный `httpserver.RequestGate` ограничивает одновременно работающие uploads
восемью. Это локальная concurrency, а не бюджет агентов или business effects.
Worker context принадлежит composition и отменяет request contexts. Shutdown
сначала закрывает admission, затем отдельно ожидает actual handler join.
`Server.Shutdown` timeout либо возврат Serve не доказывают join. После timeout
broker возвращает ошибку процесса и не закрывает owner transport, которым ещё
может пользоваться handler; поздний join не объявляется успешной quiescence
задним числом. Синхронные handlers не предоставляют hijack/stream route.

gRPC composition применяет `RequestLifecycle` к полной unary interceptor/handler
цепочке с локальным пределом128 admitted handlers. При отмене worker сначала
закрывает admission и отменяет linked contexts; затем existing
`grpcserver.GracefulStop` выполняет ограниченный graceful/forced shutdown внутри
worker, до `WorkerGroup.Wait`. Повторный shutdown callback лишь наблюдает join,
не запускает новый unbounded GracefulStop. Каждый из graceful/force budgets
равен configured shutdown timeout; force deadline отсчитывается отдельно после
начала graceful budget. Returned Stop и Serve не заменяют handler release.
Неприсоединившийся RPC оставляет owner/provider/verifier/telemetry dependencies
открытыми и завершает процесс с ошибкой. Это local join, не доказательство
remote SSH quiescence. Через Context7 проверены gRPC-Go GracefulStop/Stop и
отсутствие ожидания handlers у обычного Stop.


`make test-ssh-upload-app` воспроизводит actual loopback TLS, namespace/probe
readiness, cancellation/join, оба порядка deferred cleanup и отказ exact
production entrypoint с exit1 при synthetic app error. Последний тест
подменяет только app port во временном модуле и не выдаётся за полный startup
broker либо issuer109. Через Context7 проверена документация Go `net/http`
`Server.Shutdown`: возврат Serve и исчерпание shutdown context не заменяют
ожидание активных handlers; hijacked connections вообще не входят в его join.

# Владение private namespace при установке

Оба web-профиля включают отдельный `kodex-ssh-identities` с Kodex ownership
marker и restricted Pod Security labels. В нём нет штатных workloads и
placeholder Secrets. Deny-all NetworkPolicy закрывает любой случайно
созданный Pod. Единственный RoleBinding предоставляет ServiceAccount
`secret-broker` из `kodex-system` только Secret get/create/delete; нет
list/watch/patch/update или выдачи access другим workloads. Namespace readiness
имеет отдельный ClusterRole с единственным GET exact namespace name.

Bootstrap namespace использует общий ownership/UID readback и не усыновляет
заменённый namespace. Metadata-only inventory знает эту область, но имя само
по себе не доказывает владение. Cleanup неизвестного namespace либо объекта
требует рассмотрения ownership; `--preserve-namespace kodex-ssh-identities`
сохраняет всю область. Штатный name-based `dev.sh down` не удаляет её:
удаление private keys требует отдельного exact inventory/UID-bound решения.
Owner revoke не выдаёт автоматического разрешения удаления Secret: retention
и orphan cleanup обязаны получить свежий grant владельца и UID/RV preconditions.
Пока этот owner cleanup consumer не подключён, неопределённый material
сохраняется и не объявляется отсутствующим.

`make test-ssh-identity-render` проверяет итоговые оба web renders, узкие RBAC,
отсутствие key mounts/workloads в purpose namespace, namespace UID replacement
и сохранение неизвестных объектов в cleanup plan. Это локальная проверка без
Kubernetes API; включение listener/TLS runtime profile и authority109 остаются
следующими обязательными consumers полного unit.

# Lifecycle и неопределённые исходы

| Переход | Авторитетный результат и восстановление |
| --- | --- |
| Setup intent/claim | Owner Tx закрепляет subject/policy/target/package/attempt и generation; key существует только в bounded broker upload и purpose Secret |
| Lost material-create ACK | Только после durable explicit Complete(UNKNOWN) исходной WRITE; missing completion/lease expiry не выдают Observe. Exact deterministic object readback по immutable owner intent; имя не разрешает принять другой UID/content либо generic Secret |
| Completed invalid material-create ACK | Durable terminal `ACK_REJECTED` с bounded witness; no Create/Observe, доступный ACK UID сохраняется до семантической проверки; новый объект не усыновляется |
| Setup complete | Common BI metadata и SSH private pins присоединяются одной owner Tx; final UPDATE проверяет DB-derived authorizedUntil после dependency cancellation; late completion после revoke/rotation не меняет current binding |
| Setup cancel/expiry | Owner закрывает claim/grant; orphan cleanup получает отдельный exact owner grant и delete preconditions; отсутствие ACK не объявляется отсутствием material |
| Dispatch не начат | Definite rejection, без remote effect; безопасная диагностика без key/command/SDK details |
| Start отправлен, ACK/output потерян | `UNKNOWN`, даже если SSH Start вернул ошибку; автоматического resend нет |
| Managed intent записан, effect/receipt interrupted | Durable UNKNOWN остаётся в private journal; recovery только читает исходную attempt |
| Managed APPLIED receipt | Exact operation/input/profile/attempt совпадают с immutable owner receipt; иной результат не усыновляется |
| Cancel/lease expiry/revoke во время session | Broker закрывает local channel и join своих goroutines; это не remote join; owner сохраняет ambiguity и отзывает дальнейший доступ |
| Credential rotation | Новый immutable BI revision и binding generation; старые verification/attempt admission не получают новую identity |
| Повтор после UNKNOWN | Только новое отдельное owner решение; recovery старой attempt не исполняет исходный effect |

Setup grant закрывается в той же owner Tx при следующих конкретных командах:

| Источник перехода | Закрытие и authoritative read |
| --- | --- |
| Self-revoke personal binding | Текущий actor/OCC, exact binding и purpose SSH; pending setup CANCELLED и ACTIVE attempt CLOSED до commit binding REVOKED. Прежний audit/receipt revoke; отдельное setup event не создаётся, состояние читается GetSSHIdentitySetup |
| Connection disable | Авторизованный exact Connection/OCC закрывает все его pending SSH setups и attempts; прежнее Connection event, отдельного setup event нет |
| Public configuration update | После pinned schema/OCC и active-effect guards, до смены config/version закрываются pending SSH setups/attempts; прежний Connection event, authoritative setup read |
| Credential policy change | Только штатный disabled/drained policy command; при фактической смене режима закрываются прежние SSH setups/attempts без преобразования personal identity в shared; прежний Connection event |
| Connection delete | Только штатный disabled/idle/OCC soft-delete; setups/attempts закрываются перед lifecycle transition. Прежний DELETED event; публичный hidden read не доказывает отсутствие material |

Поздний exact Known/UNKNOWN/ACK_REJECTED receipt после этих переходов может
пополнить private ledger под прежним fence, но не вернуть attachment либо
grant. Material и первоначальный input snapshot не удаляются. Native
`test-ssh-setup-revoke-development` проверяет эти переходы на disposable
047 development overlay; это не canonical upgrade048 и не signed109 proof.

Схема и owner transitions определяют audit/event cardinality через существующий
invocation/setup owner. Нельзя добавлять отдельный worker retry engine либо
объявлять runtime event из helper самостоятельным авторитетным terminal.

# Проверки и доступность

`make test-managed-ssh` — bounded local protocol/helper/isolated OpenSSH
контур. Installed helper fixture проверяет root-owned binary/config,
non-root identity, kernel traversal, CAS, receipt recovery и deadline;
networkless tmpfs доказывает сохранность между процессами, а не host reboot.
Real SSH fixture проверяет pinned key до auth и потерю ACK без повторения.

Эти проверки не заменяют owner PostgreSQL lifecycle, purpose setup/API/UI,
grant/consent consumer, deploy render, authority109 и полную квалификацию
финального SHA. Выбранные живые SSH host/credentials отсутствуют, live NOT RUN.


# Прямой upload private key

Purpose HTTPS handler broker принимает только
`POST /v1/ssh-identities/{setupRef}/material` с raw bounded key и
`POST /v1/ssh-identities/{setupRef}/observe` без тела. Permit передаётся только
в `Authorization`, не в URL, query либо generic TOKEN API. Exact Host/SNI,
TLS1.3, configured browser Origin, отсутствие Cookie и закрытый CORS preflight
проверяются до owner RPC. CLI без Origin использует тот же permit и owner
admission. Body ограничен16384bytes и read deadline10s; buffer очищается после
joined workflow. Новый transport не добавляет retry либо credential selector.

Ответ upload классифицирует только принятый receipt: `EVIDENCE_RECORDED`,
`STORAGE_UNKNOWN` либо `ACK_REJECTED`. Он не утверждает текущее состояние setup,
attachment или успешную authentication: late completion может сохранить orphan
после cancel. UI обязан заново прочитать setup у CP. Отсутствующий owner receipt
означает `UNAVAILABLE`, а не право на новый WRITE либо Observe.

В `make test-ssh-identity-setup` добавлен настоящий local TLS1.3 path через HTTP,
production parser и domain workflow. Owner/store ports там синтетические;
отдельные K8s TLS fixtures предыдущего раздела сохраняются. Проверяются CORS,
wrong CA/SNI, IP endpoint без DNS SNI, oversized/chunked body, key/diagnostic
redaction и отсутствие второго material write. Это пока не app/listener wiring,
public ingress, owner PostgreSQL048 или policy109: они остаются обязательными
частями полной вертикали, не отдельным готовым профилем.

Проверены Context7 `/golang/go/go1.26.7` и фактические комментарии/реализация
`net/http` pinned Go1.26.10: MaxBytesReader, ResponseController.SetReadDeadline,
server TLS/ReadTimeout и независимый Shutdown context.


# PostgreSQL setup owner048

Purpose repository реализует Begin/Get/Cancel/Resolve/Claim/Complete через один
существующий CP owner port. Begin разрешает current USER и Connection/package,
затем тот же common BI allocator выбирает личный binding либо явно выбранный
active SERVICE. Один immutable snapshot связывает org/actor/connection/subject,
binding version/generation, definition/configuration, target, namespace, content
и deadline90s. Raw permit выдаётся лишь после первого commit; replay возвращает
safe metadata без нового permit. Личный binding не получает shared subject.

Claim удерживает Connection → binding → setup, перечитывает canonical snapshot
и тратит один WRITE навсегда; следующая generation ограничена восемью и30s,
не продлевает setup. Expired attempt закрывается без создания UNKNOWN receipt.
Observe требует original WRITE UNKNOWN, записанный owner до истечения её grant.
Любой exact replay completion возвращает metadata до переходов current state:
он не закрывает новую observation attempt и не обновляет deadline.

Complete сохраняет immutable evidence под исходным fence даже после cancel,
revoke либо expiry. First known UID/resourceVersion не заменяется. Attachment
идёт в savepoint после evidence: exact material fact и completed attempt дают
DB-derived authorizedUntil, а общий final UPDATE проверяет его после закрытия
зависимостей. Отказ attachment откатывает его cancellation и сохраняет orphan
receipt без нового допуска. Повреждённый successful ACK навсегда запрещает
Create/Observe; противоречие уже известному material quarantines исходную BI
revision. Новая replacement revision от такого позднего receipt не отзывается.

| Переход | Audit/event и авторитетное чтение |
| --- | --- |
| Первый Begin | Один audit; события нет, public Get по setup либо Begin key |
| Begin replay | Новый audit/event отсутствует; current eligibility и exact intent |
| Claim/lease expiry | События нет; durable attempts и public Get; expiry не подтверждает внешний исход |
| Новый Complete receipt | Один audit в той же Tx; события нет, private fenced Complete/public Get |
| Exact Complete replay | Audit/event и повторный переход отсутствуют |
| Cancel | Один audit и historical idempotency receipt; события нет, current eligibility перед replay |
| Replacement/late quarantine | События нет; immutable purpose receipt и актуальный credential owner read |

Этот checkpoint не объявляет схему048, policy109, runtime invocation или
положительный PostgreSQL lifecycle готовыми. Для них обязательны отдельные
native проверки и общий frozen047 predecessor. Unit-тесты проверяют outcome
union, различие reported UNKNOWN и missing completion, snapshot corruption и
независимые owner coordinates. Проверены Context7 PostgreSQL18: execution order
модифицирующих CTE и RETURNING dependency, row locks/savepoint rollback.


# Public metadata API

| Endpoint | Gateway → protected RPC | OCC/idempotency и ответ |
| --- | --- | --- |
| POST Connection/ssh-identity-setups | BeginSSHIdentitySetup | Explicit binding ETag0 для первого personal binding либо Connection ETag для shared; отдельный expectedConnectionVersion; exact content digest/bytes; первый permit либо reconcile-only replay |
| GET Connection/ssh-identity-setups?beginIdempotencyKey | GetSSHIdentitySetup с Begin-key oneof | Только safe текущий read; raw permit не восстанавливается |
| GET Connection/ssh-identity-setups/setupRef | GetSSHIdentitySetup с setup-ref oneof | Current actor/tenant/owner eligibility; no material locator |
| POST Connection/ssh-identity-setups/setupRef/cancel | CancelSSHIdentitySetup | Positive setup ETag + idempotency; empty typed body; closed safe metadata |

Все пути начинаются с `/api/v1/integration-connections/{connectionRef}`.
Actor берётся из проверенного OIDC/CSRF → protected CP context. Gateway не
принимает subject, target, policy, namespace, Secret locator либо key bytes.
Metadata body ограничен4096bytes, required null/duplicate/case aliases и extra
fields отклоняются до RPC. Zero ETag поддержан только purpose Begin и не
ослабляет общий positive-version parser. Safe response конструируется явно;
чужие refs, неизвестные enums, mixed shared/personal versions и unsafe JSON
integers дают502 без private upstream message.

Первый Begin возвращает только response-only URL-safe32-byte permit в отдельном
`upload` envelope; повторный ответ строго не содержит upload origin/permit.
Ответы no-store. Это не подтверждение native109 authorization либо готовой
browser setup flow. `make test-ssh-setup-api-unit` использует actual generated
HTTP router и typed RPC client с recorder, не имитируя OIDC/PG owner.


# Отказы owner dependency

Definitive Forbidden/NotFound/Conflict/VersionMismatch может закрыть утраченный
owner grant с сохранением material evidence. Unavailable, query/scan failure,
corrupt state, cancellation и savepoint failure не означают потерю полномочий:
вся Tx откатывается без изменения state/version и без нового receipt credit.
Неудачный Complete не разрешает новый material Create либо Observe. Отдельный
known-material orphan закрывает setup с явным MATERIAL_RECORDED_NO_ATTACHMENT,
не выдавая принятый receipt за отсутствующий completion.

Это следует GO-DOC-002, включая нижележащие access subject/permission readers.
Локальный классификатор не доказывает исправления общего reader. Root scoped
review f24 оставляет инфраструктурный path и quarantine invalidation OPEN до
common helper fix, native048 regressions и qualification нового SHA. Shared
empty-binding OR отдельно прошёл actual PostgreSQL18.3 custom/generic plan:
добавленный nullif является hardening, не исправлением доказанного FAIL.


# PWA setup scenario

Отдельные feature adapter/controller/panel отправляют в Begin лишь SHA/byte count,
а private key — напрямую на server-returned HTTPS broker origin с exact hostname,
без Cookie, redirect и API proxy. Key не попадает в Pinia либо storage, локальная
форма очищается до первого await, owned byte copies очищаются после joined local
workflow. Permit остаётся только в нереактивной памяти текущего owner context;
pagehide/logout/context change закрывают локальные запросы и очищают его.

После любого upload receipt либо transport failure состояние перечитывается у
CP. UNKNOWN не повторяет material POST. Observe — отдельное действие после fresh
owner read, без material body; без сохранившегося permit оно недоступно. Lost
Begin response восстанавливает только safe metadata по original key. Смена owner
или Connection до async ACK проверяется до любого применения результата.

`make test-ssh-setup-ui-unit` проверяет эти переходы и source-context races,
single POST и очистку буфера. Panel пока не подключён к personal/shared surface:
mount требует fresh explicit credentialDelivery metadata047 и запрещает TOKEN
fallback при missing/unknown kind. Это не browser/native/full048 qualification.


# Повреждённый successful Observe и нижележащий read

Повреждённый полностью полученный HTTP2xx при initial Observe закрывается тем же
terminal witness, что и Create: bounded response SHA, truncation и unambiguous
metadata.uid до semantic validation. Wrong kind либо unknown field не стирают
однозначный UID, но witness никогда не становится usable locator. Duplicate
либо aliased metadata/uid и неполный JSON не создают UID witness. Interrupted
body/transport и404 отдельно сохраняют неопределённость исходной записи; они не
разрешают новый Create. Read API unavailable/absence не является SSH auth rejection
и сохраняет отдельную классификацию вплоть до private credential consumer.

Owner recovery дополнительно требует, чтобы каждая исходная attempt имела
явный timely UNKNOWN receipt, а latest setup state был UNKNOWN. ATTEMPTING и
старый WRITE UNKNOWN не выдают новую observation generation после потерянного
completion последнего Observe. Replay старого receipt не меняет state новой
attempt. Финальный UNKNOWN/COMPLETED update проверяет DB deadline после owner
preflight; начало запроса до expiry не продлевает grant.

Actual TLS regression root на a6b03 воспроизвёл lost CREATE → malformed successful
GET → последующий replacement adoption. Исправленный broker path проходит тот
же тест, private GET error matrix и existing setup/material consumers. Это не
native PostgreSQL048: owner latest-attempt/replay, R1 infrastructure и R2
quarantine graph остаются открытыми до полного native fixture.


# Development-проверка owner до завершения048

Два исходных пакета `ssh-managed` и `ssh-execute` имеют `NOT_READY` в registry и
shipped metadata. Public CreateConnection сохраняет прежний закрытый guard
`adapter_readiness=READY`: наличие package не делает незавершённый consumer
исполняемым. Доступ к target проходит только через фиксированный внутренний
`ssh_broker` destination; endpoint хоста и ключ не находятся в package.

`make test-ssh-setup-owner-development` использует отдельный RAM PostgreSQL18.3,
точный неизменённый046 и явно закреплённую development047 из авторского checkpoint
`aaa7a06134c1679f75ddedd38493382a755b1b1c`. SHA256 файла047:
`790756bd484fae92ce0889ba923481f9948d3780b271758bc4cffe588b97a712`.
Этот временный контур требует доступного Git object данного checkpoint; он не
заменяет ещё не frozen047 predecessor, миграцию048 и normal/backup restore proof.
SSH ledger пока находится только в явно названном disposable SQL fixture.

Owner fixture отдельно проверяет отказ public Create для NOT_READY, затем
выполняет обычный Connection INSERT с точными shipped version/digest и разрешёнными
сервером actor/tenant. Это проверка настоящих Begin/Resolve/Claim/Complete и common
BI attachment, но не положительный продуктовый public Create→setup сценарий.
Не используются вымышленные package IDs, fake READY либо отключение owner guards.
Переход к положительному public path требует завершённого runtime consumer.

Матрица контура: first empty binding→Known, durable WRITE UNKNOWN→OBSERVE→damaged
ACK, настоящий expiry незавершённого OBSERVE и старый receipt replay, native
successful-SQL/failed-Scan в owner validation и attachment, SQL58000 после записи
material до attachment. Неудачная инфраструктура должна сохранять все owner rows,
versions и receipts; после восстановления доставляется тот же original receipt.
Эта матрица не закрывает R2 quarantine dependency graph, verification/runtime
selection, подписи109 и native owner+TLS+sshd составной сценарий.


Actual development PostgreSQL выявил неоднозначный `version` в UPDATE setup с
присоединённым Connection. Closing queries явно квалифицируют owner alias.
Claim использует один MATERIALIZED actual clock для created_at и expires_at:
два отдельных clock_timestamp способны нарушить точный предел30s на микросекунды.
CHECK остаётся строгим; перед INSERT дополнительно проверяется фактический deadline.
Это не увеличение lease либо зачисление authority по началу долгого запроса.


# Public metadata и формы настройки

Свежие Definition/Connection/personal views передают закрытый `credentialDelivery`
через существующие Proto поля. HTTP mapper отделяет `NONE|TOKEN` от
`BROKER_IDENTITY` с exact purpose, отвергает mixed/partial metadata и выдаёт только
`brokerIdentity{ref,revision}` и binding generation. Старый immutable receipt без
kind остаётся UNKNOWN; его metadata не дописываются задним числом. Ref/revision
не являются grant и не содержат Kubernetes Secret locator либо private key.

Сценарий personal: authenticated actor → owner-scoped GET списка/своей binding →
pinned package metadata и OCC → HTTP closed union → отдельный SSH setup component
при BI/SSH purpose и двух известных definition keys → Begin → direct TLS upload →
owner Get. Отсутствие либо конфликт kind закрывают generic TOKEN save и показывают
необходимость нового readback. Неподтверждённые metadata не выбирают transport.

Shared: manager выбирает текущий Connection → fresh protected Get по exact ref →
явный PROJECT_SHARED/BI/SSH snapshot и action hint → отдельная форма в dialog.
Она не находится внутри generic credential form и не превращается в TOKEN при
потере metadata. Начальный service_subject_ref остаётся лишь selector в Connection;
active SERVICE, tenant и manager permission повторно разрешаются owner. Поле key
не входит в CP/Gateway payload; shared Begin использует Connection version.

Наличие подключённых форм и local unit checks не квалифицирует browser, deploy
listener/NetworkPolicy, signed109, runtime execution либо полный048. Пока адаптер
NOT_READY, обычный public Create остаётся закрыт и эти формы не объявляют SSH доступным.

# Общая очередь проверки shared Connection

Manager TestConnection использует прежнюю очередь `integration_connection_tests`.
Источник representation — точный сохранённый package, а не наличие secret key.
Новая запись явно фиксирует `NONE|TOKEN|BROKER_IDENTITY` и SHA256 PostgreSQL
`input_snapshot::text`. Старые terminal rows с NULL kind остаются неизвестной
историей: они не получают representation или готовность задним числом. Активация
схемы требует отсутствия прежних DUE/CLAIMED; исторические bytes не обновляются.

| Переход | Полномочия и immutable input | Результат и потребитель |
| --- | --- | --- |
| Manager TestConnection → DUE | Проверенный actor, tenant, org.manage, exact Connection OCC и package; BI требует явного активного SERVICE, purpose/target/current RI | Owner назначает snapshot/digest; прежние NONE/TOKEN JSON keys сохраняются. Существующее событие изменения Connection и protected Get читают TESTING |
| DUE → CLAIMED | Workload/route из проверенного transport; Connection/package/configuration/snapshot совпадают; READ/NONE health capability | Новый fenced lease. TOKEN передаёт прежний descriptor; NONE не получает credential; BI передаёт только purpose/ref/revision и input digest |
| NONE/TOKEN Complete | Exact workload/test/lease/fence/generation/deadline | Прежний terminal receipt и событие изменения Connection; generic Complete отвергает BI при любом outcome |
| BI purpose Complete | Отдельный purpose owner проверяет фактический probe и текущий source | Общий claim не доказывает успешный probe. Реализация и закрытие purpose graph обязательны до READY |
| BI lease expiry | Истёкшая exact owner attempt, без повторного generation | CANCELLED, lease закрыт; только совпадающий current RI/Connection version теряет TESTING. События нет: авторитетен protected Get и owner test readback; новый probe требует нового TestConnection |
| NONE/TOKEN lease expiry | Прежняя retry policy | DUE с новой attempt; существующие TOKEN retry semantics сохраняются |
| Source/actor revoke, cancel, FAILED | Owner закрывает зависимости в одной транзакции; immutable snapshot не меняется | Closing не требует заново активного source. Полный SERVICE/RI quarantine closure проверяется purpose owner отдельно |

SQL либо Scan failure до owner transition возвращает Unavailable с rollback;
нулевые поля не означают отсутствие SERVICE или identity. Общая claim-проекция
не читает purpose material и не создаёт второго credential selector. Библиотечные
правила CHECK/NULL и сохранения owner/ACL при CREATE OR REPLACE сверены через
Context7 с PostgreSQL18. Development fragment047 и локальный native target
`make test-shared-integration-health-development` не заменяют frozen047/048,
подписи108/109 либо составной owner→broker→SSH probe.

Gateway разделяет свежие typed claims до generic TOKEN descriptor guard.
Фиксированный registry содержит только KUBERNETES_TARGET и SSH_TARGET; неизвестный
kind, mixed metadata либо отсутствующий consumer закрыто отклоняются. BI health
не вызывает generic Begin/Complete: отдельный purpose owner проверяет actual
test/lease/fence/generation/input digest, current source и результат probe.
Dispatcher копирует claim перед передачей consumer, сохраняя исходные pins.
Метрика уже начатой проверки сохраняется при lost completion ACK. Наблюдённая
authentication при последующем CANCELLED не даёт binding состояние READY.

Purpose consumer получает уже claimed invocation, не второй competing Claim
loop. Он владеет renewal исходной attempt, Gate, frame exchange, owner stop до
transport cleanup и outer completion. Возврат WAITING_OWNER или STREAM_ACTIVE
требует activity с app-owned lifecycle до terminal/reconciliation; завершение
unary request не доказывает её join. RemoteOutcome отделён от lifecycle State:
terminal cleanup ACK сам не является business success. Отсутствующий purpose
consumer остаётся закрытой границей, а не generic token fallback.

`make test-broker-identity-gateway` проверяет common dispatch и прежний TOKEN
worker на controlled owner ports; exact NOT_READY SSH metadata используются лишь
для проверки маршрутизации. Это не admission, private material read, actual probe,
подпись109 либо исполнение SSH. Конкретные purpose consumers и полная composition
обязательны до readiness всего unit.

Существующий `IntegrationInvocationClaim` получает additive поля26–29:
credential delivery kind, broker purpose, metadata ref и revision. Их назначает
owner из exact validated package и current selection до commit исходного lease;
caller не выбирает purpose. Caster сохраняет NONE/TOKEN/BI и не добавляет Secret
descriptor для metadata-only source. Explicit future/malformed purpose остаётся
invalid enum, в том числе рядом с TOKEN/NONE: проекция не скрывает mixed source
как отсутствие purpose. Отсутствующий kind остаётся неизвестным и не включает
generic token fallback. Публичный `make test-broker-identity-claim-metadata`
проверяет настоящий Proto roundtrip; owner facade и runtime consumer должны быть
квалифицированы отдельно на том же полном unit.

SSH использует профиль22 `SSH_SCOPED_RESOURCE_V1` существующего credential selector.
Identity health подтверждает лишь `ssh.target.inspect` либо `ssh.identity.inspect`.
Для другой capability owner обязан назначить exact registered resource profile:
его prerequisite — та же подтверждённая identity; вызов затем заново проверяет
operator target/user/qualification, типизированный recipe, input и grant. Health
не исполняет WRITE, не подтверждает произвольный shell и не объявляет глобальные
права на host. Без profile неподтверждённая capability остаётся Unconfirmed.
SCOPED для managed create/replace и EACH для destructive/service/execute
сохраняются в едином recipe registry; profile не понижает эти floors.
SSH Compose не использует новый profile. Его прежний resource kind9 остаётся
отдельным scope; protocol отличают exact package/adapter/purpose и owner facts.
Профиль пока является source contract: NOT_READY definitions и обязательный
purpose consumer запрещают считать его готовым исполнением048/109.

### Health owner: личная и общая очередь

`ManagedSSHOwnerService` получает исходные `testRef/lease/fence/generation/inputDigest`
от broker. Digest относится ко всему immutable test snapshot; пустой input команды
не заменяет его. Private key остаётся только в purpose store broker. `Begin` и
`Complete` доступны только `secret-broker`; публичный worker не завершает BI через
TOKEN endpoint. Регистрация RPC сама по себе не включает профиль109.

| Переход | Authority и эффект | Terminal/read path |
| --- | --- | --- |
| Claim → Begin | Connection → binding/test lock; exact package/purpose/current RI; gen1, database clock; unique probe credit до выдачи locator | Потерянный ACK не разрешает второй Begin либо второй внешний probe |
| Personal Begin | Existing single selector, полный исходный snapshot и current subject; CLAIMED → PROBING | Исходная личная очередь и protected personal view |
| Shared Begin | Existing SERVICE selector, exact configuration/definition/RI snapshot; CLAIMED сохраняется, credit уникален | Исходный testRef и protected Connection view |
| Native probe | Только trusted operator endpoint, modern host pin и broker-held key; managed Inspect либо фиксированные numeric identity reads | Ошибка transport/host pin не объявляет credential invalid |
| Complete | Exact original admission; fresh source; final binding/Connection UPDATE проверяет исходный deadline | Лишь health capability; ресурсные effects требуют profile22 и своего свежего owner gate |
| Revoke/drift/expiry | Late fact сохраняется как CANCELLED без повторного attachment; infra error откатывает transaction и допускает тот же receipt | Historical exact replay, altered outcome отклоняется; personal audit без события |
| Shared completion/reset | Test и Connection изменяются одной transaction; существующий INTEGRATION_CONNECTION_CHANGED с точной version/state | Event consumer перечитывает protected owner; historical replay не создаёт новое событие |

Development DDL этого checkpoint не является frozen048 либо qualified047 predecessor.
Actual owner-native/RPC/broker/gateway composition проверяется отдельно; локальный
unit test не подтверждает работоспособность профиля048/109.

### Остановка общего gateway

Purpose consumer резервирует bounded local slot до owner Claim. `RUNNING` означает
лишь adoption app-owned activity до первого RPC. `TERMINAL` требует exact owner
terminal read/receipt и local cleanup join, не означает business success. Consumer
fatal `Failure()` отменяет app lifecycle и закрывает admission; ошибка joined
activity сохраняется отдельно от её `Joined()`.

После stop/cancel app независимо и ограниченно ждёт common workers и purpose
activities. Только положительный join обоих наборов разрешает закрывать protected
clients, HTTP и telemetry. Timeout оставляет используемые зависимости открытыми
до fatal process exit. Actual worker result перечитывается после join без
отменённого observation context; `errors.Join(context.Canceled,fatal)` не становится
штатной отменой ни в collector, ни в main. Local join не доказывает remote writer
quiescence: этой границей по-прежнему владеют broker и canonical owner.

При ошибке startup освобождаются только фактически приобретённые ресурсы, в
обратном порядке. Constructors consumers выполняются до `Listen`; после успешного
`Listen` worker запускает `Serve` без промежуточного ошибочного шага. Technical HTTP
использует существующий `ShutdownWorker`: join `Serve` сам по себе не доказывает
join handlers после timeout. Нужен также успешный `http.Server.Shutdown`; иначе
protected clients и telemetry остаются открытыми до process failure.

Canonical join budget J равен `ShutdownTimeout=20s` для всей фазы workers и
consumers, а не для каждого последовательно. Activity cleanup получает C=15s
при подключении purpose runner; deadline отдельного RPC ограничивается оставшимся
C. В обоих web renders integration-gateway имеет Pod grace60s: J20 + HTTP10 +
clients10 + telemetry10 + запас10. Общий Go component35s и остальные workloads
этим изменением не затрагиваются. Эти значения не подтверждают готовность ещё не
подключённого runner; его deadline propagation квалифицируется отдельно.


## Точная identity исходного invocation

`ssh_invocation_identity_bindings` в development048 фиксирует original invocation,
root actor, actual custody subject, binding generation либо shared SERVICE,
immutable BI revision, применённый native health probe, полный observed operator
subject digest, definition/configuration digest, transport mode и конечный
health deadline. Pin записывается в исходной intent transaction до claim; он не
выдаёт effect authority. PostgreSQL повторно проверяет всю owner lineage при
insertion, а current-source read исключает revoke, replacement, expiry,
ACK_REJECTED и quarantine. Единый credential selector сохраняется; SSH dispatch
выполняется до обращения к TOKEN descriptor. Новая таблица пока является
развиваемой048-оснасткой, а не замороженным migration/predecessor proof.

Native purpose consumer использует существующий SSH transport и trusted operator
artifact. Перед подключением и перед SSH Start он сверяет полный profile с
owner-pinned health digest; адрес, user и host key не выводятся из operation
payload. Helper UUID стабильно выводится из original invocation, а не lease.
Managed recovery отправляет только `ReceiptRead` с исходными operation и request
digest; неудачное чтение сохраняет UNKNOWN. Recovery arbitrary execute не
подключается и не повторяет command. UNKNOWN не содержит partial output;
SUCCEEDED/FAILED output ограничен, integrity-bound и остаётся закрытым до
авторитетного owner result admission. Session exit не доказывает завершение
remote descendants. Публичный `make test-ssh-purpose-execution` проверяет actual
зашифрованный synthetic SSH transport; этот контур не заменяет ещё не завершённые
purpose owner RPC, canonical claim, cleanup graph и полный048/109.


## Общая доставка invocation с зарезервированной capacity

Единственный existing Runtime Claim принимает bounded capacity3: ordinary,
KUBERNETES_TARGET и SSH_TARGET. Gateway резервирует фактические local slots до
RPC; сумма не превышает limit, каждый purpose ограничен четырьмя reservations.
При малом limit классы по очереди получают первый slot; занятый либо отсутствующий
purpose отдаёт место ordinary работе. Весь ответ проверяется до первой adoption:
wrapper, unknown fields, known kind/purpose, exact package metadata, lease,
уникальность и соответствие обещанной capacity. BI activities принимают exact
original claim до длительной ordinary операции; generic TOKEN Begin/Complete
для них не вызывается. Unused release не освобождает adopted activity.

Mixed composition до Listen и Claim получает revision1 через private callback
реального protected owner adapter. Сам adapter проверяет typed wrapper и unknown
fields; optional KUBE Ready=false не отменяет известный delivery contract.
Revision0, future либо malformed proof не становятся fallback. Composition без
mixed consumer использует ordinary-only nil capacity; активная invocation capacity
без callback закрыто отклоняется. Проверка metadata не делает исходный NOT_READY
SSH package исполняемым: actual purpose consumer обязан проверить runnable
package и получить fresh owner admission. В protocol unit fixtures нет external
эффектов, owner authority либо подтверждения полноты047/048.

Потерянный Claim ACK и malformed successful batch — unresolved delivery. Gateway
закрывает admission обоих purpose и прекращает loop до нового Claim; known
activities проходят cancel и positive join перед закрытием dependencies. Unseen
owner leases остаются до авторитетного expiry/reconciliation, не объявляются
absent либо joined. Restart не восстанавливает старые claims по одному instance:
исполнение требует нового server-issued original lease/fence/generation либо
точного owner recovery, а не свободного local slot. Attempted metric сохраняется
даже при ошибке позднего handoff; RUNNING adoption не считается external effect.


## Общий SDK и право использования назначенной операции

SSH сохраняет единый SDK-контракт с KUBE: отдельные registered adapter, purpose и
permission profile проверяются до generic TOKEN. Сведение использует SDK/value
source catalog21f и точный общий JSON decoder; повторного parser либо selector нет.
Оба purpose сохраняют собственную строгую проверку definition/configuration и
точный внутренний broker destination. Штатный генератор добавляет KUBE definition,
не меняя существующие YAML и frozen digests. SSH definitions сохраняют NOT_READY.

Общий owner разрешает сотруднику с integration.view использовать только точную
назначенную capability KUBE/SSH, зарегистрированную в validated immutable package.
Grantability, настройка Connection и выдача grant по-прежнему требуют manage.
Единое правило проходит List/RR, personal launch/context, resolve и common
claim candidate; чужой Connection либо подменённая operation закрыто отклоняются.
Legacy adapters и SSH_COMPOSE сохраняют прежнюю policy. Это не замена fresh
проверок root/grant/identity/health/gate и не допуск незавершённого SSH consumer.

Публичные bounded targets test-ssh-shared-credential-sdk и
test-ssh-invocation-use-policy проверяют общий SDK и policy predicate. Они не
подтверждают native SSH execution owner, новый Runtime claim helper, canonical048/109
либо весь установленный профиль. SDK cache переносится из qualified0885 без
изменения bytes: сохраняются только frozen canonical metadata; mutable Package,
actor, grants и текущая eligibility заново проверяются при каждом resolve.

## Owner ledger finite execution и исходный claim

Специализированный owner использует тот же immutable invocation pin и canonical
candidate query, что обычный runtime. Дополнительный закрытый selector SSH
исключает две exact definition keys из ordinary и KUBE до LIMIT; SSH_COMPOSE
не переиспользуется. Tx helper сканирует не более32 исходных intents по
сохраняемому keyset cursor, затем берёт Connection lock, проверяет current
custody/material/health/quarantine и вызывает существующий lease builder.
Principal, actor и permission не переназначаются. Коммит принадлежит общему
capacity facade. Пока полный SSH consumer и owner profile не квалифицированы,
ненулевая SSH capacity не подключается.

| Переход | Owner boundary и результат | Событие и потребитель |
| --- | --- | --- |
| Claim | Только original READY/gen0, exact package/RR/grant/Gate/actor и pinned RI; новая fenced lease в общей Tx | Нет отдельного события; exact claim возвращается existing Runtime worker |
| Begin | secret-broker с exact method; Connection→invocation lock, fresh canonical eligibility, однократный immutable intent до выдачи private locator | Нет события; broker получает admission только после commit |
| Lost Begin ACK | Повторный Begin не выдаёт admission; Get читает original intent, не делает новый dispatch | Нет события; UNREPORTED не является доказательством отсутствия effect |
| Get перед Start | Только original fence/lease/generation/input; current source/ACL/Gate и finite deadline; возвращается тот же deadline без продления | Нет события; private broker pre-dispatch check, без output |
| Complete | Полная classified observation сохраняется отдельно от publication; final UPDATE проверяет original live source/lease | При applied completion один canonical TURN_PROGRESS; SUCCEEDED/FAILED планируют existing continuation, UNKNOWN_OUTCOME не планирует |
| Потеря authority | Observation остаётся историческим фактом; DISCARDED не выдаёт output и не объявляет весь owner graph завершённым | Нет события этой observation; авторитетное состояние invocation читается отдельно |
| Exact Complete replay | Совпадают original binding, intent и все observation bytes; без повторного effect и нового события | Только original receipt metadata, без повторной выдачи private output |
| Changed completion | Чужой binding отклоняется; altered observation не заменяет сохранённый receipt | Нет события и изменения состояния |
| Инфраструктурный отказ | Unavailable с rollback всей Tx, включая partial outer receipts и continuation | Нет committed события; тот же completion можно повторить, dispatch нельзя |
| Receipt read | Новая отдельно допущенная READ invocation разрешает original helper recipe только внутри той же RI и target qualification | Обычный результат READ; неизвестное состояние оригинальной записи не считается отсутствием изменения |

Этот срез owner source не подтверждает полный native048 профиль, RPC policy109,
положительный employee→owner→broker→SSH путь либо closing graph новых executions.
Они входят в оставшуюся сквозную квалификацию unit; NOT_READY не снимается этой
матрицей или отдельной компиляцией.


Finite SSH dispatch получает deadline не более25 секунд и не позднее пяти секунд
до original outer lease/health/SCOPED-consent deadline. Approval scope берётся
под той же блокировкой, что revoke, при каждом owner admission и publication.
Intent insert не принимает deadline за пределами этого consent, а final UPDATE
повторяет same-org scope/revoke/expiry predicate на одном DB clock, после
предшествующей работы transaction. Equality expiry не выдаёт output. Это оставляет общий четырёхсекундный
cleanup budget для Complete (до3 секунд) и единственного metadata read после
lost ACK. Publication отдельно проверяет ещё живую original outer lease и
current source; истёкший dispatch deadline не мешает записать joined UNKNOWN,
но никогда не даёт начать либо повторить команду. Get возвращает раздельно
immutable observation state и current owner invocation state; unrecorded
SUCCEEDED отклоняется. Expiry SSH READ не переоткрывает исходный intent в READY.

Добавленный finite consumer использует общий ActivityGate для pre-claim
reservations и positive local join. Caller cycle context управляет только
adoption; app lifecycle владеет operation и независимым cleanup. Запись UNKNOWN
либо strict closed owner metadata не считается remote process quiescence.
Повреждённый successful execution ACK остаётся fatal; после transport uncertainty
возможны лишь bounded metadata reads, не второй Execute. Factory Run пока
сохраняет health-only SSH/capacity0; mixed composition и delivery-contract probe
сводятся в root поверх полного047 без второго selector/readiness механизма.

Known denial непринятого READY/gen0 intent завершает только эту заявку без
external effect; keyset cursor продолжает сканирование. Admission этой заявки
изолирован savepoint: partial lease откатывается до settlement, а failure
rollback остаётся Unavailable всей owner-транзакции. Open Gate и уже записанный
execution intent не получают придуманного no-effect terminal.

Проверка fixed operator mount повторно запускает тот же Go test binary через
bubblewrap с private user/PID/network/mount namespaces и loopback-only сетью.
Это actual parser + encrypted SSH numeric probes + final owner callback, а не
проверка PostgreSQL authority. Runtime path не подменяется тестовым callback.
Проверены Context7 `/containers/bubblewrap`: private mounts, bounded child
lifetime и создание loopback при `--unshare-net`.

`make test-ssh-native-helper-process` соединяет настоящий encrypted SSH peer с
отдельным процессом существующего helper descriptor/receipt runtime. Проверяются
реальная запись и version-bound READ, потеря SSH-ответа после записи, отдельный
READ исходного durable receipt без повторного WRITE и отзыв допуска между
handshake и Start. Каждый процесс ограничен private roots текущего non-root UID,
точными bytes profile и bounded input/output; процесс завершается до результата
проверки. Нет Docker, PostgreSQL, внешних хостов или изменения host accounts.

Этот fixture не проверяет установленный root-owned profile/forced account и не
выдаёт синтетический callback за owner authority. Установленная helper boundary
имеет отдельный тест; сквозные setup/health/claim/Complete owner048 и policy109
остаются обязательными до READY. Поведение production helper и SSH transport
этим fixture не расширяется.

## Сведение с общей Runtime facade

Объединённый source048 требует совместную схему047+048: общий candidate SQL
использует девятый закрытый флаг SSH и purpose table. Его нельзя объявлять
совместимым с установленной047 без048. Семантика KUBE flags7/8 и SSH_COMPOSE
flag5 сохраняется; все прочие вызовы передают false9. Единственная owner Tx
вызывает purpose helpers и ordinary selector с непересекающимися ключами.

`INTEGRATION_GATEWAY_SSH_INVOCATION_CAPACITY` по умолчанию равна0 и ограничена4.
При0 gateway обслуживает только SSH health; профиль broker RPC не получает
Execute/Reconcile. Ненулевой явный ввод выбирает настоящий app-owned consumer,
а не callback или второй claim loop. Конструктор закрывает неполный source
registry. До любой lease-записи owner дополнительно требует configured endpoint,
оба реальных READY package и полный schema contract с privilege/immutability
checks. Это code/schema readiness, не authority для конкретной операции: её
по-прежнему определяют original binding/RR/grant/Gate/consent/current identity
и fresh native health. Общий protected delivery-contract probe остаётся до
Listen и Claim. Пока source registry NOT_READY, ненулевая capacity закрыта.

Через Context7 `/websites/postgresql` проверена PostgreSQL18
[семантика has_table_privilege](https://www.postgresql.org/docs/18/functions-info.html#FUNCTIONS-INFO-ACCESS):
список через запятую означает ANY. Schema readiness проверяет каждое нужное
право отдельно, включая immutable ledger INSERT и mutable cursor UPDATE.
Единый10s owner budget охватывает и preflight, и claim transaction; readiness
не получает дополнительный неограниченный запрос перед этим budget.

Owner receipt lookup связывает оригинал с server-read текущим invocation ID:
одинаковые Connection, RI/qualification, root actor и project, точные обе root
lineage и fresh original run.view. Shared service identity сама по себе не даёт
читать чужую историю. Новый separately admitted READ допустим в другом run того
же actor/project; payload ref не назначает owner. Cross-user/project native
negative остаётся обязательным до закрытия finding.

Отзыв до Begin отменяет dependent invocation как CANCELLED с причиной
INTEGRATION_AUTHORIZATION_DENIED, закрывает лишь его OPEN Gates и снимает
root humanwait под тем же mutex только при отсутствии других OPEN Gates.
Canonical continuation использует CANCELLED/CANCEL либо сохраняет historical
APPROVE; Gate history не переписывается. Уже записанный execution intent даёт
UNKNOWN_OUTCOME без automatic continuation и без remote-quiescence claim.
Completed-origin и sibling-Gate native cases ещё требуются; source fix отдельно
не считается закрытием review finding.

Composed development seed после normal merge root baseline применяет compiled047
из того же checkout, затем явные provisional shared-health/SSH fragments. Старый
external aaa7 input остаётся воспроизводимым на frozen69b0; его не называют
проверкой нового schema shape. Новый seed печатает только SHA256 embedded047 и
не является frozen predecessor либо canonical048/restore proof. При включении
shared fragment в canonical047 seed обновляется явно, без условного DDL fallback.


## Кандидат полного owner-пути

Следующий private source successor явно регистрирует оба SSH package и адаптер
как READY: это изменение настоящего producer source, без тестовой подмены
registry либо пакетов. Описанные выше NOT_READY checkpoints сохраняют прежние
SHA и доказательства. Публичный fixture теперь создаёт Connection штатной
командой, выбирает личную policy, выполняет setup и health, затем запускает
настоящий RuntimeRevision, resolve и единственный common capacity claim.
Проверяются purpose Begin/Complete, неизменный receipt replay и отказ при
изменённых result bytes. Synthetic observation проверяет owner persistence;
сам по себе этот тест не подтверждает SSH host authentication.

Это ещё кандидат, а не допуск установки: canonical048/signed109, положительный
сквозной native consumer, consent expiry и замечания receipt scope/revoke Gate
требуют итоговой квалификации. Deployment capacity по умолчанию остаётся0;
никаких live hosts, image publication либо изменения установленной схемы этот
source transition не выполняет.


## Миграция журнала SSH

`20261114160000_managed_ssh.sql` применяет схему 048 после 047.
Общий снимок проверки подключения различает NONE, TOKEN и BROKER_IDENTITY.
Таблица проверок блокируется на время перехода. Строки DUE и CLAIMED
отклоняют миграцию, чтобы прежняя попытка не получила другой источник
учётных данных. Исторические строки сохраняют исходный снимок и результат.

Миграция снимает наследуемые права с 11 новых таблиц и назначает каждому
журналу требуемый набор SELECT, INSERT и, где нужно для перехода, UPDATE.
Для трёх журналов исполнения UPDATE всегда закрыт. DELETE проверяет
точную строку в служебном списке штатной очистки проекта, привязанном
к транзакции и backend. Readiness сверяет эффективные права LOGIN,
события, функции и аргументы этих триггеров.

`make test-ssh-setup-owner-development` и
`make test-shared-integration-health-development` прошли на PostgreSQL 18.3 для
`8499c71c1082325e07c619d8b34d1812ab844d31`. Проверены прямой переход 047 → 048,
повторный up, отзыв, истечение разрешения, общие проверки подключения
и отказ readiness при расширении прав. Составное развёртывание ждёт
квалификации полного графа purge/restore и доставки подписанной политики.

## Результат исходного SSH вызова для Runtime

`ManagedSSHOwnerService.ReadManagedSSHInvocation` использует отдельную permission
`platform.runtime.ssh.invocation.read` только для runtime-controller. Actor и
project выводятся из exact live RuntimeRevision/lease/fence/generation/input;
payload содержит лишь адрес исходного invocation. Проверяются original SSH pin,
exact package, run.view и integration.view. Затем владелец блокирует Connection
и invocation и повторяет physical lease/deadline в final result query. До и после
commit действует консервативный monotonic deadline без продления original lease.

Callback выбирает этот reader лишь по закреплённым SSH definition/capability из
RuntimeRevision. Нет KUBE либо generic fallback, второго invoke/poll loop,
искусственного turn для NONE либо нового effect credit. Общий envelope validator
проверяет closed state, exact invocation, success receipt/digest, отсутствие
output в иных состояниях и unknown wire fields. Kubernetes reader сохраняет
собственный selector; нейтральное имя файла отражает общий dispatcher.

Нативный fixture дополнительно ограничивает original actor одной временной
view-binding, блокирует Connection после early authorization и ждёт её реального
истечения при ещё живой Runtime lease. Этот counterexample должен получить
NotFound и zero result; до qualified результата ACL-expiry остаётся открытым.
Source/readiness/policy109 не объявляются допущенными этим описанием.


Authoritative JSONB хранит исходные значения, но не presentation bytes JSON.
SSH owner перед strict capability parser заново собирает object через RawMessage
без float64 и повторно проверяет original input digest. Это применяется к current
invocation и original receipt recipe. Public/native parser не получает whitespace,
unknown-field либо duplicate-key fallback. Поэтому nonempty stored input не
отклоняется лишь из-за пробелов PostgreSQL, а изменение source bytes остаётся
закрытым отказом. Первая попытка нового reader выявила отдельную SQL42703:
ключ definition назначен Connection; successor читает его оттуда, сохраняя
invocation version/digest и immutable purpose pin.

## Опциональная доставка direct TLS upload

Canonical `tools/release/render-web-only.sh` принимает пару
`--ssh-identity-upload-configuration` и
`--ssh-identity-upload-configuration-sha256`. Закрытый document
`kodex.ssh-identity-upload/v1` содержит `origin`, `browser_origin`,
`tls_secret_name`, `ingress_namespace`, `ingress_pod_name`. Оба origin — exact
HTTPS DNS без path/port; upload имеет отдельный hostname. Browser host и AND
namespace/Pod selectors ingress-controller совпадают с текущим frontend render.
Полный source SHA закреплён release lock; immutable ConfigMap сохраняет
нормализованный document, его SHA256 и этот source SHA.

Тот же профиль добавляет только выбранный upload origin в canonical PWA
`connect-src`. Остальные CSP directives и выбранный OIDC origin сохраняются
из source template. Изменённый immutable runtime ConfigMap получает новое
data-hash имя; ровно одна ссылка frontend Deployment переключается на него.
Подмена CSP, чужой backend namespace TCP route и stale mount закрыто отклоняются
в render/preflight/readback. Default CSP без профиля broker upload не разрешает.

Профиль доставляет только broker Service8446, exact HostSNI TCP passthrough
Traefik websecure, read-only TLS Secret mount в broker, endpoint/SNI в control
plane и exact ingress NetworkPolicy. TLS завершается в broker; private SSH key
не проходит через API gateway или HTTP proxy. Новый Service не выдаёт generic
TOKEN route. Две existing workloads сохраняют image и остальные env; capacity
SSH остаётся0 до полной квалификации. TLS Secret не входит в render: оператор
предоставляет отдельный доверенный browser сертификат и DNS до включения.
Текущий leaf/SNI проверяет тот же upload runtime. Продление сертификата требует
controlled broker rollout до expiry; замена файла не объявляется hot reload.

Проверка профиля входит в deploy/preflight/readback и отвергает повреждённый
record, partial env, подмену Service/TLS/mount, другого consumer ключа и additive
NetworkPolicy, расширяющую доступ к8446. Существующий namespace
`kodex-ssh-identities` и его UID/ownership cleanup сохраняются; профиль не
создаёт, не читает и не удаляет identity Secrets. Публичная проверка
`make test-ssh-identity-upload-profile` использует offline render обоих профилей,
проверяет CLI digest и закрытые отказы без Kubernetes/SSH writes. Это не
доказательство установленного endpoint, live host либо полного048/109.

Через Context7 проверены Kubernetes NetworkPolicy AND selectors и Secret volume
permissions, а также Traefik IngressRouteTCP HostSNI/TLS passthrough. Применяются
те же canonical selectors, что в frontend render; отдельного общего TLS или
NetworkPolicy interpreter SSH не добавляет.

## Ограничение выдачи результата и закрытие revoke

Private SSH Runtime reader сохраняет срок каждого из двух текущих view permissions
через общий `requireAccessUntil`. Среди действующих альтернатив выбирается поздний
конец (unbounded только при уже действующей бессрочной альтернативе), затем ранний
срок среди permissions и runtime lease/deadline. Future-starting binding не
продлевает это окно. Monotonic bound проверяется до и после COMMIT; ожидание
Connection lock не продлевает истёкший ACL.

| Отзыв зависимости | Owner graph | Событие и авторитетный consumer |
| --- | --- | --- |
| Не начат, OPEN Gate | `CANCELLED/AUTHORIZATION_DENIED`, свой Gate `CANCELLED/CANCEL`, старые leases закрыты | Один `OWNER_GATE_RESOLVED`; canonical scheduler создаёт continuation при completed origin. Дополнительного integration-action event нет; task и exact owner result reader сохраняют `CANCELLED` |
| Не начат, APPROVED Gate | `CANCELLED/AUTHORIZATION_DENIED`, historical `APPROVE` не меняется, leases закрыты | Нет нового Gate/action event; canonical continuation и result reader показывают позднюю отмену без повторного effect |
| Был Begin intent | `UNKNOWN_OUTCOME`, historical Gate сохранён, leases закрыты | Existing unknown integration-action event; автоматического continuation нет |
| Отзыв одного grant при другом OPEN Gate | Закрывается лишь совпадающий grant invocation/Gate; другой Gate и human wait сохранены | Свой Gate event и canonical scheduler; чужой Gate не является отменённым либо одобренным |

Native profile явно использует публичный policy transition в `PROJECT_SHARED`
для shared SERVICE setup: default BI policy остаётся `INITIATOR_REQUIRED`, наличие
`service_subject_ref` само policy не переключает. Это fixture выбранного режима,
а не ослабление owner guard либо автоматический выбор SERVICE.

Focused root graph проверяется через `make test-integration-pending-root-postgres`
на standalone047 source. Composed source048 выбирает отдельный явный
`make test-ssh-pending-root-development`: тот же owner test и current047 bytes,
затем provisional048 development fragment. Это разные fixture entry points,
не runtime fallback. Bootstrap048 с047-only seed остаётся закрытым отказом и
не квалифицирует lifecycle. Ни один development entry не заменяет final048
forward/restore proof либо подпись109.

## Квалификация выбранных SSH targets в итоговом render

`render-web-only.sh --ssh-qualification <profiles.json>
--ssh-qualification-sha256 <SHA256>` принимает только явно выбранные exact bytes.
Опциональный профиль требует тот же clean source и schema/source pins release
lock, что остальные operator profiles. Installer захватывает bounded regular
single-link файл, проверяет digest и передаёт копию в private directory0700 с
record0600, закрываемый в read-only0440 перед существующей командой
`ssh-qualification render`. Единственный decoder
формата остаётся в `managedssh.DecodeQualifications`, тот же, что у broker;
Python не определяет новые правила target, UID, key либо срока квалификации.

Канонический Go renderer формирует immutable ConfigMap с точным content hash,
broker-only strategic patch и NetworkPolicy с literal IP/port каждого target.
Реальный offline `kubectl kustomize` соединяет их с итоговым environment,
сохраняя image, чужие env/mounts и остальные workloads. Объекты включают
`app.kubernetes.io/part-of=kodex` для штатного inventory и UID/ownership cleanup.
Deploy, preflight и readback заново проверяют exact projection тем же decoder:
partial env/mount, изменённый digest, истёкший profile и расширенная policy дают
закрытый отказ. `make test-ssh-operator-render` включает реальный Go decoder и
offline render обоих environment profiles; это локальное доказательство формы,
а не наблюдение указанных documentation-address hosts.
Через Context7 `/websites/kubernetes_io` сверены `kubectl kustomize` и
[strategic merge по container name](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/).
Сохранение чужих полей и целых int64 дополнительно проверяется итоговым
offline render, а не предполагается из совпадения имён patch fields.

Отсутствие qualification не создаёт default target и не включает execution.
Upload endpoint настраивается отдельным direct-TLS профилем и сам по себе
не подтверждает пригодность ключа, host либо helper. Ни render, ни local record
не выдаёт actor grant, не меняет capacity0 и не устанавливает что-либо на host.

## Ограничение execution credit текущим ACL

Begin ограничивает immutable intent минимумом original-actor integration use,
native/lease/SCOPED срока и, для recovery READ, original run.view. Deadline
сохраняется до выдачи admission с monotonic проверкой перед и после COMMIT.
Get не выдаёт повторный допуск: возвращаемый pre-Start credit принадлежит только
исходной attempt и исчезает после её срока либо сужения current authority.
Recipe recovery заново проходит тот же original actor/project lookup и свежий
run.view; shared identity либо payload ref не заменяют эту проверку.

Complete сохраняет позднее observation с `DISCARDED` без публикации результата.
Новая действующая ACL alternative не возобновляет старую attempt: final UPDATE
дополнительно проверяет original intent expiry по единственному DB clock stamp.
Отдельный public `make test-ssh-execution-deadline-development` содержит реальный
finite ACL, delayed successful COMMIT ACK для Begin/Get и поздний Complete при
восстановленной другой alternative. Отдельный случай задерживает native receipt
INSERT до original expiry и проверяет rollback частичного publication receipt;
receipt-scope профиль также удерживает COMMIT за finite original run.view при
бессрочном integration use. Эти tests входят в полный owner profile.
Original production на524425 дал expected RED всех трёх случаев; successor
нуждается в собственной native qualification и не получает READY из документа.
Системный общий инвариант закреплён в GUIDE-DOC-003.

Gateway готовит invocation через exact shipped SSH package, закрытый BI union
и один общий grant/capability/scope/input validator. Ordinary TOKEN entry
сохраняет прежний credential-presence guard; SSH entry использует уже проверенный
package после собственного purpose admission. Generic Execute не получает
обхода TOKEN presence и не становится второй SSH entry. Проверка actual shipped
claim отдельно включает READ/NONE, managed WRITE/SCOPED и execute/EACH,
подмену purpose, TOKEN descriptor, отсутствующий grant, digest и profile.
Owner PostgreSQL PASS отдельно от этого consumer path: original29d public source
test выявил его отказ до broker RPC, поэтому готовность нельзя выводить из
совпадения metadata полей либо одного owner fixture.

Полный development owner profile дополнительно собирает gateway consumer до
создания PostgreSQL. Настоящий committed common claim проходит production
`CastIntegrationInvocationClaim`, protobuf и отдельный gateway test process с
actual `PrepareSSHInvocation`; ручной wire snapshot не подставляется. Binary
имеет owned0500 inode, проверяется по полному SHA256 и запускается по inherited
descriptor, input ограничен512KiB и передаётся только через private stdin.
Отсутствие consumer, неизвестный protocol либо подмена metadata дают FAIL,
внешняя подстановка binary через env запрещена public harness. Подпроцесс не
получает DSN или key bytes, не вызывает broker и не заменяет native SSH или
policy109 proof. Это поддерживаемая связка producer→gateway, которую сохраняют
при дальнейшей сквозной owner/broker/native квалификации.
