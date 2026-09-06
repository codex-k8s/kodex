# Авторизация внутренних RPC

Модуль содержит сгенерированный транспортный контракт Go
`internalrpcauthority.v1`. Исходный Proto находится в
`contracts/proto/internalrpcauthority/v1` и изменяется только там.

Пакет `authorityclient` предоставляет общую транспортную границу для
контейнеров приложения: проверяемое подключение к каноническим UDS,
клиентский перехватчик issuer и серверный перехватчик целевого сервиса.
Серверный перехватчик требует одновременно проверенный mTLS peer и ровно один
контекст авторизации, передаёт локальному verifier фактический полный метод и
SHA-256 сертификата и допускает обработчик только с нейтральным проверенным
контекстом. Соответствие идентификатора операции конкретному RPC и получение
authority proof остаются связкой конкретного сервиса и не входят в общую
библиотеку.

Для server-stream RPC с единственным initial request сервис передаёт exact
method в optional requestBoundMethods обоих stream interceptors. Закрытый
machine profile такого RPC использует UNARY_PROTO_SHA256: digest относится
к единственному initial Proto, а ответ может состоять из bounded chunks.
Client не открывает stream до SendMsg/proof issuance. Server не публикует
проверенный context и не отдаёт bytes до декодирования и verification initial
request. Остальные методы сохраняют STREAM_SESSION; caller metadata не
выбирают режим. EOF/error закрывает child context; владелец вызова обязан
передать bounded context и отменить его при прекращении чтения.

Сгенерированные файлы не редактируются вручную. Гарантии транспортного
контракта, UDS, политики, ротации и семантики отказов задает `CONTRACT-MC-004`
(`contracts/authorization/README.md`).

Пакет `contract` проверяет скомпилированные дескрипторы issuer/verifier,
нециклический authority-proof resolver и restore controller, точные поля
запроса и зарезервированное поле полномочий, двоичный круговой проход,
сопоставление enum, безопасные целочисленные ревизии JSON и полную матрицу
ошибок. Эти тесты являются доказательством контракта и не подменяют
исполняемую реализацию.

`authorityclient.LocalAuthorityError.Diagnostic()` возвращает закрытые локальные
стадии выдачи proof/context, исходный нормализованный gRPC code и известные enum
`AuthorizationErrorDetail`. Локальный `Error()` содержит только эту классификацию;
`GRPCStatus()` сохраняет прежний безопасный status. Сообщение зависимости,
correlation из detail, credential, request и unknown fields не копируются.
`NewProofFailure` позволяет provider отметить отказ до обращения к issuer:
`proof_operation`, `grant_missing`, `grant_read`, `proof_resolve`, `proof_response`.
Диагностика не является разрешением, подтверждением эффекта или причиной retry.
Существующий bounded proof retry и запрет downstream без context сохраняются
для unary, stream, request-bound stream и continuation.
