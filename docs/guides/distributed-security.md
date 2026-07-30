---
id: GUIDE-DOC-003
title: Безопасность распределенных сервисов и служебного состояния
type: guide
status: approved
owner: architect
version: 1.0.4
updated: 2026-07-30
---

# Безопасность распределенных сервисов и служебного состояния

`GUIDE-DOC-003` задает переносимые правила для межсервисной авторизации,
подписанного служебного состояния, TLS, доставки секретов и сетевой изоляции.
Правила применимы к Go-сервисам и Kubernetes-профилям независимо от конкретного
домена. Доменные permissions, callers, методы и lifecycle остаются в
профильных контрактах.

## Базовый принцип

Security boundary считается реализованной только как полный исполняемый путь:

```text
источник полномочия
-> выпуск credential или подписанного состояния
-> защищенная доставка
-> проверка transport identity и содержимого
-> устойчивое rollback/replay state
-> доменное решение
-> безопасный ответ и наблюдаемый отказ
```

Наличие mTLS, подписи, `NetworkPolicy`, `Secret` или проверки JWT по
отдельности не доказывает безопасность всего пути. Отсутствующая зависимость,
неизвестное состояние, ошибка parsing, rollback, replay, разрыв ротации или
несовпадение transport identity приводят к закрытому отказу до доменного
действия.

## Карта доверия

До реализации или изменения security boundary фиксируются:

- авторитетный владелец identity, actor, tenant и permissions;
- точная transport identity вызывающего workload;
- полный метод или операция, audience и срок действия полномочия;
- все обязательные слои аутентификации транспорта и приложения;
- источник, владелец и срок жизни ключей, сертификатов и trust bundle;
- способ доставки, ротации, отзыва и восстановления после пропуска обновлений;
- target-owned replay/rollback state и его поведение при restart;
- readiness, failure policy, метрики, alerts и runbook;
- точный deploy ownership для publisher, verifier, sidecar, RBAC, volumes и
  network path.

Идентификатор из request, payload, query parameter или обычного client token
не становится authority без проверенного связывания с transport/signed
context либо с состоянием домена-владельца.

## Публичная и привилегированная выдача

Владелец состояния задает один авторитетный предикат доступности ресурса для
каждого класса actor. Этот предикат одинаково применяется к:

- одиночному и пакетному чтению;
- поисковой или иной внешней проекции;
- событиям, которые создают, обновляют или удаляют проекцию;
- повторной доставке и восстановлению проекции.

Gateway не восполняет отсутствующую доменную проверку. Скрытый ресурс
возвращается неотличимо от отсутствующего, если сам факт его существования не
разрешен actor. Неизвестный lifecycle или сочетание статусов закрыто
отклоняется.

Разные оси состояния не объединяются молча. Например, публикация, качество,
модерация и полнота могут образовывать eligibility rule только после явного
решения и должны одинаково трактоваться всеми read/event paths.

## Многоуровневая межсервисная авторизация

mTLS подтверждает transport peer, но не заменяет обязательный bearer token,
подписанный authorization context, permission check или replay protection.
Client adapter явно собирает все принятые слои:

1. проверяемый TLS с exact server name и доверенной CA;
2. workload identity вызывающей стороны;
3. обязательный application credential;
4. exact audience, caller, actor class, full method и permissions;
5. correlation, expiry и replay state, если они входят в policy.

Credential не передается по незашифрованному соединению. Для gRPC
`PerRPCCredentials` требует transport security. Health/readiness проверяют тот
же auth path, который используют рабочие RPC, а не упрощенный обход.

Transport, verifier и domain используют разные модели. Общий verifier
возвращает нейтральные проверенные claims; преобразование в доменный principal
выполняет service adapter после всех transport checks.

## Строгая проверка подписанных данных

Подписанные документы и токены разбираются до семантического использования:

- формат, размер, число сегментов и JSON/Proto structure ограничены;
- algorithm, `typ`, `kid`, `crit` и protected headers проверяются по закрытым
  множествам без algorithm inference;
- неизвестные и повторные поля отклоняются;
- ключ выбирается только по точной паре identity и `kid`;
- signature, issuer, audience, subject/caller, method, permissions, time bounds
  и transport identity проверяются как одна операция;
- bounded ошибка не содержит token, claims, key coordinates или diagnostics
  криптографической библиотеки.

