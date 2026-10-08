---
id: EXT-MC-003
title: Integration gateway
type: service
status: approved
owner: backend
version: 2.6.0
updated: 2026-10-08
---

# integration-gateway

`integration-gateway` — stateless worker типизированных внешних capabilities.
Метаданные подключений, grants, leases, результаты и audit принадлежат
`control-plane`. Пустой набор подключений является штатным состоянием и не
влияет на readiness.

Юнит не предоставляет универсальный proxy. Один schema-versioned YAML package
определяет adapter, configuration fields, capabilities, operation, risk,
approval policy, input и resource scope. Gateway принимает только совпадающие
`definition_version`, `definition_digest`, grant scope и immutable input
digest.

UI/Git-managed packages передаются внутри каждой protected claim и проверяются
по точным key/version/digest и compiled executable baseline. Они не заменяют
общий registry других подключений. Deadline, attempts и Human Gate учитывают
выбранную revision. Git source worker читает pinned commit/regular blob,
проверяет ancestry/digests и возвращает typed completion владельцу.
Полная карта находится в
[integration-configuration-sources-1028.md](../../../docs/operations/integration-configuration-sources-1028.md).
Git write-back и итоговый owner/profile цикл требуют отдельного завершения
в этом же unit; наличие read consumer не означает готовность всего CFG.

Поставляются семь schema-versioned packages:

- synthetic HTTP journal: read и идемпотентный write по exact effect key;
- GitHub: 41 операция repository content, branches/commits, issues/comments,
  PR/reviews/checks и Actions только в exact `owner/repository` scope;
- GitLab: 37 операций project/repository, branches/commits, issues/notes,
  MR/discussions, pipelines/jobs в exact `base_url/project_path` scope;
- Jira: 22 операции project/users, JQL/issues, transitions/comments/links и
  attachments в exact `base_url/project_key` scope;
- Confluence: 16 операций space/pages/descendants, footer comments и attachments
  в exact `base_url/space_id` scope;
- электронная почта: 21 операция health/status, папок, сообщений, threads,
  вложений, флагов, перемещения, черновиков и SMTP-отправки через типизированный
  HTTPS bridge; POP3 предоставляет только ограниченный compatibility mode;
- Mattermost остаётся за отдельным необязательным `interaction-gateway`.

Package также объявляет типизированные output fields, exact network
destinations и health operation. Универсального HTTP passthrough нет: неизвестная
операция, поле, adapter, resource scope или provider response отклоняется до
выдачи результата.

Credential claim содержит только revision ref, Kubernetes Secret
`namespace/name#key`, Secret UID, `resourceVersion` и content SHA-256. Credential
читается из server-mounted Secret непосредственно перед provider-вызовом,
проверяется по digest и не возвращается в API, логи, audit или result.

Все внешние HTTPS-вызовы идут через `egress-gateway`. Configured `base_url`
принимается только как HTTPS origin без userinfo, query, fragment, IP literal и
нестандартного порта. Оператор установки обязан материализовать каждый exact
FQDN в policy egress gateway; отсутствие host в policy является штатным
fail-closed отказом подключения. Redirect запрещён.

READ повторяется только на bounded network/`429`/`502`/`503`/`504` отказах.
Для GitHub сырой успешный SDK-ответ ограничен 2 МиБ до декодирования,
ошибочный — 64 КиБ; безопасная итоговая проекция по-прежнему ограничена
64 КиБ и проверяется package output schema. `github.pull_request.list`
возвращает компактный указатель с number/title/state/head/base/SHA/draft/URL.
Полное описание читается через `github.pull_request.read`; create/update
также сохраняют полный body. Страница не усекается и не меняет `per_page`
или `next_cursor`: слишком большой ответ закрыто отклоняется. Локальный
`INTEGRATION_RESPONSE_INVALID` не маскируется под недоступность провайдера
и не повторяется. Это не расширяет resource scope, grants или сетевые права.

