---
id: RUN-MC-002
title: Чистое развертывание Kodex
type: runbook
status: approved
owner: sre
version: 2.0.3
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

### Готовый Kubernetes

`KODEX_INSTALL_MODE=existing-kubernetes` не обновляет ОС и не устанавливает
k3s. Оператор передаёт exact kubeconfig/context и выбирает компоненты. Для
установки только приложения инфраструктурные зависимости должны уже
удовлетворять тем же контрактам DNS, TLS, OIDC, HTTPS OCI registry и runner
labels `kodex-build`/`kodex-deploy`. Exact registry write identity передаётся
через `KODEX_RELEASE_REGISTRY_USERNAME` и
`KODEX_RELEASE_REGISTRY_PASSWORD`; установщик не создаёт пользователя во
внешнем registry и не ослабляет его TLS.

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
- exact connect address, TLS server name и Kubernetes selector OIDC workload;
- ACME email и ingress selectors;
- постоянного Keycloak administrator и первого owner;
- owner PAT и ARC PAT GitHub;
- registry write identity для existing-Kubernetes без bundled registry;
- Codex `auth.json` как base64 либо путь к файлу;
- необязательные Sentry и external S3 параметры.

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

Установщик принимает только `KODEX_*` assignments, запрещает shell evaluation
и требует mode `0600`. Значения никогда не печатаются.

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

1. CRD и foundation;
2. Certificate/Bundle readback;
3. PostgreSQL и NATS;
4. authority и control-plane migrations;
5. static PostgreSQL role reconciliation;
6. broker bootstrap;
7. `internal-rpc-authority-publisher`;
8. readback всех dynamic Secret projections;
9. остальные Deployments/CronJobs;
10. release artifact materialization;
11. rollout, Job и failing Pod readback.

Нельзя запускать workloads до готовности authority projections или считать
GitHub render фактическим deployment.

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
