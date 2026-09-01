---
id: RUNBOOK-DOC-REMOTE-DEV-001
title: Удалённый hot-reload контур Kodex
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-01
---

# Удалённый hot-reload контур Kodex

## Назначение

Контур предназначен только для разработки и E2E на disposable bare-metal
сервере. Он устанавливает системные утилиты, Docker, одноузловой k3s, Traefik,
cert-manager, SeaweedFS, Kodex с монтированием исходников и Teleport Community
Edition. Для публичных интерфейсов используются реальные сертификаты Let's
Encrypt. Production-данные и production-секреты в контур не переносятся.

Teleport размещается в отдельном namespace того же disposable k3s. Прямой SSH
на порт `22` остаётся аварийным доступом. Traefik занимает публичные `80/443` и
маршрутизирует Teleport по отдельному DNS-имени.

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
Ошибка preflight закрыто останавливает установку и не расходует попытку ACME.

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

Участникам этой команды Teleport назначает роль `kodex-k8s-admin`, которая
отображается в Kubernetes group `system:masters`. Это допустимо только для
одноразового dev-контура и не является production-моделью доступа.

Проверены актуальные документы Teleport 18 через Context7:

- `teleport-cluster` Helm chart в `standalone`/`multiplex` режиме за Ingress;
- GitHub connector `v3` и callback path;
- Kubernetes Access и отображение роли в `system:masters`.

## Установка

Сначала выполняется read-only preflight, затем тот же code-owned entrypoint
применяет изменения:

```bash
./tools/dev/remote-dev.sh host-preflight --env-file .kodex-remote-env
./tools/dev/remote-dev.sh host-apply --env-file .kodex-remote-env
./tools/dev/remote-dev.sh host-readback --env-file .kodex-remote-env
```

После `host-apply` нужно открыть новую SSH-сессию, чтобы применилось членство
оператора в группе `docker`. Readback обязан подтвердить k3s, Docker buildx,
firewall и пользовательский kubeconfig.

## Запуск и проверка

```bash
./tools/dev/remote-dev.sh up --env-file .kodex-remote-env
./tools/dev/remote-dev.sh status --env-file .kodex-remote-env
./tools/dev/remote-dev.sh smoke --env-file .kodex-remote-env
./tools/dev/remote-dev.sh e2e --env-file .kodex-remote-env \
  --resource-prefix remote-e2e-001
```

`up` монтирует текущий worktree в Go- и Vue-workloads через `hostPath`. Air и
Vite отслеживают изменения исходников без пересборки полного release image.
Тяжёлые runtime/supply-chain образы пересобираются только при изменении их
входов и импортируются напрямую в containerd k3s.

Если OAuth App был создан после основного запуска, Teleport применяется
отдельно:

```bash
./tools/dev/remote-dev.sh teleport --env-file .kodex-remote-env
```

## Завершение

```bash
./tools/dev/remote-dev.sh down --env-file .kodex-remote-env
```

Команда удаляет application namespaces, но оставляет общие dev-контроллеры и
Teleport для диагностики. Перед production-релизом сервер очищается полностью,
после чего production-инсталлятор запускается на чистом хосте.
