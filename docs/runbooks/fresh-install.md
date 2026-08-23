---
id: RUN-MC-002
title: Чистое развертывание web-first MatterCodex
type: runbook
status: approved
owner: sre
version: 1.2.1
updated: 2026-08-23
---

# Чистое развертывание web-first MatterCodex

Эта инструкция предназначена только для новой установки без данных, которые
нужно сохранить. Она разрушительна. Исполнитель сначала подтверждает точный
cluster context и получает отдельное решение владельца. Implementation agent
не выполняет эти команды самостоятельно.

## 1. Preflight

1. Зафиксировать exact release SHA и выбранный профиль: `web-only` либо
   `web-with-mattermost`.
2. Убедиться, что Kubernetes context относится к новой disposable/target
   установке, а не к иной staging/production среде.
3. Проверить отсутствие требуемых пользовательских данных и активных Runs.
4. Сохранить только необходимые external secret references и OIDC/registry
   metadata без вывода значений.
5. Отрендерить manifest в файл и проверить его до применения.

## 2. Удаляемый scope reset

Целевой application namespace — `mattercodex-system`. При полном reset удаляется
именно этот namespace и принадлежащие установке cluster-scoped resources,
перечисленные в отрендеренном release manifest. Нельзя использовать wildcard,
`$HOME`, корень workspace или удалять namespace, не совпавший с preflight.

PostgreSQL database/schema и NATS stream этой установки пересоздаются пустыми.
Legacy databases, Mattermost database, старые migration jobs и compatibility
tables не импортируются. OCI registry очищается только от unreferenced artifacts
этой installation policy; promoted role images можно сохранить, если их
provenance/signature/readback доступны новой установке.

## 3. Secret material

Secret values создаются заново owner-controlled materialization tooling. В
terminal, CI output, manifest, Issue и PR выводятся только имена ресурсов/keys и
результат shape/readback. Не копировать старые application grants: authority
keys, workload TLS, database credentials, NATS credentials, OIDC client secret,
provider credentials и optional Mattermost credentials получают новые
generation/revision.

Минимальный secret contract stateful-слоя:

| Namespace | Resource | Обязательные keys | Назначение |
|---|---|---|---|
| trust-manager trust namespace | `mattercodex-installation-ca` | `tls.crt` | публичный installation CA source для `Bundle` |
| `mattercodex-system` | `mattercodex-installation-ca` | `tls.crt`, `tls.key` | cert-manager `Issuer`; доступен только cert-manager |
| `mattercodex-system` | `mattercodex-postgresql-bootstrap` | `password` | одноразовый bootstrap PostgreSQL superuser |
| `mattercodex-system` | `mattercodex-nats-credentials` | `operator.jwt`, `system-account.public`, `system-account.jwt`, `account.public`, `account.jwt` | NATS operator и два bounded accounts |

Значения создаются криптографически стойкими средствами владельца и никогда не
передаются как CLI argument. PostgreSQL application DSN, NATS user credentials,
workload TLS и authority material по-прежнему материализуются через Vault/CSI
по exact ресурсам render. Bootstrap password не используется application Pod и
после проверки миграций ротируется и хранится в owner-controlled secret
boundary для последующего восстановления StatefulSet; его нельзя удалять,
пока Pod ссылается на этот Secret.

Публичный домен, Origin, OIDC issuer/JWKS, registry endpoints и allowed external
hosts передаются параметрами deployment environment. Репозиторий не задаёт
чужой public domain по умолчанию.

## 4. Порядок новой установки

1. Создать новый owner-controlled каталог material командой
   `tools/deploy/generate-fresh-install-material.sh`. Команда требует exact
   `--release-registry-host`, отказывается писать в существующий каталог и
   создаёт файлы с режимом `0600`. NATS operator/account JWT сохраняются как
   compact JWT без armoured envelope, а account nkeys - как значения без JSON-
   кавычек. Повторная материализация этих пяти файлов из сохранённого `nsc`
   store выполняется только через
   `tools/deploy/materialize-nats-operator-files.sh`; значения команда не
   выводит.
2. Применить bootstrap registry через `infra/bootstrap-registry/bootstrap.sh`.
   Public host, IngressClass и ClusterIssuer передаются параметрами; их нельзя
   брать из константы репозитория. Выполнить `preflight`, `apply`, затем
   `readback`.
3. Применить ARC bootstrap с тем же exact public registry DNS через
   `--release-registry-host`. Этот параметр добавляет только `<host>:443` в
   `CONNECT` allowlist egress proxy; URL, port, wildcard и внутреннее
   Kubernetes DNS-имя отклоняются. Выполнить `preflight`, `apply`, затем
   `readback`.