Любая provider mutation, включая `PROVIDER_NATIVE`, автоматически не повторяется
после неоднозначного сетевого исхода; immutable invocation receipt защищает от
повторного выполнения уже подтверждённого effect. Email bridge обязан принимать
`Idempotency-Key` и возвращать один provider receipt для exact retry.

Потеря ответа, повреждённый успешный ответ или истечение mutation lease
сохраняют `UNKNOWN_OUTCOME` в PostgreSQL. Такой invocation никогда не возвращается
в `READY`; новый worker не повторяет внешний эффект. Acceptance после потери
worker проверяет durable `UNKNOWN_OUTCOME`, сохранённый intent и отсутствие
повторного provider effect. GitHub create/comment, Synthetic и email сверяют
эффект только через чтение. Если сверка не подтверждена, MCP возвращает
`INTEGRATION_OUTCOME_UNKNOWN` и
`owner_decision_required=true`. Этот исход не является успехом или отсутствием
эффекта. Контракт bridge находится в `contracts/openapi/email-bridge/v1`;
его POP/SMTP реализация принадлежит #1037.

Полный закрытый набор MVP-UI-42 находится в [OPERATION_MATRIX.md](OPERATION_MATRIX.md).
`EFFECT_KEY` новых vendor-команд означает durable дедупликацию invocation у
control-plane, а не вымышленную поддержку idempotency header провайдером.
Ответы HTTP остальных провайдеров и итоговая безопасная JSON-проекция
ограничены 64 КиБ. GitHub SDK использует отдельный сырой бюджет, описанный
выше; это не увеличивает допустимый размер итогового результата.
Большие результаты отклоняются без выдачи
частичного файла. GitHub Contents API возвращает каталог без upstream pagination;
adapter выдаёт его проверенный индекс страницами по offset, как описано ниже.
Страницы остальных списков используют provider cursor. Jira transitions,
links/attachments и exact Confluence space возвращают ограниченный полный набор.

`github.repository.content.read` проверяет полный UTF-8 файл до 1 МиБ,
но возвращает только страницу до 16 КиБ с отдельным receipt, Git blob SHA,
SHA-256 источника и страницы, byte offset, next и EOF. Native MCP envelope
сохраняет общий предел 64 КиБ, итоговая проекция — 64 КиБ, сырой SDK-ответ —
2 МиБ. `ref` закрепляет commit, continuation требует `expected_sha`;
проверяются весь UTF-8, NUL и Git blob SHA до выдачи первой страницы.
Файлы больше 1 МиБ, `encoding: none` и повреждённые источники отклоняются без
partial-success, download URL, redirect, raw fallback или автоматического retry.
Лимиты `size/offset_bytes/next_offset_bytes` совпадают с generated package.

Сквозной путь не меняется: actor/grant runtime revision → native MCP invocation
→ control-plane claim с exact definition/version/digest и immutable input
→ integration-gateway → SDK Contents API разрешённого repository/commit/path
→ проверенная bounded страница и receipt → owner completion/audit/event
→ исходный runtime consumer. `READ_ONLY/NONE`, tenant/repository authority,
idempotency, leases, grants и события остаются прежними. Новый package `5.0.0`
подключается новой owner revision с явным rebind; уже закреплённые revisions
не переинтерпретируются и не получают новые лимиты молча.

