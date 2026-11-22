---
id: OPS-DOC-PAIRED-TOKEN-RELEASE-2067
title: План адресной парной поставки CP и Integration Gateway
type: operations
status: approved
owner: developer
version: 1.0.4
updated: 2026-11-22
---

# План адресной парной поставки CP и Integration Gateway

Источники: [#2067](https://github.com/codex-k8s/kodex/issues/2067),
[#2199](https://github.com/codex-k8s/kodex/pull/2199), `GUIDE-DOC-003`,
`GUIDE-DOC-004`, `GUIDE-DOC-006`, `GOV-DOC-003`.

Это начальный контур **планирования**, не готовый оператор развёртывания.
Единственный профиль `PERSONAL_TOKEN_ALLOWANCE_SAME_SCHEMA_051` объединяет
`control-plane` и `integration-gateway`. Версии CP/IG согласуются для нового
`authorized_until`; старый owner response не разрешает новый dispatch.
Прежние forward profiles и stock installer остаются закрытыми для такого плана.

`make test-paired-token-release-plan-local` запускает ограниченные synthetic
проверки без Kubernetes, PostgreSQL, Docker и пользовательских credentials.
Тесты не являются установленным доказательством поставки.

## Сценарий и authority

Инициатор — оператор в уже разрешённой установке. Manifest выбирает exact source,
ожидаемые UID/images и scope; сам manifest не является полномочием Kubernetes,
owner database или release. Публичного HTTP endpoint либо mutation RPC нет.

`validatePairedTokenReleaseManifest` принимает только versioned manifest с двумя
компонентами в фиксированном порядке. Scope включает ровно CP/IG Deployments,
CAG Deployment и его Service. Для пары нужны завершённые
`validatedStablePublication` receipts одного source/Git context, exact images
в прежнем registry и ожидаемые full-spec fingerprints. Старый IG, сторонняя
workload, raw patch, migration command и неизвестное поле отклоняются.

`readPairedTokenReleaseOwnerFromDatabase` переиспользует exact PostgreSQL
Pod/StatefulSet boundary и существующие owner socket adapters. Он читает
канонический composed catalogue CP051 и отдельный фиксированный authority head,
затем проверяет существующий актуальный authority110 baseline через прежние
rotation receipts и owner status. Reader не создаёт Job и не запускает rotation.
Другая database, PG instance, Pod UID, installation seal либо истёкшая свежесть
закрывают результат. Его полное positive installed выполнение ещё не проверено.

Внутренний `readPairedTokenReleaseOwner` имеет adapter ports для synthetic
проверок. Его результат привязан к exact manifest и ограничен 30 секундами с
первого чтения. Сериализованный JSON не принимается обратно как owner proof.
Adapter ports не являются публичным API для выдачи release authority.

Текущие heads CP051/authority150002 проверяются отдельно от historical
`kodex-installation-profile`: `portableRunnerTarget` сохраняет его точные UID и
data hash. Исторический installer profile не объявляется актуальным schema
proof. Новая квитанция bootstrap не создаётся; пять прежних bootstrap effects
не переигрываются. Reader не подтверждает heads остальных неизменяемых сервисов
и не заменяет полный stock image-only contract #2067.

`planPairedTokenRelease` требует текущие positive CAG close и Service isolation
с их оригинальными журналами и UID. Он переиспользует serving lineage/template
validator, проверяет все текущие sidecars/statuses, instance grants, source и
node/CRI image pins. Даже owned Pod со снятой label остаётся в lineage.
Предложенные specs меняют только application image и component-source annotation.
План сохраняет исходные full specs, digests и Pod UID; patch не выдаётся.

`verifyPairedTokenReleasePlan` повторяет проверки с новым owner read. Структурная
подмена сохранённого плана, иной source/scope/spec и искусственный `APPLIED`
отклоняются. Новый read не продлевает права старого execution attempt.

## Read-only CLI

`tools/release/paired-token-release-cli.mjs` поддерживает только `plan` и
`status`. `apply`, `rollback`, `up` и произвольные команды отклоняются до чтений.

```bash
node tools/release/paired-token-release-cli.mjs plan \
  --input /absolute/private/input.json \
  --kubeconfig /absolute/private/explicit-kubeconfig \
  --output /absolute/private/plan.json

node tools/release/paired-token-release-cli.mjs status \
  --input /absolute/private/input.json \
  --kubeconfig /absolute/private/explicit-kubeconfig \
  --plan /absolute/private/plan.json \
  --output /absolute/private/status.json
```

Input — owned private JSON `kodex.paired-token-release-input/v1`. Он содержит
`manifest`, `source` с exact `path/revision`, expected `nodes` с `name/uid`,
`authority` с исходными `planPath/evidencePath`, `admission` с исходными
`planPath/evidencePath/trafficPlanPath/trafficEvidencePath`. Manifest дополнительно
закрепляет `apiSpecSHA256` фактического неизменяемого CAG. Подменять исходные
rotation/API plans новой четырёхресурсной областью нельзя: CLI сохраняет их
оригинальные hashes и broad target, проверяет common installation seal и
source ancestry. Новая область mutation по-прежнему содержит только CP/IG.

Source/Git context вычисляются из clean checkout, registry receipts проходят
прежний validator, node adapter вызывается только в `readback`. OCI archives CP
и IG различаются; каждый сохраняет собственный publisher archive digest. До и
после Gateway discovery проверяются прежние owner/backend pins, затем backend
перечитывается ещё раз после последнего Gateway snapshot. Повторный owner read
не продлевает первый 30-секундный ceiling. Перед возвратом снова сверяются source
и bytes исходных journals. Component serving digests сохраняются отдельно.

Output создаётся через `wx`, mode0600, file/directory fsync в owned directory0700.
Лимит каждого output — 4 МиБ фактического compact JSON; aggregate input journals
ограничены 16 МиБ, один private input — прежними 4 МиБ. Повтор с тем же output
путём не перезаписывает файл даже после неизвестного ACK локальной записи.
`status` создаёт новый read-only artifact и не меняет исходный plan.
Stdout содержит только state, plan hash и component UID/images. Spec, SQL result,
пути private journals и kubeconfig не печатаются.

Positive CLI проверен с явно synthetic adapter ports. Установленный positive
factory с native PostgreSQL/CRI и исходными authority110 receipts — **NOT RUN**.
Результат остаётся `PLAN_ONLY`, без разрешения изменять Deployment.

## Матрица жизненного цикла

| Этап | Результат и ограничение |
| --- | --- |
| Валидация manifest | Закрытая пара и неизменные heads; эффекта нет |
| Чтение owner | Только свежий CP051/authority110 read; ошибка не выдаёт proof |
| Capture плана | `PLAN_ONLY`, `BLOCKED_LIFECYCLE_NOT_IMPLEMENTED`; эффекта нет |
| Повторный read | Новый readback той же пары; не `APPLIED` и не новый допуск |
| Stop старого IG | Receiver проверяет API/native join; actual native factory не подключён |
| Stage/resume пары | Шестишаговый receiver проверен synthetic; реальное обновление Deployment недоступно |
| UNKNOWN после mutation | Receiver допускает только exact readback; UNKNOWN/BEFORE не вызывает второй CAS |
| Rollback | Есть synthetic paired receiver; нужен отдельный pinned compatibility receipt, actual producer не подключён |
| API reopen | Пока не реализован; исторический migration/rotation terminal не заменяет результат этой пары |

Ни одно перечисленное чтение не публикует domain event: authoritative read path —
повтор исходных owner/API/serving проверок. Plan не содержит effect receipt и
не должен передаваться старому migration CLI как другой schema/profile.

## Границы следующего шага

`paired-token-release-receiver.mjs` — следующий локальный исполняемый элемент,
не Kubernetes apply. Он использует существующие `privateJournal`, serving
lineage/template validators и fixed последовательность:

1. остановка IG и положительное завершение его прежних Pod UID;
2. остановка CP и положительное завершение его прежних Pod UID;
3. image-only handoff CP в нулевых replicas;
4. image-only handoff IG в нулевых replicas;
5. запуск CP и exact readiness;
6. запуск IG и exact readiness всей пары.

Cas request выводится из frozen full specs, UID и текущего RV. Raw patches,
произвольные имена workloads и команды из JSON не принимаются. Нативные ports CAS и forward join проверены по своим закрытым контрактам.
CLI `plan/status` пока не подключает их к операции записи. Для поставки
требуется общий исполнитель с terminal proof и восстановлением доступа к API.

Journal имеет exact record/source/target header и process-exclusive lock.
`advance/status` дополнительно используют общий singleflight одного journal
handle: concurrent/reentrant вызов немедленно получает BUSY и не ставится в
очередь после возможного UNKNOWN. После await сверяются прежние prefix/INTENT.
`BOUND` сохраняет первоначальный owner/backend. До каждого CAS проверяются
прежняя API isolation, fresh owner и source; после durable INTENT повторно
проверяется тот же первый ceiling. Сначала сохраняется INTENT, затем actual ACK
с immutable UID/RV, затем положительный readback. Неподтверждённый CAS никогда
не повторяется. UNKNOWN/BEFORE остаётся UNKNOWN даже после восстановленного
допуска. ACK либо отсутствие Pod сами по себе не заменяют native join witness.
Receiver завершает dry-run и полное чтение owner до первого native join.
Для краткоживущего witness остаётся первоначальный 5-секундный monotonic
предел, закреплённый до чтения. Предел учитывает повторные snapshots и чтения
после join, fsync INTENT, внешний CAS и фиксацию ACK. В resume ACK записываются
готовые Pod UID последнего снимка. Запоздалый readback с меньшим resourceVersion
закрывает квитанцию. STOP сверяет исходный набор Pod UID перед INTENT и
после установления TLS перед PATCH. При завершении пары неизменность обоих
наборов resume UID проверяется по последнему снимку готовности.
Read-only native join и ограниченный по времени CAS описаны ниже.

Повтор после process restart сравнивает только определённые поля, не требует
совпадения созданного `privateJournal` timestamp с новым временем. Потеря ACK
после принятого CAS разрешает только exact UID/spec/RV readback без повторной
записи. Прерванная запись pair receipt после шестого ACK восстанавливается по
fresh readiness/owner. История ограничена 64 closed events для одной пары;
poll не добавляет бесконечные observation rows.

Rollback возможен только после полного forward pair receipt и нового
`readRollbackCompatibility` для exact source, обоих прежних image/source pins,
той же схемы и исходного forward receipt. Совместимость не выводится из
бывшего healthy состояния, caller boolean или наличия preimages. Receipt
закрепляет source review hash и не remint после начала rollback. Его actual
producer пока отсутствует. Rollback выполняет те же шесть шагов к обоим
исходным specs; незавершённый UNKNOWN forward не получает обхода.

`make test-paired-token-release-receiver-local` проверяет эти сценарии на
in-memory Kubernetes с настоящими owned private journals. Успех заканчивается
`PAIR_READY_TERMINAL_BLOCKED`, а не разрешением API reopen или deployment PASS.
Native effects, authoritative compatibility admission и whole release unit
остаются **NOT READY**.

### Нативный CAS для локального k3d

`createPairedTokenNativeCAS` предоставляет два ports для receiver. В dry-run
Kubernetes API получает один PATCH с `dryRun=All`. Persisted PATCH требует
одноразового допуска из того же открытого журнала после fsync INTENT. Допуск
связывает полный запрос, scope и исходные сроки owner и join. До записи после
TLS повторно проверяются исходник, изоляция API и срок допуска. Успешный
транспортный ответ остаётся промежуточным до readback в receiver.

Фиксированный JSON Patch проверяет UID, resourceVersion и весь исходный spec,
затем заменяет spec. Оба namespace UID, installation seal и исходный объект
читаются через тот же TLS-клиент. Установочный профиль требует явный локальный
k3d kubeconfig со встроенными CA, client cert и key. Kubectl нужен для разбора
этого файла. Его stdout остаётся во внутреннем pipe. Подмена файла, внешний
адрес, credential plugin и отключение TLS закрываются до записи.

Клиент отправляет PATCH по новому TLS-соединению с ограниченным ответом.
Один монотонный предел до пяти секунд учитывает оставшееся время owner и join,
TLS и чтение ответа. Redirect, Retry-After, разрыв или истечение срока
оставляют durable UNKNOWN до точного readback. Клиент не получает права
отменить уже принятую сервером запись и не отправляет повторный PATCH.

`make test-paired-token-release-receiver-local` проверяет HTTP через настоящий
локальный mTLS с синтетическими kubeconfig и API. Шесть правильных шагов,
server dry-run, подмена scope и файла, дрейф изоляции, replay допуска,
Retry-After, redirect, потерянный и поздний ответ покрыты тестами. Для CLI
активны прежние read-only команды. Installed PATCH и терминальный сценарий
остаются отдельной приёмкой.

Для чтения lineage этот же локальный mTLS transport предоставляет закрытый
GET-интерфейс. Он читает metadata списков Pod, ReplicaSet и Job в
`kodex-system` и полное состояние только выбранных Pod и Job. Клиент
ограничивает срок и объём каждого ответа. Установленный read-only прогон
проверил все три списка и точные UID двух полных объектов. Его результат
покрывает транспорт чтения. Полное доказательство lineage и положительный
native join на стенде проверяются отдельно.

Контракт сверялся через Context7, опубликованные
[Node.js v24 HTTPS](https://nodejs.org/docs/latest-v24.x/api/https.html),
[Kubernetes v1.35 API](https://v1-35.docs.kubernetes.io/docs/reference/using-api/api-concepts/)
и [client-go v0.35.5](https://github.com/kubernetes/client-go/blob/v0.35.5/rest/with_retry.go).
Стандартный клиентский PATCH допускает встроенные повторы по Retry-After, поэтому
для effect используется отдельный однопопытный HTTPS port.

### Read-only native join старых CP/IG

`paired-token-native-join.mjs read --input /private/input.json --kubeconfig
/private/explicit-config` читает только исходный forward STOP двух receivers.
Версия input — `kodex.paired-native-join-input/v1`: `name`, `uid`, исходные
`podUIDs`, owned private `configPath`, `recordPath`, `effectJournalPath`.
Пути, source, полный stopped/staged spec, установка, Deployment UID и old UID
set связываются с прежним plan и одним durable STOP INTENT. Caller не выбирает
native endpoint, namespace, node container либо исполняемую команду.

Reader переиспользует канонический serving scope и `activeDeploymentPods`,
`portableRunnerTarget`, `readComponentNodeTarget` и существующий canonical
`runtime_absence` из `tools/dev/prove-k3d-evicted-pods.py`. Последний вызывается
через bounded Python `-I -B` bridge без копирования validator и без metadata
recovery/delete пути. Два последовательных observations на **каждой** exact node
содержат полные `crictl pods`, `crictl ps -a` и `ctr tasks list --quiet`.
Неизвестный/orphan task, target UID в любом sandbox/container, неполный runtime,
missing node или смена exact Docker/node binding закрывают результат.
В набор проверки CRI входят исходные UID и все найденные Pod текущей
Deployment lineage, включая Pod со статусом terminal. Для этих UID требуется
полное отсутствие в CRI. В остальном `kodex-system` sandbox связывается с
Pod UID и именем в metadata снимках до и после двух раундов, если он Ready,
имеет containerd task или незавершённый контейнер. Исторический NotReady
sandbox без task и с завершёнными контейнерами отражает метаданные остановки.
Первый снимок одновременно задаёт lineage, поэтому новый UID исчезнувшего
работающего Pod блокирует подтверждение. Измерение на работающих узлах
выполнялось через чтение.

API absence отдельно не даёт положительного join. До и после native reads
проверяются текущие replicas/status0, тот же полный spec, immutable UID,
installation seal и scoped ReplicaSet/Pod lineage. Hidden label не скрывает
owned Pod; NodeLost/unknown state и неизвестные node conditions отклоняются.
Начальный metadata снимок и полные Pod и Job читаются через закрытый mTLS
клиент. После двух раундов CRI повторяется тот же полный metadata scope.
Точная привязка Deployment и узлов читается параллельно с metadata на обеих
сторонах. Изменение UID, resourceVersion, состава lineage или привязки
sandbox закрывает результат до выдачи доказательства.
После двух полных native rounds снова читаются node bindings, API и original
journal. Metadata и процессы не изменяются, Jobs не создаются.

Collection имеет общий monotonic budget4s, процессы ограничены его остатком
и положительно дожидаются завершения через `allSettled`. Результат — только
`kodex.paired-component-join/v1` с UID set, начальным временем и opaque evidence
hashes, максимум 16 КиБ. Raw runtime lists, environment и private paths не
выдаются. Это не schema proof, не owner grant и не terminal permit.

`createPairedTokenNativeJoinReader` предоставляет синхронный `readJoin` port
через fixed bounded Node child и 16-КиБ stdin. Он проверяет исходные config/record
bytes до и после child; request не переназначает эти пути либо node bindings.
Существующий receiver отдельно сохраняет первоначальный 5s join ceiling.
Связывание этого READ port само по себе не подключает native CAS/apply.

Намеренное ограничение первого producer: только первоначальные old Pod UID,
закреплённые forward plan. Смена Pod UID после plan, rollback/new Pod UID и
новый execution generation закрыто отклоняются. Для них нужен отдельный exact
owner receipt path; API disappearance не подменяет происхождение нового UID.
Full paired release/rollback поэтому по-прежнему **NOT READY**.

`make test-paired-token-native-join-local` запускает настоящий Node child,
loopback mTLS, синтетические Kubernetes и Docker, а также канонический
проверяющий код Python.
Проверены оба receiver, old/second-snapshot writer, missing/stale node,
NodeLost/hidden Pod, unknown task, неполный CRI, timeout без partial JOINED и
подмена records. Дополнительные случаи покрывают неизвестный UID, живой
sandbox терминального потомка, смену имени и удаление Pod во время проверки.
Эта suite использует синтетический API и CRI.
Installed native proof остаётся **NOT RUN**.

Через Context7 проверены официальные
[cri-tools](https://github.com/kubernetes-sigs/cri-tools/blob/master/docs/crictl.md)
и реализация JSON `pods/ps`: явные Unix runtime/image endpoints, полный
`ListPodSandbox`/`ListContainers` и именованные CRI states. Это документация
протокола, а не доказательство установленного runtime.

Для реальной поставки нужны ordered stop/positive join старого IG, image-only
full-spec CAS обоих receivers, durable intent до effect и запрет повторного CAS
после UNKNOWN. Необходимы свежий schema/authority fence перед каждым effect,
прежние claim/result semantics и подтверждённое завершение новых readers.

Отдельные отсутствующие consumers — current-authority110 API reopen по receipt
именно этой пары и закрытый совместимый paired rollback. Ни forward migration,
ни новая rotation, ни переписывание installation profile не заменяют эти
контракты. Пока они отсутствуют, весь release unit **NOT READY**.

`portable-component-migration` не принимает новый профиль в
`componentMigrationConfiguration`/`componentMigrationPublication` и не выдаёт
для него `planComponentMigrationHandoff`/coordinator transition. Его
`PAIR_STOP_INTENT → PAIR_STOP_ACK → staged/resumed` и контролируемый join можно
переиспользовать только после отдельного закрытого same-schema dispatch без
migration/Goose command. `component-publication` сохраняет прежний forward
`schema_change:true`; этот CLI использует исходные stable publication receipts
и не перекрашивает старый component lock. `component-api-traffic` restore
по-прежнему требует старый rotation либо migration terminal. Нужен отдельный
terminal consumer именно same-schema пары с неизменной authority110, а не
повтор исторического 049/051 этапа. До него API reopen недоступен.

Проверена актуальная документация Node.js через Context7: `node:test`, process
isolation, `node:assert/strict` и `assert.throws`. Это документация тестового
контура; native SQL, cluster и delivery checks не запускались.

## Same-schema terminal verifier

`readPairedTokenReleaseTerminal` — отдельный read-only producer после полного
forward receipt и штатного открытия CAG **внутри изолированного Service**.
Он не вызывает coordinator, `patch`, migration, rotation либо Service restore.
`inspectPairedTokenForwardReceipt` использует тот же private `validateHistory`,
что и receiver: шесть exact ACK, завершённый forward, первоначальный BOUND,
сохранённые positive stop/stage join digests и resume Pod UID. Незавершённый
INTENT/UNKNOWN, rollback и чужой journal не дают terminal receipt. Исторический
UNKNOWN, разрешённый exact readback и последующим штатным ACK, не вырезается.

Producer сравнивает текущие полные CP/IG specs и serving lineage с исходным
планом и exact resume ACK Pod UID. Новая здоровая замена Pod не принимается как
та же execution incarnation. Для CAG переиспользуется
`requireOpenedAPIMaintenance`: original close/traffic plans и journals,
неизменный broad target, exact UID, OPEN plan от закреплённого executor,
сохранённый frozen CAG template, текущие replicas/readiness и изоляция Service
без EndpointSlice addresses. Broad close target не переписывается в новый
четырёхресурсный pair scope. Исходные события не дописываются verifier-ом.

Подготовка выполняет два полных чтения private owner CP051/authority150002/
registry110 вокруг первого снимка Kubernetes. Первоначальный BOUND фиксирует
PostgreSQL UID, CP backend и authority backend. Подготовка ограничена 30
секундами и исходными сроками обоих owner proof. Второе чтение срок не
продлевает. Две полные пары component/API observations должны совпасть,
включая RV и Pod UID.

Пятисекундное окно начинается перед вторым снимком Kubernetes. В нём общий
список метаданных охватывает три компонента и publisher. По нему снова
читаются полные Pod publisher и сверяется их готовность для того же
Deployment. После снимка повторяются backend epoch обеих баз,
UID PostgreSQL Pod и StatefulSet и сравнение полного registry и publisher
с подготовленным снимком. Нативные чтения Kubernetes идут параллельно до
и после закреплённого SQL epoch.
Source и effect journal перепроверяются после блокирующих чтений.
Итоговый допуск истекает по ближайшему из прежнего срока owner и границы
этого окна. Скачок часов назад либо истечение срока закрывает ответ.
Возвращаемые метаданные ограничены 64 КиБ.

`verifyPairedTokenReleaseTerminal` принимает только исходный in-process proof
с private WeakMap seal и тем же journal handle. JSON, изменённый body, другая
manifest/record либо начало rollback отклоняются. Чтение не выдаёт mutation
authority: `releaseReadiness=BLOCKED_NATIVE_RESTORE_CONSUMER_UNBOUND`. После
process restart требуется новый полный readback, а не восстановление свежести
из сохранённого JSON. Adapter ports остаются внутренними, не публичным API.

| Состояние | Авторитетный read path | Внешний эффект / событие |
| --- | --- | --- |
| Incomplete forward или pending UNKNOWN | Исходный receiver status и exact intent | Отсутствует; terminal отказ |
| Шесть ACK без PAIR_COMPLETE | Общий history parser | Отсутствует; terminal отказ |
| Forward complete, CAG ещё закрыт | Исходные journals/current serving | Отсутствует; terminal отказ |
| CAG OPEN, Service изолирован, exact fresh051/110 | Новый terminal verifier | Только metadata; journals неизменны |
| Замена UID/spec/backend, revoke/expiry или rollback | Новый полный readback | Отсутствует; private proof не renew-ится |
| Поздний/сериализованный terminal | Новый полный readback | Отсутствует; старый proof отклонён |

Следующая нормальная composition требует четырёх producer/consumer связей:

1. Реальный complete forward journal от native CAS receiver с fixed source и
   actual old-IG/CP join, а не импорт синтетических ACK.
2. Original owned private close/isolation/OPEN file reader с теми же hashes,
   fresh exhaustive Service/EndpointSlice/serving inventory и actual owner
   factory. В текущих тестах эти ports явно synthetic.
3. Закрытый receiver для восстановления API Service. Он принимает исходный
   in-process terminal proof и сохраняет связь с той же парой и изоляцией.
   Реализация и локальная квалификация описаны ниже.
4. Штатное чтение terminal proof подключено через `terminal-status`.
   Для effect-композиции нужны завершённая остановка процессов, рабочий бюджет
   полной терминальной проверки и подтверждённый rollback.

`make test-paired-token-terminal-local` ограниченно запускает эту suite и все
предыдущие paired synthetic suites. Positive fixture получает настоящий
шестишаговый journal и OPEN receipt из существующих state machines, но все
Kubernetes/DB inputs синтетические. Проверены подмены source/journal/backend,
Pod/sidecar/UID, отсутствие OPEN, чужой scope/Service, EndpointSlice, последняя
смена owner, сериализация, peer BUSY и реальная задержка при frozen wall clock.
Никакой установленный reopen, SQL, CRI или mutation этой suite не подтверждает.

## Параллельный metadata-only serving readback

`readMultiComponentServingInventory(client, components)` — read-only seam для
закрытого набора CP/IG/CAG. Принимаются 1–3 уникальные пары `{name,deploymentUID}`
только этих компонентов. Target/context/namespace и mTLS принадлежат trusted
GET adapter; вход не задаёт URL, credentials, selector либо метод mutation.
Существующий sync `readComponentServingInventory` сохраняет свой интерфейс и
правила; оба пути используют общие scope/Pod/Job validators в
`portable-component-migration`.

Adapter имеет только `readMetadata(kind,{signal,deadline})` и
`readObject(kind,name,{signal,deadline})`. Для metadata разрешены built-in
Pod/ReplicaSet/Job, полные объекты читаются только для выбранных Pod/Job.
`deadline` — первоначальный абсолютный `performance.now()` того же процесса;
signal принадлежит сборщику. Сам adapter обязан прерывать и положительно
дожидаться завершения bounded запроса. Сборщик не возвращается из timeout
через оставленный в фоне `Promise.race`: все начатые GET завершаются через
`allSettled`, в том числе после отказа sibling.

По [официальным API concepts Kubernetes1.35](https://v1-35.docs.kubernetes.io/docs/reference/using-api/api-concepts/#metadata-only-fetches)
список запрашивается с
`Accept: application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1`.
`spec/status` в нём отсутствуют, поэтому metadata не доказывает terminal или
готовность. Используются только встроенные namespaced routes:
`/api/v1/namespaces/kodex-system/pods`,
`/apis/apps/v1/namespaces/kodex-system/replicasets` и
`/apis/batch/v1/namespaces/kodex-system/jobs`.
Неподдерживаемый ответ/406, full-list fallback, pagination либо неверный GVK
закрывают чтение. `resourceVersion` сравнивается на равенство, без числового
порядка и без приравнивания версий разных списков. Эти правила сверены также
через Context7 с официальной документацией Kubernetes.

Сначала три полных namespace metadata списка читаются параллельно. Канонический
scope включает одновременно selector и Deployment→ReplicaSet→Pod lineage:
снятая label не скрывает writer. Полные selected Pod/Job читаются по exact
именам с дедупликацией на все компоненты и concurrency≤4. После них повторяются
три metadata GET. Выбранные projection должны совпасть побайтно по UID/RV,
name/app/namespace/ownerReferences; unrelated namespace churn не подменяет
проверку выбранных ресурсов. Каждый full Pod/Job связан с первоначальным
snapshot и проходит прежние canonical retained-CLI-Job правила. Отсутствующая
Job не превращает foreign Pod в завершённый. NodeLost/unknown и новый hidden
writer остаются закрытыми исходами.

Закрытый профиль ограничивает один raw metadata список 8 МиБ, раунд — 24 МиБ,
общее число metadata entries — 4096. Это позволяет прочитать большие
`managedFields` без переноса в selected projection. Выбранные metadata всех
компонентов ограничены 1 МиБ; selected full objects — 128, каждый меньше 1 МиБ,
совокупно не более 16 МиБ. Общий первоначальный monotonic/wall бюджет — 5 с,
без автоматического retry/fallback. Тот же client не ставит peer вызов в очередь:
конкурентный сбор получает BUSY. Ошибка не выдаёт частичного inventory.

Возвращаются component inventories со штатным `scopeSHA256` и безопасные
счётчики. Только для внутренней native CRI association у результата есть
неперечислимое readonly поле `namespacePods` с frozen `{uid,name}` из **того же
первого полного Pod snapshot**, не более4096 записей. Оно не попадает в JSON,
не содержит spec/status/labels и не является доказательством positive join.
Повторный API/native readback, exact node binding и runtime absence по-прежнему
обязательны отдельному consumer; исчезновение Pod не даёт JOINED.

`make test-multi-component-serving-inventory-local` запускает synthetic adapter
и пять прежних generic serving scenarios. Проверяются точное равенство старому
scopeProof, hidden lineage, полный terminal Job guard, параллелизм и дедупликация,
pre/post UID/RV/owner races, pagination/406-эквивалентный unsupported reply,
caps, общий timeout с frozen wall clock и положительный join всех начатых
чтений. Это не installed latency receipt: native mTLS adapter и его actual
измерения принадлежат отдельной composition root. Receiver, terminal verifier,
owner authority и разрешение apply этот срез не меняет.

## Read-only bracket вокруг fixed native observation

`readMultiComponentServingNativeBracket(client, components, {deadline,signal},
observeNative)` — внутренний seam для code composition. Он не подключён к
receiver/terminal либо CLI apply. `observeNative` должен быть фиксированным
trusted read-only observer из исходного кода; request/JSON не выбирает callback,
модуль, endpoint или credentials. Переданное значение не является authority:
bracket не интерпретирует даже `nativeState: JOINED` как positive join/permit.

Вместо двух полных serving readers bracket выполняет один набор parallel
metadata GET, выбранные полные Pod/Job и каноническую проверку discovery/Job
exclusion/ownership. После неё observer получает frozen
`{snapshot,namespacePods,signal,deadline}`. Snapshot содержит проверенные full
component inventories и names того же первого полного Pod metadata списка.
Ни metadata, ни callback не подменяют owner/Deployment/CRI доказательства.
Проверка ownership не требует остановки либо Ready и сама не даёт JOINED.

После положительного завершения самого observer выполняется только второй
набор metadata GET. Все выбранные UID/RV/app/owner lineage должны точно
совпасть с исходным scope. Mutation full Pod/Job между чтениями увеличивает RV
и закрывает результат; повторного полного GET/автоматического retry нет.
Итоговый serving report заморожен. Plain
`readMultiComponentServingInventory` сохраняет свои 5с, интерфейс и прежний
порядок metadata→full→metadata→canonical report; общие caps/validators не
ослаблены.

`deadline` обязателен и задаётся первым вызывающим native read по абсолютному
`performance.now()` в том же процессе. Остаток при входе должен быть
положительным и не более5с; bracket использует именно это значение, не создавая
новых 5с. Один signal и deadline передаются всем GET и observer. Внешняя отмена
также отменяет внутренний signal. Все начатые requests и вызванный observer
должны положительно завершиться: helper не бросает их в фоне после timeout.
Поэтому trusted observer обязан соблюдать bounded cancellation/join своего
фиксированного native adapter. После expiry/cancel callback не допускает
финального подтверждения, и прежний native join ceiling не продлевается.

Для последующей runtime association результат имеет раздельные
неперечислимые frozen `namespacePods` и `namespacePodsAfter` из соответственно
первого и последнего полных Pod metadata списков. Они **не объединяются**:
замена unrelated Pod может быть видна лишь в одном snapshot. Consumer должен
сопоставить exact native rows с обоими списками самостоятельно, вместе с
actual node/Deployment/source/owner fences. Private `nativeObservation`
содержит только клонированные JSON data observer, максимум16МиБ/depth64;
functions/getters/brands не принимаются как доказательство. Поля не попадают
в обычный JSON, callback errors не раскрываются наружу. Ни одно поле не
заменяет canonical runtime absence и positive join.

Расширенный `make test-multi-component-serving-inventory-local` проверяет
равенство plain serving, точное число GET и один deadline через все фазы,
mutation выбранных ресурсов во время observer, раздельные namespace maps,
невозможность изменения frozen snapshots/report, early discovery refusal,
external abort, timeout с frozen wall clock и join всех sibling reads. Также
проверены отсутствие side writes, отсутствие выбора кода из input и отсутствие
permit из произвольного callback result. Native transport/receiver/owner и
реальный installed bracket этим локальным прогоном не проверяются.

## Чтение owner для парного обновления

Профиль читает точный backend и канонический каталог CP 051 в одной
read-only транзакции `REPEATABLE READ`. Во второй транзакции читаются backend
authority, версия схемы и `authority_rotation_status()`. Программа сравнивает
SHA256 канонических запросов и шаблонов перед выполнением. Функция
`pairedTokenOwnerQueries()` возвращает SHA256 фактически составленных запросов.
Каждая транзакция связывает свой результат с backend одной базы.
[Изоляция транзакций PostgreSQL 18](https://www.postgresql.org/docs/18/transaction-iso.html).

В запросе парной поставки PostgreSQL вычисляет `complete_catalog` один раз
через `AS MATERIALIZED`. Все проекции получают один и тот же каталог в исходном
снимке. Компоновщик проверяет SHA256 исходного SQL и место вставки. Сравнение в
одной read-only транзакции PostgreSQL 18.3 проверяет равенство полного ответа,
включая отпечатки схемы, права, Goose и проверки активных операций. Правила
компоновки сверены через Context7 и документацию PostgreSQL 18.
[Материализация CTE](https://www.postgresql.org/docs/18/queries-with.html#QUERIES-WITH-CTE-MATERIALIZATION).

Запрос к authority выполняется после сверки исходника, реестра, publisher
и его готовых Pod. `readAuthorityHead` и `readAuthority` используют один
результат этого запроса. Baseline принимает закрытый reader с закреплённым
SQL и локальным Unix-сокетом.

В локальном профиле k3d CLI читает метаданные всего namespace двумя
параллельными раундами Pod, ReplicaSet и Job. По UID publisher и цепочкам
владения отбираются объекты для полного чтения. Обе проверки publisher
привязаны к полному Deployment из исходного чтения, включая resourceVersion,
и к той же установке. Ошибка или истечение срока закрывает проверку.
Для исходного и позднего чтения publisher и registry CLI передаёт
`--show-managed-fields=true` и сверяет полный объект с нативным API.
[kubectl get 1.35](https://v1-35.docs.kubernetes.io/docs/reference/kubectl/generated/kubectl_get/).

После проверки обеих баз и повторного чтения реестра и publisher отдельная
выборка `pg_catalog.pg_database` сравнивает SystemID и оба OID. Этот шаг
обнаруживает пересоздание базы в том же Pod. До и после выборки сверяются
UID PostgreSQL Pod и StatefulSet. Любое расхождение закрывает выдачу
доказательства. Исходные 30 секунд охватывают и финальную выборку.

`make test-paired-token-owner-batch-local` проверяет три SQL-запроса и десять
чтений metadata PostgreSQL, а также отклонение подмены любой базы, схемы,
Pod, реестра или publisher. Оснастка использует синтетические источники.
Длительность сбора на установленном стенде измеряется отдельно.

Receiver завершает чтение owner и dry-run перед подтверждением остановки
процессов. Дальнейшие проверки source, API, spec и CAS проходят в пределах
исходных пяти секунд этого подтверждения.

## Восстановление API Service после парного обновления

Операторское действие связано с exact forward receipt, исходным журналом
изоляции, UID Service и готовыми Pod Gateway из подтверждённого OPEN.
Единственный путь к журналу восстановления выводится из пути журнала пары.
До короткого окна receiver завершает server dry-run и полный API readback.
Он сверяет его pins со свежим terminal proof, срок которого включает финальный
снимок и отправку Service CAS.

Receiver записывает и синхронизирует INTENT, затем выдаёт одноразовый
in-process допуск локальному mTLS-клиенту. Клиент читает UID namespace и
установки, сверяет exact UID/RV Deployment Gateway и отправляет один JSON
Patch к фиксированному Service. Patch
проверяет UID, resourceVersion, полный spec и все annotations. Перед
отправкой, после TLS, клиент заново проверяет исходный срок и допуск. Потеря
ответа, редирект, Retry-After или завершение после срока оставляют UNKNOWN.
[Операции Kubernetes API](https://v1-35.docs.kubernetes.io/docs/reference/using-api/api-concepts/#patch-operations).

| Состояние | Авторитетное чтение | Переход и эффект |
| --- | --- | --- |
| Нет INTENT, Service изолирован | Exact pair, source, close, OPEN и изоляция | Dry-run и свежий terminal proof допускают единственный INTENT |
| INTENT, Service по-прежнему изолирован | Exact UID, spec, annotations и журналы | UNKNOWN, только readback |
| INTENT, Service восстановлен | Exact postimage, больший resourceVersion и те же ready Pod | CONVERGING до готовности EndpointSlice |
| Postimage и готовые EndpointSlice | Установленные адреса Pod, порты, owner Service и обе готовности endpoint | Синхронизируется один ACK, результат RESTORED |
| Истёкший proof до INTENT либо чужой UID, scope, source или endpoint | Текущая граница owner и Kubernetes | Отказ |
| Попытка rollback одновременно с восстановлением или после его INTENT | Та же блокировка пары и сохранённый журнал | Отказ, отдельная повторная изоляция требует самостоятельного допуска |

Проверка EndpointSlice использует готовность, доступность для запроса и
признак завершения. Каждый готовый Gateway Pod должен иметь endpoint с
его IP и портом, который выбран исходным Service. Смена endpoint owner,
порта либо Pod UID оставляет операцию без ACK. Контракт сверялся через
Context7 и закреплённую документацию Kubernetes 1.35.
[EndpointSlice](https://v1-35.docs.kubernetes.io/docs/concepts/services-networking/endpoint-slices/).

`make test-paired-token-terminal-local` включает сохранённый журнал,
восстановление после потери ответа, проверку исходного срока, возможный
параллельный rollback и настоящий loopback mTLS с синтетическим Kubernetes
API. Для штатной установленной поставки ещё требуются реальные CLI read
ports в effect-композиции и терминальная квалификация. Действующий Service
проверяется чтением.

### Терминальное чтение из CLI

`terminal-status` принимает исходный input и план пары, журнал forward и
исходные OPEN plan и evidence. Файлы должны принадлежать оператору и иметь
закрытые права. По input находятся original close, isolation и authority
artifacts. Сначала сверяется полный forward receipt. Прочитанные файлы и
содержимое исходного журнала фиксируются и проверяются повторно до выдачи
результата. Положительный readback сохраняется в новый приватный файл.

```bash
node tools/release/paired-token-release-cli.mjs terminal-status \
  --input "$PAIRED_INPUT" --plan "$PAIRED_PLAN" \
  --effect-journal "$PAIRED_EFFECT_JOURNAL" \
  --open-plan "$PAIRED_API_OPEN_PLAN" --open-evidence "$PAIRED_API_OPEN_EVIDENCE" \
  --kubeconfig "$KODEX_KUBECONFIG_FILE" --output "$NEW_TERMINAL_EVIDENCE"
```

Снимок использует закрытые mTLS GET для трёх Deployment, Service Gateway,
EndpointSlice и Pod с маркером исходной изоляции. Оба снимка каждого serving
scope проверяют полные selected Pod и Job. Повторное чтение Deployment,
Service и EndpointSlice проверяет их состояние вокруг serving discovery.
Пагинация и замена UID, версии либо идентичности установки закрывают чтение.
Каждый снимок Kubernetes получает собственный предел до пяти секунд
внутри общего срока. Финальный снимок и backend epoch делят тот же
пятисекундный срок с допуском Service CAS.
[Kubernetes API 1.35](https://v1-35.docs.kubernetes.io/docs/reference/using-api/api-concepts/).

| Вход или наблюдение | Результат |
| --- | --- |
| Точные forward и OPEN, изолированный Service и свежий owner в пределах срока | Приватный read-only файл и fingerprint |
| Незавершённый forward, изменившийся источник или файл | Отказ до сохранения результата |
| Неполный список или замена Service либо serving scope | Отказ до сохранения результата |
| Истёкший срок или недоступный owner | Отказ, текущий Service продолжает работать по своему состоянию |

Сохранённый результат представляет историческое наблюдение. Допуск к
восстановлению выдаётся только в процессе, который заново выполняет свежую
терминальную проверку. YAML и JSON не задают функции или маршруты чтения.

## Обычная остановка всех принадлежащих ReplicaSet

Deployment `observedGeneration` и counts0 не подтверждают завершение работы
ReplicaSet controller. В exact Kubernetes1.35.5 Deployment вычисляет status из
кэшированных RS, причём source прямо допускает устаревшие данные:
[deployment/sync.go](https://github.com/kubernetes/kubernetes/blob/v1.35.5/pkg/controller/deployment/sync.go#L102-L121).
Другой RS worker может ещё создавать Pod из прежнего template. Сам Deployment
UID/RV CAS не проверяет записи в соседние RS/Pod объекты.

Native child теперь до CRI читает полный объект каждого обнаруженного owned RS
через отдельный code-only `readReplicaSet(name,budget)` на фиксированном
`apps/v1/kodex-system/replicasets/<name>` route. Общий `readObject` по-прежнему
разрешает только Pod/Job. Разрешены 1–128 уникальных owned RS, concurrency≤4,
один full GET каждого, прежний object cap1МиБ. Повтора GET и нового timeout нет:
все чтения используют исходный absolute4с и signal metadata bracket; все
начатые sibling reads положительно завершаются даже при отказе одного guard.

Full RS должен иметь точный `apps/v1` GVK, namespace/name/UID, тот же первый POM
RV и ownerReferences, единственный controllerRef `apps/v1 Deployment` с original
UID/name, отсутствие deletionTimestamp и совпадающую app label. Обязательны
`spec.replicas=0`, положительная generation,
`status.observedGeneration===metadata.generation`, явное `status.replicas=0`.
`readyReplicas`, `availableReplicas`, `fullyLabeledReplicas` и
`terminatingReplicas` допускают отсутствие лишь потому, что эти поля optional
в [apps/v1 schema](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/api/apps/v1/types.go#L787-L810);
при наличии каждое равно числовому0. Старый/будущий generation, ambiguous
lineage, неверный RV или ненулевой counter закрывают чтение до CRI.

После native observation прежний bracket заново сравнивает весь selected
metadata scope, включая UID/RV/owner lineage RS. Поэтому один full GET связан
с тем же неизменившимся объектом/version на обеих границах; новый active child
или изменение RS между ними не превращается в JOINED. Evidence digest включает
точные full-RS hashes, generation и RV без выдачи raw объектов.

Это causal guard обычного controller stop: один RS key обрабатывается
последовательно, а creation batch дожидается запущенных CreatePods:
[replica_set.go](https://github.com/kubernetes/kubernetes/blob/v1.35.5/pkg/controller/replicaset/replica_set.go#L530-L543),
[batch join](https://github.com/kubernetes/kubernetes/blob/v1.35.5/pkg/controller/replicaset/replica_set.go#L751-L785).
Первый pending old-spec reconcile не может одновременно подтвердить новую
generation того же RS. Внутренние expectations при этом не являются durable
create ledger и не выводятся из status0; статус обновляется и при unsatisfied
expectations: [syncReplicaSet](https://github.com/kubernetes/kubernetes/blob/v1.35.5/pkg/controller/replicaset/replica_set.go#L667-L710).

**Граница остаётся открытой:** timed-out/lost-ACK/in-flight Kubernetes API write
не превращается в доказанное NO_EFFECT из двух POM/CRI snapshots или ожидания4с.
Timeout response сам по себе не join обработчика
([apiserver timeout](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/apiserver/pkg/server/filters/timeout.go#L59-L67)).
Для строгого запрета последующего old-generation effect нужен независимо
проверенный durable admission/effect-authority generation fence либо сценарий
остаётся BLOCKED. Текущий read-only guard не реализует этот fence, не меняет
authority110, не останавливает Kubernetes controllers и не обещает отсутствие
будущего Pod при UNKNOWN. Больший sleep не является заменой такого допуска.

`make test-paired-token-native-join-local` включает actual local child + loopback
mTLS synthetic API. Проверяются stale RS generation при current Deployment,
старый spec/ненулевые counters/foreign owners/deletion, первый full-RV mismatch,
изменение финального RV и поздний active child. Calm fixture проверяет optional
counters, единственный GET на каждый RS и concurrency4. Документы сверены через
Context7 и exact official1.35.5 source; live операции этим контуром не выполняются.
