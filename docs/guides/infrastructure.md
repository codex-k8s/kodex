---
id: INFRA-DOC-001
title: Infrastructure Guide
type: guide
status: approved
owner: SRE
version: 1.0.0
updated: 2026-07-28
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

Ручное заполнение `emptyDir`/ConfigMap, selector несуществующего workload,
ссылка на отсутствующий namespace-local auth resource и фиктивно успешный
provider path запрещены. Обязательная зависимость проверяется до открытия
готовности и до запуска workers.

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

После render сверяются полный набор разрешенных портов/destinations и
отсутствие destination-less правил. Успешный YAML parse не доказывает
семантику сети.

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
