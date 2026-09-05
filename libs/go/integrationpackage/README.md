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
Adapter/owner/route/readiness, credential, network, operation/risk/idempotency,
resource scope и выходная схема сохраняются. Наборы configuration/input fields
сохраняются, потому что adapter может читать необязательное поле.

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
