---
id: OPS-SSH-COMPOSE-1838
title: Операторский bootstrap SSH/Compose
type: runbook
status: approved
owner: sre
version: 1.0.1
updated: 2026-10-25
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
Ubuntu22.04/24.04. На Ubuntu24 требуется штатный AppArmor профиль rootlesskit
из пакета; глобальный sysctl не ослабляется. Rootful Docker socket должен быть
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

Для актуального private Go+SQLite проекта delivery остаётся **BLOCKED**.
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
