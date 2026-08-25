---
id: RUN-MC-002
title: Чистое развертывание web-first MatterCodex
type: runbook
status: approved
owner: sre
version: 1.3.7
updated: 2026-08-25
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

| Namespace                     | Resource                           | Обязательные keys                                                                              | Назначение                                          |
| ----------------------------- | ---------------------------------- | ---------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| trust-manager trust namespace | `mattercodex-installation-ca`      | `tls.crt`                                                                                      | публичный installation CA source для `Bundle`       |
| `mattercodex-system`          | `mattercodex-installation-ca`      | `tls.crt`, `tls.key`                                                                           | cert-manager `Issuer`; доступен только cert-manager |
| `mattercodex-system`          | `mattercodex-postgresql-bootstrap` | `password`                                                                                     | одноразовый bootstrap PostgreSQL superuser          |
| `mattercodex-system`          | `mattercodex-nats-credentials`     | `operator.jwt`, `system-account.public`, `system-account.jwt`, `account.public`, `account.jwt` | NATS operator и два bounded accounts                |

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

Identity, административные UI и Vault recovery устанавливаются по
`RUN-MC-023`. Постоянный Keycloak administrator и initial owner credentials
поступают только из GitHub Environment secrets в однодневный encrypted
artifact. Временный bootstrap administrator и machine secrets генерируются
workflow; bootstrap identity удаляется после reconciliation, а приватный
`age` key остаётся за пределами GitHub, Kubernetes, Vault и репозитория.

## 4. Порядок новой установки

1. Создать новый owner-controlled каталог material командой
   `tools/deploy/generate-fresh-install-material.sh`. Команда требует exact
   `--release-registry-host`, отказывается писать в существующий каталог и
   создаёт файлы с режимом `0600`. NATS operator/account JWT сохраняются как
   compact JWT без armoured envelope, а account nkeys - как значения без JSON-
   кавычек. Повторная материализация этих пяти файлов из сохранённого `nsc`
   store выполняется только через
   `tools/deploy/materialize-nats-operator-files.sh`; значения команда не
   выводит. Генератор включает JetStream для account `APPLICATION` через
   `tools/deploy/configure-nats-application-account.sh` и закрыто проверяет
   account JWT: суммарно не более 32 GiB file storage, 256 MiB memory storage,
   8 streams, 64 consumers и 100000 pending acknowledgements; каждый stream
   обязан иметь `max_bytes`, а его file/memory limit не может превышать
   соответствующий account budget. Генератор также создаёт отдельный пароль
   bootstrap-generation для `internal-rpc-authority-restore-controller` и две
   независимые пары публичных per-install корней доверия manifest/readback.
   Эти корни нельзя заменять файлами из образа или другой установки.
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
   `--mode readback`. Secrets Store CSI driver не патчится через
   `fsGroupPolicy`: live readback должен подтвердить, что каждый SPC создаёт
   файлы с mode `0444`, а каждый CSI volume и container mount read-only.
   Non-root workload проверяет этот режим через `libs/go/securefile`.
   Отдельно readback подтверждает фактический mode/UID/GID projected
   ServiceAccount token: при process-owned `0640` точный путь обязан быть на
   kernel-enforced read-only mount, где `O_WRONLY` возвращает `EROFS`.
   Init-copy и дополнительная группа `0` не используются. Изменение
   публичного Traefik выполняется только
   `infra/public-ingress/bootstrap.sh`, после чего также обязателен `readback`.
5. Материализовать identity secrets, установить Keycloak и настроить SSO по
   `RUN-MC-023`. Скрипт создаёт или приводит к exact состоянию realm, public SPA
   client с Authorization Code + PKCE S256, отдельные confidential clients для
   OAuth2 Proxy и обязательные owner claims; пароль существующего owner не
   меняется. Keycloak administrator из `master` realm получает Headlamp
   `cluster-admin` только через закрытый административный ingress.
6. Создать namespace, installation CA source и bootstrap secrets командой
   `tools/deploy/materialize-fresh-install-secrets.sh`, затем установить Vault
   через `infra/service-infrastructure/bootstrap.sh --mode apply-vault`.
   Materializer проверяет canonical shape NATS JWT/nkeys до создания namespace
   и Secret и закрыто отклоняет envelope, JSON-строку или неоднозначный вывод.
