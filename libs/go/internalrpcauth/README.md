# Internal RPC authentication

Модуль содержит сгенерированный Go wire contract
`internalrpcauthority.v1`. Исходный Proto находится в
`contracts/proto/internalrpcauthority/v1` и изменяется только там.

Public surface этого contract SHA намеренно ограничен wire contract. Runtime
API, strict JOSE wrapper и persistent adapters этим SHA не заявлены готовыми и
не представлены заглушками.

Сгенерированные файлы не редактируются вручную. Гарантии wire contract, UDS,
policy, rotation и failure semantics задает `CONTRACT-MC-004`
(`contracts/authorization/README.md`).

Пакет `contract` проверяет compiled descriptors, exact request fields и
reserved authority field, binary round-trip, enum mapping, JSON safe-integer
revisions и полную error matrix. Эти тесты являются contract evidence и не
подменяют будущую runtime-реализацию caster/issuer/verifier.
