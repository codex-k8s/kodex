---
id: OPS-INTERACTION-1030
title: Доставка Mattermost и привязка внешнего пользователя
type: operational-contract
status: approved
owner: platform
version: 1.1.0
updated: 2026-09-05
---

# Контракт #1030

Источник: эпик #1018, `MVP-UI-42`, Issue #1030. Владельцем подключения,
пользовательской привязки, grants, receipts и переходов Run остаётся
`control-plane`; `interaction-gateway` исполняет только точные Mattermost
операции. Значения credentials в документе отсутствуют.

## Карта доставки

| Фаза | Инициатор и полномочия | Команда и состояние владельца | Результат и потребитель |
| --- | --- | --- | --- |
| Создание | Core lifecycle и активный connection grant | Сервер создаёт delivery в своей транзакции | Авторитетная очередь control-plane |
| Claim | Проверенный workload interaction-gateway | `ClaimInteractionDeliveries`, точный lease/fence/generation | Одна доставка непосредственно перед исполнением |
| Подготовка | Только выданный connection snapshot | Exact credential file, HTTPS origin, team/channel lookup | Нет внешнего изменяющего действия |
| Отправка | Активная арендованная попытка | Mattermost `POST /api/v4/posts` в точном канале | Только HTTP 201 и совпадающие post/channel/thread образуют success |
| Completion | Тот же workload и lease | `CompleteInteractionDelivery`; idempotency key включает lease и generation | `SUCCEEDED`, `FAILED` при confirmed-no-effect либо `UNKNOWN_OUTCOME` |
| Истечение | Время БД владельца | Неоконченная claimed delivery становится `UNKNOWN_OUTCOME` | Не возвращается в автоматическую очередь отправки |
| Сверка | Авторитетный read path control-plane | Incident и сохранённый delivery outcome | Ошибка optional delivery не меняет core Run на FAILED |

Для перечисленных delivery-переходов отдельное доменное событие consumer не
требуется: очередь и incidents читаются через защищённые RPC владельца.
События core Run не подменяются событиями доставки Mattermost.

Тайм-аут или HTTP 5xx после попытки отправки не доказывает отсутствие post.
Только ошибка до отправки либо документированный отказ HTTP 400/401/403/404/
413/429 разрешает отметить confirmed-no-effect. Неизвестный результат и
несовпадение readback не превращаются в success.

Внешний вызов ограничен меньшим из срока цикла и lease с резервом на
completion. Gateway не арендует сразу несколько последовательных сообщений,
которые могли бы протухнуть, ожидая предыдущую отправку.

## Сеть и входящие события

Все HTTP-запросы идут через deployment-owned egress proxy и TLS 1.3 к точному
origin из allowlist. Перенаправления запрещены, ответ ограничен 4 MiB,
WebSocket frame ограничен 1 MiB, входящий текст ограничен 16 KiB.
Названия и идентификаторы team/channel сверяются с authoritative vendor
readback. Событие WebSocket подтверждается `GetPost`: другой канал, автор,
thread, изменённый текст либо удалённый post не принимаются.

Замена версии подключения или immutable credential descriptor отменяет старый
listener даже при прежнем имени Secret. Удаление подключения также отменяет
listener. Новое поколение,
включая быстро отменённое промежуточное, ждёт завершения всей цепочки
предшественников. При shutdown SDK reader дренируется до закрытия каналов.

ACK создаётся `control-plane` атомарно с acceptance receipt. Listener только
передаёт проверенное сообщение владельцу и не создаёт отдельный post.
`mattermost.acknowledgements` является внутренней delivery capability, а не
доступным агенту инструментом. Claim содержит acceptance receipt и exact
team/channel/root. Gateway сверяет канал и читает root перед отправкой, а
completion передаёт team/channel/post/thread владельцу. Повреждённый readback
после возможной отправки закрывается как `UNKNOWN_OUTCOME`.

Отклонённое владельцем сообщение не создаёт ACK и не разрывает подписку
канала: неправильный input, недоступный resource, отсутствие permission и
неприменимое состояние относятся к конкретному сообщению. Ошибка аутентификации
workload или недоступность владельца по-прежнему переводит listener в degraded.

## Human Gate

Серверная delivery несёт `gate_ref`, `gate_version` и `run_ref`. Gateway пишет
эту связку в props bot post и сверяет полный success readback. Версия
передаётся десятичной строкой, чтобы JSON не терял точность `int64`.

