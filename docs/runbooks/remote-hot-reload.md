---
id: RUNBOOK-DOC-REMOTE-DEV-001
title: Удалённый hot-reload контур Kodex
type: runbook
status: approved
owner: manager
version: 1.4.2
updated: 2026-09-09
---

# Удалённый hot-reload контур Kodex

## Назначение

Контур предназначен только для разработки и E2E на disposable bare-metal
сервере. Он устанавливает системные утилиты, Docker, одноузловой k3s, Traefik,
cert-manager, SeaweedFS, Kodex с монтированием исходников и Teleport Community
Edition. Для публичных интерфейсов используются реальные сертификаты Let's
Encrypt. Production-данные и production-секреты в контур не переносятся.

## Профиль разработки

По [решению владельца от 2026-09-08, #1343](https://github.com/codex-k8s/kodex/issues/1343)
hot reload сохраняется на весь период разработки Kodex и последующих циклов
ручной приёмки. Для Go/Vue с монтируемым кодом основной путь — точная новая
source revision и штатный scoped source release. Новый Git SHA сам по себе
не требует Docker build неизменного образа с toolchain. Завершение MVP не
предполагает перевод всех dev units на immutable application images; смена
этого профиля требует отдельного решения владельца.

Go/npm manifests сначала требуют штатной подготовки зависимостей и кэша.
Она не равна обязательной пересборке toolchain image. Docker build нужен при
реальном изменении содержимого образа: Dockerfile/base, OS packages, toolchain,
native dependencies либо встроенных binary/assets. Для runner/runtime/RoleImage
с встроенным исполняемым кодом сохраняется полный build/admission/promotion/
exact digest путь. Смена source Deployment не обновляет такой binary в Jobs;
active attempt pins и immutable inputs не перепривязываются ради обхода сборки.

Каждая поставка остаётся code-first: Issue/PR, точный source, preflight, scoped
plan/apply и actual readback совместимых component revisions. Application source
не меняет соседние sidecars, keys/trust/grants; DB migration, config и security
переходы выполняются отдельно. Существующие данные, accounts, sessions и fixture
history сохраняются: повторный wipe и `down` текущего контура запрещены.
Обычный цикл исправления не запускает глобальный `up`.

Обычные переносимые установки по-прежнему поддерживают immutable application
images без hostPath/Air/Vite/SSH; production требует отдельного решения владельца.
Критерии выбора сборки и команды ежедневной поставки находятся в
[руководстве релиза](../../tools/release/README.md#выбор-source-и-image).

### Непривилегированный status/smoke для root-owned source

`remote-dev.sh status|smoke|e2e|acceptance` запускается от штатного оператора,
даже если `create-application-source.mjs` создал checkout от root. Перед Git
скрипт проверяет канонические пути, владельца root/текущего оператора и отсутствие
записи для посторонних у source, предков и всей Git metadata. Group-write
допускается только для локальной группы с полностью разрешёнными primary и
supplementary участниками UID root/оператора; неизвестная или удалённая NSS
группа закрыто отклоняется. Для linked
worktree проверяются `.git`, обратная ссылка `gitdir` и `commondir`. Системный
root-owned sticky ancestor вроде `/tmp` допускается только как предок.

`--component-manifest` должен указывать на приватный файл 0600 текущего
оператора вне source. Только его точные `components[].sources[].path/revision`
добавляются к checkout оснастки; вложенный `mountedPath` обязан принадлежать
своему root. Проверяются SHA, origin, чистота и отсутствие приватных env.
Если исходный manifest принадлежит root:600, штатный оператор предварительно
создаёт отдельную копию в своём каталоге 0700. Expected digest берётся из
проверенного release evidence; прежний файл и права не меняются:

```bash
node tools/dev/prepare-operator-manifest.mjs \
  --source "$ROOT_OWNED_MANIFEST" --sha256 "$EXPECTED_MANIFEST_SHA256" \
  --output "$NEW_OPERATOR_PRIVATE_MANIFEST"
```

Команда требует непривилегированного пользователя, читает через bounded sudo
только exact regular input и проверяет digest до записи. Затем проверяются все
`compatibility.evidence` по их SHA. Недоступные root-owned proofs копируются
wx0600 рядом с новым manifest с неизменными bytes/SHA, их path явно заменяется,
а manifest identity пересчитывается. Отчёт содержит original/output SHA,
original/output identity и числа проверенных/скопированных proofs. Доступные
operator-owned refs остаются прежними. Ошибка проверки любого proof прекращает
подготовку до первой записи; ошибка записи сохраняет частичные приватные файлы
для диагностики и не запускает status автоматически.
Затем status/smoke получает `--component-manifest "$NEW_OPERATOR_PRIVATE_MANIFEST"`.

Последующий `component-manifest verify` по-прежнему проверяет фактические
cluster/workload/mount/binary/contract identities; это отдельный обязательный
шлюз, который разрешение Git не заменяет.

Доверие задаётся через `GIT_CONFIG_COUNT` только в процессе и наследуется всеми
потомками (`dev.sh`, component manifest, `inspectSource`). Первая пустая
`safe.directory` сбрасывает прежние значения, затем перечисляются только точные
проверенные root. Старые `GIT_*` overrides удаляются. Глобальный Git config,
ownership и permissions существующих каталогов не меняются; wildcard запрещён.
Обычные команды status/smoke и `--expected-sha` остаются прежними. Код перехода
должен быть доставлен в новый точный checkout оснастки до запуска.

Семантика command-scope `safe.directory` сверена через Context7 `/git/htmldocs`
и официальную [документацию Git](https://git-scm.com/docs/git-config#Documentation/git-config.txt-safedirectory).
Локальная проверка полного наследования, linked metadata и отрицательных случаев:
`node --test tools/dev/source-git-trust.test.mjs tools/dev/component-manifest.test.mjs tools/release/application-source.test.mjs`.

### Подготовка Go cache перед ограниченной выкладкой

При изменении Go dependencies root готовит только выбранные hot-reload modules
из точного чистого source. Команда не запускает Docker, сборку image, render,
Kubernetes apply, миграции, Air install или frontend preparation. Общий dev
профиль остаётся hot reload; новый Git SHA не требует image rebuild.

Cache root должен уже существовать и принадлежать оператору. На текущем host
это `/srv/kodex-dev/state/cache`; private evidence хранится в отдельном каталоге
0700, каждый JSONL имеет новое имя и права 0600. Root запускает скрипт из точного
checkout после доставки исходников штатным creator/prepare:

```bash
python3 tools/dev/prime-go-cache.py plan --profile staging-hot-reload \
  --source-root "$SOURCE" --revision "$EXACT_SOURCE_SHA" \
  --cache-root /srv/kodex-dev/state/cache \
  --module services/external/control-api-gateway \
  --timeout-seconds 600 --lock-timeout-seconds 60 \
  --evidence "$NEW_PRIVATE_PLAN_EVIDENCE"
python3 tools/dev/prime-go-cache.py prime --profile staging-hot-reload \
  --source-root "$SOURCE" --revision "$EXACT_SOURCE_SHA" \
  --cache-root /srv/kodex-dev/state/cache \
  --module services/external/control-api-gateway \
  --timeout-seconds 600 --lock-timeout-seconds 60 \
  --evidence "$NEW_PRIVATE_PRIME_EVIDENCE" --confirm PRIME-STAGING-GO-CACHE
```

`--module` можно повторить для ограниченного списка из закрытого реестра
`tools/dev/go_cache.py`: 12 основных Go deployables и optional interaction-gateway.
Runner/RoleImage binaries в этот source-cache workflow не входят. Plan читает
Git/module metadata и проверяет Go 1.26.6; shared cache не меняется, сеть не нужна.
Prime повторяет проверки и берёт общий `.go-prime.lock`; lock timeout не запускает
вторую подготовку. Бюджет рабочих команд до 1800 секунд, ожидания lock — 600 секунд;
после timeout/сигнала дочерний процесс завершается до закрытия cache permissions.
Завершающее восстановление permissions выполняется и при ошибке, после прекращения
рабочих команд; оно не прерывается из-за исчерпания их бюджета.

Go получает `GOWORK=off`, `GOTOOLCHAIN=local`, `GOENV=off` и отдельный HOME;
private env, host git credentials, netrc, GOFLAGS и пользовательский GOPROXY
не наследуются. Используются публичные proxy.golang.org и sum.golang.org.
Из source копируются только точные go.mod/go.sum и локальные replacement
manifests внутри libs/go. Download/verify выполняются в приватной временной
копии: исходный checkout никогда не исправляется командой Go. Необходимая правка
go.mod/go.sum даёт `GO_MANIFEST_REWRITE_REQUIRED` и требует обычного code-first fix.
Источник, revision, module identities и исходные digests проверяются до/после;
source drift закрыто прекращает подготовку.

Cache roots `go-mod-v2`, `go-sumdb` после prime доступны non-root Pods на чтение
и не имеют write bits. Workload-specific build caches остаются отдельными;
host-prime build cache доступен только оператору. JSONL содержит INTENT до
подготовки, версии и h1 sums dependencies, source/manifests digests, PASS/FAIL;
сырые stdout/stderr Go, URL credentials и содержимое module cache не публикуются.
FAIL сохраняется, следующее действие начинает с его readback; ничего не удалять
и не снимать lock живого процесса. Lock автоматически освобождается ядром после
завершения процесса; файл lock оставляется.

`render-local.sh` использует тот же lock/download/verify/seal primitive через
`prime-render-go-cache.py`. Этот trusted local adapter сохраняет существующий
dirty-source fingerprint dev renderer и дополнительно готовит locked Air.
Он не является обходным staging entrypoint. У standalone CLI dirty source
всегда запрещён. Проверки: `make test-local-go-cache-contract`.

PASS prime означает только подготовленный cache. Затем root выполняет обычные
scoped plan/apply и проверяет запуск новой replica с read-only cache, actual
serving version и неизменность соседей. Сам prime Deployments/Pods не меняет.

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

## Сериализация worker grant при обновлении

Disposable renderer задаёт `replicas: 1` и `strategy: Recreate` всем Deployment
с `platform-worker-grant-agent`, включая native sidecars в `initContainers`.
Неизвестный workload с таким агентом закрыто отклоняется до применения.
Это сериализует штатное обновление Deployment: старый grant writer завершается
до запуска нового. Одновременные writers одного workload могут выпускать разные
revision одного поколения, и устойчивый watermark отвергает прежний grant.
Base и production strategy этим правилом не изменяются.

Перед SSA выбранной фазы `deploy-local.sh` мигрирует прежнюю strategy через
`migrate-worker-grant-strategy.sh`. API-default `rollingUpdate` может сохраняться
даже при explicit null в SSA, поэтому используется атомарная JSON Patch всего
strategy с проверками UID, resourceVersion и прежнего значения. Guard проверяет
закрытый workload registry, namespace, disposable profile и selector; readback
подтверждает ту же identity, replicas1 и неизменный Pod template. Отсутствующий
Deployment создаётся обычным SSA; уже Recreate не требует patch. Конфликт,
ошибка или неоднозначный ответ останавливают фазу без скрытого повтора.

`make test-worker-strategy-upgrade` с `KODEX_STRATEGY_TEST_IMAGE=sha256:...`
запускает cached disposable k3s API без agent, сети и работающих Pods. Проверяются
upgrade API-default и явно заданного RollingUpdate, отказ старого SSA/null,
UID/version/owner guards, replay и создание нового Deployment. Образ предварительно
должен находиться локально; pull и доступ к живому kubeconfig отсутствуют.

`Recreate` не гарантирует эту последовательность при ручном удалении Pod:
ReplicaSet может создать замену, пока удаляемый Pod ещё завершается. Используйте
штатный code-owned rollout. Подтверждённый отказ старого broker grant не доказывает,
что конкуренция writers была единственной причиной его неготовности; после
развёртывания отдельно проверяются protected owner readiness и API readiness.

## Образ интеграционного gateway

`integration-gateway` использует отдельный hot-reload образ из
`tools/dev/Dockerfile.local-integration`: Go, Git и CA закреплены версиями,
образ собирается на доверенном хосте через `build-local-integration.sh` и
передаётся renderer только как exact OCI manifest digest. Git обязателен для
startup и управляемого write-back. Код остаётся read-only mount, `/tmp` —
ограниченный scratch; package install внутри Pod не выполняется.
Проверка фактического локального образа: `make test-integration-hot-reload-container`
с `KODEX_INTEGRATION_RUNTIME_TEST_IMAGE=sha256:...`. Она проверяет Go/Git/HTTPS
helper/CA, non-root bare Git workspace и запрет записи в исходники без сети.
Эта проверка не заменяет protected gateway readiness и живой Git write-back.

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
tsh-kodex ssh kodex-teleport@kodex-dev
tsh-kodex kube login kodex-dev
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl get --raw=/readyz
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i get pods --all-namespaces
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i get secrets --all-namespaces
KUBECONFIG="$HOME/.tsh-kodex-home/.kube/config" kubectl auth can-i create clusterrolebindings
```

`teleport.kodex.works` является адресом Proxy, а `kodex-dev` — именем
зарегистрированного SSH node; эти адреса не взаимозаменяемы.

Ожидаемый результат: SSH, `/readyz` и чтение pod разрешены; чтение Secret и
создание ClusterRoleBinding запрещены. Break-glass SSH-сессию нельзя закрывать,
пока новая Teleport SSH-сессия и Kubernetes readback не подтверждены.

## Первичный запуск и проверка bootstrap

Следующие команды относятся к первичной настройке либо отдельно согласованной
infrastructure-фазе. Для текущего контура и дальнейших исправлений применяется
[scoped source workflow](../../tools/release/README.md#использование), без повторного
bootstrap, сброса состояния или обязательного Teleport cutover.

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

Dev reload client PWA читает revision отдельным запросом с timeout 3 секунды;
следующий poll запускается через 1 секунду после завершения прежнего. `beforeunload`
останавливает таймер ещё до загрузки нового HTML: WebKit уже может запрещать
fetch старого документа в этом промежутке, выдавая access-control diagnostic
независимо от Promise catch. `pagehide`
останавливает таймер и отменяет fetch/body, `pageshow` возобновляет один цикл.
Повторная установка клиента завершает предыдущий экземпляр; поздняя revision
отменённого запроса не принимается. После отмены ухода пользователем `pageshow`
не возникает: следующее настоящее нажатие клавиши/указателя возобновляет один
poll. Пока пользователь не взаимодействует, оставшийся документ сохраняет
паузу; программный synthetic event её не снимает. Listener не вызывает
`preventDefault` и сам не создаёт диалог подтверждения. Только корректная revision от exact endpoint
может инициировать один reload. При обычном outage/reject клиент продолжает
ограниченный polling; это не исключение из UI `pageerror` проверок.

Локальный public lifecycle профиль PWA:
`npx playwright test --config e2e/dev-reload.fixture.config.ts --retries 0`.
Он не использует staging и проверяет Chromium/Firefox/WebKit с observer и без,
pagehide/pageshow, native timeout и отсутствие reload loop. Для изменений
`vite.config.ts` нужно доставить новый source штатным scoped workflow PWA;
пересборка application image ради SHA не требуется. После доставки отдельно
проверяется live UI; локальный regression не заменяет приёмку нового serving
source и не переписывает исторические FAIL `/__kodex_dev_reload.js` из #1358.

`e2e` запускает только browser discovery и остаётся диагностической командой.
`acceptance` требует чистый exact SHA оснастки до и после выполнения и
проверяет browser/API, synthetic integration, сборку и допуск RoleImage,
session archive и disposable backup/restore drill. Для независимых версий
передаётся `--component-manifest`; описание capture, совместимости и serving
readback находится в [руководстве релиза](../../tools/release/README.md#приёмка-независимых-версий).
SHA оснастки не присваивается работающим соседним приложениям.

По решению владельца от 2026-09-08 Teleport cutover отложен: application
`status`/`acceptance` по умолчанию не вызывают Teleport. Отдельный
`--access-profile teleport` добавляет его readback; bootstrap `up` и команда
`teleport` сохраняют прежнюю настройку. Проверки disposable cluster identity,
TLS, provider sandbox и служебной готовности остаются обязательными.

`tools/dev/full-local-e2e.sh` принимает повторяемый `--batch` со значениями
`browser`, `integration`, `role-image`, `archive`, `backup`, `hot-reload`.
Пять прикладных batch включены по умолчанию. `hot-reload` запускается только
явным выбором. `--component-manifest` требует `--skip-build` и запрещает
`--batch hot-reload`: проверка механизма разработки имеет отдельный запуск
на согласованном disposable source и не меняет исходники соседей при приёмке.
Профили GitHub и provider API key всегда получают явный итог `PASS`, `FAIL` или
`NOT RUN`; отсутствие credentials больше не маскируется как выполненная проверка.

Hot reload проверяется без постоянного тестового endpoint. Repo-owned скрипт
временно меняет существующий ответ gateway `/healthz` и безвредный маркер в
корневом CSS, который Vite обновляет без remount приложения. Скрипт наблюдает
`204 -> 202 -> 204` и появление/исчезновение маркера в Vite-модуле, после чего
восстанавливает файлы и повторно требует чистый exact SHA. При
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

Этот destructive workflow описывает отдельное удаление disposable установки,
а не шаг ручной приёмки. Для действующего dev контура он запрещён решением выше;
само наличие команды и confirmation phrase не даёт разрешения на удаление.

```bash
KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER \
  ./tools/dev/remote-dev.sh down --env-file "$REMOTE_ENV" --expected-sha "$EXPECTED_SHA"
```

Команда удаляет application namespaces, но оставляет общие dev-контроллеры и
host-owned Teleport для диагностики. Она требует отдельную точную фразу
подтверждения disposable-среды и сверяет UID, API endpoint и CA текущего
кластера с root-owned marker. Отдельная production-установка использует чистый
хост; решение о ней не является разрешением очищать текущий dev сервер.


Дополнительный regression #1358: loopback HTTP fixture задерживает новый HTML
на1800ms. Старый emitted client даёт WebKit access-control pageerror между
началом навигации и `pagehide`, с наблюдателем запросов и без него. Новый клиент
приостанавливается на `beforeunload`; обычные CORS-отказы по-прежнему наблюдаемы.
Fixtures также проверяют настоящий отменённый beforeunload и возобновление
одного poll после trusted interaction. Это dev-профиль; обработчик beforeunload
может ограничивать browser back/forward cache. Production image-профиль не
получает dev reload script; пересборка application image ради этого исправления
не требуется. Ни реальные CORS-отказы, ни pageerror reporter не игнорируются.
