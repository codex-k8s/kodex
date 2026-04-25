# Внешние репозитории документации

## Назначение

`external-docs/**` хранит необязательные submodule-ссылки на внешние репозитории документации и пакетов. Содержимое этих репозиториев не копируется в основной репозиторий `kodex`.

Обычный `git clone` основного репозитория работает без доступа к приватным репозиториям. Не используйте `--recurse-submodules`, если у вас нет доступа ко всем приватным источникам.

## Подключённые источники

| Путь | Репозиторий | Доступ | Назначение |
|---|---|---|---|
| `external-docs/private/package-store` | `github.com/codex-k8s/kodex-package-store` | приватный | Пакет авторского магазина пакетов `kodex`. |
| `external-docs/private/platform-site` | `github.com/codex-k8s/kodex-platform-site` | приватный | Пакет сайта и пользовательской документации платформы. |
| `external-docs/guidelines/common` | `github.com/codex-k8s/kodex-guidelines-common` | публичный | Общие инженерные правила. |
| `external-docs/guidelines/go` | `github.com/codex-k8s/kodex-guidelines-go` | публичный | Инженерные правила для Go backend. |
| `external-docs/guidelines/vue` | `github.com/codex-k8s/kodex-guidelines-vue` | публичный | Инженерные правила для Vue и TypeScript frontend. |

## Как подключать вручную

Submodule помечены как `update = none`, чтобы случайная инициализация не тянула приватные источники.

Для конкретного источника используйте явный путь:

```bash
git -c submodule.kodex-package-store.update=checkout submodule update --init external-docs/private/package-store
git -c submodule.kodex-platform-site.update=checkout submodule update --init external-docs/private/platform-site
git -c submodule.kodex-guidelines-common.update=checkout submodule update --init external-docs/guidelines/common
```

Для приватных submodule перед командой нужен доступ к GitHub и токен с правами чтения соответствующего репозитория.

## Как обновлять ссылку

1. Внести изменения в целевом внешнем репозитории.
2. Закоммитить и запушить изменения в этом внешнем репозитории.
3. В `kodex` обновить gitlink:

```bash
git -C external-docs/guidelines/common pull --ff-only
git add external-docs/guidelines/common
```

4. В том же PR обновить документацию `kodex`, если изменилась каноника или инструкции.
