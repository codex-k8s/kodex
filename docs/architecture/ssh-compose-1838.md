---
id: ARCH-SSH-COMPOSE-1838
title: Защищённая доставка Compose через SSH
type: architecture
status: approved
owner: architect
version: 1.0.2
updated: 2026-10-27
---

# Границы поставки

Issue #1838, эпик #1830. Решение владельца, переданное manager24.10.2026:
использовать существующий integration-gateway, exact renew/fencing,
lease-bound SSH credential projection, durable remote journal/watchdog и
CP-owned merge receipt. Нового внешнего сервиса и универсального remote exec нет.
Этот документ фиксирует утверждённый контракт, а не результат его приёмки.

Connection определяет один project/repository, public IP, TCP22,
непривилегированного remote user, exact host key, опубликованный loopback port и
reference-only platform secret. Actor/project/root назначает CP; config locator
и input модели не являются authority. Public IP попадает в exact /32 либо /128
environment render; неподготовленное назначение закрыто недоступно.

# Карта сценариев

| Инициатор/операция | Полномочия и endpoint | Owner и потребитель |
| --- | --- | --- |
| Владелец создаёт connection | OIDC/CSRF + integration.manage, штатный IntegrationConnection API; secret.view в exact project | CP проверяет config/secret ownership, OCC/idempotency/audit/event; PWA читает только metadata |
| Inspect сотрудника SRE | pinned runtime grant → invoke_integration → ResolveIntegrationInvocation | CP разрешает target и источник из успешного exact merge invocation; worker читает source через отдельный GitHub credential |
| Deploy сотрудника SRE | exact inspect receipt → Human Gate одного эффекта | CP связывает merge SHA/tree/bundle/target/secret revision; SSH worker получает короткую lease и подписанный permit |
| Credential projection | mTLS + bearer + exact operation/lease/fence/generation | Secret Broker повторно спрашивает CP, читает только pinned Secret и возвращает key только worker |
| Remote effect | exact public IP + host-key pin + owner permit | forced-command helper проверяет scope, durable generation/effect и source tree, запускает фиксированный Compose |
| Результат | receipt target/SHA/digests + actual HTTP health | worker CompleteIntegrationInvocation атомарно фиксирует effect receipt/event; UI/сотрудник получают safe projection |

# Источник релиза

`ssh.compose.inspect` использует успешный CP-owned receipt реального
`github.pull_request.merge`. Project/repository, SHA и source connection
разрешаются сервером, не извлекаются из текста модели. Worker читает exact
commit/tree/blobs через api.github.com, запрещает truncated tree, symlinks,
submodules, traversal и превышение bounds. Полный source bundle и его digest
связаны с immutable inspect receipt. Deploy повторно проверяет source
connection/grant и bytes; revocation или недоступность не разрешают fallback.

`kodex-deploy.json` содержит уже готовый runtime image по digest и argv
небольшого Python/Node приложения. OCI builder и сборка на1ГБ хосте не нужны.
Helper создаёт Compose, монтируя только собственный source directory readonly
и отдельный writable state volume. Namespace/daemon Docker принадлежит
непривилегированному rootless deploy user. Bootstrap выполняется repo-owned
кодом установки, credentials и сервером управляет только root QA-агент.

# Матрица жизненного цикла

