---
id: DOM-MC-005
title: Поставщики и учетные записи
type: domain
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-31
---

# Поставщики и учетные записи

## Назначение

Абстрагирует поставщиков среды выполнения ИИ, авторизацию, пулы учетных записей, возможности моделей и наблюдения за потреблением лимитов.

Учетная запись OpenAI/Codex с авторизацией по device code является первым адаптером поставщика, но универсальные домены не используют `auth.json` и `config.toml` как свои модели.

## В границах

- `ProviderDefinition` и возможности;
- регистрация, авторизация и отзыв учетной записи;
- безопасные названия и состояния учетных записей;
- пулы учетных записей и политики выбора;
- проверка возможностей модели и среды выполнения;
- наблюдения за потреблением и их актуальность;
- адаптер материализации конкретного поставщика;
- отзыв учётной записи и durable cleanup credential material.

## Выбор учетной записи

Новая сессия выбирает учетную запись:

- явно пользователем;
- из фиксированной привязки агента;
- из пула по `least_used`, `weighted` либо будущей политике.

Кандидат должен быть включен, авторизован, разрешен для агента и Проекта,
поддерживать модель и иметь достаточно свежие сведения о работоспособности и
лимитах.

## Привязка сессии

После первого хода учетная запись сессии неизменяема. Ее нельзя подменить при возобновлении. Повторная авторизация той же логической учетной записи обновляет ревизию авторизации. Перенос на другую учетную запись создает новую сессию и явную передачу контекста.

Managed OAuth-поставщик может обновить access/refresh token во время хода.
Такое обновление не является выбором другой учетной записи: подтвержденный
provider account ID обязан совпадать, а платформа публикует следующую
неизменяемую credential revision. Для учетной записи с rotating refresh token
допускается ровно один активный provider turn. API-key учетная запись не имеет
одноразовой ротации и использует отдельный ограниченный concurrency limit.

## Отзыв и активное использование

Отзыв учётной записи выполняется только специализированной командой `revoke`.
Активный provider turn и warm consumer, который удерживает execution grant и
может продолжить работу с закреплённой credential revision, являются
авторитетными блокерами отзыва. Проверка только состояния Pod или локальной
проекции недостаточна.

`turn/warm consumer claim` и `revoke` блокируют одну строку учётной записи и
разрешаются одной PostgreSQL-транзакцией с OCC/fence. Победитель ровно один:

- если первым зафиксирован claim, `revoke` закрыто отклоняется без изменения
  учётной записи и cleanup tasks;
- если первым зафиксирован `revoke`, учётная запись становится `REVOKED`, новые
  claims, refresh и продолжения закрыто отклоняются, а cleanup tasks всех её
  credential revisions становятся готовыми к немедленному выполнению;
- release точного turn/warm-consumer lease снимает активный блокер, но не
  удаляет историю исполнения.

Историческая `RuntimeRevision` после release всех связанных provider turn и
warm consumer не является активным полномочием и не блокирует `revoke` или
cleanup. Она сохраняет только безопасные exact metadata и digest для аудита и
воспроизводимости, но не credential plaintext и не право повторно получить
material.

## Очистка credential material

Переключение current pointer на новую credential revision в одной транзакции
помечает прежнюю revision как superseded и создаёт durable control-plane task.
Superseded material хранится 24 часа: обычная task получает
`eligible_at = superseded_at + 24h`. Принятый `revoke` атомарно создаёт
отсутствующие tasks для всех revisions учётной записи и переносит
`eligible_at` незавершённых tasks на текущее время; отзыв не ждёт физического
удаления Secret, но уже не разрешает его использовать.

Control plane является источником истины для task и хранит только `task_id`,
`accountRef`, exact `UID`, `resourceVersion`, `SHA-256`, состояние, attempt,
lease/fence, `eligible_at`, bounded backoff и безопасную диагностику. Credential
plaintext, Secret body и обратимо зашифрованная копия отсутствуют в task,
PostgreSQL, audit, event, cache, browser, prompt, `RuntimeRevision`, логах и
трассировках. Credential material существует только как opaque payload
целевого immutable Secret и не возвращается control plane при cleanup.

