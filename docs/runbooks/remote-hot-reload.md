---
id: RUNBOOK-DOC-REMOTE-DEV-001
title: Удалённый hot-reload контур Kodex
type: runbook
status: approved
owner: manager
version: 1.2.0
updated: 2026-09-02
---

# Удалённый hot-reload контур Kodex

## Назначение

Контур предназначен только для разработки и E2E на disposable bare-metal
сервере. Он устанавливает системные утилиты, Docker, одноузловой k3s, Traefik,
cert-manager, SeaweedFS, Kodex с монтированием исходников и Teleport Community
Edition. Для публичных интерфейсов используются реальные сертификаты Let's
Encrypt. Production-данные и production-секреты в контур не переносятся.

Только bare-metal bootstrap удалённого контура устанавливает именованный AppArmor profile
`kodex-provider-runtime`. Он точечно разрешает `userns` только provider-контейнеру,
чтобы Codex мог создать внутренний bubblewrap sandbox с запретом чтения
`auth.json`, `/run/secrets` и `/proc`. Системное ограничение unprivileged user
namespaces при этом глобально не отключается.
Portable base и профиль установки в существующий Kubernetes не предполагают
наличие node-local AppArmor profile: параметр остаётся пустым, а поле
`securityContext.appArmorProfile` не материализуется. Удалённый renderer задаёт
его только после code-owned host readback загруженного профиля.

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

1. Все публичные DNS-имена из `/srv/kodex-dev/private/remote.env` имеют точные `A`/`AAAA`,
   указывающие только на разрешённые ingress-адреса.
2. Входящие TCP-порты `22`, `80`, `443` доступны извне. Другие входящие
   соединения host firewall запрещает.
3. Оператор входит по SSH-ключу и имеет passwordless `sudo`. Host bootstrap
   закрепляет его как единственную break-glass SSH identity, запрещает пароль,
   keyboard-interactive и root login и проверяет effective `sshd -T` policy.
4. Репозиторий клонирован в `/srv/kodex-dev/workspace` от имени оператора.
5. Приватный `/srv/kodex-dev/private/remote.env` создан по
   [примеру](../../.kodex-remote-env.example), находится вне source checkout,
   а каталог и файл имеют mode `0700` и `0600`.
6. Как минимум один приватный Codex `auth.json` импортирован в
   `/srv/kodex-dev/state/provider-accounts/default-openai-codex/auth.json`.

DNS/HTTP preflight выполняется до создания любого публичного `Certificate`.
Он публикует через Traefik одноразовый exact HTTP-01 path, требует вернуть
уникальный token через каждый разрешённый `A`/`AAAA`, затем проверяет тот же
path с внешних узлов LetsDebug и доступность TCP `443` с узлов Check-Host.
Локальная hairpin-проверка не заменяет эти внешние probes. Произвольный
`1xx-4xx`, неполный body, недоступный внешний API или отсутствие хотя бы одного
успешного внешнего TCP readback закрыто останавливают установку и не расходуют
попытку ACME.

Host bootstrap не выполняет `apt upgrade`. Ubuntu release и версии
`containerd`, `docker-buildx`, `docker-compose-v2`, `docker.io`, `runc`
зафиксированы в `tools/install/components.lock.json`; apply устанавливает exact
версии, ставит packages на hold, а readback сравнивает фактические версии и hold.

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
- exact connector readback через `tctl get --with-secrets`; значения credentials
  сравниваются внутри процесса и никогда не выводятся.

## Установка

Сначала выполняется read-only preflight, затем тот же code-owned entrypoint
применяет изменения:

```bash
EXPECTED_SHA=$(git rev-parse HEAD)
REMOTE_ENV=/srv/kodex-dev/private/remote.env
./tools/dev/remote-dev.sh host-preflight --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh host-apply --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh host-readback --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
```

После `host-apply` нужно открыть новую SSH-сессию, чтобы применилось членство
оператора в группе `docker`. Readback обязан подтвердить k3s, Docker buildx,
firewall и загруженный AppArmor profile. Root-owned k3s kubeconfig используется
только через временный файл внутри entrypoint и удаляется после команды.

## Пользовательский вход через Teleport

Локальный Teleport-профиль Kodex нужно изолировать от рабочих Teleport-кластеров.
Repo-owned установщик загружает pinned клиент, проверяет digest и создаёт
wrapper `tsh-kodex` с отдельным `HOME`:

```bash
./tools/dev/install-tsh-client.sh apply
./tools/dev/install-tsh-client.sh readback
```

