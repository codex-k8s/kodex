---
id: OPS-AUTHORITY-DATABASE-1093
title: PostgreSQL boundary issuer и verifier
type: operations
status: approved
owner: backend
version: 1.0.3
updated: 2026-11-21
---

# Причина и граница

Refs #1093, #1059, #1031, #1018. Общая capability DB-роль не доказывает
владение строкой. Неизменяемый `session_user` разрешается в owner-owned реестре
точного workload, capability, generation и lifecycle. `SET ROLE`, произвольный
GUC и совпадение имени payload не создают identity. Application mTLS/JWS,
exact actor/permission/replay проверки сохраняются перед PostgreSQL adapter.

| Сценарий | Владелец и команда | Авторитетный результат / отказ |
| --- | --- | --- |
| Fresh / upgrade | Migrator применяет прежний baseline и новую forward-only migration | EMAIL LOGIN и 16 runtime identities доступны в обоих случаях; опубликованные байты baseline сохранены. |
| Issuer reserve | Verified proof → repository Reserve → exact caller-workload RLS | Proof watermark продвигается монотонно; reservation уникален по workload/JTI. Прямой SQL не откатывает watermark. |
| Verifier accept | Verified context/snapshot → AcceptVerification → snapshot и replay одна transaction | Snapshot требует свежий независимый receipt и совпадение всех revisions с owner history; другой workload закрыто отклонён. |
| Snapshot activation / restart | Same workload → свежий receipt того же snapshot | Direct INSERT/UPDATE проходит те же attestation и monotonic guards. Смена replica receipt не требует удаления watermark. |
| Continuation | Accepted parent replay → issuer того же workload → child reservation | Issuer читает только собственный target replay; чужой parent не создаёт child. |
| Cleanup / replay | Exact identity → DeleteExpired | Reservation неизменяем; удаление возможно только после expiry и обязательного десятиминутного retention. Caller cutoff не сокращает этот минимум. |
| Readiness / restore fence | Рабочий LOGIN → own snapshot и write probe | Те же RLS/trigger guards, недоступный реестр или закрытый restore fence не превращаются в готовность. |
| Retirement | Только owner forward migration | CURRENT → RETIRED необратим; прежний LOGIN теряет RLS/write access и в уже открытой сессии. Миграция также выполняет exact NOLOGIN, REVOKE membership, termination и независимый readback перед завершением retirement. Runtime не получает lifecycle DDL. |

Новых public endpoints или business events нет. Авторитетное состояние —
PostgreSQL receipt/watermark и owner registry. Runtime не получает SELECT/DML
реестра, SECURITY DEFINER использует фиксированный search_path; ACL функций
закрывает PUBLIC. Табличные права остаются ограничены RLS и обязательными
триггерами, поэтому прямой SQL не обходит identity/attestation/monotonic/retention.
`TRUNCATE`, изменение trigger/RLS и создание SECURITY DEFINER runtime запрещены.

Проверка: `make test-internal-rpc-authority-postgres`, service race/vet/build,
authority render и общий baseline нового exact SHA. Реальные LOGIN проверяют
own/cross-workload writes, reads, revision rollback, replay deletion,
caller GUC, retirement существующей сессии и миграцию baseline → upgrade → up.
Evidence выполненных проверок привязывается к точному SHA в PR #1060;
исторический baseline e1e8 не подтверждает исправление. Live acceptance —
**NOT RUN**, она выполняется отдельно после общего review и owner gate.

Context7: PostgreSQL18 `/websites/postgresql_18`, `CREATE POLICY`, FORCE RLS,
`session_user`/`current_user` и безопасный `SECURITY DEFINER search_path`.
Rollback не удаляет durable state и не оживляет retired identity: только
совместимая forward migration. Секреты и приватные данные не публикуются.

## Адресное добавление издателя artifact-retention

`tools/release/artifact-retention-issuer-migration.mjs` выполняет только
`20261115000100 → 20261115000200`, с argv `up-artifact-retention-issuer`.
Обычный `up`, ротация, пароли, Secrets и доставка ключей в этот оператор не входят.
Новая установка сохраняет штатный fresh-install путь.