`github.repository.content.list` читает только immediate children закреплённого
repository/path на обязательном 40 lowercase hex commit `ref`. Весь ответ
Contents API проверяется до страницы: SHA, тип, размер, граница каталога и
уникальность путей; порядок канонический по path. Каталог с 1000 и более
элементами отклоняется: upstream cap не доказывает полноту, скрытого Trees
fallback нет. Сырой SDK budget и canonical полный индекс ограничены 2 МиБ.
`catalog_digest` SHA256 связывает
полный проверенный индекс с repository, path и commit; `total_count` не больше 999.
`cursor` — offset элементов от нуля, `limit` по умолчанию 20 и максимум 50.
Продолжение требует прежний `expected_catalog_digest`; mismatch закрыто
отклоняется. Страница адаптивно сокращается по окончательному native MCP
envelope с обоими представлениями и JSON escaping до 64 КиБ. `next_cursor`
равен offset плюс фактически выданный count и присутствует только до `eof`.
Exact offset == total_count возвращает пустой EOF; больший offset отклоняется.
Малый count не означает EOF. EOF индекса не подтверждает чтение исходников:
содержимое каждого нужного blob читается отдельно через content.read.

Package 5.0.0 имеет новый несовместимый LIST contract и не принимает исторический
4.0.0 как текущий. CP и gateway обновляют compiled package совместно; forward
owner publication/rebind, fresh TEST и новые runtime snapshots требуются до
следующего запуска. Apply миграций, runtime-controller/runner ABI, grants и
network destinations этим изменением не меняются.

`github.pull_request.read` сохраняет полное описание PR и возвращает точные
`head_sha`, `base_sha`, `changed_files`. `github.pull_request.file.list`
возвращает только метаданные, без patch или исходного текста: до четырёх файлов
на страницу с обязательными ожидаемыми SHA и общим числом изменённых файлов.
До и после чтения проверяются неизменность PR и согласованность provider cursor.
Предел GitHub в 3000 файлов не превращается в частичный успешный индекс.
Полный индекс до EOF не заменяет чтение исходников закреплённых commit/blob.

Страница текста адаптивно сокращается с учётом JSON escaping и UTF-8 границ,
сохраняя точный offset и хеш всего источника. Проверка native envelope относится
к закреплённому генератору обычных идентификаторов вызова, а не гарантирует
размер произвольного JSON-RPC ответа с неограниченным caller ID. Общие wire
guards не ослаблены. Явно заданный меньший `maximum_bytes` сохраняется.

Jira users ограничены assignable users выбранного проекта, без email/address
профиля пользователя. JQL не может выйти из project-условия; верхнеуровневый
`ORDER BY` сохраняется. Attachments и links разрешаются через issue, а не через
глобальный ID. Confluence comments/attachments разрешаются через страницу и
точное пространство; reply сначала разрешает parent comment. Скачивание
Confluence использует только проверенный same-origin download path. Ответы CDN
с redirect закрыто отклоняются, новые внешние назначения не разрешаются скрыто.

GitHub workflow dispatch принимает `workflow_inputs`: JSON-объект не более
25 строковых/числовых/boolean значений, без вложенных объектов и duplicate keys.
Это параметры закреплённого workflow, а не произвольное тело HTTP API.
Пустые file/body поля явно отмечены `allowEmpty`; MCP JSON Schema использует
`minLength: 0` только для них. Идентификаторы и connection config остаются непустыми.

## Обновление каталога

Текущие версии: GitHub `5.0.0`, GitLab/Jira/Confluence `1.2.0`, Email `1.4.0`,
Mattermost `2.2.0`, Synthetic `3.1.0`.
Публикация новых packages не расширяет существующие grants автоматически.
Старая pinned revision не переинтерпретируется: владелец публикует новую
UI/Git-managed ревизию, явно выполняет rebind/test и выбирает новые capabilities.
Git-owned конфигурация по-прежнему не перезаписывается через UI.

Runtime deployment, exact egress, secret mounts, probes, RBAC и метрики
наследуются от foundation и проверяются итоговым render. Расширение не добавляет
сетевые назначения, CP RPC, миграции или отдельный worker. Mail и Mattermost
packages этим изменением не меняются.

## Проверка расширения

Из каталога сервиса: `go test -race ./...` и `go vet ./...`.
Из `libs/go/integrationpackage`: `go test -race ./...`.
Из корня: `make check-integration-package-codegen test-integration-gateway-render
test-integration-synthetic test-integration-gateway-postgres`.
PG-цель включает подготовку provider capacity: одиночный regex `integration`
не воспроизводит её fixture и не является поддерживаемой точкой входа.

