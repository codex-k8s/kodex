---
id: RUN-MC-023
title: Identity и административные интерфейсы
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-24
---

# Identity и административные интерфейсы

Runbook задаёт воспроизводимую установку Keycloak, OAuth2 Proxy, Grafana,
Vault UI и Headlamp. Он применяется только после read-only preflight и
отдельного разрешения владельца на целевой Kubernetes context.

## 1. Граница доступа

| Интерфейс      | Публичный host        | Внешний шлюз                                                | Дополнительная authority                         |
| -------------- | --------------------- | ----------------------------------------------------------- | ------------------------------------------------ |
| Control Center | installation variable | OAuth2 Proxy, realm `mattercodex`, role `mattercodex-owner` | собственный OIDC/PKCE и API authorization        |
| Grafana        | installation variable | OAuth2 Proxy, realm `mattercodex`, role `mattercodex-owner` | доверенный auth-proxy header только от ingress   |
| Vault UI       | installation variable | OAuth2 Proxy, realm `mattercodex`, role `mattercodex-owner` | нативный Vault OIDC и policy `mattercodex-owner` |
| Headlamp       | installation variable | OAuth2 Proxy, Keycloak `master` realm, role `admin`         | отдельный ServiceAccount с `cluster-admin`       |

Headlamp намеренно использует общий административный ServiceAccount. Это
допустимо только потому, что его единственный ingress закрыт OAuth2 Proxy и
пропускает исключительно Keycloak administrators с realm-role `admin`.
Назначение этой роли означает полный доступ к Kubernetes. Обычный owner или
пользователь realm `mattercodex` не получает доступ к Headlamp автоматически.

Keycloak login и OIDC endpoints не закрываются его же OAuth2 Proxy: это создало
бы циклическую зависимость. Keycloak Admin Console использует собственную
аутентификацию Keycloak. Публичные Prometheus и Alertmanager ingress не
создаются.

## 2. GitHub environment `production`

В Environment variables создать:

| Variable                                   | Пример формы      | Назначение                                       |
| ------------------------------------------ | ----------------- | ------------------------------------------------ |
| `MATTERCODEX_KEYCLOAK_ADMIN_USERNAME`      | непустой username | постоянный администратор Keycloak `master` realm |
| `MATTERCODEX_OWNER_USERNAME`               | непустой username | первый владелец MatterCodex                      |
| `MATTERCODEX_OWNER_EMAIL`                  | email             | email первого владельца                          |
| `MATTERCODEX_GRAFANA_HOST`                 | DNS без схемы     | публичный Grafana host                           |
| `MATTERCODEX_VAULT_HOST`                   | DNS без схемы     | публичный Vault UI host                          |
| `MATTERCODEX_HEADLAMP_HOST`                | DNS без схемы     | публичный Headlamp host                          |
| `MATTERCODEX_PUBLIC_IPV4_CIDR`             | один IPv4 `/32`   | точный публичный egress к Keycloak               |
| `MATTERCODEX_VAULT_RECOVERY_AGE_RECIPIENT` | `age1...`         | публичный recipient owner recovery key           |

Повторно используются существующие deployment variables:
`MATTERCODEX_PUBLIC_HOST`, `MATTERCODEX_PUBLIC_ORIGIN`,
`MATTERCODEX_OIDC_ISSUER`, `MATTERCODEX_INGRESS_CLASS`,
`MATTERCODEX_CLUSTER_ISSUER`, `MATTERCODEX_INGRESS_NAMESPACE`,
`MATTERCODEX_INGRESS_POD_NAME`, `MATTERCODEX_KUBERNETES_API_SERVICE_CIDR`,
`MATTERCODEX_KUBERNETES_API_ENDPOINT_CIDRS` и
`MATTERCODEX_KUBERNETES_API_ENDPOINT_PORTS`.

В Environment secrets создать:

| Secret                                        | Назначение                                                         |
| --------------------------------------------- | ------------------------------------------------------------------ |
| `MATTERCODEX_KEYCLOAK_ADMIN_INITIAL_PASSWORD` | начальный пароль постоянного администратора, не короче 20 символов |
| `MATTERCODEX_OWNER_INITIAL_PASSWORD`          | начальный пароль owner, не короче 20 символов                      |

