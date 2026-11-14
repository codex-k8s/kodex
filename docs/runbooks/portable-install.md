---
id: RUN-DEPLOY-1979
title: Опциональный профиль внешней инфраструктуры
type: runbook
status: approved
owner: sre
version: 1.6.11
updated: 2026-11-14
---

# Опциональный профиль внешней инфраструктуры

## Compiler stages вспомогательных образов

Release preflight проверяет закреплённую Go toolchain во всех `Dockerfile` и
`*.Dockerfile` под `services/` и `infra/`. В покрытие входит
`infra/admission-tools/Dockerfile`: canonical builder собирает этот helper до
основного списка компонентов. Его compiler должен соответствовать Go floor
модулей; `GOTOOLCHAIN=local` и проверка фактической версии сохраняются.
Проверка: `scripts/tests/go-toolchain-contract-test.sh`; synthetic regression
проверяет также отклонение устаревшего helper compiler до запуска Docker.

Для замены images при прежних schema heads предусмотрен отдельный opt-in
`kodex.bootstrap-upgrade/v2` с `SAME_SCHEMA_IMAGES`. Он сохраняет все пять
bootstrap effects и требует прежний receipt вместе со свежим native catalog
proof. Порядок и границы: [обновление без миграции](../operations/image-only-upgrade-2067.md).

## Обновление принадлежащего установщику ClusterIP Service

Существующий ClusterIP Service обновляется через JSON Patch с UID/resourceVersion
preconditions и прежним approved inventory fence. Это позволяет менять selector
после Update без SSA conflict и без `--force-conflicts` или пересоздания Service.
Выделенные `clusterIP`, `clusterIPs`, `ipFamilies` и `ipFamilyPolicy` сохраняются;
явно заданное отличающееся значение отклоняется. Отсутствующий desired field не
снимает сохранённую сетевую идентичность. Headless и существующий dual-stack
обрабатываются тем же способом, без изменения состава IP families.

Preflight использует тот же CAS с server dry-run. Чужой owner reference, другой
UID, изменение spec после inventory либо гонка RV отклоняют запись. При неизвестном
ACK сначала нужен readback; старый CAS не повторяется. Создание Service и прежние
SSA-пути NodePort, LoadBalancer и ExternalName сохраняются. Изменение не расширяет
RBAC, сетевые ACL, TLS или image admission. Контракт: #2064.

