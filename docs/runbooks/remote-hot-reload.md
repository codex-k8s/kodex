---
id: RUNBOOK-DOC-REMOTE-DEV-001
title: Удалённый hot-reload контур Kodex
type: runbook
status: approved
owner: manager
version: 1.1.0
updated: 2026-09-02
---

# Удалённый hot-reload контур Kodex

## Назначение

Контур предназначен только для разработки и E2E на disposable bare-metal
сервере. Он устанавливает системные утилиты, Docker, одноузловой k3s, Traefik,
cert-manager, SeaweedFS, Kodex с монтированием исходников и Teleport Community
Edition. Для публичных интерфейсов используются реальные сертификаты Let's
Encrypt. Production-данные и production-секреты в контур не переносятся.

Host bootstrap также устанавливает именованный AppArmor profile
`kodex-provider-runtime`. Он точечно разрешает `userns` только provider-контейнеру,
чтобы Codex мог создать внутренний bubblewrap sandbox с запретом чтения
`auth.json`, `/run/secrets` и `/proc`. Системное ограничение unprivileged user
namespaces при этом глобально не отключается.

Teleport Auth, Proxy, SSH и Kubernetes services работают как root-owned
`systemd`-служба на хосте и хранят состояние вне disposable k3s. В кластере
остаются только публичный сертификат Let's Encrypt, проверяющий внутреннюю CA
маршрут Traefik и ограниченный Kubernetes RBAC. Между Traefik и host Teleport
используется отдельный приватный TLS-сертификат; `systemd`-служба получает
точный trust bundle из системных CA и этой внутренней CA. Поэтому пересоздание
k3s не удаляет access plane: после нового bootstrap маршрут привязывается к
тому же Teleport.

Прямой SSH на порт `22` остаётся break-glass доступом установщика. Обычный
GitHub-пользователь входит через Teleport как отдельный Linux-пользователь
`kodex-teleport` без `sudo`. Постоянный административный kubeconfig не
копируется в домашний каталог оператора и не выдаётся пользователю: code-owned
bootstrap использует временную приватную копию root-owned k3s kubeconfig, а
в приватной копии Teleport единственный context получает имя `kodex-dev`.
Пользователь работает через `tsh kube login`.

## Предварительные условия

1. Все публичные DNS-имена из `.kodex-remote-env` имеют точные `A`/`AAAA`,
   указывающие только на разрешённые ingress-адреса.
2. Входящие TCP-порты `22`, `80`, `443` доступны извне. Другие входящие
   соединения host firewall запрещает.
3. Оператор входит по SSH-ключу и имеет passwordless `sudo`.
4. Репозиторий клонирован в `/srv/kodex-dev/workspace` от имени оператора.
5. Приватный `.kodex-remote-env` создан по
   [примеру](../../.kodex-remote-env.example) и имеет mode `0600`.
6. Как минимум один приватный Codex `auth.json` импортирован в
   `/srv/kodex-dev/state/provider-accounts/default-openai-codex/auth.json`.

DNS/HTTP preflight выполняется до создания любого публичного `Certificate`.
Он публикует через Traefik одноразовый exact HTTP-01 path, требует вернуть
уникальный token через каждый разрешённый `A`/`AAAA` и отдельно проверяет TCP
`443`. Произвольный `1xx-4xx` больше не считается успешной проверкой. Ошибка
preflight закрыто останавливает установку и не расходует попытку ACME.

## GitHub OAuth для Teleport

Teleport Community Edition использует отдельный GitHub OAuth App:

- Homepage URL: `https://<KODEX_REMOTE_TELEPORT_HOST>`;
- Authorization callback URL:
  `https://<KODEX_REMOTE_TELEPORT_HOST>/v1/webapi/github/callback`;
- `Client ID` сохраняется как `KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_ID`;
- `Client secret` сохраняется как
  `KODEX_REMOTE_TELEPORT_GITHUB_CLIENT_SECRET`;
- доступ получает только команда из
  `KODEX_REMOTE_GITHUB_ORGANIZATION/KODEX_REMOTE_GITHUB_TEAM`.

Участникам этой команды Teleport назначает роль `kodex-dev-access`. Она
разрешает SSH только на host с label `environment=development` и отображается
в Kubernetes group `kodex-teleport-dev-observers`. ClusterRole разрешает только
`get/list/watch` диагностических ресурсов и намеренно исключает `Secret`,
мутации, `pods/exec`, impersonation и `system:masters`. Изменения контура
выполняет только code-owned bootstrap через break-glass identity установщика.

Проверены актуальные документы Teleport 18 через Context7:

- host-owned Teleport Auth, Proxy, SSH и Kubernetes services;
- GitHub connector `v3` и callback path;
- Teleport role labels, SSH login и Kubernetes group mapping.
- пользовательский CA через `SSL_CERT_FILE` для приватного TLS backend.
- static kubeconfig без взаимоисключающего `kube_cluster_name`; имя кластера
  задаётся единственным context `kodex-dev`.

## Установка

Сначала выполняется read-only preflight, затем тот же code-owned entrypoint
применяет изменения:

```bash
EXPECTED_SHA=$(git rev-parse HEAD)
./tools/dev/remote-dev.sh host-preflight --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh host-apply --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh host-readback --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
```

После `host-apply` нужно открыть новую SSH-сессию, чтобы применилось членство
оператора в группе `docker`. Readback обязан подтвердить k3s, Docker buildx,
firewall и загруженный AppArmor profile. Root-owned k3s kubeconfig используется
только через временный файл внутри entrypoint и удаляется после команды.

## Запуск и проверка

```bash
./tools/dev/remote-dev.sh up --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh status --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh smoke --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh e2e --env-file .kodex-remote-env \
  --resource-prefix remote-e2e-001 --expected-sha "$EXPECTED_SHA"
```

`up` монтирует текущий worktree в Go- и Vue-workloads через `hostPath`. Air и
Vite отслеживают изменения исходников без пересборки полного release image.
Тяжёлые runtime/supply-chain образы пересобираются только при изменении их
входов и импортируются напрямую в containerd k3s.

Перед первым browser smoke entrypoint устанавливает системные зависимости и
только Chromium через зафиксированный в `package-lock.json` локальный
Playwright. Браузер хранится в cache пользователя, после установки entrypoint
обязан реально запустить и закрыть его. Проверены актуальные документы
Playwright 1.61 через Context7: `install-deps chromium`, `install chromium` и
Linux cache `~/.cache/ms-playwright`.

Если OAuth App был создан после основного запуска, Teleport применяется
отдельно:

```bash
./tools/dev/remote-dev.sh teleport --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
```

## Завершение

```bash
KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER \
  ./tools/dev/remote-dev.sh down --env-file .kodex-remote-env --expected-sha "$EXPECTED_SHA"
```

Команда удаляет application namespaces, но оставляет общие dev-контроллеры и
host-owned Teleport для диагностики. Она требует отдельную точную фразу
подтверждения disposable-среды и сверяет UID, API endpoint и CA текущего
кластера с root-owned marker. Перед production-релизом сервер очищается
полностью, после чего production-инсталлятор запускается на чистом хосте.
