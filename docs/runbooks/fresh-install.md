---
id: RUN-MC-002
title: Чистое развертывание Kodex
type: runbook
status: approved
owner: sre
version: 2.2.0
updated: 2026-08-31
---

# Чистое развертывание Kodex

Runbook предназначен для новой установки без переносимых данных. Полный reset
bare-metal узла уничтожает все Kubernetes workloads, PVC и containerd state.
Для production reset обязательно отдельное решение владельца.

## 1. Два поддержанных сценария

### Пустой bare-metal сервер

`KODEX_INSTALL_MODE=bare-metal` устанавливает и настраивает:

1. системные пакеты и firewall;
2. k3s с encryption at rest;
3. cert-manager и trust-manager;
4. Keycloak;
5. Grafana, Prometheus, Alertmanager и Headlamp;
6. локальный OCI registry;
7. GitHub Actions Runner Controller и rootless BuildKit sidecar;
8. PostgreSQL, NATS и control-plane deployables в `kodex-system`, а runtime
   workloads и session-archive workers в `kodex-runtime`.

S3-compatible storage является обязательной зависимостью web-first MVP.
Production использует заранее созданные внешний HTTPS endpoint и bucket;
встроенный object storage не разворачивается.

При установке компонента `secrets` установщик требует S3 env и материализует
Secret `kodex-external-s3` в `kodex-system` с keys `endpoint`, `region`,
`bucket`, `access-key`, `secret-key` и credential-only Secret с тем же именем
и keys `access-key`, `secret-key` в `kodex-runtime`; значения не входят в Git
или render. При установке только компонента `platform` оператор заранее создаёт
оба exact Secret, а установщик выполняет закрытый readback. Control-plane
получает metadata через `secretKeyRef`, credentials через read-only files и
остаётся not ready, если Secret, bucket либо authenticated `HeadBucket`
недоступны. Session-archive worker получает только credential projection в
своём namespace. Bucket создаётся оператором до запуска platform.

Служебный bootstrap registry принадлежит namespace `kodex-infra`; application
registry и workloads Kodex принадлежат `kodex-system`. Смешивать эти границы
или возвращать исторические release namespaces запрещено.

### Готовый Kubernetes

`KODEX_INSTALL_MODE=existing-kubernetes` не обновляет ОС и не устанавливает
k3s. Оператор передаёт exact kubeconfig/context и выбирает компоненты. Для
установки только приложения инфраструктурные зависимости должны уже
удовлетворять тем же контрактам DNS, TLS, OIDC, HTTPS OCI registry и runner
labels `kodex-build`/`kodex-deploy`. Exact registry write identity передаётся
через `KODEX_RELEASE_REGISTRY_USERNAME` и
`KODEX_RELEASE_REGISTRY_PASSWORD`; установщик не создаёт пользователя во
внешнем registry и не ослабляет его TLS.

Admin host, с которого запускается профиль `existing-kubernetes`, должен иметь
`curl`, `dig` из пакета `dnsutils`, `python3` и GNU `timeout`: они выполняют
fail-closed DNS/HTTP-01 preflight до создания публичного `Certificate`.
Bare-metal installer устанавливает `dnsutils` и остальные необходимые утилиты
сам и проверяет их наличие при readback.

Для release build ingress задаётся тремя независимыми параметрами:
`KODEX_INGRESS_NAMESPACE`, `KODEX_INGRESS_POD_NAME` и
`KODEX_INGRESS_SERVICE_NAME`. Service обязан иметь единственный HTTPS port
`443`, выбирать Pod с label `app.kubernetes.io/name=<pod-name>` и иметь готовый
EndpointSlice. Envoy принимает CONNECT только для точного публичного registry
authority, но направляет raw TLS tunnel на этот внутренний Service. Поэтому
клиент продолжает проверять публичные hostname и сертификат, а single-node
установка не зависит от pod-to-public-node hairpin NAT.

Текущий релиз изолируется в namespace `kodex-system`. Произвольное имя
namespace пока закрыто отклоняется, чтобы manifests и security policy не
создавали ложную изоляцию.

### Локальный hot-reload

`dev.sh up` добавляет к web-only render SeaweedFS release 4.41 в
`kodex-system`: один StatefulSet с PVC, внутренний S3 Service на TCP/8333,
deny-by-default policy и bucket bootstrap Job. `tools/dev/deploy-local.sh`
создаёт immutable `kodex-external-s3` из случайных credential files и никогда
не печатает значения. Job создаёт `kodex-artifacts` до migrations и запуска
control-plane; повторный apply выполняет exact Secret/job/readiness readback.