После отдельного code-owned `teleport` и до application `up` пользователь выполняет:

```bash
tsh-kodex login --proxy=teleport.kodex.works:443 --auth=github
tsh-kodex ssh kodex-teleport@kodex.works
tsh-kodex kube login kodex-dev
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl get --raw=/readyz
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i get pods --all-namespaces
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i get secrets --all-namespaces
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i create clusterrolebindings
```

Ожидаемый результат: SSH, `/readyz` и чтение pod разрешены; чтение Secret и
создание ClusterRoleBinding запрещены. Break-glass SSH-сессию нельзя закрывать,
пока новая Teleport SSH-сессия и Kubernetes readback не подтверждены.

## Запуск и проверка

```bash
./tools/dev/remote-dev.sh teleport --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh up --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh status --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh smoke --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh e2e --env-file "$REMOTE_ENV" \
  --resource-prefix remote-e2e-001 --expected-sha "$EXPECTED_SHA"
./tools/dev/remote-dev.sh acceptance --env-file "$REMOTE_ENV" \
  --resource-prefix remote-acceptance-001 --run-timeout-ms 1800000 \
  --expected-sha "$EXPECTED_SHA"
```

`up` монтирует текущий worktree в Go- и Vue-workloads через `hostPath`. Air и
Vite отслеживают изменения исходников без пересборки полного release image.
Тяжёлые runtime/supply-chain образы пересобираются только при изменении их
входов и импортируются напрямую в containerd k3s.

`e2e` запускает только browser discovery и остаётся диагностической командой.
Канонический owner gate использует `acceptance`: он требует чистый exact SHA до
и после выполнения, сначала проверяет Teleport, затем deployment readback,
реальный hot reload Go и Vue, browser/API сценарии, synthetic integration,
сборку и допуск RoleImage, session archive и disposable backup/restore drill,
после чего повторяет Teleport readback. Для быстрого сбора независимых дефектов
`tools/dev/full-local-e2e.sh` принимает повторяемый `--batch` со значениями
`hot-reload`, `browser`, `integration`, `role-image`, `archive`, `backup`.
Профили GitHub и provider API key всегда получают явный итог `PASS`, `FAIL` или
`NOT RUN`; отсутствие credentials больше не маскируется как выполненная проверка.

Hot reload проверяется без постоянного тестового endpoint. Repo-owned скрипт
временно меняет существующий ответ gateway `/healthz` и маркер в `App.vue`,
наблюдает `204 -> 202 -> 204` и появление/исчезновение маркера в Vite-модуле,
после чего восстанавливает файлы и повторно требует чистый exact SHA. При
сигнале `INT`/`TERM` восстановление выполняется через `trap`; оставшийся dirty
checkout закрыто блокирует следующий rollout.

Browser discovery сохраняет скриншоты `1920x1080` и `1440x900` только в
приватном state directory. Redacted report содержит имя evidence, viewport,
размер, SHA-256 и source SHA, но не абсолютный путь и не содержимое изображения.
Успешный browser gate требует шесть уникальных visual evidence на том же SHA.

Перед первым browser smoke entrypoint устанавливает системные зависимости и
только Chromium через зафиксированный в `package-lock.json` локальный
Playwright. Браузер хранится в cache пользователя, после установки entrypoint
обязан реально запустить и закрыть его. Проверены актуальные документы
Playwright 1.61 через Context7: `install-deps chromium`, `install chromium` и
Linux cache `~/.cache/ms-playwright`; для новых сценариев также проверены
`Locator.drop`, viewport, `TestInfo.attach` и custom reporter attachments.

Для внешнего ACME preflight проверены официальные API-документы LetsDebug и
Check-Host. Внешние сервисы используются только для публичных DNS-имён и
одноразового challenge path; credentials и приватные адреса им не передаются.

Host-owned Teleport применяется отдельно до application rollout. Повторный
`up` обновляет только Kubernetes route и не перезапускает access plane:

```bash
./tools/dev/remote-dev.sh teleport --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
```

## Завершение

```bash
KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER \
  ./tools/dev/remote-dev.sh down --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
```

Команда удаляет application namespaces, но оставляет общие dev-контроллеры и
host-owned Teleport для диагностики. Она требует отдельную точную фразу
подтверждения disposable-среды и сверяет UID, API endpoint и CA текущего
кластера с root-owned marker. Перед production-релизом сервер очищается
полностью, после чего production-инсталлятор запускается на чистом хосте.
