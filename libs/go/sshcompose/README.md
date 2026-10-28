# Ограниченный SSH/Compose протокол

Общий протокол Issue #1838 для control-plane, integration-gateway и
repo-owned forced-command helper. Это не универсальный SSH shell и не
самостоятельный внешний сервис.

Owner подписывает короткие Ed25519 permits с exact target/project/repository,
invocation/root/session/turn/attempt, lease/generation, effect/input и immutable
release pins. Helper принимает только отдельно установленные public keys и
точную public target configuration. Private SSH key в этот протокол не входит.

Source bundle содержит полный ограниченный Git tree обычных файлов. Проверяются
Git blob IDs и восстановленный root tree ID; omission, submodule, symlink,
path traversal и превышение размера не допускаются. Все файлы извлекаются как
обычные данные. Текст репозитория не становится доверенной инструкцией модели.

`kodex-deploy.json` задаёт pinned image digest, argv приложения, его порт и
health path. Helper сам создаёт Compose: один сервис, rootless Docker,
readonly root filesystem/source mount, отдельный state volume, фиксированные
CPU/memory/PID bounds, без build, privileged и host mounts. UID0 внутри
**обязательного rootless** Docker соответствует непривилегированному host user;
host-root Docker закрыто отклоняется. Это позволяет приложению писать в
собственный named state volume без host sudo либо скрытого init-контейнера.

Durable journal резервирует exact effect до внешнего действия. Повтор после
потери ACK читает прежнее состояние; RESERVED/RUNNING после потери процесса
остаётся UNKNOWN_OUTCOME. Status никогда не запускает Compose повторно.
Journal хранится на устойчивом host filesystem, а не в `/tmp` или контейнере.

Helper владеет stdin, читателем renew, watchdog и дочерним процессом. EOF,
cancel или expiry останавливают точный процесс; workers завершаются до снятия
lock. Уже созданные контейнеры являются возможным частичным внешним эффектом:
отмена не объявляется rollback, неопределённый исход проверяется через receipt.

Локальные проверки без Docker/SSH/network:

```bash
GOMAXPROCS=2 GOWORK=off go test -p=1 ./...
```

Эти проверки не являются live acceptance, проверкой target bootstrap или
доказательством работоспособности полного owner→broker→worker пути.