Физическое удаление выполняет только специализированная операция
`ProviderCredentialMaterializerService` с точным набором
`accountRef + UID + resourceVersion + SHA-256`. Универсальный delete Secret и
удаление только по имени запрещены. Materializer разрешает Kubernetes resource
из server-owned account binding, выполняет exact readback всех четырёх
координат, удаляет совпавший объект с Kubernetes preconditions по UID и
resourceVersion и подтверждает его отсутствие. Несовпадение любой координаты
или precondition conflict закрыто останавливает удаление и не позволяет удалить
заменившийся Secret.

Состояния task образуют закрытое множество:

- `PENDING` — task сохранена, ожидает `eligible_at` или bounded backoff и не
  имеет действующей lease;
- `CLAIMED` — один worker владеет bounded lease с точными attempt и fence;
- `COMPLETED` — exact delete и контрольное чтение отсутствия подтверждены либо
  ранее сохранённый exact receipt доказывает тот же идемпотентный результат;
- `DEAD_LETTER` — исчерпан bounded retry либо обнаружен безопасно неустранимый
  mismatch; автоматическое удаление прекращено до отдельного аудируемого
  operator repair/requeue.

Lease expiry позволяет одному worker вернуть `CLAIMED` в `PENDING` с новым
fence либо перевести task в `DEAD_LETTER` при исчерпанном лимите. Временная
ошибка использует bounded exponential backoff; stale worker после reclaim не
может завершить task. Retry не меняет exact delete tuple и никогда не читает и
не копирует credential body. Сама task, exact receipt и audit являются
авторитетным read path результата; credential-bearing domain event для cleanup
не публикуется.

### Матрица переходов

| Переход | Обязательная блокировка и проверка | Атомарный результат | Закрытый отказ |
| --- | --- | --- | --- |
| `turn/warm claim` | строка account, `AUTHORIZED`, current revision, capacity, отсутствие победившего `revoke` | exact revision закреплена в lease и новой `RuntimeRevision` | после `revoke` claim не создаётся |
| `release` | exact turn/consumer lease, attempt и fence | активный блокер снят; historical `RuntimeRevision` остаётся только историей | stale или чужой release не меняет usage |
| `refresh commit` | same account, old UID/RV/SHA, turn lease/fence/generation и CAS current revision | новая current revision; прежняя superseded; `PENDING` task с retention 24h | current pointer и task не меняются частично |
| `revoke` | строка account, точный owner/permission, отсутствие активных turn и warm consumer | `REVOKED`; missing tasks созданы, незавершённые ускорены до текущего времени | победивший claim оставляет account активным, `revoke` отклоняется |
| `cleanup claim` | due `PENDING`, exact tuple, target revision не удерживается активным consumer, retry budget | `CLAIMED` с одной bounded lease, attempt и fence | ранняя, stale или уже занятая task не выдаётся |
| `lease expiry/reclaim` | истёкшая `CLAIMED` lease и тот же exact tuple | `PENDING` с новым fence/backoff либо `DEAD_LETTER` при исчерпанном лимите | прежний worker теряет право complete |
| `exact delete/complete` | действующая lease/fence; materializer readback `accountRef+UID+resourceVersion+SHA-256`; delete preconditions UID/RV | совпавший Secret удалён, отсутствие прочитано, task `COMPLETED` | mismatch/precondition conflict не удаляет объект и ведёт в безопасный retry или `DEAD_LETTER` |
| `retry/dead letter` | классифицированная временная ошибка и оставшийся bounded budget | `PENDING` с bounded backoff либо terminal `DEAD_LETTER` | бесконечный retry и изменение exact tuple запрещены |
| `operator requeue` | `DEAD_LETTER`, точная operator permission, устранённая причина, тот же immutable tuple | новая retry generation, `PENDING`, новый fence/budget и audit | скрытое редактирование tuple или автоматический requeue запрещены |

### Негативные сценарии

- Одновременные `turn claim` и `revoke` не создают одновременно execution
  lease и `REVOKED`: проигравшая транзакция перечитывает победившее состояние.
- `revoke` при активном provider turn закрыто отклоняется и не ускоряет cleanup
  tasks.
- `revoke` при отсутствии turn, но при живом warm consumer закрыто отклоняется.
- Одна historical `RuntimeRevision` после подтверждённого release не блокирует
  `revoke` и не даёт повторно materialize credential.
- Superseded task до истечения 24 часов не claim-ится; принятый `revoke` делает
  её due немедленно без синхронного удаления внутри owner transaction.
