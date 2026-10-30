---
id: OPS-SSH-COMPOSE-1838
title: Операторский bootstrap SSH/Compose
type: runbook
status: approved
owner: sre
version: 1.0.4
updated: 2026-10-29
---

# Операторский bootstrap SSH/Compose (#1838)

Сквозная приёмка текущего forwardport имеет статус `NOT_READY`; shipped consumer
зарегистрирован как `READY`, но доставка и live отдельно не подтверждены. Команды описывают
проверяемый оператором путь; они не являются разрешением подключаться к target
или результатом live-приёмки. Private SSH key вводится через write-only
project secret с purpose `SSH_COMPOSE`; модель и эти public tools его не читают.

`target.py` принимает отдельный owned regular JSON с девятью строковыми полями:
`project_ref`, `owner`, `repository`, `address`, `user`, `host_key`,
`published_port`, `runtime_image`, `ssh_secret_ref`. `address` — один public IP,
`host_key` — полный заранее сверенный Ed25519 pin, `runtime_image` — готовый
образ по OCI digest. DNS/CIDR/root/неприкреплённый image отклоняются.

```bash
python3 tools/ssh-compose/target.py network-policy --connection connection.json
python3 tools/ssh-compose/target.py bootstrap-plan --connection connection.json \
  --owner-keys owner-public.json --authorized-key deploy-public.pub \
  --helper-sha256 <sha256> > bootstrap-plan.json
```

`owner-public.json` содержит только public trust map CP signing keys.
Отдельная owner CLI `libs/go/sshcompose/cmd/ssh-compose-owner-keygen` создаёт
новые `--private-file` и `--public-file` только в owned0700 directory; private
signing bytes предназначены исключительно CP Secret
`kodex-ssh-compose-signing`/`private-key`, а не gateway или target. На target
доставляются public trust map, public SSH authorized key, pinned helper binary
и public plan. Переносом private credentials управляет только владелец.

Оператор заранее устанавливает проверенные пакеты Docker/rootless extras,
Compose plugin, uidmap, slirp4netns и поддерживаемую cgroupv2 конфигурацию на
Ubuntu22.04/24.04/26.04. На Ubuntu24 и новее требуется загруженный штатный AppArmor
профиль rootlesskit: в проверенной Ubuntu26.04 он принадлежит пакету `apparmor`,
а не `docker-ce-rootless-extras`. Глобальный sysctl не ослабляется. Rootful Docker socket должен быть
выключен. Автоматической загрузки пакетов и `curl|sh` в bootstrap нет.

На самом выделенном target root выполняет `bootstrap.py` с неизменным
`--plan` и `--expected-digest` плана. Для `preflight` и `apply` дополнительно
нужен `--helper` с уже проверенными бинарными bytes. Команды никогда сами не
подключаются по SSH. `preflight` сверяет host key, локальный exact IP, Ubuntu,
пакеты/cgroup и hash helper. `apply` до первого изменения сохраняет PENDING,
создаёт отдельного пользователя, forced-command authorized key, root-owned
helper/config, rootless Docker, user slice bounds и boot unit.

После любого сбоя сначала выполняется `readback` того же plan digest.
Повторять `apply` при наличии marker нельзя. Неопределённый PENDING не является
успехом: оператор сначала сверяет account/home/uid и точные созданные пути.
`rollback` отзывает только собственный SSH authorized key, останавливает
выделенный user manager и отключает linger; source/state/journal сохраняются.
Частичный bootstrap до записи uid требует ручной операторской сверки, без
автоматического усыновления существующего пользователя.

Compose публикует приложение только на loopback `published_port`; настройка
публичного HTTP80 через проверенный proxy — отдельная операторская проверка
целевого окружения. Bootstrap не меняет firewall, SSH daemon, чужие проекты
либо глобальную сеть. Перед live необходимо проверить apply/readback/rollback,
reboot только committed container, отказ UNKNOWN resume и ресурсные bounds
на реальном Ubuntu1CPU1GB без swap. Сейчас эти проверки `NOT RUN`.

Безопасные локальные проверки без target, credentials и subprocess effects:

```bash
cd tools/ssh-compose
python3 -m unittest -v test_target.py
```

