---
id: GOV-DOC-003
title: Стратегия проверок
type: governance
status: approved
owner: reviewer
version: 2.0.0
updated: 2026-08-23
---

# Стратегия проверок

MatterCodex использует единый поддерживаемый профиль `Web-first baseline`.
Проверки не обращаются к staging/production, живым данным и промышленным
credentials. Отсутствие или незапущенный контур не выдаётся за успех.

## Обязательные контуры

Для каждой изменённой области выполняются применимые проверки:

- форматирование, статический разбор, сборка и `git diff --check`;
- Proto format/lint/build и воспроизводимый codegen;
- OpenAPI и AsyncAPI validation и воспроизводимый codegen;
- Go unit-тесты домена, authorization, lifecycle, idempotency, OCC,
  event ordering, redaction и provider-neutral delegation;
- PostgreSQL component-тесты на disposable PostgreSQL для baseline schema,
  bootstrap, concurrency, outbox, graph, gates, grants и audit;
- frontend lint, typecheck, unit-тесты stores/reducers/forms/accessibility и
  production build;
- Playwright E2E обязательных web-only, realtime, Human Gate, retry,
  nested-workflow, system-assistant и optional-Mattermost сценариев;
- security-negative проверки owner/project/resource boundary, replay, CSRF,
  Origin, credential leakage и exact grants;
- render обоих профилей `web-only` и `web-with-mattermost`, schema и policy
  assertions, immutable image refs и отсутствие запрещённых материалов.

Простой glue-код не получает тест ради покрытия. Тестовая оснастка обязательна,
если без неё нельзя воспроизводимо доказать согласованный lifecycle,
authorization boundary, realtime reducer или пользовательский сценарий.

## Поддерживаемые публичные точки входа

- `make test-go` — герметичные Go-модули и SQL boundary;
- `make test-control-plane-postgres` — disposable PostgreSQL component contour;
- `make lint-proto build-proto check-proto-codegen` — Proto source и codegen;
- `make lint-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen` — AsyncAPI;
- `make test-web-only-release` — fresh render web-only и optional Mattermost;
- `make test-authority-policy-codegen` — machine policy/codegen;
- в `services/staff/control-center`: `npm run lint`, `npm run typecheck`,
  `npm run test:unit`, `npm run build`, `npm run test:e2e:check` и
  `npm run test:e2e` для отдельной разрешённой disposable установки.

Новая suite становится обязательной после появления в канонической публичной
точке входа, фиксированных fixtures, ограниченного бюджета и однозначного
ожидаемого результата.

## PostgreSQL и браузер

PostgreSQL-тест использует только созданную оснасткой одноразовую базу. DSN
staging/production, общая база или внешняя конечная точка без disposable proof
отклоняются до подключения.

Browser E2E выполняется только против отдельной disposable установки с
синтетическими данными. Если такой установки нет и её создание не входит в
выданные полномочия, результат — `NOT RUN`, а не `PASS`. Staging/production
deployment и проверка живой среды требуют отдельного решения владельца.

## Классификация результата

- `PASS` — команда действительно выполнена на указанном SHA и завершилась
  успешным проверяемым результатом;
- `FAIL` — команда запущена и обнаружила дефект либо неисправность обязательной
  оснастки;
- `NOT RUN` — команда не запускалась или отсутствовала разрешённая безопасная
  среда/credential/инструмент.

Пустой GitHub `statusCheckRollup`, отсутствие check run или описание будущего
контура не являются успешным CI. Локальные результаты перечисляются отдельно
от GitHub checks и привязываются к точному SHA.

## Выпуск

Build выпускает неизменяемые OCI digests с provenance. Render принимает exact
release lock и deployment profile. Apply, migration в живой среде, smoke после
развёртывания, promotion и rollback выполняются только после отдельного допуска
владельца и не являются частью локальной реализации.

## Рецензирование

Доказанный production-дефект, недостижимый обязательный path или fail-open
boundary блокирует результат независимо от наличия теста. После исправлений
product, security и architecture review повторяются на одном новом SHA.
`resolved` у discussion thread не заменяет перепроверку исходного failure path
и системных аналогов.
