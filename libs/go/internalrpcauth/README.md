# Авторизация внутренних RPC

`policydeclaration.ProofProducer` задаёт единственную форму полей JSON декларации
производителя доказательств для существующих writer/reader машинной политики.
Это техническая форма данных, не проверенная личность, credential или grant.
Генератор, authority loader и доменный resolver сохраняют собственные nominal
типы на этой форме, прежние envelope versions, tags и отсутствие `omitempty`.
Списки остаются обычными изменяемыми slices: value copy не делает deep copy.
Допустимые issuer/audience, маршруты, permission matrix, TTL, retry, подписи и
eligibility проверяют прежние потребители; тип не задаёт бизнесовых значений.
Production-подпакет — чистая форма без импортов и криптографических зависимостей.
Он не читает env, не создаёт goroutine/метрики, не делает I/O, retry или
состояния активации. Ошибки syntax/canonical/unknown/duplicate JSON по-прежнему
принадлежат выбранному strict decoder, а semantic rejection — владельцу
профиля. Поля и их сериализация неизменны; нового compatibility decoder нет.

`policydeclaration.RequestProfile`, `ContinuationProfile` и `LocalPeer` тем же
способом фиксируют общие формы полей применимости запроса, точного родителя
продолжения и настроенных Unix UID/GID. Все три потребителя сохраняют свои
номинальные типы. Общая форма не задаёт defaults, не принимает родительский
контекст, не наблюдает peer credentials и не выдаёт права делегирования.
Валидация значений, рёбер и транспорта остаётся на прежних границах.

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

## Явный транспорт доверенного кластера

`serviceidentity.NewTrustedCluster` требует профиль `trusted-cluster` и закрытый
набор caller/method bindings. `UnaryServerInterceptor` и
`StreamServerInterceptor` передают допуск через `serviceidentity.FromContext`;
поток проверяется до первого чтения сообщения. Обёртка сохраняет cancellation
и deadline исходного transport context, не запускает фоновых процессов.

Этот допуск не является криптографической workload identity и не назначает
actor/tenant/project из payload. Composition root выбирает профиль явно;
NetworkPolicy ограничивает точные пары workload, а принимающий домен проверяет
пользовательскую сессию и владение ресурсом. Заголовок trusted profile не включает
этот режим на защищённом сервере. Legacy authorization metadata, неизвестные
методы и неоднозначные caller/profile отклоняются закрыто.
