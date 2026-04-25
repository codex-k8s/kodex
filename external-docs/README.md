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
| `external-docs/templates` | `github.com/codex-k8s/kodex-doc-templates` | публичный | Шаблоны документации. |

## Что держать как submodule, а что нет

- Submodule подходят для platform-owned source repos, с которыми команда `kodex` работает как с исходниками: приватные пакеты платформы, публичные гайды и шаблоны.
- Submodule не являются основным способом установки пакетов пользователям платформы.
- Пакеты платформы, пользовательские пакеты и пакеты сторонних поставщиков должны устанавливаться по зафиксированному `tag` или `commit` через пакетную платформу.
- Платные пакеты должны проходить через прокси платформы, а не через прямой submodule-доступ к приватному git-репозиторию.
- Если пакет нужен пользователю как установленный модуль, а не как исходный код для сопровождения командой `kodex`, его не нужно добавлять в `external-docs/**`.

## Как подключать вручную

Submodule помечены как `update = none`, чтобы случайная инициализация не тянула приватные источники.

Для конкретного источника используйте явный путь:

```bash
git -c submodule.kodex-package-store.update=checkout submodule update --init external-docs/private/package-store
git -c submodule.kodex-platform-site.update=checkout submodule update --init external-docs/private/platform-site
git -c submodule.kodex-guidelines-common.update=checkout submodule update --init external-docs/guidelines/common
git -c submodule.kodex-doc-templates.update=checkout submodule update --init external-docs/templates
```

Для приватных submodule перед командой нужен доступ к GitHub и токен с правами чтения соответствующего репозитория.
Если slug текущего репозитория не начинается с `github.com/codex-k8s`, не предполагать доступ к `external-docs/private/**`.

## Как обновлять ссылку

1. Внести изменения в целевом внешнем репозитории в отдельной ветке.
2. Создать и смержить PR в этом внешнем репозитории.
3. В `kodex` обновить gitlink:

```bash
git -C external-docs/guidelines/common pull --ff-only
git add external-docs/guidelines/common
```

4. В том же PR обновить документацию `kodex`, если изменилась каноника или инструкции.

## Что делать с внешними пакетами и пользовательскими источниками

- Платформенные исходники, которыми владеем мы сами, можно держать как необязательные submodule, если это помогает сопровождению.
- Внешние пакеты поставщиков, клиентские пакеты и будущие платные пакеты не должны жить в `external-docs/**` по умолчанию.
- Для них пакетная платформа должна хранить источник, версию, способ проверки, коммерческий статус и разрешённый способ доставки.
- Если позже понадобится локальная отладка конкретного внешнего пакета, это должна быть отдельная осознанная операция, а не часть стандартного clone рабочего репозитория.
