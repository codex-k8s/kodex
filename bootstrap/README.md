# Bootstrap кластера

## Назначение

`bootstrap/**` задаёт один активный путь установки MVP: зайти на сервер,
подготовить минимальный `bootstrap/host/config.env` и запустить локальный
installer.

Контур поднимает и проверяет:

- k3s на текущем сервере;
- kubeconfig для `OPERATOR_USER`;
- kubelet image GC и host image prune timer;
- `/opt/kodex` как локальный snapshot репозитория для install;
- internal registry foundation;
- проверка registry mirror и Kaniko build.

## Файлы

| Путь | Назначение |
|---|---|
| `bootstrap/host/bootstrap_cluster.sh` | Единственная активная точка входа: `preflight`, `install`, `--dry-run`. |
| `bootstrap/host/plan_backend_deploy.sh` | План первого backend deploy только на чтение: инвентарь, рендер, `kubectl kustomize` и проверки кластера без изменений. |
| `bootstrap/host/deploy_backend_ring.sh` | Реальное применение backend-колец и `staff-gateway`: registry, Kubernetes `Secret`, Kaniko-сборки, PostgreSQL, миграции и сервисы выбранного кольца. |
| `bootstrap/local/install.sh` | Локальный privileged orchestrator install-шагов. |
| `bootstrap/local/steps/*.sh` | Узкие idempotent host/Kubernetes steps. |
| `bootstrap/host/smoke_registry_kaniko.sh` | Проверка registry mirror и Kaniko build/push без Docker daemon. |
| `bootstrap/host/smoke_backend_contour.sh` | Тонкая обвязка проверки над `deploy_backend_ring.sh --skip-build`. |
| `deploy/base/bootstrap-foundation/**` | Manifests внутреннего registry. |
| `deploy/base/bootstrap-builder-smoke/**` | Manifests заданий проверки mirror/Kaniko. |
| `cmd/bootstrap-preflight` | Безопасный preflight: имена env, stack inventory, dry-run рендер, kustomize и проверки Kubernetes только на чтение. |
| `cmd/bootstrap-deploy-plan` | Безопасный план backend deploy: инвентарь MVP-сервисов, PostgreSQL/event-log manifests, service manifests, builder refs и проверки foundation только на чтение. |
| `cmd/manifest-render` | Stack-aware renderer: читает `services.yaml`, затем применяет env overrides. |
| `libs/go/stackinventory` | Общая Go-библиотека чтения корневого stack inventory для renderer/install/deploy tools. |
| `libs/go/manifestrender` | Общая Go-библиотека рендера manifest templates поверх `stackinventory`. |
| `bootstrap/host/config.env.example` | Минимальный пример env для локальной установки. |

## Подготовка env

```bash
cp bootstrap/host/config.env.example bootstrap/host/config.env
```

Заполните `bootstrap/host/config.env`. Домены, адреса, email, токены, ключи,
пароли, DSN и kubeconfig считаются чувствительными: не публикуйте их в Issue,
PR, логах и документации.

Минимально важные параметры:

- `OPERATOR_USER`;
- `KODEX_PRODUCTION_NAMESPACE`;
- `KODEX_PRODUCTION_DOMAIN`;
- `KODEX_BOOTSTRAP_PUBLIC_HOST`, если DNS нужно сверять с публичным host/IP, а не с локальными адресами сервера;
- `KODEX_INTERNAL_REGISTRY_*`;
- `KODEX_SSH_PORT`, если host firewall включён;
- пустые runtime secret seeds, которые bootstrap сгенерирует при install.

Версии и дефолтные имена образов берутся из `services.yaml`. Env-переменные
вроде `KODEX_REGISTRY_IMAGE`, `KODEX_KANIKO_EXECUTOR_IMAGE` и
`KODEX_IMAGE_MIRROR_TOOL_IMAGE` являются override-слоем, а не вторым источником
версий.

Правило defaults:

- корневой `services.yaml` задаёт версии, образы, deploy inventory и стандартные имена артефактов платформенного стека;
- это не проектный `services.yaml`: пользовательской project policy, импортом и проверенной проекцией владеет `project-catalog`;
- Go-инструменты читают корневой stack inventory через `libs/go/stackinventory`, а не через собственный YAML/awk/parser слой;
- Go config сервиса задаёт безопасные runtime defaults самого сервиса;
- Kubernetes templates не повторяют runtime defaults сервиса как `envOr`, если сервис уже имеет такой default;
- `bootstrap/host/config.env.example` хранит только локальные install-настройки и secret/bootstrap seed-поля.