Значения не передаются аргументами CLI и не печатаются. Временный Keycloak
bootstrap administrator создаётся генератором случайно, используется только
для создания постоянного admin service client и удаляется в той же
reconciliation. После первой успешной авторизации постоянный administrator и
owner меняют начальные пароли. Затем initial password secrets удаляются из
GitHub Environment, а `Secret/identity/keycloak-initial-passwords` удаляется
из кластера командой `configure-keycloak.sh --mode retire-initial-passwords`
с теми же параметрами origins. Этот режим сначала выполняет полный readback и
только затем удаляет Secret. Перед следующей чистой установкой владелец задаёт
новые известные ему начальные пароли. Повторный reconciliation не меняет пароль
существующего пользователя и не требует удалённый initial-password Secret.

## 3. Owner recovery key

Приватный ключ `age` нельзя хранить в GitHub, Kubernetes, Vault этой же
установки или репозитории. Владелец создаёт его на доверенном узле:

```bash
install -d -m 0700 /var/lib/mattercodex-owner/recovery
age-keygen -o /var/lib/mattercodex-owner/recovery/age-key.txt
chmod 0600 /var/lib/mattercodex-owner/recovery/age-key.txt
age-keygen -y /var/lib/mattercodex-owner/recovery/age-key.txt \
  > /var/lib/mattercodex-owner/recovery/age-recipient.txt
chmod 0600 /var/lib/mattercodex-owner/recovery/age-recipient.txt
```

В GitHub variable записывается только строка из `age-recipient.txt`. Приватный
ключ сохраняется минимум в двух раздельных owner-controlled offline местах.
Потеря приватного ключа делает зашифрованные installation/recovery bundles
невосстановимыми.

## 4. Identity material

Workflow `.github/workflows/prepare-installation-identity.yml` запускается для
exact source SHA после approval Environment `production`. Он генерирует
machine secrets, зашифровывает полный identity bundle публичным `age` recipient
и публикует artifact на один день. Открытые значения в artifact отсутствуют.

На доверенном узле encrypted artifact сохраняется вместе с checksum в двух
owner-controlled backup locations. Расшифровка выполняется только в `tmpfs`:

```bash
install -d -m 0700 /run/mattercodex-installation-identity
tools/deploy/import-identity-material.sh \
  --material-directory /run/mattercodex-installation-identity \
  --age-identity-file /var/lib/mattercodex-owner/recovery/age-key.txt \
  --bundle-file /path/to/mattercodex-identity-<exact-sha>.tar.age \
  --checksum-file /path/to/mattercodex-identity-<exact-sha>.tar.age.sha256
```

Скрипт принимает только закрытый список ожидаемых regular files и отказывается
перезаписывать существующий material. После этого Kubernetes Secrets создаются
одной code-first командой:

```bash
tools/deploy/materialize-identity-secrets.sh \
  --context <exact-context> \
  --material-directory /run/mattercodex-installation-identity
```

После Keycloak и management readback plaintext удаляется code-first командой:

```bash
tools/deploy/destroy-plaintext-identity-material.sh \
  --material-directory /run/mattercodex-installation-identity
```

Скрипт принимает только закрытый набор ожидаемых файлов и только путь в `/run`
или `/dev/shm`. Перезапись перед удалением является best effort для tmpfs и не
подменяет encrypted backups.

## 5. Keycloak

Сначала выполнить `preflight`, затем `apply` и `readback`:

```bash
infra/identity/bootstrap.sh \
  --context <exact-context> --mode preflight \
  --oidc-host <sso-host> --ingress-class <class> \
  --cluster-issuer <issuer> --ingress-namespace <namespace> \
  --ingress-pod-name <label>
```

Те же параметры используются для `apply` и `readback`. Затем применяется
realm/client reconciliation:

```bash
tools/deploy/configure-keycloak.sh \
  --context <exact-context> --mode apply \
  --public-origin https://<control-center-host> \
  --grafana-origin https://<grafana-host> \
  --vault-origin https://<vault-host> \
  --headlamp-origin https://<headlamp-host>
```

Команда создаёт отдельные confidential clients и secrets для каждой
поверхности. Постоянный administrator создаётся в `master` realm с ролью
`admin`, а временный bootstrap administrator удаляется; owner создаётся в
realm `mattercodex`. Повторить команду с `--mode readback`.
Keycloak обращается к своему PostgreSQL только по TLS 1.3 с `verify-full` и
installation-owned identity CA; Traefik также подключается к Keycloak по HTTPS
с exact SNI публичного OIDC host. Keycloak bind-ит основной и management
listener к Pod interface, иначе Kubernetes Service и probes недоступны.
Внутренний HTTP на `8080` нужен только для code-first reconciliation внутри
того же Pod: Service не публикует этот порт, а deny-all/allowlist
`NetworkPolicy` не разрешает к нему ingress из других Pod.
При ротации database certificate bootstrap переносит `resourceVersion` TLS
Secret в Pod template, выполняет rollout PostgreSQL и проверяет exact revision.