Перед первым `dev.sh up` оператор передаёт приватный auth snapshot, который
текущий Codex CLI подтверждает через `codex login status`:

```bash
KODEX_DEV_PROVIDER_AUTH_FILE="$HOME/.codex/auth.json" ./dev.sh up
```

После `provider-import --account-key default-openai-codex` последующие запуски
автоматически используют
`.kodex-dev/provider-accounts/default-openai-codex/auth.json`. Синтетический
credential и режим `local-development` запрещены: они не создают ложный
`AUTHORIZED` account и не позволяют объявить системного помощника готовым.
`dev.sh down` удаляет только application namespaces `kodex-runtime`,
`kodex-system`, `identity` и `kodex-trust`; общие контроллеры k3s сохраняются.

## 2. Подготовка `.kodex-env`

```bash
cp .kodex-env.example .kodex-env
chmod 0600 .kodex-env
```

Файл заполняется владельцем и не коммитится. Он содержит только входные
параметры конкретной установки:

- режим, kubeconfig/context, публичный IPv4 и необязательный точный глобальный
  IPv6 адрес bare-metal сервера;
- DNS Control Center, SSO, Grafana, Headlamp и registry;
- необязательный отдельный DNS SAN для восстановления Control Center TLS после
  внешнего ACME duplicate-certificate rate limit;
- режим публичного TLS `KODEX_PUBLIC_TLS_MODE=deferred|enabled`;
- точные comma-separated списки разрешённых IPv4/IPv6 ingress для public TLS
  preflight и bounded DNS/HTTP timeouts;
- exact connect address, TLS server name и Kubernetes selector OIDC workload;
- ACME email, ingress workload selector и имя ingress Service;
- постоянного Keycloak administrator и первого owner;
- owner PAT и ARC PAT GitHub;
- registry write identity для existing-Kubernetes без bundled registry;
- Codex `auth.json` как base64 либо путь к файлу;
- режим внешних tracing/Sentry exporters, обязательные external S3 параметры и
  необязательный Sentry DSN.

На доверенном admin host или self-hosted runner файл можно собрать напрямую из
GitHub Variables/Secrets, предварительно передав их в environment шага:

```bash
./tools/install/write-env-file.sh --output .kodex-env
```

Секретный файл нельзя загружать в GitHub artifact. Рекомендуемое разделение:

- Variables: mode, DNS, namespace/context, ingress/OIDC selectors, ACME email;
- Secrets: Keycloak/owner initial passwords, GitHub PAT, registry password,
  OpenAI auth, Sentry DSN и обязательные S3 credentials;
- локальный material: `.kodex-material`, CA/private keys и recovery state.

Bundled MVP по умолчанию использует `KODEX_DISABLE_OBSERVABILITY=true`. Это
отключает только внешние OTel tracing и Sentry exporters: Prometheus endpoints,
`PodMonitor`, `ServiceMonitor`, `PrometheusRule`, Grafana и Alertmanager остаются
рабочими. Значение `false` допустимо только после отдельной установки совместимых
`otel-collector.observability.svc` и `sentry-relay.observability.svc` и требует
непустой `KODEX_SENTRY_DSN`; эти telemetry-компоненты не входят в bundled MVP.

Установщик принимает только `KODEX_*` assignments, запрещает shell evaluation
и требует mode `0600`. Значения никогда не печатаются.

`KODEX_CONTROL_TLS_RECOVERY_HOST` обычно остается пустым. Если удостоверяющий
центр временно запретил повторную выдачу для точного набора identifiers после
нескольких чистых переустановок, владелец может указать отдельное lowercase DNS
имя, заранее направленное на тот же ingress. Оно добавляется только в SAN
сертификата Control Center: публичный host, ingress и OIDC redirect не меняются.
После успешной выдачи значение нельзя удалять до согласованной плановой ротации,
поскольку изменение `dnsNames` само инициирует reissuance. Удаление TLS Secret
для ручной ротации запрещено; используется штатный lifecycle cert-manager.

