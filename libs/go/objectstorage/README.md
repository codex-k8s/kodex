---
title: Общий порт объектного хранилища
type: guide
status: approved
owner: developer
version: 1.0.1
updated: 2026-11-17
---

# Объектное хранилище

Модуль задаёт порт `Store`, метаданные `PutInput`/`Receipt`/`Object` и
синтаксические проверки `ValidKey`, `ValidDigest`, `ValidEndpoint`.
Бакеты, ключи арендаторов, разрешения, сроки хранения и повтор операций
выбирает сервис-владелец, а не библиотека.

`ValidEndpoint` — чистый предикат без DNS, сети, журналирования или
создания ресурсов. Он допускает HTTPS либо HTTP для точного узла и явного
порта из полностью заданной `EndpointPolicy` при `AllowPlaintext=true`.
По умолчанию HTTP запрещён. Реквизиты в адресе, непустой запрос/фрагмент
и декодированный путь кроме пустого или `/` отклоняются. Пустой разделитель
`?` сохраняет прежнюю допустимость. Проверка не ограничивает список узлов
HTTPS и не является сетевым разрешением или проверкой TLS-доверия.
Локальное исключение, среда и имена бакетов остаются в сервисе; библиотека
не содержит адресов инфраструктуры и не читает переменные окружения.

Порт не подтверждает соответствие тела заявленному дайджесту и не проверяет
принадлежность ключа арендатору. Эти проверки и точное закрепление версии
объекта выполняет вызывающая сторона. Поток `Object.Body` закрывает получатель.
Ошибки `ErrInvalid`, `ErrNotFound`, `ErrConflict`, `ErrUnavailable`
возвращаются вызывающей стороне; неопределённый исход записи не разрешает
автоматический повтор без сверки состояния.

Адаптер `s3store` реализует порт через AWS SDK. Его настройки транспорта
остаются отдельным контрактом, а создание/проверка/закрытие зависимостей
принадлежат композиции сервиса. Библиотека не запускает скрытые worker и
не регистрирует глобальную наблюдаемость. Диагностику операции и политику
повтора задаёт вызывающий сервис.

Новые синтаксические helpers добавляются без изменения существующего
порта, wire/storage схемы или данных. Изменение допустимой грамматики требует
проверки всех потребителей; несовместимое изменение порта — явной миграции.
Unit-тесты не требуют сети. Проверки реального S3/SeaweedFS используют
отдельный явно настроенный disposable-контур и не считаются выполненными
по одному успешному unit-запуску.

Для отдельного HTTPS mount, например `https://storage.example.invalid/s3`,
явно выбирается `EndpointPolicy.UsePathStyle=true` и `s3store.Config.UsePathStyle=true`.
Корневые endpoints сохраняют прежнее поведение; HTTP prefix, virtual-hosted
режим с prefix и неканонические/encoded segments отвергаются до I/O.
Необязательный `s3store.Config.HTTPClient` задаёт transport/CA caller; nil
сохраняет стандартный клиент AWS SDK. Проверка адреса не выдаёт сетевые права.


`RangeReader` — необязательный bounded read того же adapter, без нового транспорта
или worker. `RangeRead` закрепляет exact non-null immutable version, ETag, полный
размер/digest объекта и отдельный SHA256 slice. Принимается один closed range
длиной1–32768 bytes. `s3store.ReadRange` использует `VersionId`, `If-Match` и
`Range` существующего AWS SDK; проверяет точные `Content-Range`, длину, source
pins и hash bytes, закрывает body до возврата. Игнорирование Range, лишние либо
усечённые bytes, corruption и expiry закрываются без результата. Максимальное
ожидание —5секунд либо более ранний caller deadline; SDK GET не повторяется.
Возвращённый buffer принадлежит caller и должен быть очищен после использования.
Artifact visibility, canonical revision/catalog, owner lifecycle и storage cleanup
проверяет сервис; один descriptor либо range hash не выдаёт право доступа.

Проверены Context7 `/aws/aws-sdk-go-v2` и source pinned S3 SDK1.109.1:
[GetObject API](https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObject.html).
Local TLS fixtures проверяют реальный SDK conditional/range path без внешнего S3;
это не proof durability установки.

`SingleAttemptDeleter.DeleteOnce` — optional exact-version transport mode для
owner с durable attempt ledger. Он использует тот же S3 client и делает одну
SDK attempt; UNKNOWN требует `Head`, fallback на обычный `Delete` запрещён.
Пустая/`null` provider version отклоняется до запроса. `Store.Delete` сохраняет
прежний SDK retry contract для существующих archive/scheduled/cleanup callers.
Порт не назначает actor, key authority, retry attempt, deadline или retention.
Caller выбирает bounded context и допускает effect в своём owner transaction.

Документы: AWS SDK Go v2 Retries and Timeouts (Context7), operation-specific
maximum attempts и context cancellation. Actual HTTP503/disconnect tests
считают запросы нового режима; отдельный случай сохраняет ordinary Delete retry.


Для `DeleteOnce` недостаточно одного SDK retry attempt: HTTP307/308 также не
должны породить повторный DELETE. S3 adapter создаёт отдельный no-redirect view
того же HTTP transport/TLS/connection pool. Обычный клиент сохраняет прежний
redirect/retry policy. Неизвестный custom HTTP client остаётся доступным обычным
операциям; `CheckDeleteOnce` закрыто отклоняет его до durable admission.


HTTP/2 может повторить empty-body request внутри transport после REFUSED_STREAM
или GOAWAY. Exact view передаёт немедленно исчерпанную неповторяемую body без
GetBody: bytes остаются пустыми, payload hash/signed headers сохраняются.
HTTP/1 сохраняет Content-Length0 без chunked transfer; HTTP/2 завершает пустую
DATA/EOF entity без Content-Length. Обычные запросы не используют этот view.
Live S3-совместимость этого режима проверяется отдельно от local TLS fixtures.
