---
id: RUN-MC-002
title: Чистое развертывание Kodex
type: runbook
status: approved
owner: sre
version: 2.0.6
updated: 2026-08-25
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
8. PostgreSQL, Redis/NATS и все deployable Kodex в `kodex-system`.

S3 не является обязательной зависимостью web-first MVP. Внешний S3 включается
только явной настройкой для backup/export. Встроенный object storage не
устанавливается скрыто.

При `KODEX_ENABLE_EXTERNAL_S3=true` установщик материализует параметры в
Secret `kodex-external-s3`. Пока deployable adapter не выбран продуктовым
профилем, этот Secret не монтируется в Pod и сам по себе не включает хранение
artifact или backup.

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

## 2. Подготовка `.kodex-env`

```bash
cp .kodex-env.example .kodex-env
chmod 0600 .kodex-env
```

Файл заполняется владельцем и не коммитится. Он содержит только входные
параметры конкретной установки:

- режим, kubeconfig/context и публичный IPv4;
- DNS Control Center, SSO, Grafana, Headlamp и registry;
- необязательный отдельный DNS SAN для восстановления Control Center TLS после
  внешнего ACME duplicate-certificate rate limit;
- режим публичного TLS `KODEX_PUBLIC_TLS_MODE=deferred|enabled`;
- exact connect address, TLS server name и Kubernetes selector OIDC workload;
- ACME email, ingress workload selector и имя ingress Service;
- постоянного Keycloak administrator и первого owner;
- owner PAT и ARC PAT GitHub;
- registry write identity для existing-Kubernetes без bundled registry;
- Codex `auth.json` как base64 либо путь к файлу;
- режим внешних tracing/Sentry exporters и необязательные Sentry/external S3
  параметры.

На доверенном admin host или self-hosted runner файл можно собрать напрямую из
GitHub Variables/Secrets, предварительно передав их в environment шага:

```bash
./tools/install/write-env-file.sh --output .kodex-env
```

Секретный файл нельзя загружать в GitHub artifact. Рекомендуемое разделение:

- Variables: mode, DNS, namespace/context, ingress/OIDC selectors, ACME email;
- Secrets: Keycloak/owner initial passwords, GitHub PAT, registry password,
  OpenAI auth, Sentry DSN и S3 credentials;
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
выдачи. После переключения выполняется повторный exact render deployment,
ожидание `Certificate/Ready` и trusted HTTPS/OIDC/browser readback.

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

Kubernetes Secrets защищаются k3s encryption at rest. Значения запрещены в
GitHub artifacts, render, logs, Issue/PR и ConfigMap.

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

Перед установкой DNS всех публичных hosts должен указывать на сервер, а порты
22, 80 и 443 быть доступны. Другие входящие порты firewall закрывает. UFW
каждый раз строится с нуля: incoming и routed по умолчанию запрещены, pod
forwarding ограничен CIDR `10.42.0.0/16`, служебный трафик -
`10.43.0.0/16`, а внешний DNAT до pod разрешён только для HTTP/HTTPS.
Host-компонент завершается только после одновременной готовности Kubernetes API
и хотя бы одного node; краткое состояние `NotReady` во время запуска Flannel
обрабатывается bounded wait, а не считается ошибкой установки.

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

1. phase-aware server-side dry-run CRD и уже известных API без persistence;
2. CRD и foundation; в режиме `deferred` публичный Certificate исключается;
3. Certificate/Bundle readback;
4. PostgreSQL и NATS;
5. authority и control-plane migrations;
6. static PostgreSQL role reconciliation;
7. broker bootstrap;
8. `internal-rpc-authority-publisher`;
9. readback всех dynamic Secret projections;
10. остальные Deployments/CronJobs;
11. release artifact materialization;
12. rollout, Job и failing Pod readback.

Нельзя запускать workloads до готовности authority projections или считать
GitHub render фактическим deployment.

Preflight повторяет семантику actual apply: mutable ресурсы проверяются через
server-side apply с тем же field manager, а удаляемые перед заменой immutable
ConfigMap и Job проверяются отдельным server-side create с временным именем.
Существующий seed Secret не переотправляется через admission policy. При apply
изменившийся immutable ConfigMap удаляется только после проверки platform-owned
labels и затем воссоздаётся из exact render. Такой же owner-checked replacement
выполняется для immutable `ImageAdmissionPolicyParameters` после установления
его CRD и до применения foundation.

## 7. Восстановление и rollback

- Повторный `install.sh` переиспользует owner material и идемпотентно сверяет
  infrastructure.
- Application rollback выполняется новым exact release старого Git SHA.
- Потеря `.kodex-material` при существующих данных является инцидентом, а не
  поводом генерировать новые roots поверх работающей установки.
- PostgreSQL/PVC backup и внешняя копия `.kodex-material` хранятся раздельно и
  в зашифрованном виде.

## 8. Обязательный readback

Установка завершена только если:

- Kubernetes API и node Ready;
- cert-manager/trust-manager/Keycloak/ARC готовы;
- Traefik подключается к Keycloak с exact public SNI через привязанный к
  `Service` `ServersTransport`, а публичный OIDC discovery отвечает без TLS
  fallback;
- Control Center, Grafana и Headlamp закрыты OAuth2 Proxy;
- Headlamp пропускает только Keycloak `admin` и использует `cluster-admin`;
- все platform StatefulSet/Deployment готовы, migration Jobs успешны;
- отсутствуют `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull` и
  `CreateContainerConfigError`;
- API/OIDC и browser E2E пройдены без раскрытия credentials.
