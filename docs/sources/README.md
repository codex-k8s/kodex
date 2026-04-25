# Репозитории-источники документации

## Назначение

`docs/sources/**` хранит необязательные submodule-ссылки на внешние репозитории документации. Содержимое этих репозиториев не копируется в активную канонику `docs/**`, но остаётся доступным рядом с ней для синхронизации и справки.

`docs/**` остаётся единственным корневым деревом активной документации в репозитории `kodex`. Каталог `docs/sources/**` нужен только как соседний источник внешних репозиториев документации внутри того же дерева `docs`, чтобы не плодить вторую верхнеуровневую папку наподобие `external-docs`.

Обычный `git clone` основного репозитория работает без инициализации этих submodule.

## Подключённые источники

| Путь | Репозиторий | Доступ | Назначение |
|---|---|---|---|
| `docs/sources/guidelines/common` | `github.com/codex-k8s/kodex-guidelines-common-ru` | публичный | Общие инженерные правила. |
| `docs/sources/guidelines/go` | `github.com/codex-k8s/kodex-guidelines-go-ru` | публичный | Инженерные правила для Go backend. |
| `docs/sources/guidelines/vue` | `github.com/codex-k8s/kodex-guidelines-vue-ru` | публичный | Инженерные правила для Vue и TypeScript frontend. |
| `docs/sources/templates` | `github.com/codex-k8s/kodex-doc-templates-ru` | публичный | Шаблоны документации. |

## Как подключать вручную

Submodule помечены как `update = none`, чтобы случайная инициализация не тянула лишние источники.

Для конкретного источника используйте явный путь:

```bash
git -c submodule.kodex-guidelines-common-ru.update=checkout submodule update --init docs/sources/guidelines/common
git -c submodule.kodex-guidelines-go-ru.update=checkout submodule update --init docs/sources/guidelines/go
git -c submodule.kodex-guidelines-vue-ru.update=checkout submodule update --init docs/sources/guidelines/vue
git -c submodule.kodex-doc-templates-ru.update=checkout submodule update --init docs/sources/templates
```

Для публичных документационных submodule достаточно обычного доступа к GitHub.

## Как обновлять ссылку

1. Внести изменения в целевом внешнем репозитории в отдельной ветке.
2. Создать и смержить PR в этом внешнем репозитории.
3. В `kodex` обновить gitlink:

```bash
git -C docs/sources/guidelines/common pull --ff-only
git add docs/sources/guidelines/common
```

4. В том же PR обновить документацию `kodex`, если изменилась каноника или инструкции.
