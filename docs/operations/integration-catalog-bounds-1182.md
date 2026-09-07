---
id: OPS-DOC-1182
title: Точные числовые границы каталога интеграций
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Границы каталога интеграций

Refs #1182, #1183. SHIPPED GitLab `pipeline_id` в двух capability inputs
имеет maximum `9223372036854775807`. CP сохраняет int64 в package → PostgreSQL
capabilities JSON → entity → Proto. Прежний HTTP mapper отклонял этот metadata
bound как небезопасный JSON number, выдавая 500 всему каталогу. PLATFORM reload
не мог завершиться; realtime readiness и создание диалога оставались закрыты.

`IntegrationConfigurationField.minimum/maximum` теперь используют
`IntegrationIntegerBound`: безопасное целое JSON number либо точную десятичную
строку для значения вне диапазона ±9007199254740991. Строка каноническая,
без exponent, ведущего плюса или лишних нулей, в диапазоне signed int64.
Один mapper обслуживает HTTP и ProtoMap realtime; поле и исходный диапазон
не удаляются, не округляются и не ограничиваются меньшим значением.

Исключение применяется только к этим двум metadata fields. Версии, counters,
authority pins и прочие int64 сохраняют прежний safe-number guard. Повреждённый
int64 закрыто отклоняется. Presence flags отличают отсутствующую границу от
нуля. HTTP не меняет actor, tenant, grants, события или cursor semantics.

Потребитель PWA #1183 сравнивает bounds через BigInt и не вызывает Number для
большой строки. Это не расширяет INTEGER input payload: connection configuration
по-прежнему принимает только предусмотренные безопасные JSON integer values.
`IntegrationPackageField` использует отдельный package schema и не получает
новый формат автоматически.

Regression использует реальные tracked SHIPPED inputs, проверяет весь HTTP
catalog и realtime projection, signed int64 extrema, safe-range edges,
повреждённые строки и сохранность version guard. Официальная семантика decimal
strings подтверждена через Context7 Protobuf-ES и
[ProtoJSON specification](https://protobuf.dev/programming-guides/json/).

Ручная проверка после обоих unit: GET integration-definitions возвращает200,
PLATFORM reload достигает live, обе кнопки нового диалога доступны при
authoritative READY/CREATE_CONVERSATION. Отдельно проверить безопасное INTEGER
значение при большом maximum и отказ недопустимому payload. Live до развёртывания
и отдельного запуска остаётся NOT RUN.