Криптографические примитивы реализуются поддерживаемой библиотекой. Проектный
код задает строгий protocol wrapper, но не пишет собственную JOSE/TLS
криптографию поверх низкоуровневых primitives.

## Монотонное подписанное служебное состояние

Для распространяемого JWKS, trust manifest, policy snapshot и аналогичного
security state различаются:

- revision исходной публикации;
- revision вложенного key set или policy;
- generation signer certificate/chain, если она меняется независимо.

Изменение любой подписываемой части выпускает новую монотонную source revision.
Same-revision mutation и rollback запрещены.

Publisher:

- обновляет только заранее созданный exact resource;
- не имеет `create`, `delete` или доступа к несвязанным secrets;
- использует compare-and-swap по resource version;
- после записи выполняет точный криптографический readback;
- не начинает использовать новый ключ до подтвержденной публикации и
  propagation window.

Verifier хранит target-owned high-watermark revision и digest вне эфемерного
pod volume. Состояние переживает restart и обновляется атомарным CAS между
репликами. Caller не может создавать, удалять или изменять этот watermark.
Пустой локальный snapshot не разрешает принять более старое состояние.

## Проверяемый readback и повтор служебной mutation

Readback security state не принимает revision, digest, generation или
произвольный proof hash caller как доказательство фактически обслуживаемого
состояния. Точная workload/role identity выводится из transport/session
identity. Значения состояния разрешаются server-side из immutable pinned
intent, а cryptographic possession и served state проверяет независимая
boundary до выдачи одноразового immutable receipt. Publisher не может
выпустить consumer receipt, записать protected readback или сам подтвердить
собственную публикацию. Promotion блокирует pinned intent и полный набор
receipts/readbacks в одной transaction.

Если possession proof требуется по сети, verifier сначала выпускает
server-owned durable single-use challenge через отдельный versioned endpoint.
Request не задаёт workload, role, generations, audience, nonce или TTL.
Challenge до ответа CAS-сохраняется с exact pinned intent, transport identity,
credential JTI/digest, purpose, key generation/thumbprint, bounded TTL,
idempotency key и canonical request digest. Consume challenge и immutable
receipt выполняются атомарно. Exact retry после потерянного ответа возвращает
сохранённый challenge/receipt; другой digest, concurrent reuse, restart либо
смена replica не позволяют второй effect.

Credential разных control-plane purposes не переиспользуются. Restore
controller credential/ACK key и обычный readback credential/possession key
имеют разные explicit `typ`, единственный exact audience, signer trust,
delivery paths и lifecycle. Multi-audience token, cross-audience replay,
совпавший workload SPIFFE и permissive fallback не заменяют проверку purpose.
Каждый trust snapshot имеет собственные `CURRENT/NEXT/PREVIOUS`,
high-watermark, cryptographic served readback и readiness.

Credential rotation, которая использует отдельные login principals либо
application keys, допускает bounded `CURRENT`+`NEXT` и не более одного
`PREVIOUS`. `NEXT` обязан пройти независимый readback, пока `CURRENT`
продолжает обслуживать запросы. Promotion атомарно меняет статусы; revoke
прежнего principal происходит только после commit, overlap и readback нового
current. Boolean active-флаг с uniqueness, запрещающей overlap, недопустим.

Runtime process не подключается к PostgreSQL через shared `NOLOGIN`
capability role. Нужен exact non-superuser `LOGIN` principal с минимальным
membership, явным `SET ROLE` при `NOINHERIT`, Vault-owned выдачей/обновлением/
rotation/revocation credential, TLS `verify-full` с exact SNI/CA и
destination-pinned NetworkPolicy. Readiness проверяет effective privileges
реальным runtime principal; `SET SESSION AUTHORIZATION` superuser в behavior
contract не доказывает достижимость production boundary.

Независимый verifier подписанного snapshot не получает его trust signer из
того же publisher-controlled канала. Отдельный владелец доставляет exact
public JWK/certificate корня и его pinned RFC 7638 fingerprint независимо от
подписанного bounded `CURRENT/NEXT/PREVIOUS` bundle. Fingerprint без открытого
ключа не позволяет проверить подпись; public key из того же изменяемого
Secret, что и JWS, не является bootstrap trust. Verifier проверяет exact
`kid`/fingerprint/public key, затем root JWS, snapshot signer и только затем
snapshot JWS. Root rotation использует bounded overlap: доверенный старый root
подписывает exact новый public key и predecessor, новый root доказывает
possession встречной подписью, а target хранит source revision/digest
high-watermark. Пропущенная cross-signature, rollback, same-revision mutation
или gap отклоняются. Purpose, audience, config и high-watermark не
переиспользуются между restore и обычным readback. Readiness проверяет реально
обслуживаемый cryptographic readback, rotation, пропущенное обновление и rejoin
от target-owned anchor.

