# Каталог пакетов руководящей документации

## Назначение

Раздел описывает каталог пакетов руководящей документации для агентов. Такие пакеты живут в отдельных репозиториях и попадают в рабочий контур агента как локально доступный источник.

Проектная документация конкретного продукта не становится пакетом руководящей документации только потому, что агенту нужно её читать.

## Первые репозитории-источники

- `github.com/codex-k8s/kodex-guidelines-common-ru` — общие инженерные правила, source submodule `docs/external/guidelines/common`.
- `github.com/codex-k8s/kodex-guidelines-go-backend-ru` — правила для Go backend, source submodule `docs/external/guidelines/go`.
- `github.com/codex-k8s/kodex-guidelines-vue-frontend-ru` — правила для Vue и TypeScript frontend, source submodule `docs/external/guidelines/vue`.

`docs/design-guidelines/**` содержит проектный overlay поверх подключённых руководящих пакетов. Универсальные изменения вносятся в соответствующий внешний репозиторий, затем в `kodex` обновляется gitlink.

Шаблоны документации вынесены отдельно и не считаются пакетом руководящей документации: публичный источник шаблонов — `github.com/codex-k8s/kodex-doc-templates-ru`, source submodule — `docs/templates`.

## Именование

- Пакет руководящей документации не должен называться по конкретному проекту-потребителю.
- Каноническая схема имени: `kodex-guidelines-<subject>-<locale>`.
- `<subject>` должен описывать предметную область пакета, например `common`, `go-backend` или `vue-frontend`.
- `<locale>` обязателен и ставится в конце slug. Примеры: `ru`, `en`.
