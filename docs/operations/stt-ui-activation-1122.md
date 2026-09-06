---
id: OPS-DOC-1122
title: Активация системного распознавания через PWA
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-07
---

# Явная активация SYSTEM_STT

Refs #1122, #1119, #1118, #1031. Публикация сохраняет ревизию; отдельная
панель редактора применяет её к `STT_SERVICE/stt-tts-service`. Ключи провайдера
в этом действии не запрашиваются. Контракт потребляется из generated SDK #1119;
INSERT-only ABSENT и UPDATE-only MATCH принадлежат Control Plane #1118.

## Сценарий и полномочия

Пользователь открывает опубликованную UI-owned организационную SYSTEM_STT.
Подготовка сначала перечитывает history целевой конфигурации (`organization.view`)
и impact (`organization.manage`), затем глобальный
`GET /api/v1/system-stt-configuration`. Только `404/NOT_FOUND` после этих чтений
позволяет предложить первую привязку. Пустой target impact сам по себе не
доказывает отсутствие. Ошибка либо несовпадение проекции закрывает подготовку.

Если глобальный GET вернул действующую конфигурацию, PWA читает её history и impact
именно по возвращённым `configurationRef/revisionRef`. MATCH использует
`ManagedConfigurationConsumer.version`, а не номер ревизии содержимого.
SUPERSEDED действующая ревизия остаётся читаемой. Смена между чтениями приводит
к отказу согласования либо к серверному OCC, а не к неявному ABSENT.

Подтверждение вызывает generated `rebindSystemSttConsumers` через feature adapter:
`POST /api/v1/system-stt-configurations/{configurationRef}/revisions/{revisionRef}/consumer-bindings`.
BFF session/CSRF и gateway signed context передают actor в typed RPC
`RebindSystemSTTConsumers`. Control Plane проверяет owner, UI/source lifecycle,
config `If-Match`, глобальный impact digest и expected binding pins в своей
транзакции. Idempotency key не заменяет полномочия. PWA не назначает binding
version, credential generation либо runtime readiness и не публикует события.
Результат показывается через повторный authoritative effective GET.

| Переход | PWA и авторитетный результат |
| --- | --- |
| Подготовка без привязки | Явный `expectedAbsent:true`, pins отсутствуют; INSERT-only CAS владельца |
| Смена | Имя и ревизия прежней конфигурации, новая ревизия; MATCH exact old binding pins |
| Отмена выбора | Удаляется только локальный план, POST отсутствует |
| Изменение selection/version или закрытие | Read abort/generation fence; старый результат не применяется |
| Подтверждение | Однократный POST; намерение сохраняется до эффекта в session storage |
| 412 | План сброшен; reload редактора и новое явное подтверждение |
| UNKNOWN, timeout, 5xx, abort | Новая отправка заблокирована, включая закрытие/открытие панели; только чтение |
| Readback UNKNOWN | Совпавшая целевая конфигурация/ревизия наблюдается как текущее состояние, без заявления receipt |
| Успех | ACK отдельно от effective readiness; `ready:false` не показывается готовностью |
| Logout | Owner signal отменяет работу; общий session cleanup удаляет локальные намерения |

Запрет UI mutation для Git-owned конфигурации сохраняется. Generic MATCH для
других конфигураций и специализированный RoleImage impact не меняются.

## Проверка и ограничения

Локальные unit проверяют ABSENT/MATCH, глобальное чтение, права до чтения
отсутствия, недоступность и гонку pins. Synthetic browser проверяет фактическую
панель и generated HTTP на 390/2900: first-use, смену, 412, UNKNOWN после повторного
открытия, CSRF/OCC, отсутствие автоматического POST, неподготовленную readiness.
Это синтетические fixtures, не live provider acceptance.

Ручная проверка после разрешённого deploy: создать отдельную STT-конфигурацию,
validate → publish → подготовить → подтвердить; убедиться в effective config и
readiness. Затем переключить на вторую конфигурацию с точными прежними pins;
в параллельной вкладке изменить binding и проверить 412/reload. Ошибку сети
проверять без повторной отправки команды. Реальное русское распознавание и
provider readiness проверяются отдельно, без вывода audio/transcript/credential.

Проверена официальная документация Vue через Context7 `/websites/vuejs`:
[watcher cleanup](https://vuejs.org/guide/essentials/watchers#side-effect-cleanup),
[watch API](https://vuejs.org/api/reactivity-core#watch).