`NOLOGIN`, отзыв membership и смена пароля не прекращают уже открытую
PostgreSQL session. Поэтому каждая привилегированная function, RLS read path и
readiness probe связывает неизменяемый `session_user` с durable
generation/status и удерживает строку identity до завершения statement либо
transaction. Retirement сначала commit-ит server-side `RETIRED` fence, затем
выполняет `NOLOGIN`, отзыв membership, rotation и bounded drain/termination.
Прямое право на таблицу не может обходить fence: каждое чтение и изменение
security state либо выполняется через exact-signature `SECURITY DEFINER` API
с тем же `session_user` check, либо защищено `FORCE RLS`. Список проверяется
по effective grants, включая уже открытые sessions, а не только по startup
principal attributes.
Если action захватил fence первым, он может commit до retirement; если первым
commit-нулся retirement, action и retry закрыто отклоняются. Crash action
освобождает lock, после чего retirement завершается, а retry видит `RETIRED`.

Владелец lifecycle database credentials является реальным deployable, а не
строковой меткой. Он имеет versioned interface или детерминированный
reconciliation job, отдельный ServiceAccount, exact Vault/PostgreSQL rights,
fenced leader/CAS, crash recovery, served readback и readiness. Он получает
desired principals/generations из versioned registry и не принимает их как
authority из RPC request.

One-time signature/JTI не отменяет semantic idempotency state-changing RPC.
Одна durable CAS transaction хранит idempotency key, canonical request digest,
JTI и полный принятый result/receipt. Exact retry после потерянного ответа
возвращает сохранённый результат без повторного effect; тот же key либо JTI с
другим digest является replay/security incident. Multi-replica coordinator
хранит canonical записи каждого принятого элемента, достаточные для
детерминированного восстановления partial set после смены leader; digest либо
count без состава set недостаточен.

## Ротация, пропуск обновлений и forward recovery

Ротация является протоколом, а не заменой файла. Он обязан закрывать:

- независимую смену authorization key и signer certificate;
- временный overlap старого и нового verification material;
- crash до и после публикации;
- истечение прежнего signer до восстановления publisher;
- пропуск одного или нескольких промежуточных обновлений verifier;
- same-revision mutation, реальный rollback и разрыв истории.

Историческая подпись может проверяться в доказанный момент публикации только
для подтверждения уже принятого predecessor. Текущий publisher одновременно
обязан иметь валидный сейчас signer. Новая публикация криптографически
связывается с ранее принятым digest.

Если transport хранит только последнее состояние, подписанный документ несет
bounded историю revisions/digests либо другое доказательство пути до
target-owned anchor. История:

- входит в подписываемый payload;
- имеет строгий порядок и уникальные revisions;
- содержит непосредственного predecessor;
- позволяет rejoin только по точной паре revision/digest;
- закрыто отказывает, если target отстал дальше утвержденного окна.

Ручное удаление watermark, обнуление revision или публикация неподписанного
bootstrap snapshot не являются recovery.

## TLS и доставка секретов

Секреты, private keys, tokens и broker credentials доставляются только через
проверяемый TLS:

- server-side TLS включается до перевода клиентов на HTTPS;
- клиент проверяет exact SNI/hostname и публичную CA;
- `skipTLSVerify`, plaintext fallback и доверие произвольному системному CA
  запрещены для внутреннего security path;
- secret value не помещается в manifest, log, metric, trace или CLI output;
- workload получает только минимальный набор keys через отдельный
  namespace-local auth graph.

Переход с plaintext на TLS выполняется системно: сначала инвентаризируются все
активные клиенты, затем им доставляется CA и exact egress, после этого
переключается server listener и последовательно проверяются connection, auth и
secret reconciliation. Исправление только одного нового клиента при поломке
существующих consumers запрещено.

## Ротация TLS

Обновление Kubernetes `Secret` не доказывает, что процесс начал выдавать новый
сертификат. Для сервера с runtime reload задается явная state machine:

```text
applied -> pending -> reload -> exact peer readback -> applied
```

