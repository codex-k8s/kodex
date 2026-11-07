---
id: OPS-DOC-JIRA-CREATE-FIELDS-2038
title: Опциональные обязательные поля Jira Server/DC
type: operations
status: draft
owner: backend
version: 1.0.0
updated: 2026-11-07
---

# Поля создания Jira Server/DC

Источник: [#2038](https://github.com/codex-k8s/kodex/issues/2038), зависимость
приёмки [#1830](https://github.com/codex-k8s/kodex/issues/1830).
Один unit integration-gateway включает общий контракт integrationpackage,
поставленное определение, потребителей control-plane и совместимую поставку.
Cloud API и прежний профиль без дополнительных полей сохраняются.

## Настройка подключения

`jira-server-dc@1.1.0` добавляет необязательную строку `issue_create_fields`.
Ею управляет владелец Connection через прежний UI/Git ownership. Это JSON-карта
понятных aliases → `id`, `type`, для OPTION также `options` (alias → option ID).
Ни defaults, ни секретов, ни URL в ней нет. Значение проверяется до сохранения.

Синтетический пример, не готовая конфигурация конкретного Jira:

```json
{
  "platform": {
    "id": "customfield_11001",
    "type": "OPTION",
    "options": {"test": "21001"}
  },
  "hours": {"id": "customfield_11002", "type": "NUMBER"},
  "epic": {"id": "customfield_11003", "type": "ISSUE_KEY"}
}
```

Поддержаны STRING (непустая строка до512bytes), NUMBER (конечное число
от−1e9 до1e9, до128bytes, exponent от−64 до64), BOOLEAN, одиночный OPTION и ISSUE_KEY только для Epic Link
(`com.pyxis.greenhopper.jira:gh-epic-link`) внутри настроенного проекта.
Массивы, cascaded select, user picker, произвольные plugin objects и системные
поля не поддержаны. Наличие BOOLEAN в контракте не доказывает наличие такого
поля в установленной Jira: фактическая primitive schema проверяется перед POST.
Максимум16полей,64варианта на select,8KiB конфигурации. Повторные aliases,
IDs, варианты и неизвестные свойства отклоняются.

В `jira.issue.create` добавлены optional scalar inputs:

- `fields_json`: JSON-строка до2KiB с явными значениями aliases, например
  `{"platform":"test","hours":0,"epic":"DEMO-42"}`;
- `reporter_self`: только `true` разрешает получить текущую внешнюю identity
  через фиксированный GET `/myself` и записать её `name` в reporter.

Чужого reporter выбрать нельзя. OIDC identity платформы не подменяет внешнего
пользователя PAT. Полный `fields_json` и `reporter_self` входят в canonical input
digest и показываются в обычном Human Gate. Изменение значения требует нового
намерения и подтверждения. NUMBER0 и BOOLEANfalse сохраняются явно. Общих
скрытых значений для обязательных полей, включая defaults Jira, не подставляется.

## Путь и полномочия

| Шаг | Авторитетный владелец и граница |
| --- | --- |
| Роль → tool | Сервер сохраняет root initiator; grant задаёт exact Connection/project/capability, определение и input schema |
| Намерение | Control-plane сохраняет canonical input, Connection version/configuration digest, personal binding generation и credential revision |
| Подтверждение | `HUMAN_EACH_EFFECT`, те же input/effect pins; лицо, подтвердившее gate, не становится владельцем PAT |
| Claim/Begin | Прежние одноразовые lease/fence/generation, проверка отзыва и initiator policy; никаких новых видов grant |
| Предпосылки | integration-gateway проверяет прежний effect marker, CREATE_ISSUES, точные project/type, create metadata, self reporter и локальный Epic Link |
| Эффект | Один POST `/rest/api/2/issue` с protected project/type/label; owner принимает прежний typed completion и audit |
| UNKNOWN | Только ограниченное чтение исходного эффекта; новый POST не выполняется |

Метаданные берутся из GET `/project/{configuredKey}` и фиксированного
GET `/issue/createmeta/{configuredKey}/issuetypes/{resolvedTypeId}`.
Выбор issue type — точное уникальное имя обычного типа в разрешённом проекте.
Subtask требует отдельного контракта и здесь запрещён. `autoCompleteUrl`,
`self`, внешние `$ref`, redirects и произвольные endpoints не используются.
Metadata endpoint — внутренняя предпосылка, самостоятельный tool не выдаётся.

Проверяются все обязательные поля формы, возможность `set`, соответствие
primitive/select/Epic Link schema, актуальный allowlist вариантов и доступность
локального связанного issue. Тип задачи — неизменяемый locator; допустим
штатный пустой `operations` при точной `issuetype` schema. Неподдержанное
обязательное поле закрывает create до POST. Это ограниченный контракт,
а не поддержка всех Jira plugins/screens и не доказательство всех write rights.

Поддержаны отдельно две формы пагинации Server: `startAt/maxResults/isLast`
с total и `start/size/last` с необязательным total. Максимум8страниц по50полей,
400полей, прежний общий deadline30s. Повторы ключей, смена формы, дрейф total,
усечённый EOF и пустая незавершённая страница закрывают запрос. Ошибки
401/403/404/429/timeout не разрешают POST, fallback или новый create.

## Lifecycle и восстановление

| Состояние | Итог и чтение результата |
| --- | --- |
| Ввод/конфигурация не допущены | Эффект отсутствует; безопасный отказ по прежнему owner read path, нового события не требуется |
| WAITING_APPROVAL | Неизменяемое намерение; штатные owner gate/event и audit control-plane |
| Отказ/expiry/cancel до Begin | Штатное закрытие полного графа и grant; gateway не получает нового разрешения |
| Отзыв/смена key или Connection | Прежние readiness/generation/version fences; preflight не заменяет проверку при Begin |
| Неизвестный внешний ответ | Сохраняется UNKNOWN; recovery только READ с исходной root/credential/configuration lineage |
| Точный найденный эффект | Exact creator/project/type/marker/payload, custom values и self reporter подтверждаются поиском И addressed GET; прежний completion/audit ровно один раз |
| Неточное/чужое/неполное evidence | UNKNOWN; нет повторного POST, нового gate или принятого чужого issue |

Recovery не перечитывает текущую форму создания и не подставляет текущие
defaults. Проверяются значения исходного запроса: option ID, primitive,
Epic Link и reporter key, равный authenticated creator identity. Числа
сравниваются как ограниченные рациональные значения (0 и0.0 равнозначны),
а не с потерей точности float. Чрезмерные exponent/size остаются UNKNOWN.
Свежая CREATE_ISSUES permission не нужна для readback уже начатого эффекта;
сам READ продолжает использовать штатные policy и credential lifecycle.
Отзыв не обещает отмену уже выполненного внешнего изменения.

## Поставка и возврат

Изменение требует immutable control-plane/CLI/integration-gateway и всех
потребляющих общий package компонентов/RoleImage из проверенного candidate.
Просто заменить gateway недостаточно: старый control-plane не знает новые
поля, а старый runner не должен допускать расширенный definition digest.
Конкретные SHA/digests фиксируются в release lock оператора установки.

Forward035 `20261107000100_jira_create_fields_compatibility.sql` меняет только
одну проверенную ветвь `integration_grant_admission`, сохраняя OID, аргументы,
owner, ACL, search_path и остальные условия. Новых таблиц и backfill нет.
Frozen `jira-server-dc@1.0.0` с точным прежним digest остаётся допустимым;
новые поля ему не выдаются. SQL допускает эту ветвь лишь при точной паре
старого и нового package digest. Подмена version/digest запрещена.

Новый профиль `jira-create-fields` либо `jira-create-fields-backup` проверяет
полный actual035 catalog и сохранённую схему034. Старые034 readers не
расширяются и отвергают035. Генератор проверяет frozen hashes; generated SQL
не основан на копировании данных из рабочей установки. `bootstrap_upgrade.py`
поддерживает только непосредственный034→035 с сохранением четырёх завершённых
bootstrap effects; пропуск ступеней запрещён. До apply нужен штатный preimage,
idle/claim fence и отдельный migration Job с `backoffLimit=0`.

Возврат старого image после035 сам по себе не является rollback. Остановить
запуски, сохранить receipt/evidence, сверить schema compatibility; использовать
совместимый immutable candidate либо отдельный проверенный forward fix.
`goose down`, переписывание034 и повтор materializer запрещены.

## Проверки и ручная приёмка

Synthetic проверки покрывают closed configuration/input, old/Cloud isolation,
HTTP wire → OpenAPI, create metadata, отказы без POST, UNKNOWN readback,
native personal admission/gate/claim/Begin, SQL old/new pins, forward/repeat
upgrade, backup restore и counterfactual лишнего WRITE. Это не live PASS.

Для ручной приёмки владелец выбирает разрешённый тестовый проект, подтверждает
точные aliases/options по create metadata, настраивает личный key и read/write
grant. Помощник предлагает явно синтетическую задачу и все её поля. Владелец
сверяет Human Gate и одобряет ровно один эффект. Проверить возвращённый key,
project/type, дополнительные значения, root initiator и audit. Потерянный
ответ проверять reconciliation до повторного create. Отрицательные live
проверки не выполняются на рабочих задачах.

Официальное происхождение ограниченного API:
[Jira9.12.8 REST](https://docs.atlassian.com/software/jira/docs/api/REST/9.12.8/),
[Server REST examples](https://developer.atlassian.com/server/jira/platform/jira-rest-api-examples/).
Context7 использован для Server API и Goose annotations; наличие последней
документации не заменяет тест закреплённых API/Goose dependencies.