7. Выполнить `infra/management-surfaces/bootstrap.sh --mode preflight`, затем
   `--mode apply-monitoring` с одним exact набором параметров по `RUN-MC-023`.
   Этот ранний этап устанавливает pinned kube-prometheus-stack и переводит
   внешние CRD `ServiceMonitor`, `PodMonitor` и `PrometheusRule` в состояние
   `Established`; Grafana ещё не публикуется наружу. Release preflight и
   `apply-state` закрыто отклоняют отсутствие этих CRD до инициализации Vault.
8. Выполнить `tools/deploy/deploy-fresh-release.sh --mode preflight` для exact
   immutable render. Скрипт отклоняет placeholder, zero digest, другой context
   и повторяющиеся resource identities, а также отсутствие установленных
   внешних observability CRD, если они используются render-ом.
9. Выполнить фазу `apply-state`. Она инициализирует Vault, создаёт exact policy,
   image PKI и static material, создаёт bootstrap restore evidence anchor и
   нулевой authority snapshot только при их отсутствии, материализует четыре
   публичных per-install authority root
   файла в immutable Secret с exact digest и монтирует только соответствующую
   пару в publisher, readback attestor и restore controller, отдельно применяет
   release-owned CRD и ждёт состояния
   `Established`, затем применяет typed admission parameters, остальные
   non-workload resources, PostgreSQL и NATS и материализует database
   credentials. Image admission использует immutable typed parameter resource;
   runtime-компоненты читают точную immutable `ConfigMap`-проекцию тех же
   значений. Отсутствие CR, drift двух проекций или попытка заменить
   `parameterNotFoundAction: Deny` является ошибкой release, а не основанием
   перезапускать API server. Общий Vault ingress разрешает по TCP/8200 только
   инфраструктурные `vault-csi-provider` и `vault-secrets-operator`, а также
   одно прямое workload-исключение
   `mattercodex-registry-node-pull-readback` в том же namespace. Исключение
   требуется для получения краткоживущего node-bound client certificate и
   ограничено exact ServiceAccount `mattercodex-image-pull-readback`, projected
   token с `audience: vault` и Vault policy, которая разрешает только issue по
   `pki-node-pull/issue/mattercodex-node-pull` и revoke собственного token.
   Client certificate использует закрытый CN
   `<16-hex-node-hash>.g<generation>.mattercodex-node-pull`; Vault role
   разрешает только поддомены этого DNS root без glob и arbitrary names.
   Базовая Vault ingress policy не разрешает другие workload. Отдельные exact
   policies добавляют только зарегистрированные publisher/issuer paths, причём
   publisher runtime policy строится из закрытого target registry в exact
   release render и содержит `create/read/update` для каждого полного KV path.
   `create` используется только для CAS=0 genesis отсутствующего keyset;
   wildcard, `delete` и произвольный create запрещены. CSI provider
   подключается к Vault по HTTPS с адресом, installation CA path и exact SNI из
   pinned Helm values. Публичный `ca.crt` монтируется read-only из
   `mattercodex-vault-server-tls`;
   `SecretProviderClass` не переопределяет transport trust, а Vault Agent
   sidecar не участвует в этом пути. Каждый SPC обязан запрашивать projected
   token через точный `audience: vault`; source- и release-render проверки
   закрыто отклоняют отсутствующую или иную аудиторию. `vaultSkipTLSVerify` и
   plaintext fallback запрещены. Существующий anchor только
   проверяется по обязательной форме и не переписывается render-ом: продвинуть
   его вправе исключительно PITR executor через forward-only policy. Уже
   опубликованный authority snapshot также только проверяется: generic apply не
   владеет `snapshot.jws` и runtime-аннотациями и не откатывает их к нулевому
   render seed. Первый
   verified Vault database connect может попасть в короткий restart PostgreSQL
   после init scripts: только `connection refused` повторяется с ограниченным
   backoff; semantic/configuration ошибки завершают фазу немедленно, а
   `verify_connection` не отключается. PostgreSQL ingress разрешает TCP/5432
   только точным внутренним workload и Vault database engine в этом же
   namespace. Все egress/ingress pod selectors указывают на фактический
   StatefulSet label `app.kubernetes.io/name=mattercodex-postgresql`; DNS aliases
   `control-plane-postgresql-rw` и `internal-rpc-authority-postgresql-rw` не
   являются pod labels. Отсутствие пути от `app.kubernetes.io/name=vault` является
   ошибкой render, а не основанием расширять CIDR или отключать policy/TLS.
   Однострочные credentials передаются между host и Pod как ровно одна запись;
   уже завершённый LF secret-файла не дополняется вторым delimiter. Следующий
   произвольный Vault KV payload сохраняется байт-в-байт без ведущего LF.