- Замена Secret с тем же именем, но другим UID, `resourceVersion` или SHA-256
  не удаляется stale task и переводит её в безопасный операторский контур.
- Worker с истёкшей lease после reclaim не может отметить task `COMPLETED`,
  даже если его поздний delete вернул неоднозначный результат.
- Повтор exact-delete после потерянного ответа возвращает сохранённый receipt
  только для того же exact tuple; другой tuple тем же idempotency key
  отклоняется.
- Исчерпание bounded retry переводит task в `DEAD_LETTER`; бесконечный цикл и
  fail-open завершение запрещены.
- Попытка передать credential body в task, audit, event или materializer delete
  request отклоняется schema/policy и не попадает в диагностику.

## Безопасность

- Исходные данные авторизации хранятся в хранилище секретов.
- Каждая успешная device-code авторизация создает новую неизменяемую ревизию
  секрета с проверенными хешем содержимого, UID и resource version. Активная
  ссылка учетной записи переключается на новую ревизию только после успешной
  фиксации метаданных; предыдущая ревизия не изменяется, остаётся доступной в
  bounded rollback window 24 часа и затем удаляется durable cleanup task.
- Повторная авторизация привилегированной учётной записи поставщика доступна
  только пользователю с точным platform permission. Она атомарно публикует
  новую credential revision, запрещена во время активного хода привязанной
  Session или живого warm consumer и не разрешает произвольную подмену
  логической учётной записи.
- Автоматический managed OAuth refresh передается provider-sidecar по закрытому
  UDS отдельному credential-relay. Только relay получает execution-scoped mTLS
  identity и ticket для callback; provider process и Role runtime их не видят.
  Runtime-controller повторно сверяет прежнюю immutable Secret и логическую
  учетную запись, создает новую immutable Secret, выполняет exact readback и
  передает control-plane только name, UID, resource version и SHA-256. Raw token
  не попадает в PostgreSQL, browser, Role runtime, prompt, event или лог.
- Control-plane переключает текущую credential revision только по
  lease/fence/generation и compare-and-swap с точной прежней revision. Повтор
  с тем же Secret idempotent, а callback устаревшего хода закрыто отклоняется.
- Если обновленный OAuth snapshot нельзя надежно материализовать и
  зафиксировать, ход закрыто завершается ошибкой. Предыдущая revision не
  изменяется; созданная, но не активированная Secret не становится текущей и
  удаляется той же durable cleanup task через exact-delete
  `ProviderCredentialMaterializerService`.
- Интерфейс показывает название, учетную запись поставщика, маскированные метаданные, состояние и время наблюдения.
- Значения токенов и учетных записей отсутствуют в логах и промпте.
- Диагностика авторизации не трактует временную ошибку поставщика как истекшую авторизацию без подтвержденного признака.
- Подтвержденная блокировка запроса политикой кибербезопасности является отдельным терминальным исходом, а не ошибкой авторизации или временной перегрузкой.
- Платформа не переформулирует заблокированный запрос для обхода защиты и не возобновляет заблокированную сессию автоматически.

## Критерии приемки

- Несколько учетных записей работают одновременно.
- Новые сессии балансируются по политике.
- Существующая сессия всегда возобновляется исходной учетной записью.
- Два хода одной rotating OAuth-учетной записи не выполняются одновременно;
  разные учетные записи и API-key account сохраняют разрешенную параллельность.
- Успешный provider refresh создает новую immutable credential revision, а
  следующий turn pin-ит уже ее exact metadata и digest.
- Истекшая авторизация дает понятное действие повторной авторизации в интерфейсе.
- Ошибка фиксации новой ревизии не меняет действующую ревизию и не удаляет
  старый секрет.
- `revoke` не побеждает активный turn/warm consumer; после release historical
  `RuntimeRevision` не блокирует отзыв.
- Superseded credential material сохраняется 24 часа, после чего удаляется
  durable task; `revoke` ускоряет все незавершённые tasks этой учётной записи.
- Cleanup выдерживает lease expiry, reclaim, bounded retry и
  `DEAD_LETTER`, а stale worker или exact metadata mismatch не удаляет Secret.
- Ни один cleanup state, request, receipt или диагностический путь не хранит и
  не раскрывает credential plaintext.
- Устаревшие сведения о лимитах явно помечаются и не выдаются за актуальные.
