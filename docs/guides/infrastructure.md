---
id: INFRA-DOC-001
title: Infrastructure Guide
type: guide
status: approved
owner: SRE
version: 1.2.2
updated: 2026-08-07
---

# Infrastructure Guide

## Правило code-first

Инфраструктура изменяется только через код, PR, review и повторяемый запуск
того же кода.

Порядок:

1. Read-only preflight.
2. Script/code или workflow в репозитории.
3. PR с ручной проверкой.
4. Review.
5. Запуск того же кода.
6. Отчет в Issue.

## Требования к scripts/code

- Preflight или dry-run mode.
- Безопасный повторный запуск.
- Проверка результата.
- Rollback/repair notes.
- Список env/secret keys без значений.
- Отсутствие production-действий в staging-контексте.

## Размещение Kubernetes-ресурсов

- Ресурс, устанавливаемый один раз на кластер или эксплуатационный контур,
  размещается в `deploy/k8s/base/<capability>`.
- Deployment, Service, ServiceAccount, policy, service-specific
  `ServiceMonitor`, alert rules и бизнесовый dashboard размещаются в
  `deploy/k8s/base/<component>`.
- Data plane и migration Job выносятся в отдельные base-каталоги, если имеют
  независимый lifecycle, права, порядок rollout или rollback.
- Environment overlay или bootstrap собирает capability и component bases, но
  не копирует их YAML и не создает второй источник правды.
- Общие Grafana dashboards находятся в
  `deploy/k8s/base/observability/dashboards`, устанавливаются один раз и
  выбирают сервис через переменную `service`.
- UID dashboard глобально уникален. Дублирование общего dashboard в другом
  namespace запрещено, даже если Kubernetes допускает одинаковые имена
  ConfigMap.

PR с Kubernetes-ресурсами проверяет каждый затронутый base и environment
overlay через `kustomize build`, а для общих ресурсов дополнительно доказывает
отсутствие дублей и способ однократной установки в bootstrap.

## Полный граф зависимостей deployable

Каждый runtime dependency должен иметь реального владельца и code-first путь.
Ссылка из `Deployment` или `VaultStaticSecret` не создает зависимость сама.

Для sidecar, init container, verifier, issuer, publisher, migration job,
database, broker, egress proxy и secret operator фиксируются:

- исходный код или закрепленный поддерживаемый artifact;
- namespace, ServiceAccount и минимальный RBAC;
- Service/endpoint и exact network path;
- volumes, access modes и источник config/trust/secret material;
- startup/readiness/failure policy;
- bounded shutdown и порядок закрытия;
- environment apply order, readback и repair/rollback runbook.

Для Deployment/Job дополнительно явно задаются утверждённые владельцем
`replicas`, `revisionHistoryLimit`, стратегия rollout,
`activeDeadlineSeconds`, retry/backoff, история cleanup, disruption policy и
поведение при отказе readiness. Значения не наследуются из случайного default
и сверяются в итоговом render окружения. Число реплик и глубина истории
относятся к контракту конкретного component, а не являются одной глобальной
константой проекта.

Ручное заполнение `emptyDir`/ConfigMap, selector несуществующего workload,
ссылка на отсутствующий namespace-local auth resource и фиктивно успешный
provider path запрещены. Обязательная зависимость проверяется до открытия
готовности и до запуска workers.

Immutable ConfigMap с постоянным именем нельзя обновлять in-place. Изменяемый
через rollout configuration snapshot получает content-addressed/versioned имя,
а Deployment reference переключается renderer/Kustomize автоматически.
Rollback выбирает ранее review-approved объект и render; delete/recreate либо
runtime mutation не являются штатным путём.

## Итоговый render и NetworkPolicy

Security review проверяет итоговый environment render, а не только base и
patch по отдельности. Kustomize/Helm lists могут заменяться целиком, поэтому
overlay способен незаметно удалить обязательный egress либо вернуть широкий.