| Переход | Инвариант и событие/read path |
| --- | --- |
| create/resolve | CP проверяет current actor/project/root/grant и immutable input; одна owner-транзакция создаёт invocation/audit и прежний integration event |
| WAITING_APPROVAL | Exact target/release показаны владельцу; approval не меняет input либо owner |
| reject/expiry gate | Прежний terminal invocation/Gate transition; effect не запускается |
| claim | Exact workload/instance, свежие lease/fence/generation и source/secret eligibility; worker не получает owner PAT либо cluster-admin |
| renew | Та же attempt/generation, monotonically extended short expiry до фиксированного hard deadline; cancel/revoke/terminal запрещают новый permit |
| complete | Exact lease и immutable effect/input/release receipt; success требует actual health; атомарный прежний integration event/continuation |
| disconnect/worker crash/lease expiry | Write → UNKNOWN_OUTCOME; journal запрещает повтор effect; новый claim только RECOVER_READ_ONLY |
| cancel/revoke/root terminal | Owner закрывает lease; EOF/watchdog прекращает remote attempt. Возможный внешний partial effect остаётся UNKNOWN, root не оживает |
| read-only recovery | Exact owner-authorized receipt read; отсутствие ответа не доказывает отсутствие эффекта; новые mutations запрещены |
| retry | Новый intent/attempt только после разрешения предыдущего UNKNOWN; прежний effect key не меняется при неопределённом ACK |
| reboot helper/host | Устойчивый journal сохраняет reservation/fences; old permit не возобновляет Compose |
| delete/purge проекта | Прежний owner terminal/purge graph отзывает grants; произвольное удаление приложения на внешнем сервере не подразумевается |

Общая lease/operation policy других adapters не расширяется. Длинный SSH
consumer имеет собственный bounded путь и renew; старые invocations не
переинтерпретируются как SSH. Context7 отклонил авторизацию; проверены
официальные API `golang.org/x/crypto/ssh`, `ssh/knownhosts` и Docker Compose.

# Историческая контрольная точка25.10.2026

Пакет был зарегистрирован с `readiness: NOT_READY`: наличие кода не разрешает
использовать его до сквозной приёмки. Эта историческая контрольная точка сохраняет прежнюю
реализацию, а не подтверждает завершение unit. PostgreSQL component-контур
завершился `FAIL`; миграции прошли, lease/custody/concurrency полностью не
проверены. Не выполнены совмещение с
SERVER_DAG_V1 #1836 и его accepted dependency/rejoin proof, изолированная
сквозная проверка SSH/broker/worker и live-приёмка выделенного target.

Обязательная последующая проверка purpose `SSH_COMPOSE` включает запрет ENV,
attach/reveal, старые опубликованные RuntimeRevision/ENV, ротацию и гонку
binding↔reservation. При смене target/source connection, grant либо SSH
revision owner-транзакция закрывает зависимые SSH leases; RUNNING WRITE
остаётся UNKNOWN_OUTCOME. Health-test connection при активной зависимости
возвращает conflict, сохраняя текущую lease.

Compose использует `restart: no`. Boot helper запускает только существующий
container точного project/image/resources/readonly source и собственного state
volume с устойчивым SUCCEEDED/health receipt. Неопределённый старый deploy при
reboot не возобновляется. Процесс остановленного worker не является rollback
уже созданного приложения.

Bootstrap пока требует отдельной операторской проверки: preflight/apply,
частичный PENDING, readback, rollback, повторный boot и Ubuntu24 rootless
AppArmor/cgroup prerequisites. Loopback published port не является публичным
HTTP endpoint; публикация через проверенный операторский proxy остаётся
отдельным требованием приёмки целевого окружения. Код не меняет firewall.

# Передача релиза Go+SQLite: BLOCKED

Последнее уточнение владельца25.10.2026: QA приложение использует Go+SQLite,
сборка выполняется вне Ubuntu1CPU1GB; private test-repo не разрешает публичную
публикацию сервисного образа. Этот выбор не подтверждает готовность поставки.

Исполняемый путь читает только `kodex-deploy.json` в корне exact merged Git tree.
Формальный manifest: `contracts/integrations/v1/ssh-compose-release.schema.json`.
Обязательны `image` (полный OCI reference с digest), `command` (argv),
`containerPort` и `healthPath`. Image должен совпадать с owner-configured
`Target.RuntimeImage`. Приложение слушает `PORT`, пишет SQLite только под
`STATE_DIR=/state`, отдаёт HTTP200 на healthPath. `/app` — readonly source mount;
готовый Go binary в сервисном image должен находиться вне этого mount, например
`/usr/local/bin/app`. На target нет `build` и установки зависимостей.

