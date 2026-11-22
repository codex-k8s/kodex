# Helper управляемого SSH

Одноразовый Linux process обслуживает только закрытый `managedssh/v1` protocol.
Он не принимает command-line options, исполняемые пути, sudo arguments,
environment, plugin либо произвольный shell от caller. Ключ SSH в helper не
передаётся. Broker держит credentials и выбирает exact host key/target/user;
control plane остаётся владельцем grants и invocation lifecycle.

Оператор устанавливает root-owned static binary и canonical configuration
`/etc/kodex/ssh-target.json` с exact executable digest. Все родители
конфигурации, executable и выбранных namespaces защищены от записи учётной
записью helper. Фактические UID/GID/groups должны совпасть с profile и быть
непривилегированными; capabilities, privilege bits, hardlinks, symlink parents,
неизвестная конфигурация и expiry закрыто отклоняются. Процесс включает
`NoNewPrivileges` и запрещает dump/ptrace своей памяти.

Managed namespaces и receipt directory принадлежат выделенной учётной записи
с mode0700. Эта identity предназначена только для root-controlled forced
command; произвольный shell или сервисы под тем же UID нарушают контракт
exclusive writer и не могут быть квалифицированы. Для чувствительного
arbitrary execute требуется отдельная выбранная identity/connection.
Установка не выполняется автоматически на host при создании connection.

Root-owned account shell `managed-ssh-shell` принимает только argv
`-c /usr/local/libexec/kodex-ssh-target`. Wrapper проверяет собственный digest
`account_shell_sha256` и inode фиксированного helper, затем заменяет process
через exec с минимальным environment. Shell grammar, interactive login,
подкоманды, alternate arguments и caller-selected executables отвергаются.
Это дополнительно закрывает обычный command path другого sshd listener;
оно не заменяет operator inventory. OpenSSH internal-sftp может исполняться
внутри sshd без login shell, а rc — раньше shell. Поэтому root-controlled home,
отсутствие user rc, alternate keys и in-process subsystem bypass, запрет
forwarding и inventory остальных sameUID writers остаются обязательными
частями квалификации. Этот контракт сверён через Context7 с официальными
`openssh-portable/sshd_config.5`, `sshd.8` и `session.c`.

Файлы разрешаются через kernel `openat2` с `BENEATH`, `NO_SYMLINKS`,
`NO_MAGICLINKS`, `NO_XDEV`, без pathname fallback. Разрешены private regular
single-link files. Create использует `RENAME_NOREPLACE`; replace/delete
проверяют полный bounded content digest под общим lock. Сначала сохраняется
и синхронизируется `UNKNOWN` intent, затем effect и parent directory, затем
completed receipt. Прерванный intent остаётся UNKNOWN. Existing attempt и
capacity exhaustion не разрешают replay. Evidence автоматически не удаляется.
Оператор обязан обеспечить persistent storage/backup lifecycle; tmpfs-тест
доказывает сохранность между процессами, а не после host reboot.

Необязательный service profile использует только system manager. Unit и
operator Polkit policy root-owned и pinned по digest, unit работает под другим
non-root UID с `NoNewPrivileges` и пустыми capability sets. User manager того
же UID не поддерживается. Service state/journal читаются типизированно;
restart/reload используют делегированную привилегию и требуют `EACH` в owner.
Политика, unit, tool binaries и profile должны иметь общий проверенный
operator lifecycle; checkbox в пользовательской configuration его не заменяет.
Без подходящей установки capability остаётся недоступной.

Один process ограничен 20s, включая peer, который не завершает stdin либо не
читает stdout. Owned nonblocking descriptors регистрируются в Go poller, имеют
deadline и закрываются при cancel с join callback. Это не обещание остановить
уже поставленный systemd job. После dispatch без подтверждённого receipt
исход остаётся UNKNOWN; closing SSH channel не доказывает remote join.

Static build: `CGO_ENABLED=0 go build ./cmd/managed-ssh-helper` на точном Go
репозитория. Установка требует отдельного target и оператора; этот README
не является выполненной установкой или grant на host root.

Native tests используют cache-only pinned container без network, host sockets,
accounts или production data. Проверяются настоящий installed binary,
root-file provenance, UID/caps/permissions/expiry/digest, независимые процессы,
process deadline, kernel traversal, CAS, потеря вывода и recovery без replay.
Контур сервиса/owner/UI и реальный выбранный SSH host квалифицируются отдельно;
локальный helper test не означает готовность всей интеграции.

## Локальная проекция operator qualification

Команда `ssh-qualification` читает canonical artifact
`kodex-ssh-qualifications/v2` из доверенного operator channel. Она не выполняет
SSH, host installation либо Kubernetes apply и не доказывает правдивость
помещённых оператором readback hashes. Runtime consumer повторно проверяет
canonical bytes и expiry перед каждым dispatch.

```sh
ssh-qualification render --directory /private/ssh-operator \
  --input profiles.json --output overlay.json
ssh-qualification check --directory /private/ssh-operator \
  --input profiles.json --output overlay.json
```

Каталог принадлежит process UID и имеет точный mode0700. Входной record имеет
mode0440, exact UID/GID и один link; symlink, writable file и другой владелец
отклоняются. `render` создаёт output через `O_EXCL`, fsync и exact readback
общего `securefile` owned-record API. Существующий либо частичный output не
перезаписывается и не удаляется. `check` сверяет точные bytes новой проекции
с сохранённым record; это не publication receipt, доказательство прежнего
fsync либо разрешение внешнего эффекта. Диагностика содержит только фиксированный
status и digests без target addresses и keys.

Bundle содержит ровно immutable ConfigMap, strategic merge patch только для
`secret-broker` и NetworkPolicy с полными literal `/32` либо `/128` destinations
и выбранными TCP ports. Patch сохраняет существующие image/env, добавляя fixed
read-only mount и полный expected SHA. Разные personal keys одного target не
размножают egress rules. Публичная проверка `make test-ssh-operator-render`
использует настоящий offline Kustomize consumer без обращения к API server.
Правила NetworkPolicy складываются с остальными политиками; один output не
доказывает изоляцию итогового environment либо поддержку CNI. Через Context7
`/websites/kubernetes_io` проверены официальные
[NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
и [NetworkPolicyEgressRule](https://kubernetes.io/docs/reference/kubernetes-api/networking/network-policy-v1/).

Local bundle не объявляет SSH integration готовой. Deployment ownership,
отзыв прежней qualification, остановка/drain прежних broker writers, точный
итоговый render и runtime readback остаются обязательными частями полного
owner/deployment lifecycle. Изменение account, sshd, helper, namespace, service
либо key требует этого перехода. Старый Pod с прежним immutable mount нельзя
считать автоматически отозванным новым ConfigMap.
