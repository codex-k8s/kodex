---
id: OPS-PROJECT-PURGE-RECOVERY-API-1818
title: HTTP-путь подтверждаемого восстановления удаления проекта
type: operation
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-19
---

# Подтверждаемое восстановление удаления проекта

Gateway unit [#1818](https://github.com/codex-k8s/kodex/issues/1818) реализует
HTTP-границу решения владельца от19.10 из
[плана CP](project-purge-1816.md#решение-владельца1910-подтверждаемое-восстановление).
CP остаётся единственным владельцем receipt, решения, состояния очистки,
идемпотентности и событий. Gateway не обращается к БД или S3 и не объявляет
байты удалёнными по факту согласия человека.

## Сквозной путь и полномочия

| HTTP | RPC и operation | Полномочия и результат |
| --- | --- | --- |
| GET `/api/v1/projects/{projectRef}/purge-recovery` | `PlatformQueryService.GetProjectPurgeRecovery`, `platform.query.projects.purge-recovery.get` | Signed actor/org из проверенной SSO-сессии; CP допускает только owner/admin организации. Возвращается прямой `ProjectPurgeRecovery`, HTTP200, `Cache-Control: no-store`, ETag версии receipt |
| POST того же route | `PlatformCommandService.ResolveProjectPurgeRecovery`, `platform.command.projects.purge-recovery.resolve` | Тот же actor/org, CSRF и Origin boundary. `If-Match` — версия восстановления, а не проекта; `Idempotency-Key`, точный `snapshotDigest` и явный `acknowledgeUnknownOutcome=true`. HTTP202 означает сохранённое намерение, не физическую очистку |

Оба operation organization-scoped и не входят в project-required profile.
`projectRef` в пути — только locator surviving receipt. Даже совпадающий
`X-Kodex-Project-Id` отклоняется: после удаления проекта нельзя требовать
доступ к отсутствующему объекту либо подменять owner scope браузерным заголовком.
Обычные project routes сохраняют прежнюю привязку к проекту.

В ответе только безопасные поля: `projectRef`, `version`, `snapshotDigest`,
`state`, `unknownContentCount`, `cleanupPendingCount`, `projectPresent`,
`canResolve`, `resolutionRef`, `projectVersion`. Ключи хранилища, имена файлов,
сырые provider errors и содержимое не передаются. Проверяются exact project,
закрытый enum, digest, безопасные JSON-числа, счётчики и согласованность
`projectPresent`/`projectVersion`; неверная проекция закрывается HTTP502.

## Матрица исходов

| Сценарий | Поведение gateway |
| --- | --- |
| Preview существующего/удалённого проекта | Один точный query RPC, без загрузки Project и fan-out; после удаления `projectPresent=false`, `projectVersion=0` |
| Нет сессии, чужая организация, отозванная сессия | HTTP401 до handler/RPC; bearer не извлекается из body |
| Нет CSRF либо недопустимый Origin для POST | Закрытый отказ общей boundary; read GET не требует CSRF |
| Нет согласия, неверный digest, неизвестное поле | HTTP400 до RPC; `actor`, организация и полномочия не принимаются из body |
| Нет OCC/idempotency/CSRF header | Generated route отклоняет запрос; weak/wildcard/небезопасная версия не используется |
| Stale snapshot или версия | CP `Aborted` → HTTP412; gateway не перечитывает и не повторяет согласие автоматически |
| Повтор/изменённый input с тем же ключом | Авторитетная CP receipt/idempotency boundary; `AlreadyExists` → HTTP409 |
| Чужой ресурс/недостаточная роль/неподходящее состояние | CP `NotFound`/`PermissionDenied`/`FailedPrecondition` → HTTP404/403/409, без раскрытия provider details |
| Успех согласия, worker retry/expiry/terminal | Ответ не выдаёт deletion proof. Последующие GET читают CP-состояние; gateway не запускает worker и не меняет leases |

Аудит, lifecycle и `PROJECT_CHANGED`/`PROJECT_PURGED` принадлежат CP.
Новый gateway event/inbox, постоянное состояние, worker, Secret, TLS-профиль,
права Kubernetes либо сетевой доступ не требуются. Используются существующие
signed client adapter, replay protection, HTTP error mapping, низкокардинальные
метрики и общий bounded lifecycle. Манифесты deploy не меняются.

## Проверки и ручная приёмка

Герметичный контур: в `services/external/control-api-gateway` выполнить
`go test ./...` и `go vet ./...`; в корне —
`make gen-control-api-gateway-openapi-go`, затем нулевой diff generated Go
и `git diff --check`. Unit покрывает точный RPC, версия/ETag, receipt после
удаления, malformed input, upstream shape, owner error mapping, session/CSRF,
сохранение signed actor и отсутствие project authority для recovery route.
Отдельный unit загружает исходный OpenAPI и проверяет новый path вместе с
транзитивными request/response схемами через закреплённый `kin-openapi`.
Его прежняя версия не меняется; зависимость становится прямой тестовой.

На19.10 применимая route validation прошла. Попытка полной проверки всего
OpenAPI тем же валидатором обнаружила прежний `$ref` sibling `description`
в неизменённой схеме `ImageAdmissionRiskDecision`: глобальный результат
`FAIL`, не `PASS` нового route. Массовое исправление чужих схем не входит
в этот unit; воспроизводимая генерация Go не подменяет этот результат.

После разрешённого rollout войти как owner/admin, открыть корзину и preview.
Проверить в Network точные GET/POST, отсутствие object keys и отсутствие
`X-Kodex-Project-Id`. Только человек подтверждает живой отказ от результата.
После решения проверить HTTP202, свежий ETag, состояние cleanup и повторный GET
после удаления проекта. Устаревший снимок должен потребовать нового согласия,
а не автоматической повторной отправки. Browser/deploy acceptance не следует
из unit-тестов и до фактического исполнения остаётся `NOT RUN`.

Проверена актуальная документация Context7 `/oapi-codegen/oapi-codegen`:
std-http-server generation проверяет обязательные параметры, но не заменяет
ручную проверку request/schema и безопасной upstream-проекции.

Rollback — возврат предыдущего gateway image/source. Сохранённые owner
решения и cleanup obligations CP не отменяются, статусы БД вручную не меняются.
