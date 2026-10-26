---
id: OPS-DOC-GATE-CONSEQUENCES-1928
title: Последствия решения Human Gate
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-27
---

# Последствия решения Human Gate

Исправление [#1928](https://github.com/codex-k8s/kodex/issues/1928) уточняет
`decisionConsequences` при публичном чтении OPEN Gate. Решение, права, OCC,
attempt, lease и внешние действия остаются у существующих owner-команд.
Прогноз читается после проверки видимости Gate в том же snapshot владельца.
Для SERVER используется опубликованный план, точный producer через WAITING_FOR,
канонические условия accepted attempt и pending work. Ссылки на другие Gate,
внутренние receipts и execution provenance в ответ не добавляются.

| Состояние | APPROVE: terminalForRun | Основание |
| --- | --- | --- |
| SERVER recovery, включая attempt 0 и 2–4 | false | Продолжение либо свежая попытка; принятие результата не подменяется |
| SERVER result Gate, остались непринятые шаги | false | Отсутствие физических узлов не доказывает завершение |
| SERVER result Gate, другой OPEN Gate | false | Соседнее решение ещё требуется |
| SERVER result Gate, pending integration/UNKNOWN/callback/continuation | false | Канонический барьер владельца ещё не пройден |
| Последний SERVER result Gate | true | Все последние попытки приняты, после точного одобрения нет других blockers |
| Integration или delivery | false | Решение разрешает отдельное действие, а не завершение root |
| Legacy ordinary | Прежний прогноз | Сохраняется фактический legacy transition |
| Закрытый Gate | Сохранённый исход | Последующее изменение root не переписывает историю решения |

REJECT/CANCEL и доступные решения не изменяются. Исчерпанная recovery attempt
не получает нового APPROVE. Неоднозначная identity производителя, повреждённый
план либо ошибка чтения не превращаются в положительное обещание завершения.
Предварительно сохранённые consequences остаются неизменными.

Путь: `GetOwnerGate`/graph/event projection → owner scope → intent projection →
OPEN semantic predicate → существующий gRPC/HTTP mapping → пользовательский
preview. Публичная схема и PWA не меняются. Owner resolver повторно проверяет
свою актуальную authority и версию; прогноз не является полномочием исполнения.

Локальная unit-регрессия проверяет настоящий `projectGateIntent`, включая
legacy, закрытую историю, финальное принятие и отрицательные SERVER-состояния.
Подготовленные native-регрессии читают Gate через `Service.GetOwnerGate` после
штатных create/publish/launch/claim/complete; бизнес-строки SQL не создаются.
`TestOwnerGateServerCompletionComponent` сравнивает прогноз двух result Gate
с фактическими owner APPROVE и SUCCEEDED. Recovery и integration проверяются
также существующими lifecycle fixtures. Запуск native без DSN означает SKIP,
а не доказательство работы PostgreSQL.

Ручная проверка на отдельно разрешённой среде: прочитать recovery Gate и
убедиться, что APPROVE обещает продолжение; после отдельного разрешения
подтвердить решение и проверить fresh attempt. Для двух result Gate первое
одобрение не завершает root, последнее допускает SUCCEEDED. Повторное чтение
первого закрытого Gate сохраняет его прежний nonterminal outcome. Эта инструкция
сама не разрешает live-запуск или решение Gate.

Rollback — возврат приложения без DDL. Он вернёт прежний неточный OPEN preview,
но не изменит authority, историю решений и исполнение процесса. Локальные
проверки не заменяют независимое review и readback на staging.