10. Выполнить фазу `apply-migrations`. Она последовательно запускает
   `internal-rpc-authority-migrate`, создаёт runtime database roles, назначает
   отдельный пароль `ira_restore_controller_g1`, сохраняет DSN только в Vault и
   через `VaultStaticSecret` материализует exact restore-controller Secret.
   Для каждого issuer/verifier target из закрытого publisher registry эта же
   фаза создаёт и ротирует exact Vault static database role, а
   `VaultDynamicSecret` проецирует только `dsn` и `username` в отдельный
   Kubernetes Secret целевого sidecar. Фаза дожидается готовности каждого
   dynamic secret только для текущей `metadata.generation`: `lastGeneration`,
   `Ready.observedGeneration` и `SecretSynced.observedGeneration` должны
   совпасть, после чего bounded readback сверяет закрытый набор ключей destination
   Secret до запуска
   `control-plane-migrate` и `control-plane-broker-bootstrap`. Успешный Job не
   перезапускается; неуспешный удаляется и создаётся заново из того же render.
11. Выполнить фазу `apply-workloads`. Она проверяет restore evidence anchor и
    существующий опубликованный authority snapshot, повторно проверяет готовность
    всех registry-derived database Secret, применяет workload-ресурсы за
    исключением этих runtime-owned ресурсов и всех `Job`, дожидается image supply
    chain, отдельно выполняет
    `release-artifact-materializer`, затем ждёт rollout каждого Deployment и
    DaemonSet. Первая публикация authority graph в новой пустой БД обязана иметь
    `source_revision: 1`; историческая ревизия без predecessor chain закрыто
    отклоняется и не может использоваться как shortcut чистой установки.
    Migration/bootstrap Job принадлежат только фазе
    `apply-migrations`: generic apply не обновляет их immutable template при
    переходе на новый release digest. Перед generic apply state/workload-фаза
    сравнивает semantic payload закрытого списка release-owned immutable
    ресурсов (`mattercodex-role-environments`, image admission ConfigMap и
    `ImageAdmissionPolicyParameters`). Совпадающий ресурс не изменяется; drift
    устраняется bounded delete/create с немедленным exact readback. Добавлять в
    этот список неизвестный ресурс без отдельного lifecycle запрещено.
12. Выполнить фазу `readback`. Она повторно проверяет Vault, включая точное
    совпадение publisher runtime policy с target registry этого release render,
    готовность и закрытую форму target database Secret, StatefulSet, Deployment,
    DaemonSet, Job, форму фактически обслуживаемого restore evidence anchor и
    отсутствие terminal container waiting states.
13. Настроить Vault OIDC, немедленно зашифровать Shamir 5/3 recovery material и
    установить OAuth2 Proxy/Headlamp по `RUN-MC-023`, затем выполнить полный
    management-surfaces `readback`. Monitoring chart уже установлен на шаге 7;
    повторный `apply-monitoring` допускается только как идемпотентный readback
    того же pinned release. До удаления
    plaintext root token и shares необходимо проверить encrypted bundle и его
    checksum.
14. Для Mattermost выбрать профиль `web-with-mattermost` и передать exact DNS
    через `--mattermost-host`; web-only запрещает этот параметр и не содержит
    interaction deployment, trust material или external credential mounts.
15. Выполнить отдельный service-graph smoke после локальной readiness всех Pod.

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
