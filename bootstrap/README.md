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
- registry mirror и Kaniko build smoke.

## Файлы

| Путь | Назначение |
|---|---|
| `bootstrap/host/bootstrap_cluster.sh` | Единственный активный entrypoint: `preflight`, `install`, `--dry-run`. |
| `bootstrap/local/install.sh` | Локальный privileged orchestrator install-шагов. |
| `bootstrap/local/steps/*.sh` | Узкие idempotent host/Kubernetes steps. |
| `bootstrap/host/smoke_registry_kaniko.sh` | Registry mirror + Kaniko build/push smoke без Docker daemon. |
| `bootstrap/host/smoke_backend_contour.sh` | Backend smoke после подготовки образов сервисов. |
| `deploy/base/bootstrap-foundation/**` | Manifests внутреннего registry. |
| `deploy/base/bootstrap-builder-smoke/**` | Manifests mirror/Kaniko smoke jobs. |
| `cmd/manifest-render` | Stack-aware renderer: читает `services.yaml`, затем применяет env overrides. |
| `libs/go/stackinventory` | Общая Go-библиотека чтения корневого stack inventory для renderer/install/deploy tools. |
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

Frontend, business services deploy и full runtime build orchestration не входят
в этот bootstrap foundation-срез.
