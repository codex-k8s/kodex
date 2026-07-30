# Internal RPC authentication

Модуль содержит сгенерированный Go wire contract
`internalrpcauthority.v1`. Исходный Proto находится в
`contracts/proto/internalrpcauthority/v1` и изменяется только там.

Пакет `authorityclient` предоставляет общий transport boundary для
application-контейнеров: проверяемое подключение к каноническим UDS,
issuer client interceptor и target server interceptor. Target interceptor
требует одновременно проверенный mTLS peer и ровно один authorization context,
передаёт локальному verifier фактический full method и SHA-256 сертификата и
допускает handler только с нейтральным verified context. Соответствие
operation ID конкретному RPC и получение authority proof остаются
service-specific wiring и не входят в общую библиотеку.

Сгенерированные файлы не редактируются вручную. Гарантии wire contract, UDS,
policy, rotation и failure semantics задает `CONTRACT-MC-004`
(`contracts/authorization/README.md`).

Пакет `contract` проверяет compiled descriptors issuer/verifier,
нециклического authority-proof resolver и restore controller, exact request
fields и reserved authority field, binary round-trip, enum mapping, JSON
safe-integer revisions и полную error matrix. Эти тесты являются contract
evidence и не подменяют будущую runtime-реализацию.
