---
id: RUN-DEPLOY-1979
title: Опциональный профиль внешней инфраструктуры
type: runbook
status: approved
owner: sre
version: 1.3.0
updated: 2026-11-03
---

# Опциональный профиль внешней инфраструктуры

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

Поддержка внешнего IdP сохраняет текущий прикладной контракт токенов: RS256,
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

`--role-base-runtime-engine opencode` при full release build выбирает
`agent-runner-opencode` также для documents base. По умолчанию остаётся `codex`;
оба runner образа сохраняются в inventory. Явный OpenCode profile фиксируется в
lock как `role_base_runtime_engine: opencode`. Render с Git-managed OpenCode
отклоняет lock без соответствующего profile до materialization/apply. Один
успешно собранный optional runner не подтверждает совместимость documents base.

`promoted_registry.py` согласует producer и consumers по итоговому lock:
source prefix принадлежит только публикации source artifacts, внутренние
promotion repositories сохраняют канонический `kodex/`. Materializer выбирает
строго `kodex/agent-runner` либо `kodex/agent-runner-opencode`; прочие targets
отклоняются. `promotedPullRepository`, node readback, certificate-guard readback
и `ImageAdmissionPolicyParameters` указывают на действительно материализованные
repositories с exact digest. Policy hash пересчитывается вместе с projection.

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

Пустой Role/ClusterRole (`rules: []`) API может сохранить как `rules: null`.
Preflight и фактическая запись используют одну закрытую нормализацию []→null;
readback принимает только эквивалентные пустые формы (null/absence/[]). Непустые
правила сравниваются полностью, добавление любого разрешения отклоняется.
Нормализация не включает force-conflicts и не изменяет image/schema contract.

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
ConfigMap с wrapper штатного renderer через поддержанный
`IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH`. Wrapper меняет только SDK env
native authority issuer и сохраняет Job/PVC, identities, grants и security
context. Parameter-owned label `kodex.dev/observability-profile: disabled` в
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
завершённых bootstrap Jobs/schema heads и пустой runtime. Каждый существующий
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

Закрытая нормализация API defaults допускает только пустые `command/args`,
пустой `emptyDir.medium` и `fieldRef.apiVersion=v1` в обычном и projected downward
API. Непустой аргумент, другой medium/fieldPath/version, дополнительный source
или неизвестное поле не являются defaults и закрывают exact readback.

| Этап / actor | Авторитетное состояние и допуск | Эффект и потерянный ответ |
|---|---|---|
| Operator: plan/preflight | Exact render SHA/context, initial signed history, schema/jobs, before/after executable | Только SELECT/get/server dry-run; отказ не разрешает apply |
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
