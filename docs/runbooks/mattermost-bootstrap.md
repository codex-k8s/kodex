# Mattermost Bootstrap Runbook

## Назначение

Этот runbook описывает первый кодовый этап: подготовить и проверить Kubernetes manifests для single-server Mattermost в одном Mattermost runtime namespace. Имя namespace берется из `MATTERCODEX_NAMESPACE` или `PRODUCTION_NAMESPACE`.

Скрипты читают `.env`, но не печатают значения. Значения секретов не рендерятся в manifest-файлы.

По умолчанию Ingress рендерится для публичного Traefik class `kodex-public`. Если целевой кластер использует другой class, задайте `MATTERCODEX_INGRESS_CLASS` в `.env`.

## Preflight

Локальная проверка `.env`:

```bash
bash scripts/env/check-env.sh --env-file .env
```

Проверка Kubernetes выполняется на целевом сервере по SSH:

```bash
bash scripts/remote/k8s-preflight.sh --env-file .env
```

## Render без секретов

```bash
bash scripts/k8s/render-mattermost.sh --env-file .env --render-dir /tmp/matter-codex-render
```

В render directory попадают:

- namespace;
- PostgreSQL StatefulSet/Service;
- Mattermost PVC/Deployment/Service;
- Ingress.

PostgreSQL password и DSN создаются только Kubernetes Secret step-ом.

## Remote dry-run Kubernetes

Все Kubernetes-команды ниже выполняются на целевом сервере через SSH. Локальный `kubectl` для этого пути не нужен.

Foundation:

```bash
bash scripts/remote/install-foundation.sh --env-file .env --dry-run=server
```

Mattermost manifests:

```bash
bash scripts/remote/install-mattermost.sh --env-file .env --dry-run=server
```

Если cert-manager уже установлен и нужно создать `ClusterIssuer`, передайте:

```bash
MATTERCODEX_CREATE_CLUSTER_ISSUER=true \
  bash scripts/remote/install-foundation.sh --env-file .env --dry-run=server
```

Если namespace еще не создан, `install-foundation.sh --dry-run=server` проверяет namespace через remote server dry-run, а namespaced Secret через remote client dry-run. После реального `install-foundation.sh --apply` namespaced manifests смогут проходить server dry-run полностью.

## Реальный install

Мутирующие команды требуют явный `--apply`:

```bash
bash scripts/remote/install-foundation.sh --env-file .env --apply
bash scripts/remote/install-mattermost.sh --env-file .env --apply --wait
```

Если secret `${MATTERCODEX_POSTGRES_SECRET}` уже существует, `install-foundation.sh --apply` не ротирует пароль.

## Read-only smoke

```bash
bash scripts/remote/smoke-mattermost.sh --env-file .env
```

Проверка публичного endpoint:

```bash
bash scripts/remote/smoke-mattermost.sh --env-file .env --check-url
```

## Ручная проверка владельцем

1. Открыть Mattermost URL из `PUBLIC_BASE_URL` или `MATTERCODEX_MATTERMOST_SITE_URL`.
2. Создать первого пользователя или войти существующим администратором.
3. Убедиться, что `/api/v4/system/ping` отвечает через HTTPS.

## Безопасность

- `.env` не коммитится.
- Render manifests не содержат PostgreSQL password и Mattermost datasource.
- Bootstrap secret создается через Kubernetes API на целевом сервере без вывода значений.
- `MATTERCODEX_POSTGRES_PASSWORD` можно задать заранее, но для MVP допустима генерация при первом `--apply`.
