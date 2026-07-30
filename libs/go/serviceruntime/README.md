# `serviceruntime`

Общий process lifecycle для Go deployables MatterCodex.

Публичный API предоставляет атомарную readiness, worker group с явным
cancel/join и последовательность shutdown-операций с независимыми bounded
contexts. Библиотека не читает конфигурацию, не создаёт root context, не
открывает зависимости и не логирует ошибки.
