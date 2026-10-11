# Управляемые IntegrationPackage

`Parse` строго читает JSON/YAML размером до 256 KiB, отклоняет неизвестные поля,
дубли, aliases и дополнительные документы. Результат содержит SHA256 точного
канонического JSON. Parse не выдаёт полномочий на исполнение.

`LoadShipped` принимает только SHIPPED definitions из поставки.
`NormalizeManagedRevision(raw, managedBy, shipped)` назначает UI/GIT origin из
проверенного owner lineage и возвращает package и canonical JSON. Значение
`managedBy` выбирает сервер; поле origin исходного документа не является
источником полномочий. Неизвестный key закрыто отклоняется.

`ValidateExecutableRevision(candidate, shipped)` применяется владельцем до
публикации и worker перед использованием immutable request-local package.
Оба digest повторно вычисляются; изменённый SHIPPED package не принимается.
Для UI/GIT доступны name/description/category/version, подмножество capabilities
с сохранением health operation, усиление approval policy и ограничений полей,
уменьшение timeout/attempts и увеличение ограниченного retry backoff.
Adapter/owner/route/readiness, credential, operation/risk/idempotency,
resource scope и выходная схема сохраняются. Наборы configuration/input fields
сохраняются, потому что adapter может читать необязательное поле.
Network допускает только точное подмножество поставленных tuples. Рабочая
операция отдельно требует необходимый destination в actual package.
JSON object carrier ограничен 512 KiB, package по-прежнему 256 KiB. Только
GitHub `content.update.content_base64` имеет maximumLength 349528 для файла
до 256 KiB; остальные поставленные field bounds сохранены. Управляемое
сужение поля остаётся обязательным для owner и consumer.

| Сценарий #1046 / #1028 | Проверка owner | Проверка consumer |
| --- | --- | --- |
| UI save/publish | tenant ACL, OCC/idempotency, server UI origin, canonical validation | exact key/version/digest и compiled adapter contract |
| Git accept/publish | source claim, commit ancestry, content digest, server GIT origin | тот же validator, без изменения глобального registry |
| Test/invocation | разрешённая connection/credential revision и immutable package | request-local package до credential read и внешнего действия |
| Git source read | file-read READ с approval NONE | усиленный READ gate без отдельной owner цепочки закрыто отклоняется |
| Неизвестный outcome | owner receipt/task readback | библиотека не создаёт событий или повторов |

Этот общий checkpoint предоставляет валидатор. Исполняемые owner RPC и worker
циклы проверяются в полных unit #1046 и #1028; библиотечный PASS не заменяет
их сквозную проверку. Для YAML parser проверена документация Context7
`/yaml/go-yaml`: Node traversal и Decoder.KnownFields.

GitHub 6.0.0 сохраняет единственный bounded `repository.content.list` contract:
immutable commit ref, offset cursor, count/total/next/eof и общий catalog digest.
Схема допускает cursor 0..999 и limit 1..50; adapter проверяет 40hex commit,
continuation digest, весь immediate-child индекс и окончательный native byte
budget. Старый 4.0.0 не получает новые semantics через decoder fallback.
EOF каталога не является EOF исходных файлов; upstream Contents cap >=1000
закрыто отклоняется. Публикация новой ревизии и exact owner rebind не заменяются
автоматическим изменением уже закреплённых connection/grant/runtime snapshots.

GitHub 6.0.0 публикует отдельный source budget `repository.content.read`:
полный UTF-8 файл без NUL до 8 МиБ (8388608 байт), offset/size/next 0..8388608.
Страница по-прежнему до 16384 байт, окончательный native envelope до 65536
байт. Сначала Contents разрешает exact commit/path в одном connection-owned
репозитории и проверяет file/path/SHA/size. `encoding=none` требует пустой
content; только сервером разрешённый Contents SHA передаётся SDK GetBlob
того же репозитория через `api.github.com`. До пагинации adapter проверяет весь
source, его size, Git blob SHA, UTF-8 и отсутствие NUL. Обычный raw SDK/metadata
budget 2 МиБ сохраняется; закрытый exact blob request имеет raw budget 12 МиБ,
ошибка — 64 КиБ. Download/raw URL, redirect, новый host или право не добавляются.
Историческая 5.0.0 закрыто отклоняется текущим executable registry: свежие
package/connection/grant/runtime pins назначает владелец состояния, библиотека
не выполняет rebind и не переинтерпретирует исторический snapshot.

Сценарий #1797: actor и exact grant поступают из проверенного session/turn
context и immutable package/connection revision, а не из path/ref/expected_sha.
Штатная managed MCP invocation `github.repository.content.read` проходит
gateway и request-local adapter; connection владеет owner/repository, GitHub
владеет file/blob на точном commit. `ref` закрепляет версию, `expected_sha`
проверяет целостность продолжения и не выдаёт authority. Успех возвращает
bounded page с source/chunk digests и receipt; ошибка метаданных, SHA, size,
UTF-8/NUL или бюджета закрыто отклоняет весь read до выдачи текста. Операция
READ_ONLY не меняет GitHub-состояние и не создаёт domain event; readback
выполняется тем же exact commit/path до EOF, native consumer получает только
проверенную страницу. Runtime/deploy proof этого пути принадлежит полному
unit gateway и не заменяется schema/codegen PASS этой библиотеки.