`KODEX_PUBLIC_TLS_MODE=enabled` является каноническим режимом и применяется по
умолчанию. Режим `deferred` нужен только для явно согласованного окна ACME: он
удаляет принадлежащий платформе публичный `Certificate` Control Center и его
незавершенные ACME-потомки, исключает этот Certificate из preflight/apply и не
ослабляет внутренние mTLS/JWS, OIDC или TLS-проверки. До переключения обратно в
`enabled` публичный endpoint не считается готовым и browser E2E с доверенным TLS
имеет результат `NOT RUN`. HTTP fallback и `skipTLSVerify` запрещены.

Перед переключением в `enabled` владелец отдельно подтверждает допустимое окно
выдачи. `KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES` и
`KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES` содержат comma-separated exact IP без
CIDR, wildcard, пробелов и scope zone. Их объединение должно в точности
совпадать с полным A/AAAA snapshot всех SAN из exact render. Пустой IPv6-список
допустим только при отсутствии AAAA у каждого SAN. Неиспользуемые запасные IP
запрещены, чтобы старый адрес нельзя было молча оставить разрешённым.

До server-side dry-run публичного `Certificate` и повторно непосредственно
перед apply установщик:

1. проверяет, что exact `ClusterIssuer` из render имеет `Ready=True`;
2. выполняет bounded A и AAAA lookup каждого SAN;
3. закрыто отклоняет неизвестный адрес или расхождение snapshot с allowlist;
4. через каждый опубликованный IPv4/IPv6 выполняет bounded HTTP-запрос на port
   80 с exact `Host` соответствующего SAN и принимает только HTTP 1xx-4xx.

`KODEX_PUBLIC_TLS_DNS_TIMEOUT_SECONDS` и
`KODEX_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS` задаются в диапазоне 1-99 секунд;
значение по умолчанию — 10. Проверка не создаёт solver, `CertificateRequest`,
`Order` или `Challenge`. Если применённый публичный `Certificate` не достигает
`Ready=True` за bounded wait, установщик удаляет только объект с exact именем и
owner-метками Kodex, затем ожидает garbage collection его ACME-потомков. Чужой
одноимённый объект закрыто останавливает cleanup. После успешного выпуска
выполняются trusted HTTPS/OIDC/browser readback.

## 3. Secret model

`tools/install/generate-material.sh` один раз создаёт `.kodex-material/`:

- installation CA и workload certificates;
- PostgreSQL bootstrap/static role passwords и DSN;
- NATS operator/account/user credentials;
- internal RPC authority roots/signers;
- registry identities и Cosign key;
- identity/OAuth2 secrets.

`.kodex-material/` не коммитится, имеет mode `0700` и является частью
зашифрованного owner backup. Повторный запуск переиспользует material и не
ротирует identity молча.

Cookie secrets трёх OAuth2 Proxy содержат ровно 32 случайных ASCII-байта.
Materialization проверяет длину до изменения Kubernetes и закрыто отклоняет
старый либо повреждённый material вместо запуска CrashLooping proxy.

`tools/install/materialize-secrets.sh` создаёт Kubernetes Secrets. Static
Secrets принадлежат installer field manager. Динамические key-delivery Secrets
создаёт и обновляет только `internal-rpc-authority-publisher`; installer создаёт
для них пустой generation `0`, но не перезаписывает опубликованное значение.

Provider credential `runtime-provider-openai-default-r1` материализуется как
immutable revision с `auth.json` и `auth.sha256`. Перед созданием metadata
ConfigMap installer сверяет фактический digest, UID и `resourceVersion`.
Повторный запуск с тем же материалом сохраняет revision без изменений. Старый
mutable `r1`, созданный версией installer до этого инварианта, удаляется только
при подтверждённом field manager `kodex-install` и создаётся заново; bootstrap
платформы при следующем старте создаёт новую монотонную credential revision в
БД и атомарно фиксирует новые UID и `resourceVersion`, не изменяя прежнюю
immutable revision. Повторный bootstrap после этого является no-op. Изменение уже
immutable `r1` закрыто отклоняется: ротация credentials должна создавать новую
revision, а не менять существующую.

Platform preflight и readback повторяют проверку immutable, digest и metadata.
Повреждённый либо рассинхронизированный provider Secret останавливает deploy до
создания runtime workloads и не переводит системного помощника в ложное
состояние готовности.

Session PVC системного помощника и обычных runtime создаются без фиксированного
имени `StorageClass`: Kubernetes назначает default class выбранной установки.
Финальный release readback ждёт `Bound` PVC, `Ready` Pod
`system-assistant-warm` и успешный `/assistant/readyz` leader-реплики
runtime-controller. Отсутствующий default `StorageClass` либо неготовый
системный помощник останавливает релиз до сообщения об успешном завершении.