- Default deny дополняется exact rules для фактических destinations.
- Правило только с портом, но без `to`, запрещено для PostgreSQL, broker,
  Vault, telemetry и Kubernetes API.
- Selector указывает на реально развертываемый pod за достижимым Service.
- Runtime и migration получают отдельные rules и credentials.
- Для внешнего endpoint применяется exact `ipBlock` только при устойчивом
  адресном контракте либо namespace-local egress gateway.
- SaaS с изменяемыми адресами доступен через allowlisted proxy, а не wildcard
  HTTPS egress.
- Сам allowlisted egress gateway может иметь destination-less `TCP/443` только
  как явно зарегистрированное L3/L4-исключение: его application Pods не имеют
  такого правила, immutable exact-FQDN policy сверяется с CONNECT authority и
  фактическим ClientHello SNI, DNS принадлежит gateway, весь A/AAAA snapshot
  отклоняется при любом special-purpose address, а dial получает только
  повторно проверенный literal IP. Gateway не монтирует application secrets и
  ServiceAccount token.

После render сверяются полный набор разрешенных портов/destinations и
отсутствие destination-less правил вне утверждённого egress gateway exception.
Успешный YAML parse не доказывает семантику сети.

## Каркас нового Go-компонента

Технический каркас создается типизированным генератором:

```bash
go -C tools/go-service-template run . \
  -config /path/to/service-config.json \
  -output /tmp/catalog-runtime
```

JSON-дескриптор содержит wiring, имена Kubernetes/Vault-ресурсов, точные
digest образов, probes, resource policy и явные ingress/egress. Значения
секретов в дескрипторе запрещены.

Генератор:

- отказывается перезаписывать существующий каталог;
- принимает итоговый, builder и runtime image только как точные
  `@sha256:<64 hex>` ссылки;
- формирует pinned multi-stage Dockerfile и distroless runtime;
- закрепляет BuildKit syntax frontend полным digest, не используя mutable tag;
- задает restricted security context, rolling update, probes, HPA и PDB;
- создает deny-by-default NetworkPolicy с явными разрешениями;
- при пустом `grpcCallers` не создает gRPC ingress rule;
- создает VaultStaticSecret только по ссылкам на Vault;
- не выполняет apply и не меняет cluster state.

Результат проходит review, переносится в `services/**` и
`deploy/k8s/base/<component>`, дополняется service-specific конфигурацией.
Сервис с
PostgreSQL/Redis или migration Job дополнительно получает самостоятельные
data/migration bases, если их lifecycle или права различаются.

Multi-stage Dockerfile включает полный local-module closure: manifests всех
локальных replaced Go modules копируются до download, их исходники и
Dockerfile ignore rules — до build. Runtime и migration OCI targets проверяются
как самостоятельные artifacts; локальная host-сборка их не заменяет.

Канонический renderer принимает отдельные неизменяемые дайджесты для builder,
runtime service, migration/job, agent runtime и инструментов допуска. Он также
принимает точные source/commit или дайджест архива исходников, revision policy
и все обязательные входы конфигурации. Один дайджест по умолчанию не
используется для разных artifacts. Нулевой, изменяемый или отсутствующий вход
останавливает render до apply; каждый вход материализуется в типизированных
config, annotation/manifest и readback.

## Поставка и допуск образов

Локальная цепочка образов считается исполнимой только как полный путь
потребителя:

```text
immutable source + Dockerfile/module graph
-> bounded build Job/controller
-> exact mTLS BuildKit identity
-> staging push identity/storage
-> provenance + SBOM + vulnerability verdict + signature
-> server-owned admission receipt
-> isolated promotion identity/storage
-> node-reachable pull endpoint
-> pull/readback exact digest на каждой node boundary
```