Все фазы требуют `--context` и owned private `--kubeconfig`. Для `plan` нужны
`--cluster-uid`, `--namespace-uid`, `--publication-plan`, `--publication-evidence`
`--output` и ещё не существующий `--evidence` в закрытом каталоге. Путь журнала
закрепляется планом: повтор не может выбрать новый пустой файл. Frozen checkout должен совпадать с source SHA опубликованного
immutable authority image. План закрепляет namespace/installation, migration
credentials и CA metadata, PostgreSQL Pod/StatefulSet UID, локальный backend и
Service/EndpointSlice; чужие активные migration/rotation закрывают допуск.
`apply --plan … --evidence … --approved-plan-sha …` сохраняет durable create intent
до единственного создания Job с фиксированным именем. `status --plan … --evidence …`
проверяет тот же UID/spec, positive Pod join, imageID и owner head/role readback.
Неизвестный ACK требует только status/readback: даже отсутствие Job не разрешает
новое создание. Повторный apply после подтверждения проверяет прежний effect.
Failed Job остаётся FAILED; отдельно отражается, успел ли owner перейти на target.

Локальная synthetic проверка — `make test-artifact-retention-issuer-migration-local`.
Она не подтверждает live DDL, Secret installation, issuer login либо доставку
полномочий. Эти release operator действия и установленный readback выполняются
отдельно в разрешённом target/scope; содержимое credentials не выводится.

## Подготовка проекций artifact-retention перед107→110

`tools/release/artifact-retention-issuer-assets.mjs` предоставляет отдельные
plan/apply/status. План закрепляет чистый source, cluster/namespace/installation,
старый UID/spec artifact-retention, шесть private путей close/isolate/migration049
и единственный путь fsync-журнала. Каждый effect повторно проверяет actual
STOPPED049 admission и отсутствие новых credential refs; старый workload должен
иметь replicas0 и положительный join. Шаблон Deployment передаёт следующему
оператору отдельный immutable receipt, обновление шаблона сюда не входит.

Допустимы ровно7 пустых Opaque generation0 Secret по официальным dynamic
проекциям installer, один source Certificate и issuer NetworkPolicy. Затем
выполняются пять адресных CAS: publisher Role с get/update семи имён, три
существующих ingress и ART runtime NetworkPolicy. Последняя получает только
source-derived egress к exact control-plane podSelector TCP8443; прочие правила
сохраняются. План и ресурсный receipt v2 закрепляют14 ordered GVK/namespace/name. UID, resourceVersion и прежнее поле входят в условия CAS;
соседние правила сохраняются. Secret читается через metadata/count projection.
CAS передаётся через существующий private regular patch-file с удалением после
child join; Node pipe не переоткрывается через `/dev/stdin`.
Certificate проверяется с exact Ready generation и закрытыми admission defaults.
TLS Secret проверяется отдельно по type/именам ключей и certificate/issuer/SAN
metadata без payload; штатное отсутствие ownerRef допускается. Первый TLS UID
и metadata hash сохраняются в журнале: удаление или новая incarnation закрывают
Ready, даже если condition Certificate ещё не обновился.

| Состояние шага | Следующее действие и авторитетный read path |
| --- | --- |
| План, intent отсутствует | apply проверяет preimage и durable intent до одной записи; status возвращает NOT_STARTED. |
| Запись с известным ACK | UID ACK устойчив до GET и связывает последующий VERIFIED. |
| ACK неизвестен | Только exact GET по прежнему intent; отсутствие объекта не разрешает повтор записи. |
| Поле уже точно совпадает | Durable NOOP_VERIFIED закрепляет прежний UID и поле без PATCH/перезаписи metadata; unknown intent не преобразуется в no-op. |
| VERIFIED | Повторные apply/status сверяют прежние UID/поля; incarnation или content drift закрывают результат. |
| Certificate ещё не Ready | Сохраняются созданные ресурсы и journal; ожидание не повторяет create. |
| Все14 шагов подтверждены | Fresh assets+STOPPED проверка выдаёт typed pre-stage receipt. |
| Следующий оператор изменил ART template | Требуется отдельный typed STAGED proof; прежний no-new-refs receipt не выдаёт свежую готовность. |

`readArtifactRetentionIssuerAssetsReady({plan,events},kube)` использует production
readers и не принимает готовый boolean. Ресурсный sub-reader
`readArtifactRetentionIssuerAssetResources` выдаёт только PRE_ROTATION metadata
без authority/Ready; STAGED consumer обязан добавить свой fresh049/ART proof.
После заполнения проекций publisher этот generation0 predicate неприменим.
Синтетический контур —
`make test-artifact-retention-issuer-assets-local`; он не подтверждает установленный
cert-manager, доставку credentials, DDL049 либо signed terminal110.

## Переход Control Plane на политику 110

План миграции 049 закрепляет действующую проекцию 107 и вычисляет имя новой
неизменяемой ConfigMap из политики 110 и подтверждённого OIDC-окружения. Перед
`check`, `stop`, `apply` и `handoff` оператор заново проверяет привязки издателя,
материалы issuer и точные образы на узлах.