Для ответа gateway сначала читает сам post, затем root post через Mattermost
API. Root должен принадлежать тому же каналу и текущему bot user, не быть
удалённым или вложенным ответом. Props обычного пользователя не превращаются
в gate context. Полученные team/channel, digest внешнего пользователя и точная
gate/run/version передаются в `AcceptInteractionMessage`.

Владелец разрешает активную server-owned `InteractionIdentity`, связанную с
версией подключения, в субъект Kodex. Его `gate.resolve` или `agent.launch`/
`workflow.launch` проверяются на конкретном ресурсе; payload не содержит
самоназначенного actor. Gate decision дополнительно должен совпасть с
серверной delivery, gate/run/version и one-winner/OCC-переходом владельца.
Повторное событие возвращает receipt, а не запускает новый Run.

Административная привязка выполняется отдельными командами
`BindInteractionIdentity`/`RevokeInteractionIdentity`, читается через
`ListInteractionIdentities`. HTTP/PWA-поверхность этих новых producer-команд
подключается в зависимых unit и пока не объявляется готовой этим checkpoint.

## Типизированные операции MCP

Пакет Mattermost `2.2.0` содержит 18 capabilities. Две системные подписки
`mattermost.inbound` и `mattermost.gate_decisions` не доступны как вызываемые
агентом MCP tools. Остальные 16 операций исполняются только владельцем
`interaction-gateway`:

- чтение команды, канала и участников;
- список, чтение и поиск posts, чтение threads;
- список вложений и ограниченное чтение file ranges;
- чтение, добавление и удаление reactions;
- отправка, notification, result mirror и обновление собственного bot post.

Каждая попытка сверяет version/digest пакета, exact scope, canonical input
digest, risk и approval policy. Ответ проверяется по schema и связывается
с effect key, input digest и response digest. Изменяющая операция без
подтверждённого результата завершается `UNKNOWN_OUTCOME`.

Поиск принудительно ограничен каналом и не принимает пользовательские search
operators. File download требует attachment membership в предварительно
прочитанном post и exact Content-Range; публичные ссылки не выдаются. Агент
не может изменить bot post, содержащий Human Gate. Credential читается из
точного read-only Secret key с проверкой content digest и ограниченным
временем ожидания проекции.

Connection tests и invocations арендуются по одной непосредственно перед
исполнением. Completion сохраняет lease/fence/generation и резерв времени;
receipt от другого effect/input не принимается.

## Проверка checkpoint

Локальная точка входа из каталога unit:

```bash
go test ./... -count=1 -race -timeout=90s
go vet ./...
go build ./...
```

Тесты проверяют exact team/channel, отсутствие redirect, ограниченный body,
HTTP success/error/timeout, lease deadline, отдельную identity каждой попытки,
readback без `core_run_affected` и последовательную смену WebSocket listeners. Сетевой
WebSocket fixture работает только на loopback без реальных credentials.

Это промежуточная реализация полного unit, не объявление готовности #1030.
Локальный component fixture соединяет claim validation, exact credential,
официальный SDK, HTTP provider responses и effect receipt; отдельно проверяет
ACK и readiness подключения. Fixture подменяет только HTTP transport, не
отключает scope/schema/credential проверки. Kubernetes readiness сохраняет
workload-local границу `GUIDE-DOC-003`; доступность конкретного подключения
проверяется реальной typed connection-test операцией.

Целевой PostgreSQL-прогон включает health routing, identity/revoke, durable ACK,
UNKNOWN_OUTCOME и exact workload для connection tests. Зависимые HTTP/PWA
управления identity и финальная общая приёмка остаются отдельными unit эпика.
Live Mattermost и staging не запускались.

Проверена официальная спецификация
[Mattermost posts API](https://github.com/mattermost/mattermost-api-reference/blob/master/v4/source/posts.yaml)
и установленный официальный Go SDK `server/public v0.4.3`.
Context7 не вернул описание требуемой гарантии идемпотентности; гарантия
дедупликации `CreatePost` не предполагается.
Для file ranges дополнительно проверен официальный
[WriteFileResponse](https://github.com/mattermost/mattermost/blob/master/server/platform/shared/web/files.go),
использующий `http.ServeContent`; adapter требует точного Content-Range.
