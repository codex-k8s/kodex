# Go чек-лист перед PR

Используется как self-check перед созданием или обновлением PR. Для раннего MVP применяются только релевантные пункты; невыполненные будущие пункты фиксируются как явное ограничение.

## Архитектура и структура

- Сервис размещен в `services/<zone>/<service-name>/` согласно `docs/architecture/service-boundaries.md` и `docs/guides/repository-structure.md`.
- Entrypoint тонкий: `cmd/<service>/main.go` только грузит config, logger, context/signal и запускает `internal/app`.
- `internal/app` является composition root: собирает зависимости, HTTP server, graceful shutdown и lifecycle.
- Transport слой (`internal/transport/http`) не содержит доменную orchestration-логику.
- Транспортные ответы описаны именованными DTO в `internal/transport/http/models`.
- Доменные value objects и snapshots живут в `internal/domain/types/**`, а use-case/service логика - в `internal/domain/service`.
- Повторяемые строки для путей, статусов и сервисных имён вынесены в constants/value objects, если используются больше одного раза.
- Для типов, которые должны реализовывать интерфейс, есть compile-time assertion.
- `context.Background()` создаётся только в `cmd/<service>/main.go`; ниже используется переданный context или производный context.
- Runtime config читается через `github.com/caarlos0/env/v11`; условно обязательные значения проверяются в `Validate()`.

## HTTP и Mattermost

- Slash callback валидирует HTTP method, ограничивает размер form body и проверяет Mattermost slash token без раскрытия значения.
- Mattermost response строится через typed SDK/model, если библиотека доступна.
- Health/readiness endpoints не зависят от внешней сети и не требуют секретов.
- `/metrics` отдаётся через Prometheus handler и custom registry.
- Пользовательские тексты для MVP могут быть constants в Go; при появлении многоязычности или шаблонов их нужно вынести в отдельный слой.

## SDK и зависимости

- Для доступных внешних интеграций выбран SDK/library вместо ручного REST/JSON, если это не усложняет MVP.
- Добавленная внешняя зависимость отражена в `go.mod`, `go.sum` и `docs/design-guidelines/common/external_dependencies_catalog.md`.
- Перед использованием/обновлением библиотеки проверены актуальные docs через Context7 или официальный upstream-документ.
- Тяжёлые framework dependencies не добавлены ради одной простой ручки.

## Безопасность

- Секреты читаются из env/Kubernetes Secret и не логируются.
- Ошибки и health responses показывают только факт настройки токенов, не значения.
- В тестах не используются реальные токены из `.env`.
- Shell wrappers не печатают секреты и не сериализуют их в render manifests.

## Тесты и проверки

- Выполнен `go mod tidy`.
- Выполнен `gofmt` для изменённого Go-кода.
- Выполнен `go test ./...` или `make test-go`.
- Если менялись deploy scripts/templates, выполнены `bash -n` и render manifests.
- Если live-контур доступен, выполнены remote deploy/smoke проверки через SSH по `.env`.
