---
id: EXT-MC-003
title: Integration gateway
type: service
status: approved
owner: backend
version: 2.3.0
updated: 2026-09-05
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
- электронная почта: health, status и отправка текстового письма через
  provider-neutral HTTPS bridge с provider-native idempotency;
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
Любая provider mutation, включая `PROVIDER_NATIVE`, автоматически не повторяется
после неоднозначного сетевого исхода; immutable invocation receipt защищает от
повторного выполнения уже подтверждённого effect. Email bridge обязан принимать
`Idempotency-Key` и возвращать один provider receipt для exact retry.

Потеря ответа, повреждённый успешный ответ или истечение mutation lease
сохраняют `UNKNOWN_OUTCOME` в PostgreSQL. Такой invocation никогда не возвращается
в `READY`; новый worker не повторяет внешний эффект. GitHub create/comment,
Synthetic и email пытаются сверить эффект только через чтение. Если сверка не
подтверждена, MCP возвращает `INTEGRATION_OUTCOME_UNKNOWN` и
`owner_decision_required=true`. Этот исход не является успехом или отсутствием
эффекта. Контракт bridge находится в `contracts/openapi/email-bridge/v1`;
его POP/SMTP реализация принадлежит #1037.

Полный закрытый набор MVP-UI-42 находится в [OPERATION_MATRIX.md](OPERATION_MATRIX.md).
`EFFECT_KEY` новых vendor-команд означает durable дедупликацию invocation у
control-plane, а не вымышленную поддержку idempotency header провайдером.
Файлы и ответы SDK/HTTP ограничены 64 KiB до декодирования; итоговая JSON/base64
проекция также входит в этот бюджет. Большие результаты отклоняются без выдачи
частичного файла. GitHub Contents API возвращает каталог без pagination;
страницы остальных списков используют provider cursor. Jira transitions,
links/attachments и exact Confluence space возвращают ограниченный полный набор.

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

Версии этого расширения: GitHub `2.2.0`, GitLab/Jira/Confluence `1.2.0`.
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

`TestEveryAdvertisedOperation` вызывает каждую из 121 executable capabilities
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