Проверенные внешние документы: Docker [rootless](https://docs.docker.com/engine/security/rootless/),
[troubleshoot](https://docs.docker.com/engine/security/rootless/troubleshoot/),
[Ubuntu](https://docs.docker.com/engine/install/ubuntu/).
Context7 был недоступен из-за отклонённой авторизации; используются официальные
источники. Команды установки и root credentials модельному SRE не передаются.

Уточнение29.10.2026: фактический выделенный target работает на Ubuntu26.04.
Context7 `/docker/docs` подтвердил поддержку26.04LTS официальным Docker Engine
и поставку AppArmor-профиля rootlesskit в deb-пакете `docker-ce-rootless-extras`.
Bootstrap допускает эту LTS, сохраняя прежние проверки host key/IP, root-owned
пакетов, cgroup, helper digest и отсутствия rootful socket. Другие новые версии
не допускаются автоматически. Набор `test_target.py` содержит шесть проверок,
включая отказ26.04 при чужом host key/доступном для записи пакете и ранний отказ
неподдержанной26.10. Это проверки preflight без SSH и внешних изменений;
Реальные package/bootstrap/readback квалифицируются отдельно от этих unit-проверок.

Живой операторский checkpoint29.10.2026 на чистом030f6316: публичные пакеты
Docker29.9.2/rootless extras и Compose5.6.1 установлены из официального
`resolute` repository с проверенным Docker signing fingerprint; без upgrade
прочих пакетов. `bootstrap.py preflight/apply/readback` вернули PASS на
выделенной Ubuntu26.04. Подтверждены отдельный non-root account, forced-command,
rootless/systemd/cgroupv2, slice640MiB/90%CPU/256tasks и отсутствие rootful socket.
Профиль AppArmor принадлежит штатному `apparmor` и загружен, глобальное
ограничение user namespaces сохранено. Это подготовка инфраструктуры, не
приёмка SSH-вызова сотрудника, deploy/health, rollback или reboot. Эти эффекты
ещё `NOT RUN`; приложение хост-агент не реализовывал и не развёртывал.

### Отдельная проекция ключа подписи CP

Опциональный repo-owned component
`deploy/k8s/components/control-plane-ssh-compose-signing` добавляется в разрешённый
overlay после создания `kodex-ssh-compose-signing` с единственным ключом
`private-key`: ровно64 бинарных bytes результата owner keygen. Значение не
попадает в Git, ConfigMap, диагностический архив или модель. Mount read-only,
mode0440, доступен только основному контейнеру CP с прежним fsGroup контура;
gateway, runtime, scanner и target не получают signing private key. На target
доставляется только public trust map. Компонент не создаёт Secret и не расширяет
RBAC/egress. Без него SSH deploy закрыто недоступен; наличие записи adapter READY
само по себе не доказывает доступность подписи.

UID основного контейнера и fsGroup Pod проверяются раздельно: в базе fsGroup10001,
а в обследованном staging overlay fsGroup1001 при UID10001 основного контейнера.
Компонент не меняет ни одну из этих identity; оператор сверяет сохранённый
securityContext, а не требует от живого overlay значений базы.

Перед адресным включением оператор сверяет cluster/namespace UID, сохраняет
приватный снимок exact Deployment и отсутствие прежней настройки, проверяет
64-byte Secret и итоговый render. При подключении к уже работающему staging
применяется только этот patch с проверкой resourceVersion; полный render
остальных ресурсов не применяется. После rollout сверяются обе ready реплики,
exact image/source SHA, env path и единственный read-only mount. Возврат убирает
только добавленные env/mount/volume при совпадении исходного UID и ожидаемого
значения, затем проверяет rollout. Ключ сохраняется в приватном custody для
разбора неопределённого исхода; это не процедура ротации или отзыва remote trust.

| Переход | Авторитетный вход и эффект | Проверка/отказ |
| --- | --- | --- |
| Подготовка | Оператор, разрешённый staging, точные UID и новый keygen output; приложение ключ не генерирует | Несовпадение UID, существующая настройка либо неверная длина закрывает apply |
| Включение | Exact Secret→read-only CP mount; deployment patch после приватного INTENT | Неизвестный результат сначала читается; повторное создание ключа или blind apply запрещены |
| Рабочий вызов | CP выдаёт подписанный exact SSH lease, worker видит только свой invocation и SSH credential | Нет signer/secret/grant/merge proof — отказ; runtime не получает private signing bytes |
| Возврат | Оператор снимает только собственную проекцию; данные target и journal сохраняются | Старый lease не получает новых полномочий; UNKNOWN remote outcome требует status, не повторного deploy |

Для прежнего private Go+SQLite профиля delivery остаётся **BLOCKED**.
Обязательный root path — `kodex-deploy.json`, schema —
`contracts/integrations/v1/ssh-compose-release.schema.json`. Source bundle не
является Go build artifact. Нужны проверенные off-host build/provenance exact
merged SHA→OCI digest и private pull credential custody; сейчас эти входы не
материализованы. Public base image допустим как base, публичная публикация
образа private проекта не разрешена. OwnerPAT/TEST_REPO_GITHUB_PAT не переносятся
на target. GitHub Actions declarations без grants и реального CI receipt не
считаются поставкой. Неподготовленный private pull закрыто завершается ошибкой.

Для текущего forwardport Context7 app connector проверил
`/websites/pkg_go_dev_golang_org_x_crypto_ssh`: FixedHostKey/HostKeyCallback,
ограничение ClientConfig.Timeout установлением TCP и обязательное закрытие
соединения при cancel. Это уточняет прежний исторический отказ прямого
Context7 route, не изменяя ключи и доверенную границу. SSH protocol tests
используют isolated loopback transport и synthetic ключи; ни bootstrap, ни
внешний SSH-подключение эти тесты не выполняют.
