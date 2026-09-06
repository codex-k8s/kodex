---
id: OPS-HTTP-MANAGED-SOURCE-1083
title: Явный доступ HTTP к исходникам managed RoleImage
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Исходники managed RoleImage

Refs #1045, #1083, #1031; существующий PR #1066.

Авторитетный CP добавляет optional `ManagedConfigurationSet.sourceEditable`
и `ManagedConfigurationRevision.sourceAvailable`. HTTP сохраняет присутствие
и явное `false`. Для ROLE_IMAGE поле обязательно; для остальных видов оно
отсутствует и не меняет существующие полномочия. Сводный каталог передаёт
`sourceEditable`, а его currentRevision остаётся только набором revision pins.

Цепочка чтения: проверенный actor → существующий HTTP endpoint → generated
Query/Command → CP с точным `image.source.view/manage` → typed mapper → SDK.
HTTP не выводит разрешение из projectRef, managedBy или NextAction. Поле
редактирования не заменяет проверку команды, свежие полномочия, OCC и receipt.
Текущая ревизия, история и результат команды проходят одну проверку. При
`sourceAvailable=false` непустые content или validationDiagnostics означают
противоречие владельца: HTTP возвращает 502 без исходного текста.

Ручная проверка после доставки полного owner: прочитать ROLE_IMAGE с допуском
и без него, затем историю и результат разрешённой команды. Убедиться, что
metadata и pins доступны согласно owner eligibility, а скрытый content и
diagnostics отсутствуют. Для остальных managed kinds новые поля отсутствуют.

Локально canonical Go/TS/AsyncAPI generation/replay, strict SDK и targeted managed /
RoleImage race PASS (2.344 s), включая history parity. Первый compile нового
теста завершился FAIL: ошибочное имя response исправлено, повтор PASS.
Исполняемый полный CP73,
общий baseline, повторный review и live acceptance пока NOT RUN.

Риск: старый owner без обязательных ROLE_IMAGE flags будет закрыто отклонён.
Доставлять вместе с согласованным CP и SDK; rollback только согласованной
группой. Секреты и реальные исходники в evidence не публикуются.
