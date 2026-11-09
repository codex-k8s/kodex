# Ограниченный контракт GitLab Self-Managed

`openapi.yaml` самостоятельно описывает используемые REST API v4 поля и операции
по официальным документации и исходникам GitLab `v19.2.9`; ссылки закреплены в
`x-provenance`. Схема, examples и payload внешней установки не копировались.
Это авторский subset Kodex под лицензией репозитория, а не импорт полной upstream
OpenAPI и не заявление о полном покрытии GitLab API либо всех его версий.

Покрыты project/issue/MR/pipeline/file reads, создание Issue/ветки/MR и фиксированные
metadata prerequisites текущей identity, собственного токена и protected rules.
`gitlab.permission.*` — внутренние предпосылки, а не новые Capability/MCP tools.
Token ID берётся исключительно из self metadata. Project, actual endpoint
и Secret refs остаются в конфигурации, не в модели. Один synthetic server служит
примером; runtime использует проверенный configured origin и штатную SSRF/TLS границу.

Контракт не выдаёт grant: сохраняются Definition/Connection/Capability/Grant,
root initiator, personal Begin и Human Gate. `PROJECT_SHARED` сохраняет прежний
путь. Personal writes дополнительно проверяют явный `granular` flag, актуальность
токена, action scope и membership/features точного проекта. Неизвестные metadata
закрывают запись; успешный GET не становится глобальным WRITE proof.

Начальный checker поддерживает metadata19.x начиная с19.2, legacy `api` при
явном `granular:false`, а также подтверждённые granular `selected_memberships`
для exact project и `all_memberships` при явной роли в проекте. Group/personal
scope ancestry не угадывается. Protected destination ref не создаётся; target
защищённой ветки в MR не означает разрешения на merge. Остальные права и версии
не объявляются подтверждёнными.

Native synthetic tests сверяют wire requests/responses со схемой и проверяют
отказы, finite pagination и отсутствие повторного POST. Live qualification,
проверка scopes до запуска, UNKNOWN reconciliation и готовность полного unit
фиксируются отдельно; наличие этого файла не является приёмкой развёртывания.

Subset1.1.0 дополнен optional author/id и bounded sorting для read-only
issue/MR recovery по official docs v19.2.9 (Context7). Operation count16 и
allowlist прежние; нового универсального HTTP tool нет. Только recovery
consumer требует полный exact author/project/payload/unique marker и detail
readback. Пустой/неполный/изменённый ответ не разрешает повторный POST.
Branch existence не доказывает creation provenance: auto-recovery branch
не включён. Ошибка recovery READ сохраняет UNKNOWN на стороне owner.

Subset 1.2.0 добавляет внутреннее чтение canonical identity группы с обязательным
`with_projects=false`. Вложенный каталог проектов не запрашивается; project discovery
сохраняет отдельную pagination. Проверены ограниченный ответ и закрытый отказ, если
провайдер игнорирует параметр и превышает прежний HTTP body budget. Контракт
`gitlab.permission.group.read` не является дополнительным модельным инструментом.
