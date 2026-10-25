---
id: RUN-QA-S3-1003
title: Offline-план отдельной static S3 identity для backup drill
type: runbook
status: approved
owner: sre
version: 1.2.0
updated: 2026-10-25
---

# Offline-план #1003/#1830

Этот документ утверждает порядок подготовки плана. Он не разрешает читать или
копировать ключи, создавать Secrets, менять auth, выполнять Pod exec, probes или
rollout. Точка входа `static_backup_identity_plan.py` работает только с локальными
несекретными metadata snapshots и не имеет Kubernetes-клиента. Исполняемый
`static_backup_identity_operator.py` подготовлен для отдельного owner gate;
сейчас разрешены только его offline fixtures. Dynamic IAM unit `adf197b71` не является зависимостью
этого плана и не переносится: его Filer content CAS недоказуем на pinned версии.

## Поддержанный путь и цена изменения

Используется SeaweedFS 4.41, digest из `tools/dev/components.lock.json`, upstream
`de34a1a87c02893507f961cda9574172ee5064e9`. Его
[static loader](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/s3api/auth_credentials.go#L597)
принимает S3 config с identities и policies; identity связывается с policy через
`policyNames`, policy `content` — строка JSON. Для новых записей применим native
protojson camelCase из
[iam.proto](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/pb/iam.proto).
Статические identity защищены от замены одноимённой dynamic записью при merge;
это не даёт права менять Filer и не доказывает отзыв после удаления static записи.

Новый immutable Secret только с `s3.json` монтируется существующим томом
`s3-config`. Его baseline identities, credentials и policies должны оставаться
семантически прежними; добавляются ровно одна dedicated identity и её policy.
Старый shared credential Secret остаётся неизменным. Его текущая identity —
shared Admin, она не становится least privilege из-за добавления новой identity.
Отдельный immutable Secret с пятью S3 keys предназначен только custody backup;
shared access/secret в него не копируются. Оба новых Secret запрещено помечать
`kodex.dev/local-credential=object-storage`: штатный discovery ожидает исходный
bundle и одну baseline identity.

Изменяется только существующий `StatefulSet/seaweedfs`, ссылка `s3-config` и две
own annotations PodTemplate. Ссылки control-plane/session-archive, Service,
NetworkPolicy, static args, images, resource limits и PVC сохраняются. При одной
реплике обновление PodTemplate заменяет интегрированный Pod master+volume+filer+S3.
Нормальный цикл — **два rollout и два окна недоступности всего локального S3**:
подключение новой revision и возврат baseline. Это не hot reload и не HA; SIGHUP
не подключит другой immutable Secret к существующему Pod. Требуются отдельное
согласие владельца на проекцию baseline identity и оба перезапуска.

## Snapshot и offline-команды

`tools/dev/fixtures/static-backup-s3-snapshot.json` — синтетическая схема входа.
UID исходных namespace/StatefulSet/Secret зафиксированы для этой QA задачи;
resourceVersion, Pod и PVC берутся свежим metadata read. `sourceSecret` содержит
только name, UID, RV, immutable и имена keys, без `data`, `stringData` или values.
В StatefulSet сохраняются полный `spec`, безопасные name/namespace/UID/RV/generation
и четыре rollout status поля; managedFields и last-applied аннотации не нужны.
Pod/PVC представлены закрытым набором несекретных полей fixture. Контекст только
`k3d-kodex`, namespace только `kodex-system` с exact UID. Пароли/.env не принимаются.

Каталог файлов — owned `0700`, входы/выходы regular single-link `0600`. Пример
синтетического offline запуска; nonce должен быть новым для будущей операции:

```sh
python3 -B tools/dev/static_backup_identity_plan.py plan \
  --snapshot /private/snapshot.json --run static1003 \
  --owner-nonce 8f87b5e6b10b49e28a230c959375d827 --output /private/plan.json
```

Вывод — только `OFFLINE_REVIEW_ONLY` и canonical SHA256. Сам план содержит old
Secret UID/RV, STS UID/RV, old Pod/PVC pins, SHA256 полного старого/нового template,
exact candidate names, policy, две owner approvals и JSONPatch. Keys не создаются.
План — материал review; ни имя Secret, ни annotation, ни файл не являются authority.

Для этого примера новые имена:

- auth config: `qa-backup-auth-static1003-8f87b5e6b10b49e2`;
- dedicated keys: `qa-backup-key-static1003-8f87b5e6b10b49e2`;
- identity: `qa-backup-static-static1003-8f87b5e6b10b49e2`;
- policy: имя identity с суффиксом `-policy`;
- QA prefixes: `qa-1830-8f87b5e6b10b49e28a230c959375d827/` и
  `e2e-restore-8f87b5e6b10b49e28a230c959375d827/`.

Обе области данных включают **весь nonce immutable операции**, а не только run
или имя identity. Повтор run с новым nonce не наследует права на прежние backup/
restore objects. Plan/receipt используют schema v2; старые v1 preview/receipt
не принимаются новым оператором. Source prefixes остаются прежними.

## Policy и предварительные условия окна обслуживания

Новая policy допускает HEAD bucket readiness и GET versioning четырёх точных
buckets. GET list/list-versions ограничены `s3:prefix` с обязательной `/` перед
`*`. Source read-only: `kodex-artifacts/organizations/` и
`kodex-session-archives/session-archive/v1/`. Первый prefix охватывает все owner
данные staging; это не tenant narrowing и не выдаётся моделям. QA repository и
restore target допускают GetObject/GetObjectVersion/PutObject и **только**
DeleteObjectVersion внутри двух свежих prefixes. Admin, DeleteObject, bucket
delete, policy mutation и retention вне этих prefixes отсутствуют.
Pinned [action resolver](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/s3api/s3_action_resolver.go)
различает versionId, а
[condition extractor](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/s3api/policy_engine/engine.go)
формирует `s3:RequestMethod` и `s3:prefix`. Offline tests проверяют декларацию,
но не заменяют реальное прохождение authorizer и полноту restore.

Перед owner вопросом и effect обязательны:

1. Fresh metadata pins, один Ready Pod, converged revisions, прежний Bound PVC,
   exact image, достаточно места и отсутствие других recovery/backup/restore jobs.
2. Окно без записи S3: закончить и приостановить archive/artifact uploads и их
   фоновые producers, backup/retention/drill и любые deploy-local/GitOps/reconcile
   записи StatefulSet/Secrets/IAM/bucket policy. Просто отсутствие Jobs не
   доказывает отсутствие writers. Как именно остановить producers, root
   фиксирует в approved maintenance checklist; до этого apply BLOCKED.
3. Root-only проверка фактического backup config: ровно эти два source prefixes
   со slash boundary, отдельная repository/target область, no source writes.
   Текущий план не читает credential payload и не утверждает, что он существует.
4. Root-only проверка отсутствия collision новых Secret/identity/policy names,
   отсутствия bucket policy, которая расширит effective доступ, и существующего
   dynamic shadow. Обнаруженное состояние не изменять: остановить подготовку.
   До генерации пары и любого auth effect оператор также выполняет для каждой
   exact QA области ListObjectsV2 и ListObjectVersions с prefix+`/`, max-keys1,
   без delimiter/marker/cursor. Оба ответа должны быть полными пустыми страницами:
   точные bucket/prefix/MaxKeys/IsTruncated, KeyCount0 для V2, отсутствие current
   objects, versions, delete markers, common prefixes и continuation markers.
   Wrong scope, unknown/missing/duplicate поля, invalid XML/namespace, truncation,
   unavailable и malformed response закрыто отклоняются до keys/Secrets/STS.
   Положительные XML fixtures привязаны к независимым декларациям pinned4.41
   [ListBucketResultV2](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/s3api/s3api_object_handlers_list.go)
   и [S3ListObjectVersionsResult](https://github.com/seaweedfs/seaweedfs/blob/de34a1a87c02893507f961cda9574172ee5064e9/weed/s3api/s3api_object_versioning.go).
   Ответы не архивируются; сохраняются только hashes и matching scope metadata.
5. Явное owner approval exact plan digest, baseline credential projection и
   двух outage; исходное разрешение full backup не заменяет это согласие.

## Применение после отдельного разрешения — ещё NOT RUN

Root сохраняет durable INTENT с plan digest и immutable payload digests до effect.
Только его изолированный процесс читает точный old Secret после UID/RV проверки,
генерирует одну новую пару и собирает native static config в памяти. Не передавать
ключи в prompt/env/argv/logs/private audit. Baseline часть сверяется семантически,
ничего существующего не удаляется/не ротируется. Создание новых immutable Secrets
только create-only; их UID/RV, own labels и content digests фиксируются в receipts.
После lost ACK не генерировать другую пару и не создавать новое имя: сначала
authoritative readback exact planned name, immutable payload digest и UID.
UNKNOWN остаётся UNKNOWN, если исходные байты/UID невозможно подтвердить.

Перед единственным STS patch повторяются namespace/source/candidate Secret/PVC
pins, старый full spec и отсутствие writers. Плановый JSONPatch тестирует STS
name/namespace/UID/RV и **весь spec** в одной API операции; меняет только secretName
и own template annotations. Это
[Kubernetes JSONPatch](https://kubernetes.io/docs/reference/using-api/api-concepts/#http-patch-using-json-patch).
CAS атомарен внутри STS, не между STS/Secrets/PVC: `secretName` не pin-ит UID.
Поэтому исключение параллельных writers и последующий exact readback обязательны;
не заявлять межресурсную транзакцию или защиту от произвольного cluster-admin.
Stale RV/spec требует нового metadata observation и review актуального patch;
не убирать tests и не выполнять blind retry после timeout.

После patch нужны новый Pod UID, точный STS owner UID, прежний PVC UID, exact image,
новый config ref/revision и converged Ready/EndpointSlice. Ready проверяет
доступность процесса, но не policy. До запуска drill root выполняет signed
positive/negative probes нового credential: source HEAD/list/list-version allow,
sibling/unprefixed list deny, QA write/read/exact-version
delete allow только для собственных синтетических objects, bucket Admin deny.
Не делать negative source PUT: ошибочный Allow сам изменит source; этот инвариант
проверять authorizer без effect либо harmless endpoint, пока такой путь не доказан
он остаётся NOT RUN. Не использовать shared Admin как fallback.

`static_backup_identity_transport.py` использует authenticated Kubernetes API,
проверяет HTTPS/CA/no-skip в exact context и устанавливает exec в exact
`seaweedfs-0`/container/image. Constant shell выводит HELLO; до credential stdin
повторяется Pod UID/ref/Ready pin. Затем существующая exec session выполняет
штатный [curl `--aws-sigv4`/`--config -`](https://curl.se/docs/manpage.html#--aws-sigv4)
к `127.0.0.1:8333`. Keys находятся только в памяти и stdin, не в argv/env/файлах.
`-q`, no proxy, no redirects, HTTP1.1 и bounded response исключают внешние
curl config/proxy routes. Runtime наличие SigV4 в curl pinned image проверяется
рабочей probe; до live проверки оно NOT RUN. Новый Go binary/service не нужен.
Filer используется только HEAD четырёх exact IAM путей: legacy config/policies
должны отсутствовать, exact name collisions отклоняются. Его values не читаются,
его Create/Update/Delete не вызываются. Ни HTTP за пределами Pod, ни изменение
egress/API exposure/TLS/NetworkPolicy не добавляются. Старый dynamic unit не перенесён.

Custody consumer `scoped_backup_drill.py custody-plan --static-identity-receipt`
принимает новый dedicated Secret только с owner receipt. Проверяются exact
identity/policy/plan/payload digests, Secret UID/RV/immutable/purpose labels, current
STS template и Pod UID/Ready/EndpointSlice. Auth Secret проверяется metadata-only;
его shared Admin data consumer не читает. Dedicated keys проверяются payload hash
до/после чтения; отказ не разрешает shared fallback. Source prefixes получают
обязательный trailing slash; canonical restore target prefix сохраняется, его
actual listing нормализован отдельным исправлением `19282e66`.

Новый custody plan требует receipt не старше 300s. После одобрения его immutable
SHA последующие `validate_plan`/restore/custody reads не повторяют wall-clock TTL:
они авторитетно перечитывают все exact live pins. Это позволяет закончить backup
дольше пяти минут, но return, Pod replacement, metadata rotation и чужой template
немедленно закрывают путь. Исторический receipt сам по себе не оживляет доступ.

## Исполняемые операции после owner gate

`tools/dev/fixtures/static-backup-approval.template.json` заведомо не разрешает
ничего: flags false, expiry0, SHA placeholders. Только после явного ответа
владельца root подготавливает owned regular0600 approval в private0700 каталоге,
с exact canonical plan SHA, чистым reviewed operator Git SHA, expiresAt и
подтверждением quiescent window. Это запись внешнего ручного решения, не
самовыдаваемый bearer token. Файл не заменяет owner instruction в рабочей сессии.

Оператор имеет четыре закрытые команды и общий hard budget900s. API request20s/
child30s; exec handshake8s/response12s/curl8s; rollout wait240s с poll2s. Одна
state directory защищена flock и exact origin plan/revision. Каждый внешний
effect предваряется append-only durable INTENT; затем сохраняются ACK pins и
authoritative readback DONE. Файлы содержат только metadata/hashes/receipts.
Один API/curl effect отправляется один раз; pending INTENT требует readback.
SIGINT/SIGTERM и исчерпание budget прекращают работу с сохранением origin;
неизвестный in-flight effect требует отдельного readback. CLI выводит только
закрытый именованный diagnostic code, provider stdout/stderr не выводятся.

Примеры ниже описывают будущие команды, сейчас запускать их запрещено:

```sh
python3 -B tools/dev/static_backup_identity_operator.py apply \
  --plan /private/plan.json --approval /private/owner-approval.json \
  --state /private/static-state --output /private/static-receipt.json

python3 -B tools/dev/scoped_backup_drill.py custody-plan \
  --static-identity-receipt /private/static-receipt.json \
  --object-storage-secret qa-backup-key-static1003-8f87b5e6b10b49e2 \
  --run static1003 --role backup --output /private/custody-plan.json

python3 -B tools/dev/static_backup_identity_operator.py return \
  --plan /private/plan.json --approval /private/owner-approval.json \
  --state /private/static-state --output /private/static-return.json
```

Dedicated pair сначала сохраняется в own immutable scoped Secret; auth Secret
собирается с той же парой. После lost ACK readback принимает только matching
owner/payload/immutable pins; известный UID/RV не заменяется. Если create не
состоялся, а исходная пара утрачена, outcome остаётся UNKNOWN: новую пару и новый
nonce оператор не генерирует. Metadata recovery не создаёт Secrets и не повторяет
patch. С истёкшим approval доступна только команда `readback`; новый внешний
effect требует нового явного разрешения. CLI ошибки и output redacted.

`initial-freshness.done` сохраняется до первого key generation и привязан к
exact journal plan/revision, baseline Pod UID и обоим nonce prefixes. Если процесс
остановился **до** create INTENT, следующий apply снова проверяет current пустоту;
одной старой записи недостаточно. После первого Secret INTENT rejoin требует
исходный proof и exact owned immutable pair. Историческая начальная пустота не
перепроверяется против собственных поздних backup/restore writes и не создаёт
новую attempt. Receipt v2 включает этот proof; custody проверяет его scope до
выдачи dedicated projection. Это не устраняет условие exclusive quiescent writers
между S3 observation и Kubernetes effect: межресурсной транзакции здесь нет.

Для `render-backup`, `render-readback` и `render-restore` в static mode также
передаётся `--static-identity-receipt`; exact `--prefix` равен
`qa-1830-8f87b5e6b10b49e28a230c959375d827`. Render/credentials destination используют
этот repository prefix; restore inputs содержат exact operation target prefix.
Fingerprint, preflight, restore, target inspection и cleanup читают одни и те же
immutable targets.json. Apply restore повторно сверяет весь render с approved
custody plan; prefix substitution не создаёт Secret и не доходит до restore.

QA write probes используют только два exact own `_static-auth-probe/<owner>` key.
PUT имеет If-None-Match:* и metadata owner; GET сверяет bounded synthetic body,
HEAD — owner/ETag/VersionId; DELETE всегда содержит exact non-null VersionId.
Lost ACK PUT восстанавливается bounded own body/metadata read, DELETE — exact
version absence; повтор mutation запрещён. Source PUT/DELETE запрещены также
самим transport, даже при ошибочном Allow. Только декларативная policy closure
проверяет source write denial; runtime source-write probe остаётся NOT RUN.
Versioning всех четырёх buckets должен быть Enabled; helper его не включает.

| Исход | Следующее действие |
| --- | --- |
| Неизвестный create/apply/return | Только `readback` того же state/plan/revision, без нового эффекта |
| Чужой UID/RV/spec/owner/payload | Закрытый отказ, чужой ресурс не изменяется и не удаляется |
| Обычный возврат | Consumers stopped, old template CAS, новый Pod/EndpointSlice, baseline read, InvalidAccessKeyId, own Secrets delete CAS |
| Bad OrderedReady Pod | После observed return template отдельная `recover-pod`, только при owner flag и exact UID/RV |
| Истёкшее approval | Чтение origin для recovery; apply/return/recover/QA mutation запрещены |
| Не закрытый QA effect при возврате | Restore baseline/revocation могут завершиться, но own Secrets удерживаются и результат остаётся UNKNOWN до owner recovery |
| Lost ACK Secret delete | Readback absence; чужая replacement запрещает cleanup PASS |

Известный собственный QA object закрывается exact-version cleanup до возврата,
когда desired Pod доступен. Если S3 effect остаётся unknown либо Pod недоступен,
доступность baseline имеет приоритет перед cleanup: после возврата helper не
выдаёт final PASS, не удаляет recovery inputs и не делает Admin fallback.

## Возврат и неизвестные исходы

До возврата остановить и дождаться завершения dedicated backup/restore workloads.
Root проверяет old Secret immutable UID/RV и candidate receipts; новый readback
STS должен иметь exact owner annotation, desired spec и прежний UID. Генерация
offline return patch после approved plan digest:

```sh
python3 -B tools/dev/static_backup_identity_plan.py return-patch \
  --plan /private/plan.json --expect-plan-sha256 APPROVED_CANONICAL_SHA256 \
  --current-sts /private/current-sts.json --output /private/return-patch.json
```

Return patch использует свежий RV, exact full spec и восстанавливает только
исходную secretName/annotations; чужой edit не перезаписывается. `rollout undo`,
полный apply baseline, generic deploy-local reconcile и unguarded Pod delete
не используются. ACK patch не доказывает, что новый Pod обслуживает old config.
Timeout требует readback прежнего/desired/foreign spec, owner annotations и
поколения; не повторять effect и не считать baseline readback доказательством,
что промежуточного эффекта никогда не было.

У OrderedReady есть официальный
[forced rollback limitation](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback):
неставший Ready Pod может блокировать возврат даже после good template patch.
Только после подтверждения exact baseline spec разрешено подготовить
`broken-pod-delete`: отдельный current Pod metadata snapshot должен доказать
его STS owner UID, неготовность и ref новой auth revision. Полученный DeleteOptions
с exact Pod UID/RV, grace35s применяется к строго `/pods/seaweedfs-0`, без force.
Race с заменой Pod закрыто конфликтует. Неподтверждённый Pod остаётся owner recovery
BLOCKED; нельзя удалять replacement по одному имени. В этом аварийном случае
обычное число rollout не обещает границу числа фактических рестартов.

Возврат считается завершённым только после второго нового Pod UID, старого
auth ref/revision, прежнего PVC/image, Ready/EndpointSlice, положительного read
baseline identity и signed старым dedicated key ответа **InvalidAccessKeyId**.
Общий 403, AccessDenied, timeout или ACK не доказывают отзыв; store cleanup не
заменяет cache revocation. Если dynamic shadow сохранил ключ, остановиться,
не писать Filer. Только после этого удалить собственные новые Secrets с
DeleteOptions UID/RV preconditions и проверить absence; старый Secret/PVC не
удалять. До revocation proof dedicated key сохраняется в root custody для probe.

## Проверки и оставшиеся границы

`make test-static-backup-identity-plan` ограничен 30s, не использует сеть,
credentials, PG, Go или containers. Fixtures проверяют JSONPatch test/replace,
stale RV/UID, concurrent spec edits, чужую replacement, точный возврат, prefix
boundary, отсутствие source write actions и recovery DeleteOptions. Это модель
RFC6902, не Kubernetes server-side dry-run.
`make test-static-backup-identity-operator` запускает offline planner/operator/
transport/custody regressions с двумя timeout60s, без живых клиентов. Включены
lost ACK на create/patch/QA write/delete/cleanup, old generation/UID replacement,
expired readback, source mutation guard, no credential output, cold return и
OrderedReady recovery. Новый plan на metadata живого STS — только наблюдение.
Freshness regressions покрывают тот же run/new nonce, current object/old version/
delete marker до keys, malformed/truncated/incomplete/outage, exact scope и
origin-preserving rejoin после собственных writes. Legacy render без static
receipt сохраняет прежний контракт; он не является fallback для static отказа.
Actual static loader/policy/curl probes, quiescent proof, server-side dry-run,
rollout, revocation, DB reader grants и полный backup/restore остаются NOT RUN.
Готовность к review кода не является разрешением auth/deploy или full DR.

Context7 при подготовке вернул `Invalid API key`; использованы официальные
Kubernetes/curl docs и перечисленные exact upstream исходники SeaweedFS 4.41.