## Preflight и dry-run

```bash
bash bootstrap/host/bootstrap_cluster.sh preflight --env-file bootstrap/host/config.env
```

План без запуска install-шагов:

```bash
bash bootstrap/host/bootstrap_cluster.sh install --env-file bootstrap/host/config.env --dry-run
```

Preflight проверяет ОС, root/sudo, базовые команды, k3s/kubectl при наличии,
DNS prerequisite, наличие bootstrap manifests и обязательные env-поля. Проверка
не печатает значения env, домены или адреса.

Если `go` доступен, preflight дополнительно запускает `cmd/bootstrap-preflight`.
Этот шаг:

- загружает `bootstrap/host/config.env`, но печатает только имена проверок, без значений;
- читает root `services.yaml` через `libs/go/stackinventory`;
- разрешает registry/Kaniko/crane/busybox image refs через stack inventory и env override-слой;
- рендерит `bootstrap-foundation` и `bootstrap-builder-smoke` через `libs/go/manifestrender`;
- выполняет `kubectl kustomize`, если `kubectl` доступен;
- выполняет проверки Kubernetes только на чтение: context, `/readyz`, namespace, `kodex-registry` Deployment/Service.

Если `go` ещё не установлен на чистом host, shell preflight фиксирует deferred
status: `00_prepare_host.sh` установит Go перед install-шагами, а dry-run
render можно повторить после подготовки host.

Для строгой проверки уже установленного кластера:

```bash
bash bootstrap/host/bootstrap_cluster.sh preflight \
  --env-file bootstrap/host/config.env \
  --require-kubernetes
```

Без `--require-kubernetes` отсутствующий `kubectl`, namespace или registry
фиксируется как deferred check: install или foundation smoke подготовят их позже.

## Установка

Установка запускается только на сервере, где должен жить Kubernetes:

```bash
bash bootstrap/host/bootstrap_cluster.sh install --env-file bootstrap/host/config.env
```

Install выполняет шаги:

1. preflight;
2. подготовка ОС и системных пакетов;
3. создание `OPERATOR_USER`;
4. установка или проверка k3s;
5. настройка `/etc/rancher/k3s/registries.yaml` на internal registry;
6. настройка kubelet image GC и host image prune timer;
7. проверка network prerequisites без установки ingress/cert-manager;
8. доставка snapshot репозитория в `/opt/kodex`;
9. подготовка runtime env без печати секретов;
10. render/apply internal registry foundation;
11. включение host firewall baseline;
12. итоговый отчёт.

Архив репозитория исключает `.git`, `.local` и `bootstrap/host/*.env`; runtime
env передаётся отдельно.

## Dry-run план backend deploy

После preflight, до любого реального `kubectl apply`, запуска jobs или сборки
образов, оператор строит план первого backend deploy только на чтение:

```bash
bash bootstrap/host/plan_backend_deploy.sh --env-file bootstrap/host/config.env
```

Команда не меняет кластер. Она:

- читает `services.yaml` через `libs/go/stackinventory`;
- проверяет deploy inventory MVP-сервисов, Dockerfile, service/migration
  manifests и зависимости по именам сервисов;
- разрешает image refs через stack inventory и env override-слой, но не печатает
  значения registry, доменов или secret env;
- рендерит `deploy/base/postgres/**`,
  `deploy/base/platform-event-log/migrations.yaml.tpl`,
  `deploy/base/bootstrap-foundation/**`,
  `deploy/base/bootstrap-builder-smoke/**` и все текущие deployable service
  manifests;
- выполняет `kubectl kustomize` для отрендеренных manifest sets, если доступен
  `kubectl`;
- выполняет проверки текущего Kubernetes foundation только на чтение: context,
  `/readyz`, namespace, registry Deployment/Service, PostgreSQL
  StatefulSet/Service и runtime Secret refs.

Для проверки только render/inventory без чтения Kubernetes:

```bash
bash bootstrap/host/plan_backend_deploy.sh \
  --env-file bootstrap/host/config.env \
  --skip-live-kubernetes
```

Для уже установленного foundation-контура можно включить строгий режим:

```bash
bash bootstrap/host/plan_backend_deploy.sh \
  --env-file bootstrap/host/config.env \
  --require-kubernetes
```

Если нужен каталог с отрендеренными файлами для ручной проверки, передайте
пустой `--render-dir`. Непустой каталог отклоняется; команда не удаляет пути,
переданные оператором.

## Реальный deploy backend-колец и staff-gateway

После успешного preflight и dry-run плана оператор может применить backend-кольцо.
Без `--ring` используется первое кольцо, чтобы существующий путь не менял
поведение:

```bash
bash bootstrap/host/deploy_backend_ring.sh --env-file bootstrap/host/config.env
```

Второе кольцо запускается явно:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring second
```

Для новой установки, где нужно последовательно применить оба кольца одной
командой, используется `all`. Этот режим не включает `staff-gateway`:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring all
```

После успешного второго кольца `staff-gateway` запускается отдельным контуром:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring staff
```

Состав колец:

- `first`: `access-manager`, `project-catalog`, `package-hub`, `provider-hub`;
- `second`: `fleet-manager`, `runtime-manager`, `interaction-hub`,
  `governance-manager`, `agent-manager`, `integration-gateway`,
  `codex-hook-ingress`.
- `staff`: `staff-gateway`.

`staff-gateway` не входит в `all`, чтобы повторный backend deploy не менял
edge-контур без явного выбора оператора.

Команда выполняет изменения в Kubernetes и остаётся идемпотентной:

- создаёт namespace через `kubectl apply`;
- применяет internal registry foundation и ждёт readiness;
- читает существующие `kodex-postgres` и `kodex-platform-runtime` `Secret`;
- генерирует только отсутствующие ключи `Secret` и сохраняет их в Kubernetes до
  запуска workloads;
- не перегенерирует уже существующие значения `Secret`;
- нормализует локальные PostgreSQL DSN, если они не соответствуют текущему
  паролю из `kodex-postgres`;
- рендерит manifests во временный приватный каталог и удаляет его после работы;
- собирает через Kaniko образы выбранного кольца и migration-образы без Docker
  daemon;
- применяет PostgreSQL foundation, создаёт базы через
  `kodex-postgres-bootstrap-databases`;
- запускает `platform-event-log` migrations;
- запускает migrations и deployments для сервисов выбранного кольца;
- ждёт rollout и проверяет `/health/readyz` через локальный `kubectl
  port-forward`.

Секреты, DSN, токены, домены, адреса registry и kubeconfig не печатаются.
Повторный запуск удаляет только одноразовые build/migration `Job`, чтобы
Kubernetes не упёрся в immutable job spec, и не удаляет кластер, namespace,
registry PVC, PostgreSQL PVC или базы.

Если образы уже собраны и нужно только повторить apply/migrations/rollout для
выбранного кольца:

```bash
bash bootstrap/host/deploy_backend_ring.sh \
  --env-file bootstrap/host/config.env \
  --ring second \
  --skip-build
```

## Проверка registry и Kaniko

После установки foundation:

```bash
KODEX_SMOKE_ENV_FILE=bootstrap/host/config.env \
  bash bootstrap/host/smoke_registry_kaniko.sh
```

Скрипт:

- рендерит `deploy/base/bootstrap-foundation/**` и `deploy/base/bootstrap-builder-smoke/**`;
- ждёт readiness `kodex-registry`;
- зеркалирует внешний тестовый образ во внутренний registry через `crane`;
- запускает pull-smoke из внутреннего registry;
- собирает минимальный scratch-образ через Kaniko и пушит его во внутренний registry.

Docker daemon не требуется.

## Проверка backend после deploy

Когда первое backend-кольцо применено, можно запускать обвязку проверки. Она
повторяет идемпотентный deploy первого кольца с `--skip-build`: проверяет
registry, Kubernetes `Secret`, PostgreSQL, migrations, rollout и
`/health/readyz`, не перезаписывая секреты значениями из env.

```bash
KODEX_SMOKE_ENV_FILE=bootstrap/host/config.env \
  bash bootstrap/host/smoke_backend_contour.sh
```

Эта обвязка проверяет только первое кольцо. Второе кольцо проверяется через
`deploy_backend_ring.sh --ring second` и readiness, migration jobs, rollout
status. `staff-gateway` проверяется через `deploy_backend_ring.sh --ring staff`,
readiness и rollout status. Frontend в эти контуры не входит.

Доменные и end-to-end проверки не добавляются как shell smoke scripts в
`scripts/**`. Их целевой формат — Go tests или отдельный Go integration runner;
shell остаётся только тонкой обвязкой установки, deploy или запуска Go tooling.

Чтобы проверить backend wrapper без запуска проверки registry и deploy:

```bash
KODEX_BACKEND_SMOKE_DRY_RUN=true \
KODEX_SMOKE_ENV_FILE=bootstrap/host/config.env \
  bash bootstrap/host/smoke_backend_contour.sh
```

Этот режим вызывает план backend deploy и завершает работу до любых изменений
кластера.
