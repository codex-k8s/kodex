# Bootstrap кластера

## Назначение

`bootstrap/**` задаёт воспроизводимый путь подготовки первого Kubernetes-контура `kodex` без Docker daemon:

- локальный режим: выполнение на сервере, где будет жить k3s;
- удалённый режим: доставка bootstrap bundle через `TARGET_*` и запуск по SSH;
- preflight/dry-run без раскрытия значений env;
- установка и проверка k3s, kubeconfig, image GC и host firewall;
- foundation для внутреннего registry и smoke-проверки mirror/Kaniko;
- инструкции для последующего deploy backend-сервисов через готовые smoke scripts.

Реальная установка кластера запускается только отдельной командой владельца. PR с изменениями bootstrap не должен сам выполнять install.

## Файлы

| Путь | Назначение |
|---|---|
| `bootstrap/host/bootstrap_cluster.sh` | Основной entrypoint: `preflight` и `install`, режимы `local`/`remote`, `--dry-run`. |
| `bootstrap/host/bootstrap_remote_production.sh` | Совместимый wrapper для удалённого install. |
| `bootstrap/host/smoke_registry_kaniko.sh` | Проверяет registry mirror и Kaniko build/push без Docker daemon. |
| `bootstrap/host/smoke_backend_contour.sh` | Последовательно запускает registry/Kaniko smoke и smoke готовых backend-сервисов. |
| `bootstrap/remote/*.sh` | Идемпотентные шаги, которые выполняются на целевом сервере. |
| `deploy/base/bootstrap-foundation/**` | Активные manifests внутреннего registry. |
| `deploy/base/bootstrap-builder-smoke/**` | Активные manifests mirror/Kaniko smoke jobs. |
| `bootstrap/host/config.env.example` | Пример локального env. Реальный `config.env` не коммитится и не печатается. |

## Подготовка env

```bash
cp bootstrap/host/config.env.example bootstrap/host/config.env
```

Заполните `bootstrap/host/config.env`. Значения `TARGET_*`, домены, адреса, email, токены, ключи и kubeconfig считаются чувствительными: не публикуйте их в Issue, PR, логах и документации.

Минимально важные группы параметров:

- `TARGET_*` и `OPERATOR_*` для удалённого режима;
- `KODEX_PRODUCTION_NAMESPACE`;
- `KODEX_PRODUCTION_DOMAIN` и `KODEX_INGRESS_HOST_NETWORK` как входные предпосылки будущего ingress-контура;
- `KODEX_INTERNAL_REGISTRY_*`;
- `KODEX_KANIKO_*` и `KODEX_IMAGE_MIRROR_*`;
- токены и секреты сервисов-владельцев, если они не должны генерироваться bootstrap-скриптом.

Если runtime token, PostgreSQL password или DSN оставлены пустыми, `bootstrap/remote/45_prepare_runtime_env.sh` генерирует или выводит безопасные значения на целевом сервере и дописывает их в переданный bootstrap env без печати секретов.

## Preflight и dry-run

Локальная проверка без установки:

```bash
bash bootstrap/host/bootstrap_cluster.sh preflight --mode local --env-file bootstrap/host/config.env
```

Удалённая проверка через SSH:

```bash
bash bootstrap/host/bootstrap_cluster.sh preflight --mode remote --env-file bootstrap/host/config.env
```

План без запуска install-шагов:

```bash
bash bootstrap/host/bootstrap_cluster.sh install --mode remote --env-file bootstrap/host/config.env --dry-run
```

В режиме `local` dry-run выполняет preflight на текущем сервере и печатает план. В режиме `remote` dry-run доставляет только preflight bundle через `TARGET_*`, выполняет target-side preflight по SSH и печатает план без запуска install, k3s, firewall или registry-шагов. Если `--skip-ssh` указан явно, remote preflight проверяет только локальную конфигурацию `TARGET_*` и не подтверждает состояние target.

Preflight проверяет ОС, root/sudo, базовые команды, k3s/kubectl при наличии, DNS/ingress prerequisites, registry/Kaniko manifests и обязательные env-поля. DNS prerequisite для `KODEX_PRODUCTION_DOMAIN` требует, чтобы production domain резолвился в `TARGET_HOST` или, в local mode без `TARGET_HOST`, в текущий host. Проверка не печатает значения env, домены или адреса.

## Установка после merge

Локальный режим на сервере:

```bash
bash bootstrap/host/bootstrap_cluster.sh install --mode local --env-file bootstrap/host/config.env
```

Удалённый режим через `TARGET_*`:

```bash
bash bootstrap/host/bootstrap_cluster.sh install --mode remote --env-file bootstrap/host/config.env
```

Install выполняет шаги:

1. preflight;
2. подготовка ОС и системных пакетов;
3. создание operator user;
4. установка или проверка k3s;
5. настройка `/etc/rancher/k3s/registries.yaml` на internal registry;
6. настройка kubelet image GC и host image prune timer;
7. проверка network prerequisites без установки ingress/cert-manager;
8. доставка snapshot репозитория в `/opt/kodex`;
9. подготовка runtime env без печати секретов;
10. применение internal registry foundation;
11. включение host firewall baseline;
12. итоговый отчёт с командами проверки.

Архив репозитория исключает `.git`, `.local` и `bootstrap/host/*.env`; runtime env передаётся отдельно.

## Registry и Kaniko smoke

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

## Backend smoke после подготовки образов

Когда backend-образы и migration-образы уже доступны во внутреннем registry:

```bash
KODEX_SMOKE_ENV_FILE=bootstrap/host/config.env \
  bash bootstrap/host/smoke_backend_contour.sh
```

По умолчанию запускаются smoke scripts для готового backend-контура: `access-manager`, `project-catalog`, `package-hub`, `provider-hub`, `fleet-manager`, `runtime-manager`, `codex-hook-ingress`, `integration-gateway`.

Можно ограничить набор:

```bash
KODEX_BACKEND_SMOKE_SERVICES="access-manager project-catalog" \
KODEX_SMOKE_ENV_FILE=bootstrap/host/config.env \
  bash bootstrap/host/smoke_backend_contour.sh
```

Frontend в этом bootstrap-срезе не разворачивается.

## Границы среза

- Registry в MVP-профиле работает без auth и доступен на node loopback через `hostPort` `127.0.0.1:<KODEX_INTERNAL_REGISTRY_PORT>`.
- Профиль рассчитан на single-node k3s. Для multi-node нужен отдельный registry profile.
- Ingress controller, cert-manager, frontend, full runtime image build orchestration и физический deploy pipeline не добавлены этим срезом.
- `bootstrap/**` не хранит и не печатает secret values; raw env находится только в локальном/целевом bootstrap env.