Source bundle ограничен2MiB/256files/256KiB-file и не доставляет готовый Go
binary. Public base image не заменяет сборку и доставку binary. Для private
сервисного OCI пока отсутствуют доказанный off-host build того же merged SHA,
CP-owned provenance/readback exact image digest и lease-bound private pull
auth/custody. Helper использует фиксированный пустой Docker client config;
PAT проекта и ownerPAT на target не передаются. Публичная публикация private
кода и fallback на иной image запрещены как обход отсутствующего пути.

GitHub6.0.0 объявляет typed Actions workflow/run/job операции. Их наличие не
доказывает CI: SRE grants на эти операции не выданы и live workflow/artifact
receipt отсутствует. Этот путь допустим только для отдельной проверки build и
private delivery; он не добавлен настоящим unit. Изменение RuntimeImage также
меняет TargetRef, поэтому обновление immutable helper pin требует явной
операторской перенастройки. Автоматическая смена target либо adoption старого
bootstrap не выполняется. Фактический PDLC deploy Go+SQLite остаётся BLOCKED.

# Фактические проверки контрольной точки25.10.2026

Исходный committed checkpoint: `c01f03134d9b2afad56f1c7536d79653577f72bf`.
PG3 выполнялся на его28 изменённых/новых исходных файлах до последующих правок
документации и отдельного изменения file cap. Source manifest:
`postgres-3-source-manifest.json`, SHA256
`94dab0de7bb471d8ca23b1b30388f39524b5b8954560e28654ab0cc2dc8d152d`.
Полные локальные receipts хранятся в
`/home/s/.cache/kodex-qa-20261024/ssh-1838/`; они не содержат credentials.

| Проверка | Фактический результат |
| --- | --- |
| CP repository/app/gRPC compile; helper unit; API SSH scope/gate projection; gateway SSH release/loopback transport unit | `PASS`, `pre-pg3-checks.json`, все4 exit0 |
| StrictNamedArgs18 SSH queries/21 call sites; release manifest1 positive/7 negative | `PASS`, `pre-pg3-sql-args.json`, `release-manifest-schema.log` |
| OpenAPI/frontend codegen; frontend typecheck; service identity5 tests | `PASS`, локальные receipts до PG3 |
| Full environment render обоих профилей | `FAIL`, неизменённый baseline `backup-controller` с нулевым release revision; exact SSH /32,/128 TCP22 и CP-only signer projection assertions прошли; `environment-render4.log` |
| SSH PG1 | `FAIL`: DB adapter CHECK; forward migration расширена закрытым SSH adapter/kind, последующие migration chains прошли |
| SSH PG2 | `FAIL`: fixture передавала transport UUID в public-ref read; fixture исправлена, PG3 прошёл эту стадию |
| SSH PG3, handle95012, exit1 | `FAIL`, `postgres-3.log`: полный migration chain прошёл; write-only SAVE→VALIDATE→PUBLISH и exact completion replay прошли; остальные сценарии остановились до проверяемого lifecycle |
| PG cleanup | После каждого terminal контейнеры fixture отсутствуют; данные target/staging не использовались |
| SERVER_DAG_V1 accepted dependency/rejoin, browser, bootstrap/target/live deploy | `NOT RUN`; private Go+SQLite image delivery также `BLOCKED` |

Доказанный оставшийся production blocker: `validateSSHComposeConfiguration`
передаёт `integration.manage` цель `RESOURCE_INSTANCE/PROJECT`.
`access.Permission("integration.manage")` допускает только `INTEGRATION`,
а `access.Evaluate` отклоняет несовпадение `ResourceKinds` до любой binding.
Поэтому создание/изменение SSH connection возвращает `forbidden` и для owner.
На историческом SHA этот guard не был исправлен после PG3: разрешённый лимит трёх PG-попыток исчерпан.
Health generation/rotation/revoke/delete/old completion и обе гонки
binding↔reservation не достигнуты, их нельзя считать `PASS` либо доказательством
нарушения custody. Тест с ожидаемым conflict получил более ранний forbidden,
а не успешное принятие ранее раскрытого ключа.

