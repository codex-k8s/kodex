---
id: RUN-DEPLOY-1979
title: Опциональный профиль внешней инфраструктуры
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-11-02
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
