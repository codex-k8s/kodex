---
id: RUN-QA-HOST-1837
title: Периметр выделенного сервера сквозного QA
type: runbook
status: approved
owner: SRE
version: 1.0.0
updated: 2026-10-24
---

# Область применения

Issue #1837, эпик #1830. Только отдельный Ubuntu-сервер, предоставленный
владельцем для небольшого приложения Docker Compose. На хосте Kodex или
узлах Kubernetes этот helper не запускается. Приложение развёртывает
сотрудник SRE платформы; настройка firewall не считается проверкой его работы.

`tools/qa/test-host-firewall.py` не читает credentials. Оператор подключается
существующим разрешённым SSH-доступом с проверкой сохранённого host key.
Пароль/ключ не передаются в argv, git, stdout или обычный диагностический архив.
Копия helper устанавливается root как regular файл без symlink/hardlink в
`/usr/local/libexec/kodex-qa-host-firewall`; hash сверяется с проверенным SHA.

## Политика

JSON-конфигурация содержит только `managementIP`, `externalInterface`,
`githubHooks` и необязательный `dhcp4Server`. Management IP берётся из
фактического `SSH_CONNECTION`, интерфейс — из маршрута к этому адресу.
GitHub hooks ranges берутся из актуального официального `/meta`, не из
произвольной инструкции сотрудника. DHCP-server берётся из действующей
systemd-networkd lease; helper повторно проверяет эту привязку перед apply.

- Management IP имеет входящий доступ; остальные источники — TCP80 для
  HTTP-01 и TCP443 только из GitHub hooks ranges.
- IPv4 и IPv6 закрыты одной inet-таблицей. Необходимые ICMP errors и IPv6
  control traffic сохраняются. DHCP67→68 разрешён только от точного текущего
  сервера и на внешнем интерфейсе, чтобы не потерять адрес после renew.
- Ответы на исходящие соединения разрешены; прежнее входящее соединение
  постороннего источника не получает отдельного исключения по established.
- Внешний forward проверяет **исходный** destination port conntrack после
  DNAT. Поэтому Docker port mapping не открывает запрещённый публичный порт.
- Никаких flush ruleset, изменений host resolver, чужих таблиц или глобальной
  политики Docker. Собственная таблица — `inet kodex_qa_access`.

## Поставка и подтверждение

1. Сохранить приватный исходный snapshot сети, firewall и известных сервисов;
   убедиться в точной identity сервера и отсутствии активной установки helper.
2. Проверить код и запустить изолированную сетевую проверку:

   ```bash
   sudo -n unshare --net python3 tools/qa/test-host-firewall-network-test.py
   sudo -n unshare --net python3 tools/qa/test_host_firewall_lifecycle.py
   ```

   Тест проверяет отличие network namespace от PID1 до первого изменения.
   Он проверяет реальные IPv4/IPv6 TCP-пакеты и Docker-подобный DNAT, затем
   завершает собственные процессы/пространства. Живые сервисы не используются.
3. Через первое SSH-соединение передать JSON конфигурации в stdin режима
   `plan`. Сохранить точные bytes плана и `planDigest` в private0600 файлах.
   Режим выполняет `nft --check`, не применяет сетевые правила.
4. Передать тот же план в `apply --plan-digest <digest>`. До применения nft
   helper записывает журнал и запускает systemd rollback timer на 120 секунд.
   Статус `APPLIED_UNCONFIRMED` не означает завершённую поставку.
5. Открыть **второе независимое SSH-соединение** с тем же management IP.
   Проверить доступ и передать план в `confirm --plan-digest <digest>`.
   Helper проверяет другой SSH tuple, остаток времени, exact nft readback и
   активный timer. Затем устанавливает собственную boot-службу, атомарно
   публикует и синхронизирует durable receipt, после чего останавливает
   rollback timer/service. Boot-служба применяет правила только при наличии
   этого receipt и отсутствии rollback intent; ранняя загрузка до commit
   не применяет firewall. `CONFIRMED` означает подтверждённый периметр,
   но не успешный релиз приложения или webhook delivery.
6. Сверить nft readback, disabled rollback timer и enabled boot-service.
   Фактическую проверку после reboot планировать отдельно с сохранением
   состояния приложения; одна enabled unit не доказывает boot acceptance.

Если второе соединение/confirm не завершились, **не повторять apply**.
Сначала выполнить `inspect --plan-digest <digest>`: это readback записанного
commit/rollback intent, отдельно требуется сверить фактическую nft-таблицу.
Без commit timer выполняет возврат; после commit timer безопасно ничего
не меняет, даже если ответ confirm потерян. Повтор exact confirm после commit
возвращает прежний receipt без новой mutation. При недоступном SSH дождаться
timer и проверить состояние после восстановления связи.
Журнал сохраняется в root-only `/var/lib/kodex-qa-firewall` и препятствует
слепой повторной установке. Повтор после завершённого rollback требует
отдельной проверенной архивации этой установки и нового исходного snapshot;
удалять неизвестный журнал ради обхода guard запрещено.

## Возврат

`rollback --plan-digest <digest>` не требует stdin и доступен от root.
Проверяет собственный журнал и сохраняет durable rollback intent до первого
внешнего эффекта. Intent запрещает boot/reconfirm даже при прерывании очистки.
Проверяет table ownership comment и удаляет только свою таблицу.
Если boot-service уже создана, сверяет её точное содержимое,
отключает и удаляет только эту службу. Журнал сохраняется для диагностики.
Чужая таблица с тем же именем и другим ownership marker закрыто отклоняется.
Прерванный rollback допускает exact повтор очистки по прежнему digest;
он не применяет firewall заново и не удаляет журнал. `expire` является
внутренним режимом таймера: после commit без rollback intent он не выполняет
внешних эффектов. `boot` — внутренний режим собственной systemd-службы.

Изменение DHCP-server, интерфейса, management IP, SSH host key или webhook
ranges не принимается молча; нужен новый проверенный план. Существующая
установка намеренно не имеет универсальной команды обновления firewall.

Источники: [GitHub webhook IP ranges](https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks),
[ACME HTTP-01](https://letsencrypt.org/docs/challenge-types/#http-01-challenge),
[порядок nftables chains](https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains).
