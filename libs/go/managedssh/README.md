# Закрытый протокол управляемого SSH

Модуль задаёт общий owner/broker private material contract и один версионированный wire contract между private adapter
`secret-broker/internal/integration/sshtarget` и установленным оператором
`tools/managed-ssh-helper`. Он использует только стандартную библиотеку Go,
не содержит credentials, соединений, grant store, shell либо HTTP transport.
Наличие модуля не означает готовность интеграции или квалификацию target.

`Profile` описывает конечные aliases, UID/GID/groups, digests установленного
helper и root-owned account-shell wrapper, expiry, receipt capacity и
необязательные выбранные system units.
Этот JSON сам по себе не является operator proof. Production consumer обязан
проверить установку и lifecycle profile, актуальные owner grants и exact target.
`Request` связывает profile digest, назначенную владельцем attempt, закрытую
operation и ограниченные arguments. Digest подтверждает содержимое; он не
выдаёт authority.

`DecodeOperationInput` связывает canonical owner input с тем же закрытым
набором recipes до создания invocation и повторно перед broker dispatch.
`OperationInput` не принимает actor, ключ, helper attempt либо profile digest.
Реальные UUID/profile pins добавляются отдельно через `Request`; receipt read
получает только original invocation reference и требует owner lookup исходных
coordinates. Произвольная команда имеет отдельный `ExecuteInput` и всегда
`EACH`, независимо от её текста. Лимит command8192 байта совпадает с transport,
decoded stdin/file content ограничены16384 байтами. `Clear` очищает owned
copies; formatter не раскрывает command, stdin и paths.

SSH package `ValidateInput` вызывает этот decoder до общего scalar validator:
null не превращается в пустой file content, а альтернативный base64 не меняет
точные согласованные bytes. Поля поступают как canonical JSON от owner/client
adapter; whitespace, alternate key ordering/escapes и неоднозначные scalars
закрыто отклоняются этим private profile. Общие input codecs других adapters
не меняются. `make test-ssh-operation-input` воспроизводит helper round trips,
input negatives, package/transport limits и actual synthetic SSH transport.

| Операции | Минимальное решение владельца |
| --- | --- |
| Inspect, system/process/file/service/journal reads, receipt read | `NONE` после квалификации typed recipe |
| Managed file create/replace | `SCOPED`, exact target/user/recipe/arguments и effect limits |
| Managed file delete | `EACH` |
| Управление system service под отдельным UID | `EACH` для делегированной привилегии |

Произвольный shell не представлен ни одним read recipe этого протокола.
Его отдельный чувствительный broker path требует `EACH`. Неизвестный operation
отклоняется и никогда не получает классификацию READ по умолчанию.

Кодирование принимает точный canonical JSON. Duplicate keys, case aliases,
неизвестные поля, alternate scalar encodings и несовместимые union branches
отклоняются roundtrip-проверкой стандартного `encoding/json` до использования.
Request ограничен 64 KiB, reply — 32 KiB, file content/fragment — 16 KiB.
Полная content version допускает bounded regular file до 1 MiB. Коллекции
ограничиваются числом records и encoded bytes: `EncodeBoundedReply` сначала
проверяет полную семантику и только затем усекает read records с явным
`Truncated`. Receipts, mutations и exact file fragments не укорачиваются.

`UNKNOWN` означает отсутствие авторитетного завершения уже принятой attempt,
а не разрешение повторить effect. `ReceiptRead` сохраняет original digest и
operation; broker дополнительно сравнивает их с immutable owner receipt.
Новая attempt после неопределённого результата требует отдельного решения
владельца. Смена profile/expiry меняет digest и не переносит прежний consent.

Модуль не запускает workers, не создаёт события и не владеет lifecycle grants.
Readiness, audit, terminal/cancel/revoke, credential rotation и доставку
operator metadata обеспечивает авторитетный control plane вместе с broker.
Несовместимый wire version закрыто отклоняется без legacy/shell fallback.

`Qualifications` задаёт отдельный bounded operator artifact с lifetime не более
24h. Он связывает назначенный сервером target ref, literal endpoint/user,
SHA-256 wire bytes обоих SSH public keys, наблюдённые non-root UID/GID/groups
и hashes deployment, effective sshd configuration, identity inventory и
runtime readback. `MANAGED` дополнительно закрепляет полный helper `Profile`;
`EXECUTE` отвергает helper profile и не меняет обязательный `EACH`. Один target
ref не может одновременно обозначать оба режима. Полный artifact ограничен
64KiB и32 profiles; любой malformed/expired entry закрывает весь artifact.
Несколько личных public keys одного connection допускаются отдельными profiles
с одинаковыми deployment/account/scope pins. Другой key не может изменить
endpoint, host key, UID, mode либо helper profile того же target ref. Duplicate
target/key pair закрыто отклоняется. Individual capture/expiry и runtime
readback могут различаться; owner grants и personal binding остаются отдельными.
Для EXECUTE обязательны operator digest фиксированного `/usr/bin/id` и group
set с primary group. Broker перед business command наблюдает real/effective
UID/GID и numeric groups через fixed probes в том же SSH connection; root,
drift, malformed/неполный ответ запрещают dispatch. Это доверенные наблюдения
выбранного SSH host, а не доказательство независимого от host root исполнения.

Этот формат сам по себе не удостоверяет операторские наблюдения. Broker
читает его только из фиксированного read-only deployment mount с заранее
закреплённым полным content SHA на каждом dispatch. Обычный product JSON,
caller path либо helper self-report не являются источником доверия. Удаление
mount/profile или expiry запрещает будущий dispatch; уже отправленный effect
сохраняет отдельный owner cancel/recovery lifecycle. Наличие DTO и reader не
означает завершённости operator producer, owner setup либо deploy wiring.

Проверка протокола: `go test -race ./...` из этого модуля на точном toolchain
репозитория. Native helper и настоящий isolated OpenSSH проверяются отдельным
публичным контуром управляемого SSH.

`IdentityPreparation`, `IdentityProvision` и `IdentityLocator` задают общий
private contract между авторитетным CP owner и broker purpose store. Они не
содержат private key и не являются descriptor для gateway, generic TOKEN
либо пользовательского credential view. Preparation намеренно не имеет
ещё не выведенного authentication hash. Полный Provision принимает только
actual content/public-key digests broker; mutable claim/fence не входят в
physical Secret identity. Fixed purpose — SSH_TARGET, имя неизменно
`sshbi-<SHA256(identity revision ref)[0:16]hex>-r<revision>`.

UID/resourceVersion opaque и byte-exact: не требуют формы UUID/числа и не
становятся latest selectors. Shared Valid проверяет syntax/целостность tuple,
а не существование owner, grant, выбранного target либо remote effect.
Runtime consumer остаётся private `secret-broker` store с exact namespace и
текущим owner admission; CP048 должен использовать этот же контракт при
attachment вместо копирования predicate. Само наличие типов не означает
готовность owner048 persistence либо policy109.

Operator artifact `kodex-ssh-qualifications/v2` закрепляет exact raw
`host_key_algorithm` вместе с SHA256 SSH wire public key, endpoint/account,
режимом и authentication key digest. Обязательный algorithm позволяет выбрать
тот же key у сервера с несколькими host keys; это не разрешение SHA1 signature
для raw RSA type. Broker отдельно требует RSA3072+ и RSA SHA2 negotiation.
Неполный v2, старый v1, certificate/unknown algorithm не угадываются и закрыто
отклоняются. Product Connection содержит только target ref; адрес и host pin
материализуются из актуального trusted operator artifact, а не из operation
payload или ответа peer.
