---
id: RUN-QA-S3-1003
title: Offline-план отдельной static S3 identity для backup drill
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-10-25
---

# Offline-план #1003/#1830

Этот документ утверждает порядок подготовки плана. Он не разрешает читать или
копировать ключи, создавать Secrets, менять auth, выполнять Pod exec, probes или
rollout. Точка входа `static_backup_identity_plan.py` работает только с локальными
несекретными metadata snapshots и не имеет Kubernetes-клиента. Полноценный live
оператор здесь не реализован. Dynamic IAM unit `adf197b71` не является зависимостью
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
- QA prefixes: `qa-1830-static1003/` и `e2e-restore-static1003/`.

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
positive/negative probes нового credential: source GET/list-version allow,
source PUT/delete deny, sibling/unprefixed list deny, QA write/read/exact-version
delete allow только для собственных синтетических objects, bucket Admin deny.
Не делать negative source PUT: ошибочный Allow сам изменит source; этот инвариант
проверять authorizer без effect либо harmless endpoint, пока такой путь не доказан
он остаётся NOT RUN. Не использовать shared Admin как fallback.

Transport будущего verifier — authenticated Kubernetes API с exact Pod UID,
container/image и trusted verifier, секреты только stdin и loopback S3 внутри Pod;
или уже утверждённый exact TLS S3 endpoint. Не добавлять внешний plaintext путь,
egress, API exposure или ослабление TLS/NetworkPolicy. Код verifier из rejected
dynamic unit автоматически не принимается. Его перенос/проверка — отдельный шаг.
Actual custody consumer пока не принимает этот static receipt: до адресной
адаптации с exact identity/policy/content/Secret UID pins и независимого review
backup **BLOCKED**, подстановка shared source запрещена.

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
RFC6902, не Kubernetes server-side dry-run. Новый plan на metadata живого STS —
только наблюдение, не auth/apply/restore acceptance. Полный render, actual static
loader/policy probes, custody adapter, quiescent proof, rollout, revocation и
backup/restore остаются NOT RUN/BLOCKED до отдельного решения владельца.

Context7 при подготовке вернул `Invalid API key`; использованы официальные
Kubernetes docs и перечисленные exact upstream исходники SeaweedFS 4.41.
