---
id: GUIDE-DOC-LOCAL-DEV-001
title: Локальный запуск Kodex
type: guide
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-16
---

# Локальный запуск

Точные параметры доступны через `./dev.sh identity --help` и `--help`
соответствующего helper. Перед запуском проверяются локальный kubeconfig,
принадлежность существующих ресурсов и приватный каталог состояния.
Удалённый hot reload описан отдельно в `docs/runbooks/remote-hot-reload.md`.

## Отдельный этап SSO

Из корня клона после безопасной загрузки `KODEX_LOCAL_OWNER_USERNAME`,
`KODEX_LOCAL_OWNER_EMAIL` и `KODEX_LOCAL_OWNER_PASSWORD`:

```bash
./dev.sh identity \
  --kubeconfig /home/s/.kube/kodex-dev-local \
  --context default \
  --state-directory /home/s/.local/state/kodex-local-trusted \
  --management-surfaces control-center
```

Этап использует штатные bootstrap, генерацию материалов и настройку
Keycloak. Он не требует provider authorization или сборки runner, не
разворачивает прикладные сервисы и не доказывает вход через Control Center.
Результат этого этапа нельзя обозначать как успешную приёмку приложения.

`--management-surfaces control-center` исключает чтение и запись Secrets
Grafana/Headlamp и настройку их OIDC clients. Режим `all` сохранён для
полного контура; его нельзя применять к общим ресурсам без проверки
принадлежности. Параметр поддерживается также командами
`materialize-identity-secrets.sh` и `configure-keycloak.sh`.

После успешной генерации файлов сохраняется отдельный
`generated-material-contract-revision.json`. Он разрешает продолжить
bootstrap с теми же материалами, но не означает завершения настройки
NATS или успешного развёртывания. Несовпадение контракта не приводит к
удалению namespace или материалов: требуется отдельное решение о восстановлении.

## Исходники hot reload

`render-local.sh --security-profile trusted-cluster --host-uid … --host-gid …`
проверяет non-root identity и read-only source mounts. Helper
`local_hot_reload.py prepare-source-mask` создаёт только отсутствующие
`.agents`/`.kodex-dev`, проверяет существующие `.git` и `.env*` без чтения
содержимого и отклоняет симлинки. Права существующих каталогов не меняются.
Кэш должен находиться вне клона. Успешный render не заменяет проверку
фактических Pod, владельца cache marker и hot reload в браузере.