Официальные источники, проверенные через Context7:
[управление полями SSA](https://kubernetes.io/docs/reference/using-api/server-side-apply/),
[Service v1 API](https://kubernetes.io/docs/reference/kubernetes-api/core/service-v1/).

## Promoted registry для BuildKit

Trusted base и readiness могут использовать promoted pull endpoint. Его отдельная
таблица в `kodex-buildkit-config/buildkitd.toml` использует существующие `pull-ca.pem`
и `base-registry-client.crt/key`; проверка TLS и прежний узкий registry ACL сохраняются.
SHA конфигурации закрепляется в Pod template BuildKit: обновление TOML требует
нового daemon Pod, одного изменения смонтированного файла недостаточно.
Формат проверен по [BuildKit v0.30.0](https://github.com/moby/buildkit/blob/v0.30.0/docs/buildkitd.toml.md).

Fresh material содержит source/promoted aliases отдельно для node pull и BuildKit
base reader. Каждая identity сохраняет собственный credential. Для существующего
material directory явный `reconcile-pull-docker-config.sh --include-buildkit-base`
обновляет только зарегистрированные файлы этих двух identities. До записи
проверяется весь набор файлов; после сбоя нужно сверить их digest и завершить
идемпотентное согласование, затем штатно материализовать projections и перечитать
их у consumers. Ручная подмена Kubernetes Secret либо общий node credential
вместо BuildKit credential не допускаются. Без флага прежний pull-only режим
сохраняется. Успех render/согласования не заменяет настоящие build/push/admission.

Issue #1979 — общая зависимость #1791. Квалификация полного immutable release,
RoleImage и фактической новой установки принадлежит #1977. Этот профиль задаёт
входы установки, а не подтверждает приёмку конкретного IdP или proxy.

## Выбор режима

Без `--deployment-profile` прежний установщик и render сохраняют своё поведение.
`install.sh --components all` по-прежнему выбирает bundled установку.
Новый локальный путь выбирается явно: `install.sh --portable-release`.
Он устанавливает приложение в подготовленный Kubernetes и не создаёт GitHub
Variables/Secrets, Actions runner, внешнего IdP или настройки общего proxy.
Инфраструктурные prerequisites подготавливаются отдельным repo-owned кодом.

| identity.mode | public_tls.mode | Результат |
|---|---|---|
| bundled | managed | Прежние OIDC workload selectors и собственный публичный TLS |
| external | managed | Внешний IdP и собственный публичный TLS приложения |
| bundled | external | Существующий bundled IdP, внешний HTTPS proxy для приложения |
| external | external | Внешние IdP и HTTPS proxy, ingress backend на HTTP/80 |

Примеры находятся в `examples/deployment-profiles/`. Они используют
зарезервированные домены и тестовые IP и не предназначены для live apply.
Профиль не содержит credentials, CA private keys или client secret. Разрешён
публичный клиент с Authorization Code и PKCE S256. Bundled bootstrap выполняется
прежней identity-стадией; external render и локальный delivery path её не вызывают.
Режима анонимного доступа к защищённому API нет.

## Источник настроек и контракт IdP

Несекретный профиль принадлежит Git конкретной установки. Внешний issuer может
иметь произвольный path. Discovery и JWKS должны совпадать с явными параметрами,
а auth/token/logout endpoints — использовать тот же HTTPS authority. Разрешён
TCP/443; другие порты, redirects и пересылка credentials на другой host закрыты.
CA материализуется прежним механизмом `kodex-oidc-ca`; preflight получает файл
той же доверенной CA. Системное доверие вместо выбранной CA не используется.
TLS1.3 соответствует текущему verifier; несовместимый IdP — блокер, не downgrade.

По умолчанию (`claims`) поддержка внешнего IdP сохраняет прежний прикладной контракт токенов: RS256,
точные issuer/audience, sub, sid, organization_id, положительный session_revision,
jti, auth_time и существующие правила expiry/refresh. Наличие discovery не
доказывает совместимость claims. Для ID token сохраняются audience клиента,
nonce и совпадение identity/session claims с access token. Проверить настройки
client/claims нужно до живого входа. Этот профиль сам не меняет общий realm.

Первоначальный owner назначается существующей явной trusted mapping: специальная
scope `kodex.owner` вместе с ролью `kodex-owner` в проверенных claims. Никто не
становится owner по порядку входа. Группы сами по себе не выдают полномочий;
прикладные bindings принадлежат control-plane. Несовместимые claims либо
отсутствие owner mapping должны быть устранены отдельным согласованным изменением.

### Прикладная authority на стороне платформы

Опциональный `--oidc-authority-profile <json>` у `render-web-only.sh` выбирает
независимый от bundled/external топологии контракт. Отсутствие flag сохраняет
`claims`; неизвестный или пустой явно выбранный profile закрыто отклоняется.
Проверяются exact source checkout, schema heads и release lock. Полученный render
передаётся штатному `install.sh --portable-release` по точному digest.

Пример несекретного Git-owned profile:

```json
{
  "schema": "kodex.oidc-authority-profile/v1",
  "managed_by": "git",
  "mode": "platform-managed",
  "policy": {
    "schema": "kodex.oidc-identity-policy/v1",
    "revision": 1,
    "issuer": "https://identity.example.invalid/realm/synthetic",
    "bootstrap_subjects": ["20000000-0000-5000-8000-000000000001"],
    "session_lifetime_seconds": 3600
  }
}
```

`bootstrap_subjects` содержит **issuer-qualified UUID**, полученный функцией
`oidcidentity.ScopedSubject` из независимо проверенных exact issuer и signed `sub`.
Это не имя, email или raw UUID из Keycloak. Указанный UUID синтетический; перед
live apply оператор явно выбирает проверенную identity владельца. Первый
произвольный login не создаёт owner. Для остальных пользователей нужны явные
действующие application bindings; AD/OIDC groups не являются готовым разрешением.

В этом режиме нужны стандартные RS256 issuer/audience/sub/sid/jti/iat/auth_time/
expiry и обычные state/nonce/PKCE, но не `organization_id`, `session_revision`,
`kodex.owner` или `kodex-owner`. Organization/actor и revision сессии разрешает CP.
Access и ID token проверяются независимо; вход не выдаёт credentials внешних API.

Источник истины — profile в deployment Git. Renderer создаёт immutable ConfigMap
с canonical policy, read-only mount только в CP, digest annotations обоих
workloads и следующие новые параметры:

- `CONTROL_PLANE_OIDC_AUTHORITY_MODE=platform-managed`;
- `CONTROL_PLANE_OIDC_IDENTITY_POLICY_FILE=/etc/kodex/oidc-identity-policy/policy.json`;
- `CONTROL_API_GATEWAY_OIDC_AUTHORITY_MODE=platform-managed`.

UI не редактирует эту policy. CP хранит immutable revision/digest history;
изменение content требует увеличения `revision`, удаление mode/file и rollback
revision закрыто отклоняются. Прежние sessions после policy advancement требуют
свежего входа. Generated ConfigMap не является вторым источником настройки.

Forward migration `20261105000100` добавляет durable OIDC policy/session state
независимо от выбранного authority mode. Для обновления exact032 используется
immutable migration profile `platform-oidc-identity`, а при настроенном штатном
backup reader — `platform-oidc-identity-backup`. Оба требуют exact032 catalog,
отсутствия частичных объектов033 и прежнего idle/claim fence до остановки owner.
После применения сверяются полный033 catalog и frozen032 predecessor. Backup
вариант отдельно проверяет ровно SELECT новых четырёх таблиц; права на sequence
не расширяются. Команда `restore-workspace-backup-acl` поддерживает exact033,
восстанавливает этот закрытый набор атомарно и откатывает всю операцию при drift.
Goose Down для033 запрещён: возврат старого image не отменяет schema/revocation.

Forward034 добавляет персональные credentials независимо от выбранной policy.
Для exact033 доступны `personal-credentials` и `personal-credentials-backup`.
Они требуют полный033 catalog/idle fence и проверяют новый catalog вместе с
полным графом project purge. Backup-reader получает только SELECT пяти новых
и трёх изменённых таблиц; `restore-workspace-backup-acl` поддерживает exact034.
Goose Down запрещён. Подробная матрица —
[персональные подключения](../operations/personal-credentials-2033.md).

### Обновление bootstrap receipts при переходах032→033 и033→034

После настоящей forward migration и `capture` доступен отдельный optional
`portable-release.sh --mode upgrade`. Требуются точный upgrade render,
новый private preimage directory, composite receipt и явный
`--allow-reconciled-bootstrap-completion`. Обычный `apply` сохраняет прежнее
поведение новой установки. Upgrade проверяет cluster/namespace, пять
bootstrap identities/specs и полный catalog целевой033/034 до первой rollout mutation;
receipt прежнего install или initial reconciliation вместо composite не подходит.

В upgrade пять bootstrap effects не запускаются заново. Остальные foundation,
authority publisher и workloads проходят штатные preflight/CAS/readback фазы.
Source revision authority задаётся закреплённым render; history/fence не
сбрасываются. Release materializer при существующем exact Job присоединяется к
его выполнению, при отсутствии создаётся один раз. Failed/чужой/неизвестный Job
требует отдельного reconciliation и не удаляется ради повтора. Capture output
нового install не заменяет composite receipt после обновления.

`tools/install/bootstrap_upgrade.py` задаёт отдельный optional profile
`kodex.bootstrap-upgrade/v1`, `managed_by: git`. Он закрепляет SHA предыдущего и
целевого raw render, оба source SHA, `from_schema_heads`/`to_schema_heads` и
`schema_profile: plain|backup`. Поддержаны только exact032→033 и033→034 при неизменных heads
internal-rpc-authority/email-bridge. Прежние install/resume modes и receipts
сохраняют поведение без выбора этого профиля.

`prepare` выполняется offline на operator host: читает private0400/0600 inputs,
проверяет byte pins и сохраняет четыре уже исполненных bootstrap Job из прежнего
render. Их commands/arguments/env/volumes и остальные поля обязаны совпадать с
целевыми; различаться могут только immutable images. Новая CP migration остаётся
в target render с `backoffLimit=0`. Предыдущая migration может иметь только
штатный backoff2 новой установки либо backoff0 прежнего upgrade; прочие поля
её effect обязаны совпадать. Операция не запускает Jobs и не принимает
изменение bootstrap effect как обновление image. Результат содержит отдельный
immutable ConfigMap с hash выбранного upgrade profile.

Secret seeds из canonical render исключаются при чтении предыдущего и целевого
render, как в штатном apply. Upgrade output не переносит их payload и не
перезаписывает работающие проекции; отдельный secret/material lifecycle остаётся
владельцем этих ресурсов. Отключённая observability допускает только штатный
`OTEL_SDK_DISABLED=true` рядом с DSN file reference; произвольные env/envFrom,
дубликаты и другие значения отклоняются.

Operator lifecycle отдельно проверяет target/namespace, provenance полного
release lock/images и отсутствие активной работы, сохраняет preimage/backup,
останавливает writers, выполняет exact migration precondition и единственный
forward-up Job. Перед create нужно проверить, что прежний CP migration Job уже
отсутствует; существующий Job требует reconciliation. При неизвестном результате
create/up выполняется readback того же UID, без автоматического повторного apply.

После успешной миграции `capture` на operator host с `kubectl` и `node` проверяет
cluster/namespace UID, прежний pinned completion receipt и четыре сохранённых
Job. Для новой CP migration нужны новый Job UID, terminal `Complete`, отсутствие
предыдущих failures, один Job-owned Pod с неизменённым executable spec,
действительный image digest и exit0 всех containers. Затем проверяются heads всех
трёх БД и полный catalog закреплённой целевой033/034 штатным plain/backup verifier.
Предыдущий033 proof не подтверждает034. Смена UID/spec либо
неуспешная проверка закрывает capture; schema version сама по себе недостаточна.

Результат — receipt `FORWARD_MIGRATION_UPGRADE`: исторические rows сохраняются,
новая row получает только реально наблюдённые UID/seal/completion time. Значения
Secret/DSN и business rows не читаются. Для последующего readback нужен явный
`--allow-reconciled-bootstrap-completion`; receipt привязан к immutable profile
целевого render. TTL-удаление подтверждённого Job не запускает его заново.
Capture не изменяет Kubernetes status и не означает завершения rollout.

| Состояние | Проверяемое действие | Результат/восстановление |
|---|---|---|
| PLANNED | Raw render/source/schema pins; неизменность четырёх effects | Offline `prepare`; никаких mutations |
| QUIESCED | Exact target, preimage/backup и idle fence | Выполняет operator lifecycle перед migration |
| MIGRATION UNKNOWN | Тот же Job/Pod UID и actual schema | Readback; новый create не выполняется |
| MIGRATION COMPLETE | Terminal Job, actual Pod/image и полный целевой033/034 catalog | Read-only `capture`, новый composite receipt |
| ROLLOUT/REJOIN | Новый render, profile и pinned receipt | Existing readback проверяет live UID/spec; отсутствующие known Jobs не повторяются |
| FAILED/DRIFT | Неизвестный effect, extra Pod/privilege либо несовпадение pins | Закрытый отказ; backup/schema compatibility определяют дальнейший operator plan |

Проверки: `test_bootstrap_upgrade.py` и прежние `test_bootstrap_completion.py`,
включая missing TTL Jobs, foreign UID, mutated command/env/image, nonterminal Job,
прошлый failure и schema rejection. Последовательный upgrade033→034 сохраняет
четыре прежних effects и выбирает034 proof; пропуск032→034 и неизвестные
версии отклоняются до cluster access. Native033/034 schema/backup proof проверяется
отдельно на disposable PostgreSQL. Семантика terminal conditions сверена с
[Kubernetes Job API v1.35](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/)
и [Job controller](https://kubernetes.io/docs/concepts/workloads/controllers/job/):
`Complete` после завершения Pods, отдельно от `SuccessCriteriaMet`.

Подпись и provider expiry проверяются при каждом native admission. Типизированные
`OIDCSessionService` requests пусты: actor, organization и revision выбираются
по verified credential и owner state. Обычные HTTP/WS требуют current eligibility.
Только отдельные self-closure RPC читают собственный существующий tombstone,
включая revoked/disabled/application-expired состояние, пока provider bearer жив.
Они не дают обычного application access и не создают user/session.

Logout сохраняет `REVOKING` family CAS до owner command; refresh и обычные запросы
сразу закрываются. CP атомарно пишет revoke и один audit, повтор читает тот же
tombstone. После точного подтверждения family стирает tokens и хранит terminal
receipt для повторного cookie cleanup. Origin/CSRF/If-Match обязательны и при
повторе; receipt принимается лишь на exact DELETE session. Потерянный ответ не
повторяет provider code/refresh. Истёкший bearer не получает обход expiry:
незавершённое закрытие остаётся fail-closed и требует нового свежего входа.
Новая platform-managed browser family запрашивает fresh authentication, чтобы
не переоткрыть прежний revoked SID/auth_time. API bearer bootstrap сохраняется
через отдельную encrypted family без фиктивного refresh token и без продления
срока access token. Refresh не меняет authority mode или owner lifetime.

Схема `20261105000100` forward-only. Policy/session high-watermarks и tombstones
не удаляются при возврате image; старый image без доказанной schema/policy
совместимости не является безопасным rollback. До миграции нужны согласованный
plan/preimage/backup, maintenance fence и проверенный forward recovery. Нельзя
переключать profile старым render либо исправлять policy SQL/sed внутри Pod.

Проверки: `python3 -B -m unittest discover -s tools/install -p
'test_oidc_authority_profile.py'`, native verifier/browser и gateway security
`go test -race`, CP typed contracts/policy и disposable PostgreSQL
`TestStandardOIDC.*Component`. Native registration/audit/revoke, layer tests,
release render, normal browser E2E и installed acceptance учитываются отдельно;
локальный PASS не означает установленную функцию.

Внешний режим передаёт один client ID, scope и явные auth/token endpoints как в
PWA runtime config, так и в server-side browser client. Никаких токенов в PWA
runtime config нет. Legacy значения сохраняются при отсутствии новых inputs.

## Внешний proxy

Внешний origin остаётся HTTPS. Из app render исключается только Certificate
`staff-control-center-public`; CA, ServersTransport, backend mTLS, TLS registry,
S3 и IdP сохраняются. Все три app маршрута используют точный Host и entrypoint
`web`. Middleware сначала проверяет непосредственный source IP по списку
точных /32 и /128, затем фиксирует forwarded scheme/host. IP strategy по XFF не
включается. Смена IP proxy требует нового проверенного render.

По умолчанию оператор передаёт readback точного Traefik Deployment с
`--entryPoints.web.address=:80`, завершённым rollout и точным
`forwardedHeaders.trustedIPs`. Опциональный `public_tls.ingress` допускает
DaemonSet и отдельный container/backend port при прежнем HTTP Service port80:

```json
{
  "controller_kind": "DaemonSet",
  "entrypoint_port": 8000,
  "proxy_protocol_trusted_cidrs": ["192.0.2.19/32"]
}
```

Объект содержит ровно эти три поля. Без него прежний контракт сохраняется;
пустой `proxy_protocol_trusted_cidrs` запрещает PROXY protocol. Непустой список
задаёт точные transport peers отдельно от `trusted_proxy_cidrs` HTTPS proxy.
Traefik принимает PROXY v1/v2 только от этих peers; insecure trust запрещён.
Оператор обязан ограничить доступ к доверенному transport hop: общий relay,
которому посторонние могут отправлять собственный PROXY header, не доказывает
источник. Это отдельная сетевая проверка, не свойство JSON профиля.

`--ingress-controller <name>` выбирает имя ресурса; его kind берётся из профиля.
Прежний `--ingress-deployment` сохранён как alias. Проверка сверяет controller
status, владельцев текущих Pod (Deployment→ReplicaSet→Pod либо DaemonSet→Pod),
их исполняемую конфигурацию и полный EndpointSlice состав выбранного Service.
Service port80 должен вести на закреплённый backend port. Не допускаются
insecure trust, редирект на websecure или неоднозначный static config.
Этот код общий ingress controller не перенастраивает. Сетевая граница и
фактический source после NAT должны быть проверены оператором; middleware —
дополнительная защита, а не доказательство ограничения VM:80 firewall.
SSH и сетевые маршруты не меняются.

External identity не зависит от bundled OAuth2 Proxy middleware. Browser session,
API authorization, CSRF/Origin и server-side revocation остаются в gateway.
Защита API не заменяется доступностью HTML shell или health endpoint.

## Render и применение

Сборка записывает `schema_heads` трёх БД в release lock. Portable render требует
их совпадения с исходным деревом и exact checkout SHA из lock; tracked дерево
должно быть чистым. Старый lock без
этих pins остаётся допустим для legacy пути; для portable требуется новый lock.

Вызвать `tools/release/render-web-only.sh` с прежними lock/digest, публичным
origin, ingress и Kubernetes API inputs, добавив `--deployment-profile`.
External identity задаёт OIDC inputs из профиля; CLI workload selectors в этом
режиме запрещены. Immutable runtime ConfigMap получает новое имя и ссылку,
revision согласуется с readiness. Профиль и его digest сохраняются в render.

Локальный путь принимает уже полученный exact render:

```sh
./install.sh --portable-release --mode plan \
  --render /private/release/render.yaml --render-sha256 <sha256>
./install.sh --portable-release --mode preflight \
  --context <exact-context> --render /private/release/render.yaml \
  --render-sha256 <sha256> --oidc-ca-file /private/oidc-ca.pem \
  --ingress-namespace <namespace> --ingress-deployment <deployment> --ingress-service <service>
```

`plan` не вызывает Kubernetes или сеть. `preflight` выполняет только чтение и
ограниченные запросы discovery/JWKS с TLS/SNI, без токенов, retries, redirects
и ambient proxy. DNS snapshot должен полностью входить в endpoint allowlist;
HTTPS подключение использует проверенный IP без повторного DNS lookup.
Успех отдельно показывает NOT_RUN для workload connectivity и login.

`--mode apply` требует дополнительно новый абсолютный `--preimage-directory`.
Render копируется и сверяется с переданным digest, preimage сохраняется с правами
0700/0600. Он содержит candidate render и metadata/image pins существующих
workloads; он не является backup БД или полным rollback artifact. Перед обновлением
нужны отдельные проверенные backup и совместимость миграций. Перед первой записью читается полный inventory выбранных ресурсов. Совпавшее имя
без `app.kubernetes.io/part-of=kodex` закрыто отклоняется, включая Jobs и namespaces.
Профиль не присваивает чужие namespaces: подготовить их ownership нужно своим
инфраструктурным кодом. Новые namespaces secret materializer создаёт с этим label.
Heartbeat зарегистрированного controller-owned Lease сохраняется и не считается
изменением install intent; installer не переписывает его holder/renewTime.
Inventory связан с exact context и render digest, содержит UID и hash исходного
состояния. Create не заменяется SSA upsert; update передаёт текущий resourceVersion,
а delete — атомарные UID/resourceVersion preconditions. Drift требует readback,
а не слепого повтора. Старые immutable catalogs сохраняются; их GC и legacy repair
устаревших admission objects не выполняются этим portable путём.

Затем вызываются
прежние prepare-preflight, preflight, apply и readback; preflight prerequisites
повторно проверяются перед каждой стадией. Prepare-preflight изменяет принадлежащие
установке ресурсы и поэтому выполняется только внутри явно выбранного apply.
`--mode readback` использует тот же digest и bounded dependency checks. После
проверок готовности каждый выбранный ресурс сравнивается с candidate: images,
env/config references, ConfigMap data, Ingress/NetworkPolicy/Middleware/transport,
labels и исходные pins. Только документированные Kubernetes defaults нормализуются;
лишние исполняемые/security поля и изменённые config values закрывают результат.
Pod spec сравнивается с закрытым набором defaults, включая volume defaultMode;
произвольные command/envFrom/initContainers/hostNetwork не считаются defaults.
Portable readback при неготовом Certificate завершает проверку ошибкой и не
запускает legacy auto-repair или удаление сертификата. Дополнительно
через локальный PostgreSQL socket читается migration head трёх БД, без DSN/password,
SQL repair или downgrade. Это SRE readback, не полномочие сотрудников платформы.

Переход существующего public Certificate в external TLS автоматически не
выполняется: требуется отдельная миграция с preimage и проверкой ownership.
`deferred` остаётся отдельным согласованным окном ACME прежнего установщика.

## Матрица проверки и возврат

| Сценарий | Изменение / отказ | Авторитетный readback |
|---|---|---|
| plan | Нет cluster/domain mutation | Exact render digest |
| external IdP render | Только exact OIDC network edges, browser config и runtime revision | Profile digest, Deployment env, immutable ConfigMap reference |
| external TLS render | Только app Certificate/routes и proxy middleware | Host/entrypoint, middleware order и прежний backend ServersTransport |
| malformed endpoint / broad CIDR / unknown field | Отказ до apply | Закрытый diagnostic, без body/credential |
| discovery/DNS/JWKS/CA mismatch | Отказ до mutation | Bounded dependency preflight; live login NOT RUN |
| неизвестный исход apply | Не повторять внешний effect вслепую | Прежние owner receipts, exact resources, migration head, jobs/status |
| restart / повторный apply | Прежний lifecycle и forward-only migrations | Images/config/schema, sessions/files и отсутствие повторных effects |
| rollback | Только совместимые config/image/schema | Отдельная инструкция принятого release; goose down не используется |

Локальные точки входа: `make test-web-only-release`, Go tests модуля
`libs/go/oidcverifier`, тесты gateway security/session/app. Проверяются оба профиля,
независимые режимы, отрицательные endpoint/proxy случаи, сохранение backend TLS и
семантики browser grant. Живая приёмка отдельно включает login/roles/refresh/logout,
API/WS/SSE/upload, обход proxy, spoofed headers и restart. Наличие ConfigMap,
зелёных Pod либо успешного host preflight не заменяет эту приёмку.

Справочники: [Traefik v3.6.8 entrypoints](https://github.com/traefik/traefik/blob/v3.6.8/docs/content/routing/entrypoints.md),
[Traefik v3.6.8 ServersTransport](https://github.com/traefik/traefik/blob/v3.6.8/docs/content/routing/providers/kubernetes-crd.md),
[go-oidc v3.20.0 verifier](https://github.com/coreos/go-oidc/blob/v3.20.0/oidc/verify.go).
Context7 проверен для Traefik и go-oidc; наличие документации более новой версии
не подтверждает возможности установленного controller. Версию controller и
совместимость нужно закрепить в release lock конкретной установки.

Дополнительно проверены через Context7 официальные
[Traefik v3.7.15 entrypoints](https://github.com/traefik/traefik/blob/v3.7.15/docs/content/reference/install-configuration/entrypoints.md)
и [ipAllowList](https://github.com/traefik/traefik/blob/v3.7.15/docs/content/reference/routing-configuration/http/middlewares/ipallowlist.md):
раздельные trustedIPs для forwarded headers и PROXY protocol, RemoteAddr без XFF
ipStrategy. Локальные synthetic проверки не доказывают live source restrictions.

### Один внешний адрес для optional services

[Helm unit portable services](../../infra/portable-services/README.md) (#1986)
размещает S3 API и storage UI на явных отдельных prefixes, OCI на `/v2/`.
Он применяется отдельным infrastructure prerequisite до платформы. Для S3
нужны HTTPS endpoint с prefix и path-style; отдельные hostname и внешние
службы остаются штатной альтернативой. Secret values не входят в Helm values.

## Опциональный source registry внутри Kubernetes

Для новой установки #1977 `tools/install/registry_source.py` принимает закрытый
контракт `kodex.registry-source/v1`. Этот дополнительный шаг вызывается явно
после штатного release render; без него поведение прежнего установщика сохраняется.
Он не изменяет образы, release lock, bundled/external OIDC или публичный TLS.

Синтетический пример конфигурации:

```json
{
  "schema": "kodex.registry-source/v1",
  "namespace": "synthetic-infra",
  "service": "synthetic-registry",
  "service_uid": "11111111-1111-1111-1111-111111111111",
  "pod_labels": {"app.kubernetes.io/name": "synthetic-registry"},
  "target_port": 8443,
  "ca_configmap": "synthetic-ca"
}
```

`registry.push` и `registry.node_pull` в проверенном release lock должны точно
совпадать с `<service>.<namespace>.svc.cluster.local`. Service предоставляет
HTTPS/443, endpoint — указанный TCP target port. CA ConfigMap в `kodex-system`
содержит только публичный сертификат `ca.crt`. Пароли и private keys в контракте
запрещены; source credential по-прежнему материализуется штатным Secret backend.

В owned каталоге0700 выполнить `registry_source.py render --configuration
<profile.json> --release-lock <lock.json> --lock-sha256 <sha256> --input
<fresh-render.yaml> --output <absolute-new-render.yaml>`. Выход создаётся0600
без перезаписи. В apply передаётся именно этот окончательный render и его digest.
Это контракт новой установки, а не live patch immutable admission policy.

Materializer получает read-only CA, `SSL_CERT_FILE`, точный `NO_PROXY` и
NetworkPolicy с namespace+Pod selectors в одном peer. TLS hostname verification
сохраняется. Source registry не становится адресом допуска: promoted RoleImage
repository выводится из отдельного `pullRegistryHost`, admission hash и его
параметры пересчитываются согласованно. Для штатного внутреннего pull gateway
добавляется HTTPS/443 alias к прежнему Service/5000, а внешний TCP route убирается.
Подпись, provenance, SBOM, scanner и mTLS не обходятся.

До apply выполнить режим `preflight` с теми же configuration/lock/digest и
`--context <exact-context>`. Он проверяет Service UID, selector, target port,
публичную CA и принадлежность ready EndpointSlice/Pod. Это read-only проверка
метаданных; TLS/auth реального workload и exact node pull проверяются отдельно.
Kubelet/containerd находятся вне Pod DNS: оператор адресно настраивает разрешение
имени, доверие CA и read-only pull credential на каждом целевом узле. Импорт
прикладных образов не заменяет эту проверку. Смена Service UID/IP требует нового
проверенного плана; trust-all, plaintext и внешнего fallback нет.

Проверки: `python3 -B tools/install/test_registry_source.py` (также в
`make test-web-only-release`). Fixtures синтетические; live cluster не используется.
Документация K3s private registry через Context7 проверена; конкретная настройка
узлов зависит от закреплённой версии дистрибутива и не выполняется этим overlay.

Для registry за существующим HTTPS ingress предусмотрен
`kodex.registry-source/v2`: к тем же полям добавляются `tls_hostname`,
`service_ip` и `service_port_name`. `registry.push` точно равен TLS hostname;
`registry.node_pull` остаётся отдельным immutable адресом прикладных образов.
Materializer получает единственный `hostAliases` для этого hostname на закреплённый
ClusterIP ingress Service, сохраняя исходные SNI, HTTP Host и проверку сертификата.
Публичный loopback DNS локального стенда не используется внутри Pod. Конфликтующий
alias, loopback/link-local/unspecified адрес и неизвестные поля отклоняются.
Ingress, его маршруты и общесистемный DNS этим преобразованием не изменяются.

Когда публичный promoted pull endpoint обслуживает тот же ingress Service,
explicit `kodex.registry-source/v3` дополнительно требует `promoted_tls_hostname`.
Он обязан совпадать с immutable admission policy и exact `HostSNI` passthrough
маршрутом в `kodex-image-registry-pull:5000`. Профиль добавляет только BuildKit
hostAlias и его exact namespace/Pod/port egress; mTLS identity, CA, SNI, digest,
promotion ACL и node pull конфигурация не меняются. Preflight сверяет также live
маршрут и policy. Это не настройка общесистемного DNS и не доказательство node pull.

| Состояние promoted route | Проверка / действие | Readback / отказ |
|---|---|---|
| Plan | Exact source Service UID/IP, source и pull hostname, policy и TCP route | Только render/get; коллизии и wildcard закрыты |
| Apply | До первой RoleImage build; exact previous/desired delta и fenced inventory | NetworkPolicy, immutable profile, затем BuildKit rollout; чужие workloads не затрагиваются |
| Restart / потерянный ответ | UID/seal/RV прежнего Deployment и фактический desired template | Rejoin/readback; новая build не запускается повторно |
| Readiness | Настоящий BuildKit pull/build/push с прежними mTLS identities | Не заменяется успешным DNS lookup или готовым registry Pod |
| Активная build / changed Service | Требуется отдельное завершение build либо новый approved profile | Initial repair не отменяет task/lease и не меняет authority |

Проверены через Context7 официальные Kubernetes документы
[HostAliases](https://kubernetes.io/docs/tasks/network/customize-hosts-file-for-pods/)
и [NetworkPolicy selectors](https://kubernetes.io/docs/concepts/services-networking/network-policies/).

Сквозной путь: операторский lock/profile → immutable ConfigMap и Job →
Service с точными UID/ClusterIP/selector → ready EndpointSlice и Pod с проверенным
именованным портом → HTTPS ingress → source registry. Проверка перед apply
сверяет все эти Kubernetes-связи; NetworkPolicy разрешает только выбранные
namespace/Pod labels/port. Ошибка останавливает материализацию до запуска;
событие домена не создаётся, авторитетный read path — Kubernetes и exact digest
registry. Привилегии задаёт операторский профиль, содержимое OCI их не расширяет.
Повторное применение идемпотентно; replacement Service требует нового профиля.
Готовность TLS/auth и копирование каждого digest подтверждаются отдельно живым Job.
Проверены Context7 `/websites/kubernetes_io`, официальные
[HostAliases](https://kubernetes.io/docs/tasks/network/customize-hosts-file-for-pods/)
и [именованные порты Service](https://kubernetes.io/docs/concepts/services-networking/service/).

## Закреплённый OpenCode build-рецепт без смены приложения

В #1977 возможна отдельная packaging-коррекция OpenCode download stage при
сохранении frozen application source. `build-release.sh` принимает явный
`--build-recipe-source-sha <exact40hex>`. Скрипт запускается из чистого tooling
checkout этого SHA, cwd остаётся чистым application checkout `--source-sha`.
Оба commit должны быть доступны application Git object database.

Опция разрешает заменить только stage `opencode-binary` в
`services/jobs/agent-runner/Dockerfile` внутри одноразового build context.
Другие stages должны совпадать byte-for-byte; изменения любых других
`services/`, `libs/` или `contracts/` отклоняются. Artifact checksum и native
version probe сохраняются. Живое редактирование исходников не допускается.
Без опции контекст и формат прежнего release lock сохраняются.

Полный lock дополнительно содержит `build_recipe`: schema
`kodex.build-recipe/v1`, отдельные recipe/application SHA и SHA256 Dockerfile.
`source_sha` продолжает обозначать application source, а не выдаёт packaging
commit за версию работающих сервисов. В итоговом отчёте нужны оба pin.
Повторный build после частичной публикации предваряется readback опубликованных
manifest digests; после него сравниваются сохранённые компоненты. Cache hit и
повторный push одинаковых bytes не заменяют image admission/node pull.

Штатный OpenCode download stage использует тот же pinned Node/Debian base,
что full runtime: готовые curl/CA, HTTPS-only redirects, ограниченные timeouts,
checksum и проверку версии. Повторная установка APT для этого stage не требуется.
Тест: `python3 -B tools/release/build-recipe-test.py`, включён в общий release
target. Git fixtures синтетические, внешнего registry и credentials нет.

## Общий runtime profile и promoted namespace

Full release build допускает только `--role-base-runtime-engine opencode`
и использует его по умолчанию. Единственный `agent-runner` служит также documents
base; отдельного Codex либо optional OpenCode runner в inventory нет.
Lock фиксирует `role_base_runtime_engine: opencode`; другой runtime закрыто
отклоняется до materialization/apply. Full RoleImage дополнительно проходит
совместимость ABI, admission и проверку фактического digest.

`promoted_registry.py` согласует producer и consumers по итоговому lock:
source prefix принадлежит только публикации source artifacts, внутренние
promotion repositories сохраняют канонический `kodex/`. Materializer выбирает
строго `kodex/agent-runner`; прочие targets
отклоняются. `promotedPullRepository`, node readback, certificate-guard readback
и `ImageAdmissionPolicyParameters` указывают на действительно материализованные
repositories с exact digest. Policy hash пересчитывается вместе с projection.

Каталог RoleImage использует тот же `--promoted-pull-host` и канонический
namespace `kodex/` для standard и documents base. Source repository prefix
остаётся источником публикации и не подменяет promoted namespace. Exact base
reference/digest standard совпадают с platform default RoleImage: shipped-рецепт
можно копировать без ослабления `CopyBootstrapSelection`. Копия ещё должна пройти
обычную сборку, настоящий verified inventory, admission и promotion; исправление
reference само по себе не даёт допуск и не закрывает #1850. OCI context сохраняет
собственные immutable source pins.

Та же exact promoted repository/digest пара назначается trusted runtime base в
admission parameters и rollout annotations control-plane, role-image-builder и
BuildKit. Иначе каталог может пройти копирование, а сборщик отклонит свою
configured trusted base при запуске. Helper принимает только прежний exact source
alias либо уже согласованный promoted alias выбранного lock runtime; произвольный
host, другой компонент или digest отклоняются. Повторный bind идемпотентен,
policy hash и параметры пересчитываются вместе. Source context и materializer
source references остаются отдельными; их полномочия и проверка inventory прежние.

Штатный render вызывает этот шаг самостоятельно. Optional registry-source
overlay использует тот же helper; запускать его нужно из полного pinned tools
checkout вместе с каноническим materializer script. Script SHA256 фиксируется
в аннотации ConfigMap. Изменение script/inputs выполняется до apply; это не
разрешение hot patch уже действующей immutable admission policy. TLS/mTLS,
подписи, provenance, SBOM, scanner и разделение source/promotion credentials
сохраняются.

## Установка без Prometheus Operator

`render-web-only.sh --disable-monitoring-resources` отключает экспорт OpenTelemetry
через `OTEL_SDK_DISABLED=true` и исключает ServiceMonitor, PodMonitor и
PrometheusRule из итогового render. Prometheus Operator/его CRD для такого
профиля не нужны. По умолчанию monitors и alerts сохраняются. Неизвестные
ресурсы monitoring API отклоняются, а не удаляются молча. Security policies,
RBAC, подписи образов, lifecycle gates и readiness probes не отключаются.
Установленные operator/CRD и чужие ресурсы команда не удаляет.

Для уже закреплённого render того же контракта доступен эквивалентный
postprocessor `python3 tools/install/observability_profile.py --render <yaml>
--disabled`. Версию tools закрепить отдельно, сохранить прежний render и
снова выполнить plan/preflight с новым SHA до apply. Это не миграция
application/schema и не разрешение пропустить inventory существующих ресурсов.

Inventory/readback совместимы с ответом `kubectl get -f - -o json` как объектом
либо `List` из одного объекта. GVK/namespace/name обязаны точно совпасть с
запрошенным descriptor. Пустой ответ/List означает отсутствие; несколько или
чужой объект отклоняются. Это не основание менять ownership labels существующих
ресурсов: UID/seal fencing и запрет чужих объектов продолжают действовать.

Git-managed model-provider projection получает `app.kubernetes.io/part-of=kodex`
при генерации. Portable inventory до discovery/mutations проверяет ownership
всех non-Secret ресурсов render; apply повторяет проверку desired resource.
Непомеченные ресурсы и реальные чужие объекты не принимаются автоматически.
При частичном прежнем apply сначала сверить точные intent, UID, spec и preimage;
не добавлять labels посторонним объектам для обхода защиты.

Provider credential recovery выбирает account по installation owner и точному
stable key, читает definition из авторитетной записи и допускает только Codex либо
optional OpenCode gateway. Тип credential и gateway definition обязаны совпадать
с account до создания Secret и CAS. Имя account не подменяет тип provider; fallback
между Codex и gateway или между двумя gateway отсутствует.

| Этап | Полномочия и переход | Отказ и восстановление |
|---|---|---|
| Seed → recovery | Operator после Ready control-plane; owner-scoped account и текущая credential revision | Неизвестный/неоднозначный account либо неподдержанный provider закрыто отклоняется |
| Credential import | Exact definition, account ref, version и credential ref; immutable Secret → SQL CAS с audit | Другой gateway, Codex auth для gateway, foreign owner, stale version не меняют account |
| Lost create / SQL response | Deterministic Secret identity и readback current revision | Не удалять прежний Secret; следующий запуск сначала читает сохранённый результат |
| Canonical recovery | Проверка descriptor и свежей owner projection, затем metadata CAS | Exact replay не добавляет revision/audit; token values не публикуются |

Этот шаг не обращается к модельному API и не доказывает успешный runtime turn.
Без параметра definition прежние Codex SQL operator callers сохраняют свой тип.

Egress gateway читает runtime signing и proxy CA Secrets с mode0440 через свою
группу10001: Pod задаёт `fsGroup: 10001` и `OnRootMismatch`. UID остаётся non-root,
mounts read-only, capabilities ALL отброшены. Secret bytes/mode/rotation не меняются.
Для закреплённого partial render без этой группы выполнить публичный
`tools/install/egress_secret_access.py --render <yaml>`, сохранить новый digest и
preimage, затем передать `--repair-egress-secret-access` в `resume-workloads`.
Без флага дополнительный delta отклоняется; произвольная группа, Secret или
расширение привилегий отклоняются. Этот флаг не разрешает replay bootstrap Jobs.

Пустой Role/ClusterRole (`rules: []`) API может сохранить как `rules: null`.
Preflight и фактическая запись используют одну закрытую нормализацию []→null;
readback принимает только эквивалентные пустые формы (null/absence/[]). Непустые
правила сравниваются полностью, добавление любого разрешения отклоняется.
Нормализация не включает force-conflicts и не изменяет image/schema contract.

### Адресная замена immutable bootstrap image

В рамках #1977 доступен **опциональный** профиль stateless компонента
`egress-gateway`, `registry-pull-authorizer`, `control-plane` либо
`integration-gateway`. По умолчанию он отсутствует; bundled/external identity и TLS
выбираются независимо. Stateful компоненты, миграции, RoleImage, admission policy,
authority history и credentials этот профиль не меняет. Это продолжение частичного
bootstrap, а не квалификация полного нового релиза или общий production upgrade.

Operator сначала собирает stock image и проверяет его source/provenance, тесты,
неизменность общих контрактов/библиотек и schema heads, затем exact digest через
node CRI. Требуются полный прежний release lock и отдельная запись нового
component source/image. При разных source SHA сохранить оба набора pins; не
переписывать source всего базового lock ради одного нового образа. Совместимость
кода и node pull не доказываются самим параметром установщика.

Синтетический профиль (hex-строки должны быть заменены проверенными значениями):

```json
{
  "schema": "kodex.bootstrap-component-image/v1",
  "component": "egress-gateway",
  "base_release_lock_sha256": "<64-hex exact baseline lock bytes>",
  "base_source_sha": "<40-hex baseline source>",
  "source_sha": "<40-hex component source>",
  "expected_image_ref": "images.example.invalid/base/egress-gateway@sha256:<64-hex>",
  "image_ref": "images.example.invalid/fixed/egress-gateway@sha256:<64-hex>"
}
```

Для `registry-pull-authorizer` используются соответствующие refs/source и
дополнительное обязательное поле `"runtime_engine": "codex"` либо `"opencode"`.
Значение должно совпасть с `role_base_runtime_engine` исходного lock (при отсутствии
поля lock подразумевается Codex). Меняется только контейнер `pull-authorizer`
Deployment `kodex-image-registry-pull`, его image/provenance и env
`REGISTRY_PULL_RUNTIME_ENGINE`; соседние registry и certificate-guard сохраняются.

Для `control-plane` и `secret-broker` обязательно поле `schema_heads`, полностью совпадающее с
исходным полным lock (все имена и положительные integer heads). Profile разрешает
только одноимённый основной контейнер соответствующего Deployment; sidecars, конфигурация,
replicas и bootstrap/migration Jobs сохраняются. Operator отдельно подтверждает
неизменность схемы и общих библиотек/контрактов; наличие поля этого не доказывает.
Профиль не позволяет применить изменение схемы либо откатить миграции.

Для нескольких компонентов тот же CLI принимает оболочку
`{"schema":"kodex.bootstrap-component-images/v1","profiles":[{"profile":{...},"sha256":"<64-hex>"}]}`.
Массив содержит от одного до четырёх уникальных одиночных v1 profiles; вложенные
оболочки и неизвестные цели запрещены. Child SHA вычисляется по UTF-8 bytes
`json.dumps(profile, indent=2, ensure_ascii=True) + '\n'`, с сохранением порядка
ключей JSON. Он остаётся provenance отдельного компонента; внешний SHA закрепляет
весь bundle. Уже установленный одиночный profile с теми же canonical bytes не
меняет provenance при добавлении второй цели. Не совпадающий прежний SHA требует
разбора происхождения, а не перезаписи annotations. Все profiles связаны с одним
полным base lock. Оболочка не отменяет source compatibility и CRI pull checks.

При новой установке renderer сам назначает authorizer runtime из явного release
lock и проверяет соответствие выбранной trusted role base. OpenCode разрешает
только дополнительный `kodex/agent-runner-opencode` для BuildKit base reader и
installer/node reader. Другие identities, методы, credentials и admission gates
сохраняются. Нельзя включать этот env у старого образа authorizer как замену
проверенному executable image. У Codex прежний env и разрешения не меняются.

| Путь | Полномочия / результат | При отказе |
|---|---|---|
| Operator → profile CLI | Exact profile SHA и base-lock bytes → render image/provenance | Никаких обращений к внешнему API; неизвестный image/host отклоняется |
| CI/operator → resume wrapper → preflight | Exact renders и before/profiled/after workload spec; schema/history остаются авторитетными | Нет записи при несовпадении; runtime должен оставаться пустым |
| Apply → Deployment controller → node runtime | Только закреплённый image; обычные registry/admission/readiness gates | Сохранить preimage/receipt; reconcile прерванный apply до повторного действия |

Профиль привязан к точным байтам base lock и своему SHA256. Старый image обязан
совпасть с `pull_ref` компонента в этом lock; новый — другой digest того же registry
authority. Mutable tags, другой host, дополнительные поля, чужой Deployment,
неизвестный predecessor и конфликтующие provenance annotations закрыто отклоняются.
Поддерживается только явно перечисленный компонент; расширение набора требует
отдельного проверенного контракта, а не произвольного имени workload из параметра.

Для исправления только PWA доступен opt-in `staff-control-center`: заменяется
только одноимённый container одноимённого Deployment в `kodex-system`. Сохраняются
replicas, env, volumes, ServiceAccount, API/gateway/runtime images и bootstrap Jobs.
Дополнительный sidecar закрыто отклоняется. Совместимость HTTP-контрактов и generated
client должна быть проверена до выбора отдельного image; base lock и source остаются
явно закреплёнными. Без component profile прежняя поставка не меняется.

Материализовать новый render до apply:

```bash
python3 tools/install/component_image_profile.py \
  --render candidate.yaml --configuration component-image.json \
  --configuration-sha256 "$component_profile_sha256" --base-release-lock release-lock.json
```

Сохранить новый render digest, свежий preimage/backup и точный предыдущий render.
В `portable-release.sh --mode resume-workloads` явно передать
`--component-image-profile component-image.json`,
`--component-image-profile-sha256 "$component_profile_sha256"` и
`--base-release-lock release-lock.json`, а также прежние параметры continuation.
Wrapper делает snapshot обоих JSON, проверяет pins и материализованный render до
записи в Kubernetes. `authority_registry.py preflight-workloads` получает те же
три параметра; допустим только полный exact delta image/provenance относительно
предыдущего render. Live workload обязан совпасть с before, результатом точных именованных profiles
до замены image либо after. Промежуточный вариант допускается только при явном
component image profile; произвольные комбинации не принимаются. Runtime
остаётся пустым, пять bootstrap Jobs проверяются, не переигрываются, schema и
опубликованная authority history сохраняют прежние проверки. Без опции новый
image отклоняется. `--check` у profile CLI проверяет render без его изменения.

### Подготовка component locks из штатной публикации

`tools/release/component-publication.mjs` соединяет существующие
`build-stable-local.sh` и `stable-image-publication.mjs` с component migration.
Он не строит образы, не меняет registry/CA/RBAC и не объявляет admission или
совместимость кода доказанными по успешной публикации. Operator review exact
cohort, установка native registry transport и разрешение на target обязательны.
Полные source SHA, base release lock, schema и существующие Deployment images
берутся из readback; installation seal не переписывается.

Сборка выполняется в чистом checkout нужной ревизии. Для последовательного
обновления042→043→044 нужны разные совместимые source revisions: бинарник,
требующий044, нельзя возобновить на043. Publisher принимает отдельный
`--source-checkout /private/source`: он закрепляет build source и source своего
executor отдельно, проверяет оба при apply/readback. Этот параметр повторяют
во всех фазах. Если его нет, сохраняется прежняя работа из текущего checkout.
Все обращения к выбранному кластеру выполняют с явными `--kubeconfig` и
`--context`, в том числе внутренний worker publisher.

Приватный JSON config имеет schema `kodex.component-publication-config/v1`:
`profile`, `sourceSHA`, `sourceCheckout`, `context`, `clusterUID`, `namespaceUID`,
`baseSourceSHA`, `baseReleaseLockSHA256`, путь `baseReleaseLock`, три map
`baseSchemaHeads`, `schemaHeadsBefore`, `schemaHeadsAfter`, все узлы `nodes`
с полями `name`/`uid` и ordered `components` закрытого профиля. Для каждого
компонента нужны `name`, прежний `expectedImageRef`, изменённый source `anchor`
в корне этого компонента и пути `buildReceipt`, `publicationPlan`,
`publicationEvidence` к настоящим результатам штатного publisher. Последний —
JSONL с подтверждёнными INTENT/PASS. Inputs — owned regular0600 без ссылок,
canonical compact либо pretty JSON с LF; duplicate keys отклоняются.
Registry этого producer фиксирован штатным publisher:
`registry.local.kodex/kodex`. Другой registry нельзя объявить опубликованным
путём изменения строки в config.

```bash
node tools/release/component-publication.mjs prepare \
  --configuration "$private_dir/component-config.json" \
  --output "$private_dir/component-prepared.json"
node tools/release/component-publication.mjs qualify \
  --prepared "$private_dir/component-prepared.json" \
  --kubeconfig "$kubeconfig" --context "$context" \
  --approved-plan-sha "$prepared_sha256" \
  --output "$private_dir/component-publication.json"
```

`prepare` не обращается к кластеру. Он сверяет actual build archive context,
receipt bytes, journal hash, base lock bytes и Git binary diff. Production
manifest консервативно содержит все changed files разрешённых корней, включая
тесты; SHA256 связывает полный binary diff, удаления и file modes. Это
provenance, а не автоматическое заключение о совместимости. Raw component-lock
bytes/hash находятся в prepared artifact; node proof там отсутствует.

`qualify` сейчас поддерживает только k3d: проверяет точные cluster/namespace UID,
полный список Ready узлов без DiskPressure и соответствующие Docker container
ID/labels. Он вызывает native CRI pull только для объявленных immutable images
и проверяет `inspecti.status.repoDigests` на каждом узле. Credentials остаются
в существующем node registry transport. Конфигурацию containerd, кластер и
workloads этот шаг не изменяет. Совпадение digest в локальном кеше не является
новой проверкой сетевого TLS/CA либо image admission.

При потерянном ответе нет итогового publication/PASS. Повторный `readback`
использует те же `--prepared`, `--kubeconfig`, `--context` и новый `--output`,
без `--approved-plan-sha`: он только читает CRI и не повторяет pull. Только
после полной проверки source/receipts и всех node digests появляется формат,
принимаемый `control-plane-migration.mjs --component-publication`. Статус
`NODE_QUALIFIED_NOT_DEPLOYED` не означает readiness обновлённой платформы.

Локальная проверка `make test-component-publication` исполняет настоящие CLI и
worker со synthetic Git/Kubernetes/CRI/registry boundaries: разные source и
executor, target drift, неверный digest, потерянный ответ и отсутствие повторного
pull/import. Live node readback выполняется отдельно. Проверены Context7 K3s
private registry/default endpoint fallback и actual CLI `crictl inspecti`;
используется установленный CRI client, а не неподдержанный `k3s crictl` alias.

### Согласованная пара компонентов и forward migration 043

Для `workspace-unbound-stop` существует отдельный opt-in контракт
`kodex.portable-component-migration/v1` в штатном
`tools/release/control-plane-migration.mjs`. Он допускает только CP042→043
(`20261112000100`→`20261113000100`) и пару `control-plane`,
`runtime-controller` из одного exact source SHA. RunnerInput v10 и ABI5
сохраняются. Это адресная квалификация пары; полный базовый release lock и
pins остальных images остаются прежними. Старый stable-publication путь
миграций не меняется. Same-schema component profile выше не разрешает DDL.

Publisher передаёт два настоящих `kodex.component-image/v1` lock: исходные
байты в `componentLockRawBase64` и их SHA256, прежний `expectedImageRef`,
source/base-lock pins, schema heads и явный проверенный cohort
`WORKSPACE_UNBOUND_STOP_UPGRADE`. Новый image вычисляется из явных `registry`,
`repositoryPrefix`, имени компонента и digest в lock. Нельзя подменять image
после проверки либо создавать фиктивную stable-publication историю для другого
publisher. Registry/context в синтетических примерах имеют вид
`registry.example.invalid` и `portable-fixture`; фактические значения оператор
передаёт через приватные inputs.

Каждому lock соответствует `kodex.component-node-proof/v1`: exact source,
lock hash, immutable image, cluster/namespace UID и положительный CRI
`repoDigestVerified` для всех объявленных node UID. Это передаваемый результат
реальной квалификации на узлах, а не команда публикации или автоматическое
доказательство совместимости. CLI повторно проверяет UID, имена и Ready узлов.
Подпись/authority publisher и проверка code compatibility остаются обязанностью
оператора, который создаёт эти приватные inputs.

`--portable-target` использует существующий
`kodex.portable-runner-target/v1`: installation ConfigMap UID/data hash,
cluster/namespace UID и exact UID трёх Deployments: CP, runtime-controller,
control-api-gateway. Сначала нужно прочитать настоящие installation seal и
ownership установленного target; отсутствие seal не разрешает его выдумать.
Gateway уже должен иметь ноль replicas с положительным join всех его Pods.
Оператор заранее закрывает admission/внешних producers, делает backup и
выполняет предусмотренный этим target maintenance lifecycle. CLI сохраняет
stock полный owner drain/precondition и не превращает WAITING_HUMAN в idle.

Inputs/plan/evidence хранятся в отдельном приватном каталоге с mode0700,
файлы — mode0600. Source checkout закреплён и не меняется между фазами.
Сначала только планирование:

```bash
node tools/release/control-plane-migration.mjs plan \
  --profile workspace-unbound-stop --context "$context" \
  --source "$source_checkout" --revision "$source_sha" \
  --base-render "$private_dir/base-render.yaml" \
  --component-publication "$private_dir/component-publication.json" \
  --portable-target "$private_dir/portable-target.json" \
  --plan "$private_dir/migration-plan.json"
```

Для следующих фаз передаются те же `--profile`, `--context`, `--plan` и
`--evidence "$private_dir/migration-evidence.jsonl"`; snapshots publication/target
уже находятся внутри плана. Exact base render мигратора сохраняется неизменным.
Изменяющие фазы требуют штатное явное
`--confirm APPLY-STAGING-CP-MIGRATION`. Последовательность:

| Фаза | Проверяемый переход / результат |
|---|---|
| `check` | Новый immutable CP image выполняет stock connection-check Job; source, target и CRI подтверждаются |
| `stop` | Повторный полный drain; CP→0 и положительный Pod join, затем runtime-controller→0 и положительный join |
| `apply` | Оба writers остановлены; stock pinned precondition, единственный forward-up Job, terminal writer join и полный 043 postcondition |
| `inspect` | Авторитетный readback Job/Pods/schema и точного состояния обоих Deployments, запись наблюдения для UNKNOWN recovery |
| `handoff` | Повторный postcondition/drain; оба templates получают новую пару только при нуле replicas; остальные поля spec сохраняются |
| `resume` | Сначала новый CP, затем новый runtime-controller; прежнее число replicas, actual CRI images и положительная readiness обоих |

Каждый Deployment patch использует UID/resourceVersion/spec CAS, server dry-run
и durable intent до эффекта. После потерянного ответа сначала выполняется
`inspect`; повтор не создаёт второй Job и не переигрывает уже наблюдённый image
patch. Drift закрывает операцию. После успешной 043 старые binaries не
возобновляются: failure остаётся под maintenance для исправления вперёд,
без DB rollback. У этого opt-in пути нет автоматического abort/resume старой
пары и до DDL: отказ check/drain либо неуспешная/неподтверждённая миграция
требуют readback и отдельного операторского решения под maintenance.
Gateway/admission возобновляет внешний maintenance lifecycle
после подтверждения всей пары. `schemaPostcondition: PASS` означает только
миграцию; итог пары подтверждают `pairReady: true`, `cpStage: UPGRADED` и
`forwardOnly: true`. Это не полная продуктовая приёмка релиза.

### GitHub scope: согласованный cohort и forward migration 044

Профили `github-repository-scope` и `github-repository-scope-backup` требуют
`kodex.portable-component-migration/v2`: пять компонентов одного source SHA в
точном порядке `control-plane`, `runtime-controller`, `integration-gateway`,
`control-api-gateway`, `staff-control-center`. Прежний normal/backup путь без
component publication закрыто отклоняется: terminal migration Job не разрешает
вернуть CP043 поверх схемы044. Для каждого компонента нужны настоящий immutable
lock, node pull proof и проверенный cohort `GITHUB_REPOSITORY_SCOPE_UPGRADE`.
RunnerInput v10 и ABI5 не меняются. Secret broker и runner этим профилем не
пересобираются и не заменяются.

`baseSchemaHeads` точно совпадает с неизменяемым seal исходной полной установки
042 либо043. `schemaHeadsBefore` отдельно требует043, `schemaHeadsAfter` —044;
их контрольные версии CP равны `20261113000100` и `20261114120000`.
Полный фактический predecessor catalog PostgreSQL проверяется до DDL;
исторический seal042 не доказывает прохождение043. Schema heads других сервисов
сохраняются. Профиль backup имеет отдельные ACL/catalog proofs и не получает
fallback к normal. Исходные source/lock/installation pins не переписываются под
видом нового полного релиза: новый cohort хранится в publication и journal.

Target закрепляет UID всех пяти Deployment. До планирования API уже должен
быть в maintenance с нулём replicas и положительным join. `stop` сначала
останавливает CP, затем остальные consumers и проверяет Pod descendants
каждого компонента. Перед DDL обязательны join всех writers и новый полный
owner drain. После DDL `handoff` требует terminal writer join, полный
postcondition044 и новый read-only owner drain; predecessor043 SQL повторно
не используется как postcondition. Drain SQL закреплён хешем accepted source и
включает integration effects, credential tests, repository bundles и config work.
При неизвестном исходе patch сначала выполняется `inspect`; UID/resourceVersion/
spec CAS и сохранённый journal предотвращают слепой повтор.

`resume` восстанавливает исходные replicas остальных четырёх компонентов, но
сохраняет API на нуле и выдаёт `gatewayMaintenance: true`. Это не готовность
пользовательского стенда: maintenance снимается отдельным штатным operator
переходом после проверки нового cohort и сохранённого preimage. Число replicas
API не угадывается. Во все фазы CLI можно передать явные `--kubeconfig` и
`--context`; пример выше сохраняет остальные exact inputs.
При повторном `resume` после запуска нового CP его outbox claim sessions
допускаются лишь при сохранённом resume intent, exact новом image и readiness
всех CP Pods. Первый запуск и `handoff` требуют нуля claims; остальные проверки
активной работы не ослабляются. Потерянный ACK запуска CP не вызывает второй patch.

Публичная проверка — `make test-control-plane-migration`: closed pins/cohort,
сохранение maintenance, full postcondition/drain, positive join, потерянный ACK,
подмена UID/source и регрессия043. Локальная оснастка не заменяет установленный
readback настоящих images, schema и пользовательского сценария.
Общий бюджет полного исторического набора CLI ограничен 1500 секундами; прежние
600 секунд исчерпывались до portable-проверок при продолжающемся последовательном
исполнении исторических профилей. Лимиты отдельных CLI-вызовов и production
maintenance не изменяются; таймаут набора по-прежнему означает неуспешную проверку.

### Следующий исправленный component profile

Refs #2025, #1977. Если прежний исправленный image уже установлен, optional
`--previous-component-image-profile previous-components.json` и
`--previous-component-image-profile-sha256 "$previous_component_profile_sha256"`
задают ровно один допустимый predecessor. Оба параметра требуют target component
profile и тот же full base release lock. Старый профиль проходит прежние byte,
immutable image/source, registry и schema checks; его components входят в target
set, а base source/image, schema heads и runtime engine совпадают.

`portable-release.sh`, `deploy-platform.sh` и `authority_registry.py
preflight-workloads` передают одинаковые pinned inputs. Wrapper копирует старый
JSON до первой проверки, повторно сверяет hash и использует snapshot при
последующих preflight/apply; после создания preimage сохраняет target profile,
base lock и predecessor profile рядом с render. Изменение исходного файла
не меняет snapshot. Private backup данных по-прежнему нужен отдельно.

Desired render строится только из target profile. Для live-readback разрешён
ещё exact predecessor, полученный штатным transform того же initial render.
Допустимы baseline/previous/target по целым Deployment при partial rollout;
смесь containers/annotations/env внутри одного Deployment не принимается.
Неизвестный image, sidecar/spec/provenance drift, неверные pins либо неполный
набор flags закрыто останавливают продолжение. Старая authority history и Jobs
не переигрываются, runtime должен оставаться пустым. Без этих опций прежний
контракт сохраняется: иной исправленный image не считается baseline.

Это допуск точного предыдущего состояния для продвижения к новой цели, а не
разрешение downgrade. Rollback всё ещё требует проверки schema/data compatibility
и отдельного pin. Preflight PASS не доказывает serving, warm или real turn.

Не применять к Job с неизвестным внешним эффектом; сначала reconciliation.
Rollback старого image допустим только после отдельной проверки схем/состояния,
не означает разрешения на schema downgrade. В приёмке отдельно записать build,
node pull, rollout/readiness и реальный функциональный прогон.

## Завершённые bootstrap Jobs после TTL

`ttlSecondsAfterFinished` может удалить завершённый Job вместе с Pod. Продолжение
частичного initial bootstrap сохраняет результат завершённых пяти Jobs: нельзя
создавать их заново только потому, что объекты исчезли из API. Точный список —
email migration, authority migration, control-plane migration, PostgreSQL runtime
credentials и broker bootstrap. Materializer и любые другие Jobs проверяются live.

Portable wrapper сохраняет `bootstrap-completion.json` в private preimage после
завершения пяти Jobs, до дальнейших зависимостей. Snapshot выполняет только чтение
Kubernetes и migration heads; receipt содержит cluster/namespace UID, Job UID,
точный fingerprint desired Job, seal observed spec и observed completion time.
Он не содержит Secret values и не выполняет SQL/bootstrap повторно. Без явно
выбранного receipt прежний preflight требует живые завершённые Jobs.

Для continuation/readback передать одинаковые параметры:

```bash
--bootstrap-completion-receipt "$receipt_file" \
--bootstrap-completion-sha256 "$receipt_sha256"
```

Wrapper проверяет private400/600 mode, владельца, единственную hardlink и exact
hash до snapshot. Receipt применяется только к этим пяти Jobs. Живой Job обязан
сохранить UID/spec и успешный terminal state; новый UID, active/Failed, другой
кластер/namespace, изменённый desired spec, неполный список либо неверный hash
закрыто отклоняются. Схемы БД читаются заново; authority history, fences, runtime
isolation и остальные readback/readiness проверки сохраняются.

| Этап | Источник полномочий / проверка | Результат |
|---|---|---|
| Install operator → capture | Kube context, пять live completed Jobs, exact spec и schema heads | Новый private receipt; повторный output не перезаписывается |
| Operator/CI → continuation | Exact receipt bytes, context/namespace UID, current schemas и live remnants | Только отсутствие уже завершённых пяти Jobs допускается без replay |
| Continuation → readback | Те же pins, обязательные остальные объекты/Jobs | Реальная готовность компонентов проверяется отдельно |

### Историческая сверка, если TTL уже сработал

Если live snapshot не был сохранён, нужен аудируемый импорт **доверенным install
operator**. Он отвечает за происхождение сохранённого успешного preflight report;
обычные пользователи платформы не управляют install state. SHA256 защищает
выбранные bytes от подмены, а не заменяет доверие к исполнителю исторической
проверки. При отсутствии доказанного прежнего completion оставить continuation
закрытым. Не генерировать success status и не восстанавливать Kubernetes Job.

`bootstrap_completion.py reconcile` требует exact private inputs: прежний
`resource-inventory.json` с пятью UID/seals, соответствующий `--inventory-render`,
`namespaces.json` из того же preimage и envelope сохранённого успешного preflight.
Для каждого JSON требуется отдельный SHA256. У envelope проверяются
`preflight_exit: 0`, `candidate_render_sha256` текущего render, закреплённые
`source_sha`/`source_archive_sha256` и `readback` со штатным
`WORKLOAD_BOOTSTRAP_RESUME_PREFLIGHT_PASSED`, `VERIFIED_NOT_REPEATED`,
`PRESERVED_NOT_REPLAYED`. Operator задаёт source/archive pins проверенного verifier.
При несовпадении source, report, context, namespace UID или Job spec импорт закрыт.
Текущие migration heads и сохранившиеся live Job UID/spec/status проверяются заново.

Команда имеет вид (все файлы private; SHA — точные 64-hex, source — 40-hex):

```bash
python3 tools/install/bootstrap_completion.py reconcile \
  --context "$context" --render candidate.yaml --output reconciled-completion.json \
  --inventory resource-inventory.json --inventory-sha256 "$inventory_sha256" \
  --inventory-render previous.yaml \
  --namespace-inventory namespaces.json --namespace-inventory-sha256 "$namespaces_sha256" \
  --verification verified-preflight.json --verification-sha256 "$verification_sha256" \
  --verification-source-sha "$verifier_source_sha" \
  --verification-archive-sha256 "$verifier_archive_sha256"
```

Результат помечен `RECONCILED_VERIFIED_PREFLIGHT`; прежнее completion time остаётся
`null`, поскольку оно не наблюдалось в этом импорте. Для его использования нужно
дополнительно явно передать `--allow-reconciled-bootstrap-completion` во все
continuation/readback callers. Полный набор исходных evidence хранится приватно;
receipt фиксирует их hashes. Это operator reconciliation прежней проверки и не
доказательство нового запуска Jobs или текущей готовности приложения. Capture
такого receipt требует privileged install workflow; доступ агента к project tools
не предоставляет доступ к install state.

Официальное поведение TTL: [Kubernetes Job TTL controller](https://kubernetes.io/docs/concepts/workloads/controllers/ttlafterfinished/),
[API v1.35 JobSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.35/).

## Registry authority при первой установке и после частичного bootstrap

Историческая ревизия каталога в исходниках не является ревизией конкретной
установки. Для новой БД её первая публикация должна иметь revision1.
Опциональный `--authority-registry-configuration <json>` у renderer принимает
`{"schema":"kodex.authority-registry/v1","source_revision":1}`. Профиль меняет
только эту ревизию и provenance annotation, сохраняет targets/roles/generations.
Без опции прежняя ревизия каталога сохраняется. Deployment inputs владеют
ревизией: повтор сохраняет значение, последующие изменения каталога должны
проходить штатную монотонную rotation. Нельзя сбрасывать revision активной БД.
Для frozen application render доступен тот же pinned postprocessor:
`authority_registry.py render --render <yaml> --configuration <json>`.

Если миграции уже завершены, а первый publisher остановился до любого durable
authority effect, допускается отдельный `portable-release.sh --mode
resume-authority-bootstrap` с новым render SHA/context/private preimage.
До записи проверяются точные schema heads и completed job executable specs,
единственный owned publisher с replicas0 и завершёнными Pod, пустой runtime,
неизменные targets и0snapshot/0rotation при OPEN fence. Любое частичное durable
состояние требует отдельного существующего recovery protocol; этот режим его
не удаляет и не обходит. Publisher предварительно quiesce адресным решением
оператора с UID/RV/preimage, без удаления БД/PVC/keys.

Resume применяет только ConfigMap registry и publisher, ждёт bootstrap
projections и продолжает обычную цепь workloads/provider/materializer/RoleImage.
Миграции, database credentials и broker bootstrap повторно не запускаются.
Сохраняются UID/seal fencing, immutable images, подписи/mTLS и полный конечный
readback. После изменения schemas старый image не считается rollback.

Изменение уже owned ConfigMap/Deployment выполняется JSONPatch с обязательными
UID/resourceVersion tests после preimage seal check. Это позволяет продолжить
первый install после create/quiesce без принудительного захвата SSA fields.
Patch передаётся только через stdin; Secret этим путём не обновляется. Поля
data/spec заменяются точно по manifest, controller revision annotation
сохраняется; image/security/readback инварианты остаются прежними.
Downward API `fieldRef.apiVersion=v1` считается только закрытым API default,
изменённый fieldPath/API version не принимаются.

Server-side dry-run использует тот же CAS patch для существующих owned
ConfigMap/Deployment, а не несовместимый SSA preview. API defaults
`serviceAccount` принимаются только при точном совпадении serviceAccountName.
Неизвестные identities/поля или другой executable spec не становятся defaults.

Прежний `--disable-observability` сохраняет контракт exporters-only: он
выставляет OTEL_SDK_DISABLED, сохраняя monitors/alerts. Это также сохраняет
семантику bundled KODEX_DISABLE_OBSERVABILITY=true. Новый отдельный opt-in
`--disable-monitoring-resources` выбирается только установкой без Prometheus
Operator; установленные operator/CRD он не удаляет. Default профиль не меняется.


StatefulSet PVC templates при повторном apply сохраняются в точном API
представлении только после закрытой проверки всех исходных полей. API defaults
`apiVersion: v1`, `kind: PersistentVolumeClaim`, `volumeMode: Filesystem` и
`status.phase: Pending` учитываются при readback; другие значения не маскируются.
Это предотвращает SSA конфликт immutable/atomic volumeClaimTemplates после create.
Размер, storageClass, dataSource, accessModes, labels и неизвестные поля менять
таким продолжением нельзя. Dry-run и apply используют одну проверку, write сохраняет
UID/seal inventory fencing и resourceVersion. PVC/StatefulSet не пересоздаются,
force-conflicts не применяется. Изменения данных/хранилища требуют отдельной миграции.


Для admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy/Binding write и
readback явно учитывают только defaults matchPolicy=Equivalent, пустые
namespaceSelector/objectSelector и resourceRules/excludeResourceRules.scope=*.
Это сохраняет atomic matchConstraints/matchResources при create→SSA без
force-conflicts. Полный spec и security annotations сравниваются закрыто:
другие selectors/scope, CEL, failurePolicy, validationActions и неизвестные поля
не считаются defaults. UID/seal/resourceVersion guards остаются обязательными.
CLI при отказе выводит только закрытый reason code; произвольный текст исключения,
payload и credentials не выводятся. Перед повтором apply сначала проверить весь
набор server-side dry-run, а не только первый ранее сломанный ресурс.


Authority projections ожидаются только для Secret references настоящих Pod specs
выбранного render: volumes/projected sources, env/envFrom, init containers и
imagePullSecrets. Глобальный catalog включает optional services; существование
seed Secret не означает наличие consumer. Неиспользуемый ключ optional component
не блокирует web-only. Для выбранных projections сохраняются все owner/generation/
required-key/allowed-key проверки; bootstrap и event-scoped фазы не объединяются.

Если initial authority snapshot уже опубликован, после остановленного failed apply
использовать отдельный `portable-release.sh --mode resume-authority-published` с
новым preimage. Readonly preflight требует точный registry revision1, тот же publisher
executable, пять завершённых bootstrap Jobs и schema heads, единственный publisher
Deployment и пустой runtime. В БД должны быть ровно одна rotation и один подписанный
снимок, связанные intent/source/predecessor и SHA registry, restore fence OPEN.
Publisher Ready ещё может ждать consumer readbacks. Resume не переписывает registry,
не перезапускает publisher, migrations или broker/credentials bootstrap; он проверяет
выбранные projections и продолжает workloads/materializer. После частичного старта
других Deployments этот initial resume закрыт: требуется отдельная reconciliation,
а не обнуление history или слепой повтор. Резервная копия данных обязательна отдельно;
metadata preimage не является rollback БД. Default protected профиль сохраняется.

## Продолжение частично запущенного initial graph

Все startup consumers, включая `role-image-builder`, создаются до ожидания
готовности control-plane. Их issuer/readback не зависит от готовности main
контейнера. Готовность builder по-прежнему требует его настоящие зависимости;
успешный readback не означает готовность сборки. До запуска email-bridge
установщик создаёт каноническую пустую `email-bridge-mailbox-projection`, только
если её ещё нет. Существующая mutable проекция принадлежит control-plane и
никогда не заменяется пустым bootstrap. Пустой список не означает настройку почты.

Опциональный `observability_profile.py --disabled` устанавливает
`OTEL_SDK_DISABLED=true` также для native sidecars (`initContainers` с
`restartPolicy: Always`); одноразовые init templates сохраняются. Для будущих
managed image-admission Jobs профиль монтирует content-addressed immutable
ConfigMap с производным canonical renderer через поддержанный
`IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH`. Генератор меняет ровно SDK block
native authority issuer; остальные Job/PVC, identities, grants и security
context сохраняются. Перед выдачей YAML script проверяет SHA256 настоящего
штатного renderer в immutable image. Неизвестная версия закрыто отклоняется,
скрытого выполнения другой ревизии нет. Используются имеющиеся Bash/jq/awk/
sha256sum/cat; дополнительные parser/runtime пакеты в controller не требуются.
Parameter-owned label `kodex.dev/observability-profile: disabled` в
`ImageAdmissionPolicyParameters` разрешает ровно этот SDK env в CEL; label
самого Job не даёт такого разрешения. Подписи, mTLS, admission, probes и остальные
проверки остаются обязательными. Custom renderer нельзя молча заменить.
Без явного отключения observability исходный render полностью сохраняется.

Если initial graph уже частично запущен, `resume-authority-published` закрыт.
Узкий `portable-release.sh --mode resume-workloads` требует `--previous-render`
и `--previous-render-sha256`, exact desired render SHA/context и новый private
preimage. Разрешённое отличие candidate — результат публичного
`observability_profile.transform(previous, disabled=True)` и, только при явном
`--buildkit-runtime-profile nested-userns`, соответствующий BuildKit transform: прежние images,
schema, registry и прочие настройки неизменны. Readonly preflight проверяет
начальную signed publication, registry revision1/digest, OPEN fence, все пять
завершённых bootstrap Jobs/schema heads и начальную границу runtime. Допускается
пустой namespace либо единственный `system-assistant-warm`, который ещё не
исполнил ни одного контейнера: Pending, exact runner image из immutable policy,
штатные три контейнера и два initializer, waiting statuses без imageID, restart,
lastState и ephemeral containers, правильные managed/warm labels и ServiceAccount.
Дополнительно authoritative PostgreSQL read требует нулевые `runs` и
`session_turns`. Это состояние возникает до публикации первого runner в pull
registry; guard не удаляет Pod и не объявляет его готовым. Каждый существующий
Deployment обязан точно соответствовать predecessor либо successor; неизвестный
workload или executable drift закрывает продолжение. Для policy update применяются
UID/resourceVersion CAS и полный readback, без force или ослабления CEL.

Resume не повторяет migrations, bootstrap credentials, broker setup либо authority
publication. Если materializer Job уже существует, проверяются его exact spec и
результат; active Job дожидаются, completed используют, failed требует отдельной
reconciliation. Неизвестный внешний эффект не разрешает delete/recreate/retry.
Перед продолжением обязателен отдельный актуальный backup данных. Старый image
и metadata preimage не гарантируют безопасного отката схемы. После restart снова
проверить API/login и настоящий runtime turn/tool/resume; Ready Pod этого не доказывает.

Для уже применённого publisher image plan новый чистый descendant executor может
выполнить только `authority-publisher-image.mjs status --predecessor-executor-sha
<exact-old-sha>` с прежними `--plan`, `--evidence`, `--kubeconfig` и `--context`.
Прежний checkout должен оставаться чистым на закреплённом SHA; Git ancestry
проверяется до readback. Journal сохраняет исходный header и отдельную запись
`READBACK_EXECUTOR`; plan, image, UID, node proof и исходный mutation authority
не переписываются. Флаг запрещён для `nodes`, `plan` и `apply`. Этот readback
не повторяет patch и не заменяет проверку нового deployment/native runtime.

Закрытая нормализация API defaults допускает только пустые `command/args`,
пустой `emptyDir.medium` и `fieldRef.apiVersion=v1` в обычном и projected downward
API. Непустой аргумент, другой medium/fieldPath/version, дополнительный source
или неизвестное поле не являются defaults и закрывают exact readback.

| Этап / actor | Авторитетное состояние и допуск | Эффект и потерянный ответ |
|---|---|---|
| Operator: plan/preflight | Exact render SHA/context, initial signed history, schema/jobs, before/after executable | Только SELECT/get/server dry-run; отказ не разрешает apply |
| Runtime пуст | Начальная signed history и остальные preconditions | Продолжение без создания бизнес-хода |
| Initial warm Pending | Exact image/spec identity, все контейнеры никогда не стартовали, runs/turns пусты | Pod сохранён; первый pull/start выполняет штатный controller после публикации |
| Warm работал, есть бизнес-история или неизвестный Pod | Initial-resume недопустим; неполный status также закрыт | Нет удаления, cancel, retry или нового grant; требуется отдельный lifecycle действующего исполнения |
| Проверка недоступна либо потерян ответ apply | Отсутствие доказательства не является пустым runtime | Остановка и readback; эффекты не повторяются вслепую |
| Installer: bootstrap seed | Named CP-owned mutable Secret, канонический пустой payload только при отсутствии | Create-only; после ошибки readback; существующее поколение не обновляется |
| Installer: profile assets/workloads | Git-owned exact render, fenced inventory UID/seal/RV, parameter-owned opt-in | Exact CAS/create, readback; неизвестный исход требует reconciliation |
| Issuer/publisher: activation | Настоящие consumer attestations, signed snapshot и OPEN fence | Штатный durable authority protocol; установщик не подделывает readback/activation |
| Installer: materializer | Exact Job spec и его Kubernetes status | Existing active/completed не пересоздаётся; Failed/UNKNOWN требует разбора receipt |
| Operator: restart/приёмка | Images/schema/config pins, role/API/runtime receipts и данные | Адресный restart и реальная проверка; старый image не откатывает migration |

Installer repair не создаёт бизнес-событий от имени приложения: его журнал —
private preimage/inventory и readback. Прикладные consumers сохраняют собственные
штатные events, grants и lifecycle receipts; repair не переигрывает их операции.

## Опциональный BuildKit в nested user namespace

По умолчанию `buildkit_runtime_profile.py --profile standard` ничего не меняет.
В некоторых nested Kubernetes/runc средах `privileged: true` заставляет sandbox
создать sysfs в network namespace, которым его user namespace не владеет.
Ошибка возникает до запуска прикладного контейнера; bind `/sys` в нём не исправляет
создание sandbox. Обсуждение runtime:
[runc #5309](https://github.com/opencontainers/runc/issues/5309).

Для такого контура отдельно материализовать
`python3 tools/install/buildkit_runtime_profile.py --render <yaml> --profile nested-userns`
и закрепить новый render SHA. Изменяется только точный `kodex-buildkit/buildkitd`
с прежним pinned image и `hostUsers: false`: вместо privileged назначаются
namespace capabilities ALL, `procMount: Unmasked`, Unconfined seccomp/AppArmor и
разрешённый privilege escalation внутри mapped namespace. Это доверенный build
daemon, чей стандартный privileged режим уже снимает эти ограничения; профиль
не предназначен для сотрудников, agent runtime или произвольных workload.
Неподдержанная custom security context закрыто отклоняется. Host namespaces,
Kubernetes token, доступ к чужим credentials, сетевые политики, mTLS и image
admission не расширяются. Дополнительных hostPath/volumes профиль не создаёт.

До применения проверить bounded synthetic sandbox на целевых nodes: mapped UID,
readonly sysfs, Pod cgroup limits, отсутствие credentials/сети. Это не заменяет
проверку настоящего buildkitd, Dockerfile RUN, push, materialization/admission,
RoleImage и restart. Не использовать `hostUsers: true` или `noProcessSandbox`
как запасной путь. Для partial bootstrap дополнительно передать явный профиль
в resume-workloads; одного label в candidate недостаточно для допуска delta.

Существующий `batch/v1 CronJob` обновляется через тот же закрытый реестр typed
UID/resourceVersion CAS, что и прочие поддержанные mutable resources. Это сохраняет
exact template при atomic fieldRef defaults и прежнем Update field manager,
не требуя force-conflicts. До dry-run и apply проверяются owner и inventory seal;
после записи — полный CronJob spec с закрытой нормализацией defaults. Schedule,
suspend, concurrency/history policy и service identity входят в проверку.
CronJob не удаляется/не пересоздаётся; существующие Jobs и их effects не меняются.

### Kubernetes API до и после DNAT

Переносимый render сохраняет точный Service host CIDR/TCP443 и endpoint host CIDRs/
ports для девяти зарегистрированных API clients. Control plane и secret broker
тоже читают/публикуют Kubernetes resources. Default Service route publisher
заменяется согласованным Service route из остальных clients. Выход на весь CIDR
или произвольный порт не используется; CA/hostname, RBAC и service identity прежние.
Семь endpoint anchors должны совпадать, шесть Service anchors — также; неизвестный
selector/policy, неоднозначность, subnet/except/UDP/дубликаты закрывают repair.

Для точного partial initial bootstrap опциональный флаг
`--repair-kubernetes-api-endpoints` добавляется только к `resume-workloads`.
`kubernetes_api_profile.py --render <protected-file>` воспроизводит тот же delta:
endpoint rule двух clients и Service rule publisher; остальные ресурсы неизменны.
Guard сравнивает весь candidate с предыдущим render, штатными optional SDK/BuildKit
transformations и этим явным repair. History/schema/completed Jobs, UID/inventory
fencing, preimage и empty runtime по-прежнему обязательны. Три NetworkPolicy
применяются до ожидания control plane; typed CAS сохраняет UID/resourceVersion и
проверяет весь policy/annotations после записи, без force-conflicts/пересоздания.
Это не общий произвольный network patch или разрешение повтора unknown effects.

При прежнем ProgressDeadlineExceeded сначала подтвердить причину и фактическое
состояние всех consumers. Новый guarded resume требует отдельной сверки частичной
попытки и свежего backup/preimage. HTTP401 unauthenticated probe подтверждает лишь
TLS/маршрут; это не проверка application permissions или успешный вход.

### ClamAV на локальном RWO storage

Явный `--clamav-storage-profile node-local-rwo` у renderer, portable delivery и
`deploy-platform.sh` создаёт отдельный `clamav-database-local` с ReadWriteOnce.
Scanner и единственный updater используют этот claim; антивирусный verdict,
обязательная проверка артефактов, TLS/egress и расписание updater сохраняются.
Все потребители этого тома размещаются на узле, выбранном storage provisioner:
это локальный тестовый профиль без обещания доступности при потере узла.
По умолчанию сохраняется `shared-rwx`; смена профиля требует явного параметра.

Профиль не изменяет accessModes существующего PVC и не переносит его данные.
`resume-workloads` допускает только точное преобразование закреплённого render,
проверяет опубликованную authority revision, receipt миграций и пустой runtime,
создаёт новый claim до workloads. Старый pending claim можно удалить отдельно
только после проверки ownership, отсутствия volumeName/Bound и потребителей;
для занятого тома требуется отдельное решение о данных.
`--preserve-observability-profile` у `resume-workloads` сохраняет уже
закреплённый профиль наблюдаемости побайтно на уровне структуры render вместо
повторного применения disabled overlay; любые дополнительные изменения этой
области по-прежнему отклоняются сравнением полного ожидаемого render.
Семантика проверена через Context7 по документации
[Kubernetes PersistentVolume access modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes)
и [WaitForFirstConsumer](https://kubernetes.io/docs/concepts/storage/storage-classes/#volume-binding-mode).
Для первой установки профиль также материализует отдельный Job из точного
CronJob template с именем по digest. Delivery проверяет готовность egress gateway
и завершение этого Job до ожидания control-plane: пустой новый том получает
подписи без ожидания очередного шестичасового расписания. Повтор проверяет
существующий Job и его результат, не подменяет образ или verdict. Факт Ready
PVC не доказывает успешное сканирование.

### Опциональный ClamAV

`render-web-only.sh --clamav-mode enabled|disabled` сохраняет enabled по умолчанию.
При enabled прежние scanner/updater, freshness check и поведение scan-required
операций не меняются. Значение явно передаётся также в portable-release/deploy-platform
и resume-workloads; несогласованный flag/render закрыто отклоняется.

Disabled снимает только CP sidecar skill-scanner и его четыре volume dependencies,
приостанавливает clamav-db-updater и устанавливает replicas0 у clamav-egress-gateway.
ConfigMap/ServiceAccount/network configuration остаются неактивной конфигурацией для
повторного включения. Новый disabled install не создаёт clamav-database PVC. Уже
существующие PVC/PV и файлы не удаляются: installer не выполняет prune этих данных.
Текущая короткая updater Job не пересоздаётся/не повторяется; suspend не обещает
отмену уже начатого эффекта. Режим виден в annotation kodex.dev/clamav-mode.

Это отключение scanner dependency, а не разрешающий scanner stub. По умолчанию
artifact upload/skill validation paths при недоступном socket возвращают
Unavailable/ERROR. CLEAN и malware verdict не создаются без проверки. Без отдельного
явного режима обычных файлов ниже их приёмка остаётся BLOCKED. SBOM, image vulnerability admission, подписи, mTLS/JWS и остальные gates
не отключаются этим профилем. Не называть disabled успешным antivirus smoke.

Для включения заново отрендерить canonical source с enabled, обеспечить актуальные
подписанные базы и пройти штатную проверку scanner freshness/clean/infected. Попытка
включить уже обрезанный disabled render без canonical source отклоняется. При
partial initial bootstrap explicit disabled mode допускает только этот typed delta
поверх прежних SDK/BuildKit/API profiles; images/schema/history/Job guards прежние.

### Опциональный AppArmor-профиль provider runtime (#2027)

На узлах, где unprivileged user namespace требует именованного AppArmor-профиля,
renderer принимает `--provider-apparmor-profile kodex-provider-runtime`. Профиль
`infra/apparmor/kodex-provider-runtime` должен быть установлен штатным способом
на каждом допустимом runtime-узле; container runtime и kubelet должны подтвердить
поддержку AppArmor. Предварительно проверить именно UID/provider image и user
namespace выбранного выпуска, без credentials и внешней сети. Этот флаг не
устанавливает профиль на хост и не исправляет node prerequisites.

Опция независима от Codex/OpenCode. Без флага render сохраняет прежнее поведение.
Явный пустой аргумент `--provider-apparmor-profile ''` выбирает прежнее отсутствие
именованного профиля через новую runtime-проекцию; это не отключение bubblewrap,
user namespace, image admission, mTLS/JWS или остальных security boundaries.
Произвольные имена профилей, `Unconfined` и `RuntimeDefault` здесь не принимаются.

Источник image admission остаётся неизменным: immutable ConfigMap
`kodex-image-admission-policy`, его hash, typed parameters, image Jobs и authority
history не переписываются. Renderer создаёт отдельный immutable ConfigMap с тремя
полями `nodeReadbackImage`, `roleRuntimeContractRevision`, `providerAppArmorProfile`.
Имя/hash связывают эти данные с исходным image-policy SHA. Только runtime admission
binding и существующий env runtime-controller ссылаются на новую проекцию.
Registry overlay пересчитывает её anchors после изменения source registry.
Общие image/tag, replicas, sidecars и admission expressions этим флагом не меняются.

Для fresh install передать опцию renderer и тот же явный аргумент установщику.
Для initial `resume-workloads` она также входит в exact predecessor comparison.
После опубликованного bootstrap не запускать заново прежнюю failed install:
адресный maintenance-переход выполняется отдельной точкой входа:

```bash
python3 tools/install/provider_sandbox_profile.py \
  --render /protected/candidate.yaml --profile kodex-provider-runtime
python3 tools/install/provider_sandbox_transition.py plan \
  --context example-context \
  --previous-render /protected/previous.yaml --previous-render-sha256 <sha256> \
  --render /protected/candidate.yaml --render-sha256 <sha256> \
  --state-directory /protected/profile-operation
```

Сначала скопировать предыдущий pinned render в candidate; преобразователь меняет
только candidate. Закрепить оба SHA после преобразования. Для завершённых bootstrap
Jobs, удалённых TTL, передать предусмотренный completion receipt с SHA; исторический
reconciled receipt требует отдельного существующего opt-in. У оператора должны быть
штатные Kubernetes/DB полномочия и защищённый каталог0700; receipts/preimage0600.
Не передавать пароль PostgreSQL или provider token в командной строке.

После backup и проверки плана назначить короткое окно без запусков, расписаний и
webhooks. Смена Pod UID runtime-controller может пересоздать installation warm Pod;
проверять отсутствие активных turns/leases, а не только Ready Deployments.
`apply` с теми же аргументами и `--maintenance-confirmed` допускает только три
объекта, сохраняет preimage и сверяет UID/resourceVersion. Обычные runtime Pods,
active/unknown leases, queued/running/cancelling runs и running nodes закрывают
переход. Никакие пользовательские runs или credentials оператор не создаёт.

SQL maintenance transaction берёт фиксированные SHARE locks на runs/run_nodes/
runtime_leases и transaction advisory lock. Новые claims/записи ждут окончания
окна либо получают свой штатный timeout. Обновление блокировки идёт каждые2s,
SQL statement ограничен5s, ожидание idle-сессии30s, продление — максимум240s.
Перед каждой Kubernetes mutation проверяется живая сессия и запас25s для HTTP CAS
с пределом20s. Controller rollout ожидается максимум120s. Нельзя бесконечно
продлевать maintenance или повышать бюджеты для обхода неизвестной причины.

PostgreSQL и Kubernetes не образуют атомарную транзакцию. Уже отправленный API
write может завершиться после потери SQL-соединения; обещания отмены такого эффекта
нет. Любая ошибка оставляет `RECONCILIATION_REQUIRED`, не запускает rollback и не
повторяет create. Сначала вызвать `readback` с прежними pins/state-directory,
проверить UID/spec и состояние controller, затем при необходимости отдельно
`apply --resume --maintenance-confirmed`. Readback desired resources после потери
ответа не объявляет непрерывность прежней блокировки доказанной; receipt сохраняет
все attempts. Новый resume заново получает maintenance lock. Успешный повтор apply
не меняет ресурсы и не перезапускает controller.

| Сценарий | Владелец состояния / переход | Ожидаемый результат |
| --- | --- | --- |
| Опция не задана; Codex/OpenCode | Renderer → прежний manifest | Побайтово прежние resource values |
| Явный профиль | Operator → projection → runtime binding/RC → provider Pod | Exact три объекта; image policy/history прежние |
| Активный/чужой workload, drift/другой UID | Live cluster/DB → preflight/fence | Отказ до нового write |
| Потеря fence между чтением и записью | SQL backend → per-write gate | Новая mutation запрещена |
| Потерян ответ create/последнего patch | API effect → private receipt → readback | Unknown outcome; без слепого повтора |
| Resume/deadline/отмена | Operator → fresh fence или закрытый отказ | History сохраняется; timeout не даёт authority |
| Повтор успешного apply | Cluster + завершённый receipt | Без новых эффектов |
| Возврат к пустому профилю | Новый pinned candidate + отдельный maintenance | Forward config change; прежняя projection не удаляется |

Проверки: `make test-web-only-release` включает pure transform и synthetic CAS/
recovery; `make test-provider-sandbox-native` запускает одноразовый pinned
PostgreSQL18 в Docker без сети и проверяет реальные lock/heartbeat/backend-loss.
Эти проверки не доказывают работоспособность AppArmor на целевых узлах или turn
установленного provider. После apply отдельно подтвердить именованный профиль
provider-runtime, прежние security contexts остальных двух контейнеров warm Pod,
readiness, настоящий turn/tool/resume и отрицательный путь.
Откат приложения/схемы не входит в этот профиль; migrations не повторяются.

Для forward upgrade существующая mailbox projection проверяется через
`email_projection.py --existing-only`: seed Secret не нужен, опубликованный
payload не изменяется. Отсутствующая, immutable либо чужая проекция блокирует
upgrade без create/write. Штатная новая установка по-прежнему требует canonical
empty seed и использует create-only bootstrap с readback после потерянного ответа.

ImageAdmissionPolicyParameters имеет immutable spec. Для нового spec portable
upgrade требует остановленного admission controller, terminal managed Jobs и
отсутствия managed workspace PVC. Owned binding сохраняет parameterNotFoundAction
Deny и validationActions Deny на протяжении замены. Прежний объект удаляется
по UID/resourceVersion preconditions, foreground disappearance подтверждается,
затем foundation создаёт ровно один successor с exact spec readback. Потерянный
ответ требует reconciliation, force-conflicts не применяется. Preflight проверяет
эти guards и create --dry-run=server с generateName; он не пытается обновить
immutable spec. При неизменном spec metadata сохраняются через fenced CAS.
Server preflight проверяет custom resources уже served API, исключая только
ещё не установленные API.

Для проверки `/assistant/readyz` текущего runtime-controller leader доступен
явный флаг `--assistant-readback-transport api-proxy|port-forward` у
`portable-release.sh` и `deploy-platform.sh`. По умолчанию используется прежний
Kubernetes API Pod proxy. Если этот маршрут недоступен в выбранном CNI/egress
профиле, `port-forward` проверяет тот же endpoint через кратковременный
операторский канал Kubernetes с bind только `127.0.0.1` и случайным локальным
портом. Нужны штатные права оператора на чтение Pod/Lease и `pods/portforward`;
RBAC, NetworkPolicy и TLS Kubernetes не изменяются. Автоматического fallback нет.
До и после HTTP204 проверяются точный UID готового Pod и текущий holder Lease.
Redirect, чужой leader, неизвестный ответ и отказ канала закрывают проверку;
процесс всегда завершается и join-ится. Готовность не заменяет turn/tool/resume.

Composite bootstrap completion receipt передаётся во все вложенные readback
вызовы. После штатного TTL старые completed bootstrap Jobs могут отсутствовать:
точные исторические эффекты и актуальная schema подтверждаются квитанцией.
Заключительная проверка не должна терять её или повторно создавать bootstrap.


### Переход036→037 и backup reader

Forward update037 (`20261109000100`) использует обычную Goose-миграцию.
Operator сохраняет source/render/schema pins, прежние bootstrap receipts и
подтверждает новый migration Job/Pod. Исторические миграции не переписываются.

Перед обновлением проверяются версия036, owner затронутых таблиц,
доступность proof watermark, отсутствие объектов037 и незавершённой работы.
Количество уже выданных backup SELECT не фиксируется снимком каталога:
безопасный SELECT на подмножестве таблиц не меняет смысл миграции.
WRITE, grant option, посторонний grantor и membership backup reader
отклоняются. Режим `backup` дополнительно требует штатную backup identity.

После миграции readback037 проверяет её колонки/defaults, CHECK, функции,
triggers, owner/ACL и watermark. `backup` дополнительно требует SELECT на
явно перечисленных таблицах. Ответ имеет профиль `INTEGRATION_RESOURCE_POLICIES_V2`
либо `INTEGRATION_RESOURCE_POLICIES_BACKUP_V2`; это проверка условий и результата
текущей миграции, а не аттестация всего исторического каталога PostgreSQL.

`restore-workspace-backup-acl` на037 сохраняет проверку maintenance identity,
idle workspace boundary и известного schema head. В одной транзакции он
проверяет текущие объекты и возвращает только штатный SELECT и default SELECT.
Временного REVOKE для подгонки под schema snapshot больше нет. Ошибка откатывает
всю транзакцию; неизвестный результат сначала выясняется readback.

Квалификация включает disposable upgrade, повторный `up`, свежую установку,
реальные `pg_dump`/`pg_restore` и функциональные owner-тесты персональных прав,
согласований и recovery. Регрессии прежних workflow/SSH исполняются отдельными
тестами и не копируются в каждую новую проверку установщика. Команды:

```bash
bash scripts/tests/control-plane-postgres-test.sh --integration-resource-policies-upgrade
bash scripts/tests/control-plane-postgres-test.sh --integration-resource-policies-fresh-install
bash scripts/tests/control-plane-postgres-test.sh --integration-resource-policies-owner
```

Изменение embedded restore SQL меняет исполняемый control-plane CLI: требуется
новый immutable image. Прежняя сборка не получает исправление от обновления
installer scripts. Результат проверок не означает выполненный rollout либо
безопасность возврата к старому image после forward migration.

### Обычные файлы без антивирусной аттестации

Для явно согласованного профиля без AV передайте `--artifact-antivirus-mode disabled`
в render, portable release и deploy; у workload-resume тот же параметр. Default `required`
сохраняет прежний закрытый отказ. `--clamav-mode disabled` самостоятельно никогда не
разрешает непроверенные файлы. Оба флага независимы: антивирус можно оставить работающим,
но отключить именно его обязательность для обычных файлов только явным выбором.

Параметр материализуется как `CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE=disabled` у CP.
Flags/render mismatch, неизвестное значение и повторный/secret-backed env отклоняются.
Не редактировать работающий Pod вручную. Использовать совместимые pinned CP/API/PWA
после миграции `20261110000100_artifact_optional_antivirus.sql`.

Проверка: загрузить допустимый текст, получить `NOT_SCANNED`, скачать exact revision с
исходным SHA-256; закрытый/активный формат не должен стать доступным. Завершить сотрудника
с обычным result-файлом и передать его следующему execution через finalized AttachmentSet.
Проверить предупреждение в истории/выборе файла и отсутствие повторного артефакта при replay.
Skills, avatars, executable bundles и структурированный workflow evidence требуют реальный AV;
это не полный workflow/PDLC smoke. В required режиме проверить отказ без scanner.

Возврат: canonical render `--artifact-antivirus-mode required` и работающий scanner с
актуальными подписанными базами. Исторические NOT_SCANNED не меняются и не становятся CLEAN.
Не выполнять down и не возвращать старый binary после появления новых receipts: безопасный
вариант — совместимый текущий binary/schema с required policy. Backup/restore определяется
обычной политикой владельца установки; этот профиль не добавляет постоянных dumps.

Не менять artifact admission policy посреди активных execution/prepared intents: закончить либо
штатно остановить их до apply. Повтор незавершённого prepared intent после смены policy может
вернуть conflict, а не переаттестовать или повторно записать объект. Migration profile
`artifact-optional-antivirus` проверяет exact037 и отсутствие активных effects, применяет только
forward038 и читает его postcondition. Специальный backup ACL/profile для этой миграции не нужен.


## Закрытие API при согласованном обновлении компонентов

`tools/release/component-api-maintenance.mjs` выполняет `plan/apply/status`
для exact `control-api-gateway` Deployment; `component-api-traffic.mjs` — для
его Service. Это ограниченный период недоступности API, не zero-downtime rollout.
Оба используют один private `kodex.portable-runner-target/v1` с точными
cluster/namespace/installation UID, хешем installation data и явно перечисленными
Deployment/Service UID. При обновлении authority в том же target перечислены
control-plane, publisher и registry ConfigMap. Права и соседние ресурсы не меняются.

| Переход | Предусловие | Эффект и подтверждение | Ошибка / повтор |
| --- | --- | --- | --- |
| API `close` | Ready Deployment, исходные UID/RV/spec | Только replicas=0 и owned annotation; `CLOSED` после observedGeneration, нулевых counters и положительного join всех API Pod/контейнеров | Оставшиеся writers удерживают `CONVERGING`; lost ACK → `status`, без слепого повторного PATCH |
| Service `isolate` | Private close journal и свежий `CLOSED` | Только дополнительный nonce selector и annotation; `ISOLATED` после отсутствия всех EndpointSlice addresses и matching Pod | Terminating/not-ready адрес тоже блокирует; чужой UID/spec закрыто отклоняется |
| Обновление / API `open` | Обязательный fresh Service isolation proof, новая совместимая cohort проверена | Open-plan закрепляет **текущий** spec; восстанавливаются только сохранённые replicas. `OPEN` требует полного serving inventory, точных template/volumes и ready image digests | Старые image/template не восстанавливаются; пустой inventory при Ready counters не даёт `OPEN` |
| Service `restore` | Ready API, тот же isolation receipt; authoritative policy106 `RETIRED`, все16 readers, fresh owner status не старше30с | Возвращается исходный selector без изменения ports/IP; `RESTORED` — только spec readback | Старый completed Job не заменяет новый DB read. `RESTORED` не означает business E2E; нужен обычный live сценарий |

Публичные команды требуют `--kubeconfig /absolute/path --context <exact-context>`.
`plan --action close|open --target <private-target> --plan <private-plan>`
создаёт план Deployment; для `open` дополнительно нужны `--closed-plan`,
`--closed-evidence`, `--isolation-plan` и `--isolation-evidence` как при plan,
так и apply/status. Traffic-plan использует `--action isolate|restore` и всегда
те же `--closed-plan/--closed-evidence`; `restore` также требует
`--isolation-plan/--isolation-evidence`. Для restore-plan/apply также обязательны
`--authority-plan/--authority-evidence` исходной ротации и
`--authority-status-plan/--authority-status-evidence` нового completed status Job.
Общий `readCatalogAuthorityTerminal` повторно читает exact Job/Pod/logs/registry/
publisher и owner `RETIRED`; helper не создаёт status Job скрыто. Если прошло30с,
выполнить новый штатный status и подготовить новый restore-plan. `apply` принимает `--plan`,
`--evidence` и точный `--approved-plan-sha`; `status` — plan/evidence без approval
и не повторяет изменения. Traffic apply/status сохраняют ссылки на close receipt,
а restore — на isolation receipt. Исходники исполнителя должны быть чистыми
и совпадать с SHA плана. После неопределённого исхода сначала выполняется status
того же плана; смена target/source требует нового адресного плана.

Authority transition вызывает общий `requireClosedAPIAdmission` перед изменением:
либо API действительно остановлен, либо подтверждены прежний positive join,
текущая изоляция Service и пустые endpoints. Исторический close receipt сам
по себе не разрешает следующую операцию. Policy revision и durable source revision
остаются отдельными величинами; нельзя открывать пользовательский Service до
обязательных reader receipts/retirement. Пустой EndpointSlice не завершает старые
TCP sessions, поэтому isolate создаётся только после positive API join.
Протокол не защищает от параллельного привилегированного оператора, сознательно
меняющего те же объекты; такой drift закрывает дальнейшее выполнение.

`make test-component-api-maintenance` проверяет настоящие CLI на synthetic
Kubernetes transport: close/isolate/open, отказ restore без retirement, чужой target,
stale CAS, скрытые writers, потерю ACK и сохранение нового image при открытии.
Положительный restore с настоящим owner status проверяется вместе с authority
delivery unit; отдельный model proof его не заменяет. Это локальная квалификация,
не доказательство установленного rollout. Через Context7 проверены официальные
[Service selectors](https://kubernetes.io/docs/concepts/services-networking/service/)
и [EndpointSlice conditions](https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/):
terminating endpoints могут ещё участвовать в маршрутизации; проверять только
`ready=false` недостаточно.

## Git в образе integration-gateway

Для Alpine3.23 закреплён `git=2.52.1-r0`: прежний2.52.0-r0 больше не
доступен в текущем APKINDEX и останавливает fresh build. Версия проверяется
также runtime-тестом. Digest базового Alpine сохраняется, неподписанные
пакеты, edge repository и плавающий диапазон версий не добавляются. Проверены
через Context7 правила [apk pinning](https://wiki.alpinelinux.org/wiki/Alpine_Package_Keeper)
и официальный APKINDEX веткиv3.23/main/x86_64, source commit пакета
`2f20de3527863b1b84f620fefe687921498e7240`. Фактическая OCI-сборка и runtime
readback остаются отдельными обязательными проверками candidate.