Объявленный listener BuildKit без Job/CLI-владельца, secret mounts,
`NetworkPolicy`, failure policy и readback дайджеста не считается путём сборки.
BuildKit TCP требует точный серверный TLS и отдельные клиентские identities для
probe и builder; UDS допустим только тогда, когда шаг сборки физически может к
нему обратиться. Label selector остаётся сетевым слоем и не заменяет mTLS.

Registry pull, staging push, admin/retention и promotion разделяются физически:

- разные Pods/Deployments, ServiceAccounts, Services, Vault roles/SPC,
  application credentials и `NetworkPolicy`;
- pull видит только собственные TLS/auth materials и доступное только для
  чтения promoted storage, не имеет localhost/admin path и writable volume;
- push пишет только staging и не имеет delete/promotion identity;
- admin/retention ограничен staging cleanup policy;
- promotion является единственной стороной записи в promoted storage и не
  совмещается с builder либо публичным pull.

Kubelet/container runtime должен достичь endpoint pull до запуска Pod.
ClusterIP/pod DNS и том CA будущего Pod этого не доказывают. Окружение
материализует доступные с узла точные FQDN/route, DNS и доверие CA/runtime,
доступный только для чтения credential pull и code-first readback точного
дайджеста с границы каждого узла. Небезопасный registry, незашифрованный
запасной путь и скрытая ручная настройка runtime узла запрещены. Rollback
выбирает ранее допущенный дайджест тем же путём.

Сборка публикует только staging digest и неизменяемые metadata происхождения.
Отдельный владелец допуска связывает точные дайджесты source/build/image/tools,
identity/digest SPDX SBOM, revision/verdict policy уязвимостей и проверенную
identity подписи. Ограниченный подписанный claim/receipt содержит expiry,
idempotency и аудит; promotion сверяет его до copy и затем читает обратно
точные дайджест образа и OCI receipt допуска. Отсутствующее, устаревшее,
отклонённое или несовпадающее evidence закрыто блокирует promotion.

Vault CSI PKI выдаёт identities BuildKit/registry операцией записи с точными
`common_name`, SAN, TTL и server/client EKU по `GUIDE-DOC-003`. CA имеет
отдельный путь чтения; render/startup/readiness проверяют фактический mTLS, а не
только наличие `SecretProviderClass`.

Сам Vault CSI provider использует единый provider-level transport contract:
точный HTTPS address, read-only installation CA и exact TLS server name из
pinned Helm values. Workload-specific `SecretProviderClass` задаёт только auth
role и объекты Vault и не вправе переопределять address, CA, SNI или TLS verify.
Неиспользуемый Vault Agent cache sidecar в этом профиле отключён, чтобы не было
двух альтернативных путей доверия к Vault.

Secrets Store CSI Provider создаёт credential files с exact mode `0444`, потому
что текущий driver/provider не назначает UID/GID non-root workload, а изменение
`CSIDriver.fsGroupPolicy` не является доказательством фактического ownership.
CSI volume и соответствующий container mount всегда `readOnly: true` и
подключаются только к целевому контейнеру. Приложения читают такие файлы через
`libs/go/securefile`, принимают только `0400`, `0440` или `0444` и закрыто
отклоняют write/execute permissions и выход symlink за mount boundary.

Root init container, копия credential в writable volume, supplemental group `0`
и ручной `chown` не являются поддерживаемым путём доставки secret.

## PostgreSQL в Kubernetes и за его пределами

Environment выбирает один явный data path:

- принадлежащий проекту staging PostgreSQL имеет Service, workload,
  persistent storage, bootstrap/reconciler ролей, backup и readiness;
- внешний HA PostgreSQL доступен через exact egress gateway или другой
  утвержденный маршрут с сохранением end-to-end TLS.

Runtime, migration Job, DSN ownership, `NetworkPolicy` и runbook указывают на
один достижимый Service. Предполагаемый selector без data deployable и
destination-less TCP/5432 запрещены. Runtime, migration и bootstrap roles
разделяются по permissions; schema credential не монтируется в application
container.