После остановки прежних процессов `handoff` создаёт новую ConfigMap и записывает
полученный UID в журнал перед повторным чтением. После потерянного ответа
оператор проверяет объект по имени, содержимому и привязке к плану. Затем
Control Plane с нулём реплик переключается на новую проекцию. Предыдущая
ConfigMap остаётся доступной для проверки исходной конфигурации.

Control API запускается с изолированным Service. Запуск остальных сервисов
требует свежего подтверждения доставки подписанной политики и проверяет
неизменность операции перед каждым изменением Deployment. Внешний Service
возвращается после `RETIRED` той же операции, проверки готовности всех девяти
сервисов и свежего подтверждения схемы 049.


## Исторический issuer-role READ после замены PostgreSQL Pod

`artifact-retention-issuer-historical-readiness.mjs` добавляет отдельный
`HISTORICAL_ROLE_ONLY` контекст. Он не заменяет исходный mutating plan или
accepted PASS: прежние source, Job UID, Pod UID, spec/image и bytes журнала
сохраняются. `issuerMigrationIO` и rotation CAS по-прежнему требуют прежний Pod.

`captureIssuerMigrationHistoricalReadiness({planPath,evidencePath,sourceSHA,target,
incidentPlanPath,incidentReceiptPath},kube)` создаёт metadata-only контекст.
Пути incident указывают на owned private node-recovery-plan/recovery-complete;
вывод сохраняется оператором в новый private файл. Контекст закрепляет прежние
installation/dependencies/Service, StatefulSet UID, durable SystemID/databaseOID,
текущий Pod и его проверенный Service endpoint. Дополнительно связываются
canonical data PVC, PV UID/claim UID/node affinity из before incident inventory,
current node UID и полный current PVC/PV spec. Private incident records содержат
PV/claim/affinity anchors, а не historical full-spec/inode hashes; последних
этот reader не приписывает им. Counts/Ready boolean не выдают роль или допуск.

`readArtifactRetentionIssuerHistoricalMigrationReady({...original,context},kube)`
заново проверяет terminal Job/Pod join, exact source SQL/schema00200/role ACL,
отсутствие активной rotation/migrator и storage pins. Финальный SQL выполняется
после metadata checks. Повтор не создаёт Job и не дописывает исходный journal.
После следующей смены Pod/context ref reader закрывается до новой явной проверки.

Совместимый facade — `readOptionalIssuerMigrationReadiness({issuerMigration,
issuerMigrationReadiness,target},kube)`. Optional ref имеет только
`{contextPath,contextSHA256}`; SHA — semantic `context.contextSHA256`, не hash
форматирования файла. Без ref возвращается только `{ready}` прежнего normal
пути. С ref возвращается `{ready,historicalReadiness}`; proof имеет schema
`kodex.artifact-retention-issuer-historical-readiness/v1`, purpose
`HISTORICAL_ROLE_ONLY`, contextSHA256 и stable current backend/storage pins.
Observation timestamps/counters в stable projection не входят; owner READ
выполняется заново. Original ready/v1 остаётся byte-semantically прежним.

Release planner отдельно сохраняет optional reference и typed historical proof;
STAGED должен заново прочитать и сравнить их с original plan. Этот optional
profile не является generic CAS/rotation permission. Source-only synthetic
контур: `make test-artifact-retention-issuer-historical-readiness`; live PASS
и actual incident reattestation объявляет отдельно оператор стенда.


Удалённый после eviction/GC terminal Pod не создаётся повторно. Только профиль
`HISTORICAL_ROLE_ONLY` принимает прежний durable PASS с неизменным retained Job,
его original UID/spec/image и Complete1, completionTime не позже принятого PASS,
нулевыми active/failed/terminating/ready/uncounted. Присутствующий Pod всегда
проходит прежний normal positive join; malformed/substitute Pod не открывает
fallback. При отсутствии выполняются bounded namespace metadata scan и fresh
CRI-backed `/api/v1/nodes/<exact-node>/proxy/runningpods/` на всех exact incident
nodes. Любой old Pod UID/Job lineage, missing/Unknown Node, недоступный либо
malformed runtime endpoint закрывает READ. Это дополнительная проверка, не новый
`joined:true`: historical join принадлежит original accepted PASS.

Unrelated API/CRI activity, counts и observation timestamps не входят в stable
proof. После checks повторяется свежий owner SQL. Kubelet `/runningpods/` берёт
данные runtime cache, в отличие от desired `/pods`; upstream считает endpoint
debug/testing, поэтому отсутствие поддержки остаётся closed refusal.
Проверены primary sources Kubernetes `server/server.go` и `kubelet_getters.go`
ветки v1.35.0; actual installed positive readback фиксирует оператор отдельно.