- последний подтвержденный `applied` сохраняется до успешного readback;
- `pending` записывается атомарно и переживает restart reloader;
- после ошибки reload повторяется, а readiness остается отрицательной;
- readback сравнивает фактически выдаваемый DER leaf с mounted certificate при
  обязательных SNI, hostname и CA checks;
- private key отдельно проверяется на соответствие leaf;
- только exact match атомарно продвигает `applied`.

Доверенная CA меняется через overlap:

1. выпускается независимый резервный CA key;
2. bundle со старой и новой CA доставляется и проверяется у всех clients;
3. server leaf переводится на новую CA;
4. после завершения overlap старая CA удаляется отдельным изменением.

Автоматическая ротация ключа CA без overlap запрещена. CA и leaf имеют разные
lifecycle и manifests.

## Сетевая изоляция

Default-deny policy разрешает только фактические runtime paths.

- Правило egress без `to`/destination запрещено для database, broker, Vault,
  telemetry и Kubernetes API.
- Runtime и migration jobs получают отдельные destinations и credentials.
- Внешний SaaS доступен через allowlisted egress proxy, если точный IP-range не
  является устойчивым контрактом; открытый HTTPS egress не используется.
- Итоговый environment render проверяется после всех overlays. Исходный base
  или patch не доказывает результирующую policy: списки могут заменяться
  целиком.
- Разрешенный selector обязан ссылаться на реально принадлежащий deployable и
  достижимый `Service`, а не на предполагаемый pod.

Kubernetes API не описывается переносимым `podSelector` на control-plane
component. Policy строится из фактического Service ClusterIP и ready endpoints
целевого контура либо через отдельный утвержденный egress gateway. Значения
одного контура не переносятся в другой.

## Полнота deployable

Sidecar, verifier, issuer, publisher, reconciler, migration job и egress proxy
считаются частью production path только при наличии:

- исходного кода и воспроизводимого OCI artifact;
- registry entry и однозначного owner;
- ServiceAccount, минимального RBAC и namespace;
- volumes, access modes и доставки trust/secret material;
- exact ingress/egress;
- startup/readiness/failure policy;
- bounded shutdown и порядка закрытия зависимостей;
- code-first apply order, диагностики и rollback/repair runbook.

`emptyDir`, который некому заполнить, ссылка на отсутствующий `VaultAuth`,
selector несуществующей базы или manual ConfigMap не являются допустимой
production dependency.

## Доказательство результата

MatterCodex выбирает формат проверок по `GOV-DOC-003`, но review должно иметь
воспроизводимые доказательства:

- полный путь от issuer до domain decision и негативные исходы;
- restart, rollback, replay, expired signer, пропуск revision и неудачный
  reload;
- exact final Kubernetes/Helm render, а не только исходные fragments;
- отсутствие secrets и чувствительных coordinates в diff и диагностике;
- readback фактически обслуживаемого certificate/state после ротации;
- соответствие исправления всем системным аналогам, а не одной отмеченной
  строке.

Статус `resolved` у review thread подтверждает завершение обсуждения, но не
заменяет проверку diff и фактического failure path.

## Запрещенные упрощения

- доверять identity или ownership из request payload;
- считать mTLS полной авторизацией при наличии дополнительных auth layers;
- хранить replay/rollback protection только в памяти или `emptyDir`;
- принимать same-revision content mutation;
- сбрасывать security high-watermark для восстановления;
- считать обновленный Secret доказательством runtime reload;
- использовать `skipTLSVerify`, plaintext secret transport или wildcard egress;
- заполнять обязательный runtime volume вручную;
- заявлять production path без реального issuer/publisher/verifier;
- исправлять новый client ценой отказа существующих consumers.

## Проверенная документация

При подготовке документа через Context7 проверены:

- `/nats-io/nats.docs` — JetStream acknowledgement, quorum и влияние
  `sync_interval`/`fsync` на потерю уже подтвержденных сообщений;
- `/websites/developer_hashicorp_vault` — TLS TCP listener, неизменность путей
  certificate/key при reload и необходимость restart при изменении listener
  configuration;
- `/kubernetes/website` — selectors и destinations `NetworkPolicy`, а также
  discovery Service endpoints через `EndpointSlice`.

Связанные документы: `AGENT-DOC-001`, `GO-DOC-001`, `GO-DOC-003`,
`GO-DOC-004`, `GO-DOC-005`, `GO-DOC-006`, `INFRA-DOC-001`.