`TestEveryAdvertisedOperation` вызывает каждую из 139 executable capabilities
текущего каталога, включая неизменённые email/Synthetic. Отдельные проверки
покрывают scope до credential, version/risk/approval/input mismatch, потерю
mutation response, 5xx/denial/malformed success, rate limit, pagination,
empty files, escaping, JQL, parent scope и bounded bodies. Повторный разбор
неизменяемого shipped fixture не выполняется для каждого отрицательного случая;
каждый adapter/client/credential остаётся изолированным.

Через Context7 проверены go-github, GitLab REST, Jira REST v3 и Confluence REST v2.
При недостаточном фрагменте Context7 дополнительно прочитаны официальные
[GitHub workflows](https://docs.github.com/en/rest/actions/workflows),
[Jira attachments](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-attachments/),
[Confluence comments](https://developer.atlassian.com/cloud/confluence/rest/v2/api-group-comment/).
Сигнатуры SDK сверены с закреплённым `go-github/v74`, версия зависимости не менялась.

UI revision использует тот же `IntegrationPackage` JSON/YAML, что shipped
catalog. Публикация и rebind допускают только exact зарегистрированный и ready
package digest, а не произвольные operation names. Новый исполняемый профиль
поставляется вместе с adapter, схемой и fake-provider проверкой. Mattermost
виден в catalog metadata, но generic execution и credential route ему не
принадлежат.

Worker получает по одному claim, ограничивает provider phase двадцатью
секундами и сохраняет отдельный бюджет завершения внутри 30-секундной lease.
Счётчики `cycles_total` и `operations_total` учитывают результат adapter до
записи receipt, включая частичный цикл и unknown outcome.

READ invocation может быть claim-нут сразу. WRITE, SENSITIVE и DESTRUCTIVE
сначала атомарно создают отдельный Human Gate и остаются недоступны worker до
`APPROVED`. Успешный внешний effect завершается immutable receipt с exact
effect key, input digest, provider effect ref и response digest; повторное
завершение допустимо только как exact readback той же receipt.

`/healthz` отражает жизнь процесса, `/readyz` читает локальный снимок sidecar
authority. Доступность control-plane и внешних систем наблюдается отдельным
рабочим/diagnostic контуром и не меняет Kubernetes readiness pod.

## Disposable synthetic fixture

Бинарь `cmd/integration-synthetic` является только локальной E2E-оснасткой и
не входит в `web-only`, `web-with-mattermost`, staging или production render.
`tools/dev/render-local.sh` добавляет его отдельным overlay в `kodex-system` и
запускает через общий hot-reload runner.

Fixture поддерживает только закрытый контракт:

- `GET /healthz` и `GET /readyz`;
- `GET /v1/journals/{journal}` без изменения состояния;
- `POST /v1/journals/{journal}/entries` со строгим JSON `{"value":"..."}` и
  обязательным `Idempotency-Key`.

Journal ограничен 120 байтами, value — 4096 байтами, body — 8 KiB. Неизвестные,
повторяющиеся или дополнительные JSON fields отклоняются. Один ключ и тот же
request возвращают сохранённый provider readback; тот же ключ с другим journal
или value получает `409` без эффекта. Состояние ограничено, потокобезопасно и
существует только в течение жизни одного disposable процесса.

`make test-integration-synthetic` выполняет race-тесты fixture и synthetic
adapter, проверяет exact local NetworkPolicy и доказывает отсутствие Deployment
в release profiles. PostgreSQL component test `TestBootstrapComponent` содержит
lifecycle-сценарии READ без gate, WRITE до Human Gate без claim, REJECT без
effect receipt, APPROVE с одной receipt и exact retry/readback без нового claim.
