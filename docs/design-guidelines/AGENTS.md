# Design Guidelines

Документация разделена по областям:

- `docs/design-guidelines/common/` — общее для backend и frontend.
- `docs/design-guidelines/go/` — правила для Go backend.
- `docs/design-guidelines/vue/` — правила для frontend (Vue 3 + TypeScript).
- `docs/design-guidelines/common/external_dependencies_catalog.md` — единый каталог внешних зависимостей и инструментов.

Стартовая точка перед PR:
- `docs/design-guidelines/common/check_list.md`
- затем профильные чек-листы:
  - `docs/design-guidelines/go/check_list.md`
  - `docs/design-guidelines/vue/check_list.md`

Специфика `kodex`, которую нельзя нарушать:
- только Kubernetes как оркестратор;
- webhook-driven процессы (без workflow-first оркестрации);
- PostgreSQL + JSONB + pgvector как источник синхронизации состояния;
- встроенные MCP сервисные ручки реализуются в Go внутри платформы;
- интеграции с репозиториями проектируются через интерфейсы провайдеров.

Процесс разработки и ведения документации задаётся корневым `AGENTS.md` и актуальной проектной документацией.
Этот каталог не фиксирует продуктовый план работ: технические гайды должны оставаться переиспользуемыми.

## Связь с внешними пакетами

- Публичные пакеты в `docs/external/**` являются переиспользуемым baseline, а не полной текстовой копией локальной проектной каноники.
- Локальные документы в `docs/design-guidelines/**` содержат `kodex`-специфичный overlay поверх внешнего пакета.
- Если внешний источник импортирован в проект, его правила обязательны; при конфликте проектный overlay имеет приоритет.
- Если правило относится только к `kodex`, его нужно фиксировать в проектном overlay, а не протаскивать обратно во внешний пакет.
- Если правило универсально и не зависит от конкретного проекта, его можно поднимать во внешний пакет и затем обновлять gitlink.

## Внешние источники

Публичные репозитории руководящих гайдов:

- `github.com/codex-k8s/kodex-guidelines-common-ru` — source submodule `docs/external/guidelines/common`;
- `github.com/codex-k8s/kodex-guidelines-go-backend-ru` — source submodule `docs/external/guidelines/go`;
- `github.com/codex-k8s/kodex-guidelines-vue-frontend-ru` — source submodule `docs/external/guidelines/vue`.

Пока штатный импорт руководящих пакетов не реализован, `docs/external/guidelines/**` выступает подключённым baseline, а `docs/design-guidelines/**` — проектным overlay. Если меняется универсальное правило, нужно обновить соответствующий внешний пакет и затем gitlink.
