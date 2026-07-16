# Общий чек-лист перед PR

Используется как self-check перед созданием или обновлением PR. В PR достаточно написать, какие пункты релевантны и какие проверки выполнены.

## Общее

- Доменная граница не размыта: один сервис отвечает за один bounded context или один edge-контур.
- Зона `internal|external|jobs|dev`, deployable и bounded context выбраны согласно `docs/architecture/service-boundaries.md` и `docs/guides/repository-structure.md`.
- Для `external` edge слой остаётся thin-edge: валидация, auth, routing, Mattermost/GitHub callback handling; доменная логика живёт глубже.
- Для Go-кода прочитан профильный гайд `docs/design-guidelines/go/services_design_requirements.md`.
- Модели/типы/DTO размещены по слоям, а не ad-hoc в handler или main.
- Повторяющиеся литералы, ключи событий и runtime status values вынесены в typed constants/value objects, если они используются больше одного раза.
- В production-коде нет анонимных структур для транспортных контрактов, persistent payloads и публичных API-моделей.
- Секреты не хардкодятся и не коммитятся; в логах, ошибках, метриках и документации нет сырых значений секретов.
- Имена platform env/secrets/CI variables используют префикс `MATTERCODEX_`, если внешний runtime не требует другое имя.
- Новые обязательные env/secrets/config имеют staged rollout path или безопасную диагностику, чтобы не ломать уже развёрнутый сервис.

## Deploy и runtime

- Kubernetes остаётся единственным оркестратором MVP.
- Для runtime-интеграций используется Go SDK или typed library, если она доступна и уместна.
- Shell допустим как bootstrap/deploy wrapper на раннем MVP, но доменные сценарии, reconciliation и долгоживущий runtime не пишутся как shell-first логика.
- Kubernetes манифесты лежат в `deploy/**`, а не встраиваются в production-код.
- Shell-скрипты не содержат Kubernetes YAML heredoc (`apiVersion`, `kind`, `metadata`, `spec`); они только рендерят `deploy/**/*.yaml.tpl`, вычисляют значения и применяют готовый manifest.
- Новые Secrets/ConfigMaps/Pods/Jobs/Deployments/Services/Ingress добавляются как YAML template в `deploy/**`, а не через `kubectl create ... -o yaml` или inline heredoc в `scripts/**`.
- Go-код не содержит embedded shell workflow (`sh -c`, `bash -c`, многострочные shell-сценарии в строках). На границе runner/adapter допустимы только прямые вызовы готовых CLI через `exec.CommandContext` с явным списком аргументов.
- Изменения deploy tooling сохраняют последовательное обновление live-кластера: stateful dependencies -> migrations -> internal services -> external/jobs.
- Для каждого deployable-сервиса есть собственный Dockerfile и собственные image vars, если сервис реально собирается отдельным образом.
- Go deployable-сервис запускается из собранного runtime image; production Deployment не получает исходники через ConfigMap/Secret и не запускает `go run`.
- Если добавлена внешняя зависимость, обновлён `docs/design-guidelines/common/external_dependencies_catalog.md`.

## Специфика matter-codex

- Mattermost является conversational surface, Control Center - surface сложной настройки; typed commands остаются debug/fallback.
- Agent runtime проектируется как отдельный Kubernetes pod с PVC под checkout рабочей ветки.
- Git/repository является optional integration; GitHub-специфика не просачивается в universal domain.
- Состояние long-running процессов, агентных запусков и блокировок проектируется под PostgreSQL.
- Данные гибкой структуры проектируются под `JSONB`; векторный поиск - под будущий `pgvector`.
- Секреты платформы читаются из env/Kubernetes Secret; repo-токены не передаются в доменные DTO сырыми строками.

## Профильные проверки

- Если PR затрагивает Go: выполнен `docs/design-guidelines/go/check_list.md`.
- Если PR меняет зависимости Go: выполнен `go mod tidy`.
- Если PR меняет Go-код: выполнен `make test-go` или явно указан эквивалентный `go test ./...`.
- Если интеграционные проверки не запускались из-за отсутствия внешнего контура, это явно указано в PR.