4. Установить pinned service controllers командами
   `infra/service-infrastructure/bootstrap.sh --mode apply-controllers` и
   `--mode readback`. Изменение публичного Traefik выполняется только
   `infra/public-ingress/bootstrap.sh`, после чего также обязателен `readback`.
5. Настроить SSO через `tools/deploy/configure-keycloak.sh --mode apply` и
   повторить `--mode readback`. Скрипт создаёт или приводит к exact состоянию
   realm, public SPA client с Authorization Code + PKCE S256, audience и
   обязательные owner claims; пароль существующего owner не меняется.
6. Создать namespace, installation CA source и bootstrap secrets командой
   `tools/deploy/materialize-fresh-install-secrets.sh`, затем установить Vault
   через `infra/service-infrastructure/bootstrap.sh --mode apply-vault`.
   Materializer проверяет canonical shape NATS JWT/nkeys до создания namespace
   и Secret и закрыто отклоняет envelope, JSON-строку или неоднозначный вывод.
7. Выполнить `tools/deploy/deploy-fresh-release.sh --mode preflight` для exact
   immutable render. Скрипт отклоняет placeholder, zero digest, другой context
   и повторяющиеся resource identities.
8. Выполнить фазу `apply-state`. Она инициализирует Vault, создаёт exact policy,
   image PKI и static material, создаёт bootstrap restore evidence anchor только
   при его отсутствии, затем применяет non-workload resources, PostgreSQL и
   NATS и материализует database credentials. Существующий anchor только
   проверяется по обязательной форме и не переписывается render-ом: продвинуть
   его вправе исключительно PITR executor через forward-only policy. Первый
   verified Vault database connect может попасть в короткий restart PostgreSQL
   после init scripts: только `connection refused` повторяется с ограниченным
   backoff; semantic/configuration ошибки завершают фазу немедленно, а
   `verify_connection` не отключается. PostgreSQL ingress разрешает TCP/5432
   только точным внутренним workload и Vault database engine в этом же
   namespace; отсутствие пути от `app.kubernetes.io/name=vault` является
   ошибкой render, а не основанием расширять CIDR или отключать policy/TLS.
9. Выполнить фазу `apply-migrations`. Она последовательно запускает
   `internal-rpc-authority-migrate`, создаёт runtime database roles, запускает
   `control-plane-migrate` и `control-plane-broker-bootstrap`. Успешный Job не
   перезапускается; неуспешный удаляется и создаётся заново из того же render.
10. Выполнить фазу `apply-workloads`. Она проверяет restore evidence anchor и
   применяет полный render за исключением этого create-once ресурса, дожидается
   image supply chain, выполняет `release-artifact-materializer`, затем ждёт
   rollout каждого Deployment и DaemonSet.
11. Выполнить фазу `readback`. Она повторно проверяет Vault, StatefulSet,
    Deployment, DaemonSet, Job, форму фактически обслуживаемого restore evidence
    anchor и отсутствие terminal container waiting states.
12. Для Mattermost выбрать профиль `web-with-mattermost` и передать exact DNS
   через `--mattermost-host`; web-only запрещает этот параметр и не содержит
   interaction deployment, trust material или external credential mounts.
13. Выполнить отдельный service-graph smoke после локальной readiness всех Pod.

Каждый из перечисленных скриптов требует exact `--context`. После ошибки нельзя
повторять reset или предыдущие разрушительные шаги: устраняется причина и
повторяется только текущая идемпотентная фаза. Release render и material catalog
не меняются между фазами.

## 5. Проверка bootstrap

- повторный bootstrap не создаёт вторую Organization или системного помощника;
- initial owner membership разрешается из проверенного OIDC subject;
- stable key системного помощника — `system-assistant`;
- core prompt version, built-in platform capabilities, integration definitions,
  runtime defaults и policies совпадают с shipped revision;
- попытка delete/disable/archive помощника закрыто отклоняется;
- assistant readiness подтверждает фактически ready warm runtime.

## 6. Web-only smoke

1. Войти через Control Center.
2. Создать Project без integrations.
3. Создать Agent с admitted role image и опубликовать instructions.
4. Запустить Agent, увидеть `QUEUED -> RUNNING -> SUCCEEDED` по realtime stream.
5. Открыть result и скачать artifact.
6. Продолжить Session новым Turn.
7. Запустить Workflow с двумя child Agents и проверить graph/callback.
8. Разрешить Human Gate в web и убедиться, что continuation создан один раз.
9. Отменить Run и выполнить retry с новой attempt/`RETRY_OF` lineage.

Mattermost, GitHub и пользовательская Kubernetes integration при этой проверке
отключены. Их отсутствие не является warning или readiness failure.