Service DNS alias разделяет базы и TLS identities логически, но не меняет labels
одного PostgreSQL workload. Поэтому egress и ingress `NetworkPolicy` выбирают
канонический label фактического StatefulSet, а не имя `*-postgresql-rw` Service.
Итоговый render проверяет обе стороны пути для каждого migration/runtime client.

## Vault, TLS и namespace-local secret graph

До применения `VaultStaticSecret` environment materializes Namespace,
ServiceAccount, `VaultConnection`, `VaultAuth`, namespace-bound Vault role,
доверенную CA и exact egress. Порядок закрыто проверяет:

```text
Namespace -> CA/egress -> VaultConnection -> VaultAuth
-> VaultStaticSecret -> generated Secret -> workload
```

Vault API использует HTTPS с exact SNI/hostname, `skipTLSVerify: false` и
публичной CA. Server-side TLS включается до перевода clients. Переключение
listener учитывает все существующие `VaultConnection`, а не только новый
component.

Обновление certificate Secret требует runtime reload/rollout и exact readback
фактически обслуживаемого leaf. CA меняется через заранее доставленный overlap
bundle. Полный protocol задан `GUIDE-DOC-003`.

Нормативный общий capability namespace-local Vault CA delivery находится в
`deploy/k8s/base/vault-ca-delivery`. Он не хранит CA value: trust-manager
`Bundle` читает только именованный Secret в настроенном trust namespace и
доставляет target Secret/ConfigMap в точный namespace selector. Источник CA и
overlap принадлежат PKI/Vault owner, component base владеет только
`VaultConnection`, `VaultAuth`, secret CR и workload ordering. Повторять Bundle
или копировать CA в base компонента запрещено; новый unit переиспользует общий
capability отдельным target manifest и доказывает status/digest readback.
`Bundle` остаётся cluster-scoped в итоговом render: namespace назначается
namespaced ресурсам во внутреннем base, после чего штатный `PatchTransformer`
удаляет `metadata.namespace` только у CRD `Bundle`. Overlay не вводит второй
namespace transformer поверх уже собранного base.
Сам Namespace `mattercodex-system` принадлежит environment bootstrap, а не
component PR; CA target создаётся только после появления его стандартного
label `kubernetes.io/metadata.name`. Отсутствующий Namespace закрыто оставляет
CA/Vault resources неготовыми и не разрешает запуск workload.

## Доступ workload к Kubernetes API

В K3s API server не является pod с переносимым label. Статический
`podSelector` на `component=kube-apiserver` запрещён. Единственный Service
ClusterIP также недостаточен: CNI может применять `NetworkPolicy` до или после
Service DNAT.

`tools/deploy/kubernetes-api-egress.sh` read-only получает фактические
`Service/default/kubernetes` и его ready IPv4 `EndpointSlice`, проверяет exact
kube context и материализует отдельные additive `NetworkPolicy` одновременно
для Service IP/port и каждого control-plane endpoint/port. Base policy не
содержит широкого либо фиктивного API egress, поэтому пропуск шага
материализации закрыто блокирует workload. `apply` требует отдельный owner OK;
значения из одного контура нельзя переносить в другой.

Каждый workload, которому нужен Kubernetes API, получает отдельную additive
policy для своего namespace и ServiceAccount до запуска. Широкий
namespace/CIDR egress вместо readback `EndpointSlice` запрещен.

Обязательные lint, render, server-side dry-run, OCI build и smoke-проверки
определяются профилем `GOV-DOC-003`. Результат environment render проверяется
целиком, а не выводится из корректности отдельных base-файлов.

## Доступы

SRE использует только согласованные project credentials для внешнего staging.

QA не получает SRE SSH/root credentials.

Связанные документы: `OPS-DOC-001`, `RUN-DOC-001`, `DEPLOY-DOC-001`,
`GO-DOC-001`, `GUIDE-DOC-003`.
Для контура deploy защищённого графа выполнения дополнительно применяется
`GUIDE-DOC-006`.