Kubernetes Secrets защищаются k3s encryption at rest. Значения запрещены в
GitHub artifacts, render, logs, Issue/PR и ConfigMap.

Secret `kodex-external-s3` не выдаётся browser, gateway, agent runtime Pod или
agent. Специализированный session-archive worker получает в `kodex-runtime`
только `access-key` и `secret-key`; endpoint, region и bucket передаёт
контроллер из своей проверенной конфигурации. Production endpoint обязан быть
HTTPS без `skipTLSVerify`; local HTTP разрешён только для точного Service DNS
`seaweedfs-s3.kodex-system.svc.cluster.local`.

## 4. Preflight и reset

```bash
sudo ./tools/install/reset-host.sh \
  --confirm-destroy DESTROY-KODEX-HOST
```

Reset применяется только для согласованного пустого узла. Он запускает штатный
k3s kill-all, снимает mounts, удаляет Kubernetes state и проверяет отсутствие
старых firewall chains. Таблица `inet kodex_fw` удаляется отдельно, а
`nftables.service` отключается: её прежняя `FORWARD policy drop` выполнялась до
Kubernetes/UFW и могла блокировать DNAT до ingress pod после переустановки.

Перед установкой DNS всех публичных hosts должен указывать на exact разрешённые
ingress IP, а порты 22, 80 и 443 быть доступны. Опубликованный AAAA допустим
только при фактически работающем внешнем IPv6 HTTP path; иначе запись удаляется
до выпуска сертификата. Другие входящие порты firewall закрывает. UFW
каждый раз строится с нуля: incoming и routed по умолчанию запрещены, pod
forwarding ограничен CIDR `10.42.0.0/16`, служебный трафик -
`10.43.0.0/16`, а внешний DNAT до pod разрешён только для HTTP/HTTPS.
Host-компонент завершается только после одновременной готовности Kubernetes API
и хотя бы одного node; краткое состояние `NotReady` во время запуска Flannel
обрабатывается bounded wait, а не считается ошибкой установки.

Bare-metal профиль K3s/Traefik остается IPv4-only. Если публичные DNS-имена
имеют только `A` записи, `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` оставляют пустым:
IPv6 bridge не создается, а повторный apply удаляет только ранее созданные
Kodex unit-файлы. Наличие неизвестного unit или drop-in с тем же именем
останавливает операцию до ручного разбора и не приводит к перезаписи.

Если хотя бы один публичный host имеет `AAAA`, владелец задает
`KODEX_SERVER_PUBLIC_IPV6_ADDRESS` как точный глобальный IPv6 адрес, уже
назначенный интерфейсу сервера. Host installer создает две socket-activated
пары `kodex-ipv6-ingress-bridge-{80,443}.{socket,service}`. Сокеты слушают
только `[$KODEX_SERVER_PUBLIC_IPV6_ADDRESS]:80|443`, а
`systemd-socket-proxyd` передает raw TCP на `127.0.0.1:80|443`. Bridge не
завершает TLS, не использует wildcard bind и не открывает дополнительные
порты. Число одновременных соединений ограничено, service sandboxed, а unit
files имеют явный owner marker.

`preflight` закрыто отклоняет синтаксически неверный, неглобальный либо не
назначенный host-интерфейсу IPv6 адрес. `apply` идемпотентно сверяет ownership перед
записью и включает только socket units. `readback` сравнивает точное содержимое
и права unit-файлов, состояния `enabled/active/listening` обоих сокетов и
выполняет локальный HTTP-запрос через IPv6 bridge до Traefik. Проверка
публичного TLS и DNS выполняется отдельным ACME preflight после готовности
всего ingress.

## 5. Установка

Полный неинтерактивный bare-metal запуск:

```bash
sudo ./install.sh --non-interactive --components all
```

Интерактивный запуск без `--components` предлагает каждый компонент отдельно.
Явный выбор для существующего Kubernetes:

```bash
./install.sh \
  --components secrets,platform
```

В этом режиме cert-manager, OIDC, HTTPS registry и self-hosted runners уже
существуют и соответствуют входным параметрам `.kodex-env`. Если их нет,
выбираются нужные инфраструктурные компоненты явно; bundled ARC требует
bundled registry, а bundled management surfaces устанавливаются вместе с
bundled identity.

