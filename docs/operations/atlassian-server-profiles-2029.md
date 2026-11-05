---
id: RUN-INT-SERVER-2029
title: Опциональные Server/DC-профили Jira и Confluence
type: operations
status: approved
owner: backend
version: 1.0.0
updated: 2026-11-05
---

# Опциональный Server/DC

Источник: [#2029](https://github.com/codex-k8s/kodex/issues/2029), в составе
приёмки [#1830](https://github.com/codex-k8s/kodex/issues/1830).
`jira-server-dc@1.0.0` и `confluence-server-dc@1.0.0` — отдельные определения
ограниченной первой вертикали. Существующие `jira` и `confluence` используют
Cloud API и сохраняют прежние версии, digests и поведение. Тип размещения
выбирает владелец подключения; ошибочный endpoint не переключает его молча.

## Контракты и происхождение

Спецификации находятся в `contracts/integrations/providers/<key>/v1/openapi.yaml`.
Это самостоятельно описанный поднабор по официальной документации
[Jira 9.12.8 REST v2](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.8/)
и [Confluence 8.5.9 REST v1](https://docs.atlassian.com/ConfluenceServer/rest/8.5.9/).
Официальная полная OpenAPI-схема не заявляется и не копируется. Примеры
синтетические; `x-kodex-provenance` фиксирует происхождение и покрытие.
Совместимость других версий и особенности конкретных лицензий требуют
отдельной проверки. Контракты не дают прав и не включают arbitrary HTTP tool.

Оба OpenAPI проверяются `kin-openapi@v0.149.0` с запретом внешних `$ref` и
бюджетом 256 KiB. Синтетические HTTP-прогоны сверяют реальные native requests
и responses со спецификациями. Типизированные packages в
`contracts/integrations/v1/definitions` отдельно фиксируют capabilities,
scope, risk, Human Gate, output и retry policy; shipped registry генерируется
штатным `make gen-integration-packages`.

| Definition | Операции и provider endpoints |
| --- | --- |
| `jira-server-dc` | `identity.read`: GET `/rest/api/2/myself`; `project.read`: GET `/rest/api/2/project/{key}`; `issue.search`: GET `/rest/api/2/search`; `issue.read`: GET `/rest/api/2/issue/{key}`; `issue.create`: POST `/rest/api/2/issue`; `issue.comment.write`: POST `/rest/api/2/issue/{key}/comment`; `issue.transition.list/apply`: GET/POST `/rest/api/2/issue/{key}/transitions` |
| `confluence-server-dc` | `identity.read`: GET `/rest/api/user/current`; `space.read`: GET `/rest/api/space/{key}`; `page.search`: GET `/rest/api/content`; `page.read`: GET `/rest/api/content/{id}`; `page.create`: POST `/rest/api/content`; `page.update`: PUT `/rest/api/content/{id}` |

Все operation keys имеют соответствующий префикс `jira.` либо `confluence.`.
Вызов может включать несколько фиксированных GET для проверки scope или
поиска прежнего эффекта перед единственной записью. Поднабор не включает
attachments, удаление, произвольный JQL/CQL, дополнительные поля Jira,
transition screens с обязательными полями, archived/draft pages и обход
нестандартных plugin endpoints. Успешный identity/health GET не доказывает
write permission и не обещает поддержку всего API.

## Настройка

`base_url` — HTTPS origin, например `https://tracker.example.invalid`, без
пути, userinfo, query или fragment. Обязательный `context_path` — `/` для
корня либо явно заданный `/tracker`/`/docs` без завершающего slash. Запрещены
traversal, escaped separators, query и вложенная authority. Контекст добавляет
native adapter перед фиксированным REST path и включает в exact resource scope:

- Jira: `base_url`, `context_path`, `project_key`; дополнительно настроен
  `issue_type`, доступный текущей credential. Поддерживается обычный ключ
  проекта из заглавных букв, цифр и `_`, начинающийся с буквы.
- Confluence: `base_url`, `context_path`, `space_key`; числовой Cloud
  `space_id` не подставляется вместо Server space key.

Credential хранится прежним secret backend, с точным revision/UID/digest.
`auth_scheme=BEARER` использует разрешённый PAT. `BASIC` требует явного
`username`; он может не быть email. Токен/пароль не является configuration
field, не возвращается пользователю и не попадает в provider URL.
Профиль не меняет credential ownership: персональный binding и launch
preflight продолжают принадлежать control-plane. Нового общего ключа нет.

Оператор материализует только согласованный exact FQDN в egress policy,
доверенную CA и прежний защищённый secret transport. Redirect, plaintext,
`insecureSkipVerify`, IP literal и произвольный внешний `$ref` не разрешаются.
Provider next links никогда не становятся HTTP destination.

## Authority и жизненный цикл

Путь остаётся прежним: root initiator и активная runtime revision → managed
MCP → control-plane проверяет identity/tenant, connection/capability/grant и
Human Gate → назначает immutable input/effect/lease → generated protected
claim → gateway проверяет exact definition/version/digest, scope и input до
чтения credential → фиксированный provider API → bounded typed output и
receipt → owner completion/audit → исходный сотрудник.
Ключи из prompt, OpenAPI и provider response не назначают authority.
Новых RPC, таблиц, миграций, событий и event consumers нет. Авторитетные
состояние invocation, журнал, receipts и recovery остаются у control-plane.

| Сценарий | Поведение |
| --- | --- |
| Чтение | Bounded retries только прежних временных transport/429/502/503/504; 401/403/404 и неверный ответ не повторяются как временная недоступность |
| Pagination | `limit` 1–50, default 20; canonical decimal offset 0–10000. Неполные/противоречивые metadata и выход за поддержанный cursor отклоняются; snapshot между страницами не обещается |
| Jira search | Пользовательский `text` экранируется как literal внутри фиксированного JQL с exact project; Cloud cursor и сырой JQL не принимаются |
| Scope | Jira проверяет фактический project issue перед записью. Confluence проверяет metadata до storage fetch, затем повторно проверяет space/current/version в body response |
| Jira transition | Только ID из текущего доступного списка, без дополнительных обязательных полей; иначе отказ до POST |
| Jira create | Поиск exact effect label, project и прежнего summary/description; совпавший эффект возвращается без POST, неоднозначный результат закрыто отклоняется |
| Confluence create | Проверяется parent space, затем точный title/body/parent и hash marker эффекта; совпавшая страница читается без POST |
| Confluence update | Exact page space, `expected_version` и следующая версия; совпавший body/title/marker на следующей версии читается без PUT; конфликт закрывается до PUT либо provider 409 |
| Неопределённая запись | Не более одной mutation attempt, без HTTP transport replay. Потеря ответа, timeout/cancel после отправки, 5xx или неверный success остаются UNKNOWN; успешный эффект задним числом не отменяется |
| Cancel/revoke/delegation | Прежние owner lifecycle и root initiator; preflight не заменяет повторную проверку перед вызовом и не продлевает grant |
| `RECOVER_READ_ONLY` | Для новых Server/DC операций автоматический adapter recovery не объявлен: остаётся UNKNOWN без повторного внешнего запроса. Проверки rejoin через `Execute` не являются доказательством автоматического recovery |

Create-marker — дополнительная проверка при допустимом исполнении, а не
внешняя атомарная idempotency гарантия. Она не разрешает перевод UNKNOWN в
READY, параллельный повтор create, смену effect key или ручное редактирование
owner state. Изменение/нормализация marker провайдером может потребовать
ручного решения; отсутствие подтверждения не становится PASS.

## Доставка, проверка, возврат

Нужен совместимый immutable release как минимум control-plane (shipped
registry) и всех replicas integration-gateway (registry и handlers). Не
выдавать новые capabilities до завершения rollout всех consumers; старый
gateway не должен получить новое определение. Использовать штатный release
lock с source SHA/image digest; файлы или shell patches внутри Pod не нужны.
Конфигурация окружения и secret refs принадлежат приватному deployment repo.

Локально выполнить `make check-integration-package-codegen`, тесты
`libs/go/integrationpackage`, полный `go test -race ./...` и `go vet ./...`
в модуле gateway. Результаты и точный SHA перечисляются в PR отдельно от
живой приёмки; наличие сценариев в этом документе не означает PASS.
`scripts/tests/control-plane-postgres-test.sh '^TestAtlassianServerDefinitionsComponent$'`
проверяет registration и сохранение точных pins/context в отдельной disposable
PostgreSQL, отсутствие у нового подключения credentials/grants и отказ без
явного context. Это не создаёт provider effect и не заменяет живую приёмку.

Ручная приёмка в выделенном synthetic project/space: создать Connection
выбранного Server/DC definition, настроить личную credential, ограниченный
grant и сотрудника. Через сотрудника прочитать identity и разрешённый
project/space, поиск с несколькими страницами, exact issue/page. Через Human
Gate создать одну тестовую задачу, комментарий и допустимый transition;
создать и обновить одну тестовую страницу с exact version. Сверить provider
readback, invocation/receipt/audit и ответ сотрудника, отдельно 401/403/404,
отозванный ключ, чужой project/space и конфликт версии. Не проверять отказ
разрушением реальных данных. Прямой curl и synthetic fixture не заменяют
этот installed end-to-end путь.

Возврат — сначала закрыть новые grants/запуски и дождаться либо адресно
сверить незавершённые effects штатным lifecycle, затем вернуть совместимые
image pins. Не удалять connections, secrets, receipts или историю ради
rollback. Новых schema migrations нет; наличие новых pinned definitions
не делает старый binary совместимым с их исполнением. Cloud connections
не перенастраиваются автоматически.