## 6. Vault recovery

Новая установка Vault инициализируется Shamir-схемой `5/3`: создаются пять
unseal shares, для распечатывания нужны любые три. Портативный OSS-профиль не
зависит от внешнего KMS и поэтому после рестарта требует ручного unseal.

После завершения Vault bootstrap и OIDC configuration plaintext material сразу
запечатывается:

```bash
tools/deploy/seal-vault-recovery-material.sh \
  --material-directory /var/lib/mattercodex-owner/material \
  --age-recipient-file /var/lib/mattercodex-owner/recovery/age-recipient.txt \
  --output-file /var/lib/mattercodex-owner/recovery/vault-recovery.tar.age
```

Скрипт удаляет plaintext root token и все shares только после успешного
создания encrypted bundle и checksum. Bundle также сохраняется минимум в двух
раздельных offline местах. Для планового unseal material временно
восстанавливается командой `restore-vault-recovery-material.sh` с обязательным
`--checksum-file`, используется тремя stdin-backed операциями и снова
запечатывается. Значения не выводятся.

Vault UI OIDC настраивается после unseal:

```bash
tools/deploy/configure-vault-oidc.sh \
  --context <exact-context> --mode apply \
  --material-directory /var/lib/mattercodex-owner/material \
  --oidc-issuer https://<sso-host>/realms/mattercodex \
  --vault-public-origin https://<vault-host>
```

## 7. Grafana, OAuth2 Proxy и Headlamp

Pinned charts и их SHA-256 зафиксированы в
`infra/management-surfaces/charts.lock.json`. Сначала выполнить `preflight`,
затем `apply-monitoring`, `apply-surfaces` и `readback` одним exact набором
параметров:

```bash
infra/management-surfaces/bootstrap.sh \
  --context <exact-context> --mode preflight \
  --oidc-issuer https://<sso-host>/realms/mattercodex \
  --control-center-host <control-center-host> \
  --grafana-host <grafana-host> --vault-host <vault-host> \
  --headlamp-host <headlamp-host> --public-ipv4-cidr <public-ip>/32 \
  --ingress-class <class> --cluster-issuer <issuer> \
  --ingress-namespace <namespace> --ingress-pod-name <label> \
  --kubernetes-api-service-cidr <service-ip>/32 \
  --kubernetes-api-endpoint-cidrs <node-ip>/32 \
  --kubernetes-api-endpoint-ports 6443
```

Headlamp OAuth2 Proxy автоматически использует issuer `master` и роль `admin`;
остальные поверхности используют переданный realm `mattercodex`. Повторный
запуск идемпотентен и не генерирует новые secrets.

## 8. Проверка и восстановление

1. Без cookie каждый из четырёх UI перенаправляет в Keycloak.
2. Owner открывает Control Center, Grafana и Vault UI, но не Headlamp, если не
   имеет Keycloak `master/admin`.
3. Keycloak administrator открывает Headlamp и `kubectl auth can-i` readback
   подтверждает `cluster-admin` у выделенного ServiceAccount.
4. Прямые backend Services не имеют публичного ingress без OAuth2 middleware.
5. Prometheus и Alertmanager не опубликованы наружу.
6. После рестарта sealed Vault не считается ready; владелец восстанавливает
   bundle, использует три shares и снова удаляет plaintext material.

При потере только Pod/PVC identity восстанавливается из Keycloak PostgreSQL
backup. При полной потере установки нужны database backups, encrypted identity
bundle, encrypted Vault recovery bundle и приватный `age` key. Root token не
является ежедневной учётной записью: после настройки используется OIDC.

## 9. Официальные источники

- [Keycloak: bootstrap и recovery администратора](https://www.keycloak.org/server/bootstrap-admin-recovery);
- [OAuth2 Proxy: Keycloak OIDC и role authorization](https://oauth2-proxy.github.io/oauth2-proxy/configuration/providers/keycloak_oidc/);
- [Grafana: auth proxy](https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-authentication/auth-proxy/);
- [Headlamp: in-cluster installation и RBAC](https://headlamp.dev/docs/latest/installation/).