BuildKit не является host daemon. Он запускается rootless sidecar только в
ephemeral ARC build runner. Build и render выполняются на exact SHA в GitHub
Actions; установщик скачивает digest-bound render и применяет его локально к
целевому context.

## 6. Порядок платформы

`tools/install/deploy-platform.sh` соблюдает порядок:

1. в `enabled` — fail-closed exact ClusterIssuer, DNS A/AAAA и внешний HTTP-01
   preflight до публичного `Certificate`;
2. owner-checked retirement stale image-admission binding после fresh namespace
   reset, если её namespaced parameters отсутствуют;
3. phase-aware server-side dry-run CRD и уже известных API без persistence;
4. повторный public TLS preflight, CRD и foundation; в режиме `deferred`
   публичный Certificate исключается;
5. Certificate/Bundle readback;
6. PostgreSQL, NATS и проверка обязательного внешнего S3 Secret/bucket;
7. authority и control-plane migrations;
8. static PostgreSQL role reconciliation;
9. broker bootstrap;
10. `internal-rpc-authority-publisher`;
11. readback всех dynamic Secret projections;
12. остальные Deployments/CronJobs;
13. release artifact materialization, включая exact `control-plane` sentinel
    для authenticated pull-registry readback; транспортный отказ readback
    повторяется ограниченно, а несовпадение digest немедленно закрывает выпуск;
14. rollout, Job и failing Pod readback.

Нельзя запускать workloads до готовности authority projections или считать
GitHub render фактическим deployment.

Preflight повторяет семантику actual apply: mutable ресурсы проверяются через
server-side apply с тем же field manager, а удаляемые перед заменой immutable
ConfigMap и Job проверяются отдельным server-side create с временным именем.
Сам preflight остаётся read-only. Предшествующая фаза `prepare-preflight`
удаляет только cluster-scoped image-admission binding, для которой одновременно
отсутствуют namespaced parameters и labels/spec в точности совпадают с exact
render; неизвестное расхождение закрыто останавливает выпуск. Apply в той же
операции восстанавливает parameters и fail-closed binding из exact render.
Существующий seed Secret не переотправляется через admission policy. При apply
изменившийся immutable ConfigMap удаляется только после проверки platform-owned
labels и затем воссоздаётся из exact render. Такой же owner-checked replacement
выполняется для immutable `ImageAdmissionPolicyParameters` после установления
его CRD и до применения foundation.

## 7. Восстановление и rollback

- Повторный `install.sh` переиспользует owner material и идемпотентно сверяет
  infrastructure.
- Application rollback выполняется новым exact release старого Git SHA.
- Перед dispatch каждого exact release `release-platform.sh` сверяет
  неизменяемые owner actor, workflow ref, job и runner hook, затем разрешает в
  GitHub Variable `KODEX_WORKFLOW_SHA` и обоих ARC gate ровно текущий clean
  `main` SHA. Обновление Kubernetes защищено `resourceVersion`; оба слоя
  проходят exact readback, включая ожидание projected volume уже существующих
  runner Pod. Ручное редактирование variable или `kodex-runner-owner-gate`
  запрещено.
- Потеря `.kodex-material` при существующих данных является инцидентом, а не
  поводом генерировать новые roots поверх работающей установки.
- PostgreSQL/PVC backup и внешняя копия `.kodex-material` хранятся раздельно и
  в зашифрованном виде.

## 8. Обязательный readback

Установка завершена только если:

- Kubernetes API и node Ready;
- при заданном `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` exact IPv6 bridge для 80/443
  enabled/active/listening, а локальный IPv6 HTTP-запрос достигает Traefik;
- при пустом `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` owner-managed bridge units
  отсутствуют и не активны;
- cert-manager/trust-manager/Keycloak/ARC готовы;
- Traefik подключается к Keycloak с exact public SNI через привязанный к
  `Service` `ServersTransport`, а публичный OIDC discovery отвечает без TLS
  fallback;
- Control Center, Grafana и Headlamp закрыты OAuth2 Proxy;
- Headlamp пропускает только Keycloak `admin` и использует `cluster-admin`;
- все platform StatefulSet/Deployment готовы, migration Jobs успешны;
- production control-plane прошёл authenticated `HeadBucket` внешнего S3, а
  local SeaweedFS, S3 EndpointSlice и `seaweedfs-bucket-bootstrap` готовы;
- отсутствуют `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull` и
  `CreateContainerConfigError`;
- API/OIDC и browser E2E пройдены без раскрытия credentials.