Остаются отдельные границы до готовности: accepted dependency/rejoin proof
#1836 вместо одной project-bound merge связи; безопасная проекция exact merged
SHA/bundle в Human Gate; проверка read-only recovery после ротации/revocation.
Последняя — непроверенная гипотеза ограничения доступности, не доказанный
fail-open. Нужны новое решение владельца по authorization/PG и отдельная
сквозная приёмка; историческая readiness оставалась `NOT_READY`. Push/PR/deploy тогда не выполнялись.

После PG3 отдельно увеличен только bounded file cap128→256: существующий
разрешённый шаблон содержит141 blob и раньше отклонялся до приложения.
Новые синтетические fixtures полного дерева на141 и256 файлов принимаются,
257 файлов отклоняются независимо в `Bundle.Canonical` и gateway Git-tree
reader. Byte/path/blob/tree/image bounds сохранены. Полный helper unit и
узкий gateway release unit прошли последовательно, exit0 (1.970с и5.982с),
`file-cap-checks.json`. Эти unit не проверяли исправленную авторизацию:
она не изменялась. File cap и документация отличаются от source manifest PG3;
результат PG3 не переносится на последующий SHA как успешный PG-контур.

## Forwardport на SERVER_DAG_V1 (27.10.2026)

Миграция `20261027000100_ssh_compose_runtime.sql` применяется только после
`20261026001000`. Изменения003..010 не переписываются. Профиль `ssh-compose`
повторяет все schema/ACL/bundle/purge проверки predecessor010 и отдельно
проверяет purpose, bounded immutable snapshot, closed enums и guards SSH.
Операторские check/stop/readback/inspect/resume остаются штатными; результат
нового metadata proof до native запуска имеет статус `NOT RUN`.

В server DAG merge не выбирается как последний успешный вызов проекта.
Декларация DEPLOYMENT задаёт SubjectStepKey; immutable вход должен ссылаться
на accepted INTEGRATION_RECEIPT точного merge того же root и repository.
Inspect и deployment связываются с exact origin RuntimeRevision/lease/generation,
а не max generation. Все source READ grants, SSH purpose/revision, target,
image и Git object pins проверяются заново владельцем перед выдачей permit.
Обе проверки APPLICATION в environment publish и historical admission
сохраняются; SSH credential не становится переменной окружения модели.

Known-success deploy повторно не исполняется после business rejection либо
owner-approved fresh attempt. Используются существующие logical effect intent
и full execution rejoin003/009/010 с exact release/admission digest; второго
receipt registry нет. UNKNOWN допускает только STATUS/readback; итоговое
SUCCEEDED без remote receipt и health не принимается. Локальный native provider
fixture сообщает synthetic результат, а не доказывает настоящий SSH/health.
Gate показывает owner-assigned merged SHA, tree SHA, bundle SHA256 и image
в безопасном shared preview; SSH secret descriptors и private execution pins
в public Gate не выдаются. Legacy ordinary runs сохраняют прежний release key.

Source readiness `READY` означает наличие зарегистрированного typed consumer.
Это не live acceptance: до активации нужны совместимые CP/API/secret-broker/IG,
новая authority policy revision99, отдельная owner signing key custody,
pinned remote helper и проверенный exact target. Secret.purpose и новые
snapshots после записи требуют совместимого forward recovery; downgrade,
удаление purpose либо очищение истории не являются rollback. Три исторических
PG FAIL/STOP сохранены в evidence, новый план выполняет root отдельно после
review зафиксированного SHA. Настоящие SSH/Compose/модельные проверки пока
`NOT RUN`; private Go/SQLite build/provenance/pull prerequisites не подменяются
публичным base image или host shell.


## Исправление admission и сохранение Git mode

Подтверждённые source findings контрольной точки `8f29b9a50` сохраняются в
истории review. Successor проверяет canonical `integration.manage` root actor
отдельно для GitHub source connection при prepare, claim, renew и broker
projection. Owner назначает actor из root graph; payload и сохранённые agent
READ grants не заменяют это право. DEPLOY подтверждает renew перед Git release
read и повторно после него; INSPECT сначала подтверждает broker admission,
затем читает Git один раз до SSH. STATUS не читает новый release.

`admissionBlocked` и `admissionBlockReason=CURRENT_ADMISSION_DENIED` — отдельные
метаданные допуска в существующем GetIntegrationInvocation. Причина не является
provider error, receipt или доказательством отсутствия effect. Прежние state,
safeErrorCode, result, receipt, snapshot и execution generation не меняются.
В начальном профиле нет unblock/rebind: возврат полномочий не возобновляет старую
попытку. Для нового намерения нужен отдельный явный owner сценарий; текущая
реализация поддерживает только Cancel исполнения с сохранением UNKNOWN.

| Сценарий | Owner-транзакция и очередь | Consumer |
| --- | --- | --- |
| READY либо UNKNOWN, все текущие pins и ACL допустимы | Прежний claim/permit; UNKNOWN получает только STATUS | Прежний результат, marker отсутствует |
| Постоянный Forbidden/NotFound target/source/package/pins | Откат candidate savepoint, exact CAS исходного invocation, monotonic block, один audit и TURN_PROGRESS | READY немедленно возвращает attention; UNKNOWN сохраняется вместе с closed reason |
| Страница32 кандидатов полностью отклонена | COMMIT всех marker без claims; следующий poll получает следующий prefix | Это успешная пустая страница claim, не provider success |
| Blocked invocation, доступ восстановлен | Selector исключает marker до LIMIT; lease и новое поколение не выдаются | Причина остаётся видимой; автоматического повтора нет |
| Context/Unavailable/serialization/root lock contention | Marker не фиксируется; транзакция откатывается | Прежний typed transient отказ |
| Owner Cancel | Закрывает root/leases; UNKNOWN и исходный receipt не подменяются | История показывает отмену исполнения отдельно от неизвестного эффекта |
| Повреждённый marker либо неизвестная reason | Get/caster/RC закрыто отклоняют результат | Нет успешного receipt или polling READY до timeout |

Candidate savepoint начинается до target actor ACL. Marker привязан к exact
invocation/state/generation/input digest/snapshot и root; root lock берётся
NOWAIT, чтобы не ждать обратный root→invocation lock order. Claims, permanent
markers, audit и events принадлежат одной owner-транзакции. Marker исключается
до LIMIT, а не только в памяти worker. Dynamic scan не используется как замена
устойчивому progress.

Git entry100644 материализуется как0644,100755 — как0755 независимо от umask.
Mode задаётся на новом открытом descriptor. Immutable replay проверяет и bytes,
и mode без исправления существующего файла. Связь commit/tree/blob сохраняется;
symlink, иной Git mode, shell fallback и произвольное повышение прав запрещены.

Новые native fixtures создают бизнес-состояние только штатными командами:
rotate/reconnect; ограниченный root actor с отозванным source ACL; prefix32
отклонённых invocations и независимый healthy target. Проверяются atomic marker,
one audit/event, неизменность UNKNOWN и прогресс следующего poll. Отдельные
schema clones проверяют отсутствующий admission CHECK и отключённый guard.
Само наличие этих fixtures не означает их исполнения: до root native receipt
их статус `NOT RUN`. Proto codegen, module suites, browser и current011 migration
proof привязываются к окончательному SHA отдельно; исторические FAIL/STOP не
переписываются. До actual011 metadata операторский SSH predecessor010 остаётся
историческим профилем, а не готовым планом поставки после011.


Поставка successor выполняется при остановленных источниках SSH claims и без
активных SSH consumers. DDL применяется штатным отдельным checkpoint. API,
PWA и runtime-controller с пониманием marker/event поставляются до активации
нового CP/IG writer; полная совместимость подтверждается exact readback всех
реплик. Старый RC иначе продолжал бы ожидать READY, а старый API не знает
новую closed summary. После первого admission marker либо соответствующего
event возврат к несовместимому consumer не является допустимым rollback:
нужен совместимый roll-forward; marker, UNKNOWN и историю не очищают.
