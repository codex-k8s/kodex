---
id: OPS-DOC-ASSISTANT-READ-001
title: Чтение модели и ресурсов сотрудника помощником по имени
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-14
---

# Сценарий и границы

Issue1797/PR1807, остаток natural-language QA. Пользователь спрашивает модель
и ресурсы Developer по имени, без ID и без перехода в карточку. Реальная
проверка14.10 завершилась семантическим FAIL: помощник нашёл сотрудника,
но `AGENT_RUNTIME_CONFIGURATION` был скрыт вне текущего AGENT экрана.

Устраняется только ограничение чтения безопасных runtime metadata. Полные
инструкции `AGENT_CONFIGURATION` и контекст подтверждаемых изменений остаются
привязаны к server-owned текущему контексту; чтение не создаёт право изменения.

| Шаг | Авторитет и результат |
| --- | --- |
| USER → помощник | Root actor из проверенного входа; SYSTEM/PROJECT профиль назначен сервером |
| `find_platform_resources` | Авторитетный eligible поиск по имени; неоднозначность требует уточнения |
| `get_configuration_catalog` | `AGENT_RUNTIME_CONFIGURATION`, exact найденный `entity_ref` — locator, не authority |
| RuntimeController → CP | Существующий RPC и exact lease/fence/generation, без нового bearer/permission |
| CP owner read | Fresh actor из lease, tenant, `agent.view`, project boundary; чужой/скрытый target закрыто отклоняется |
| Snapshot | RepeatableRead, current agent/configuration/ENV binding/image versions и canonical SHA; metadata модели и CPU/memory, без значений env, Secret или инструкций |
| Callback → ответ | Exact source/target/project/version/SHA, bounded страницы до EOF; никаких SQL writes/audit/events |

Terminal/expiry/revocation продолжают закрывать прежнюю lease и запрещают
чтение; неверный fence/generation не восстанавливается текущим экраном.
SYSTEM не превращается в право читать чужой tenant; PROJECT ограничен своим
проектом. Source helper и target employee не подменяют друг друга.

# Checklist

- [x] Реальный no-ID кейс: дефект установлен, не исправлен корректирующим USER сообщением.
- [x] Owner read безопасных runtime metadata по resolved eligible ref, независимо от текущего экрана.
- [x] Canonical response модели и resource policy; private env/Secret/instruction fields отсутствуют.
- [x] Callback schema/parser/caster/pagination и guidance, без legacy decoder.
- [x] Быстрые unit и disposable positive/foreign/stale/expiry/no-effect проверки.
- [ ] Repo-owned доставка только при безопасном останове текущих model turns; exact source/serving readback.
- [ ] Повтор того же натурального вопроса, ответ с фактическими model/CPU/memory, Console/Network/UX.

Code-tree evidence14.10: callback package PASS6.580s, CP projection tests
PASS0.086s, disposable ProjectAssistantIntegrationGrantsComponent PASS49.334s
с positive SYSTEM/PROJECT и отдельной cross-project negative проверкой.
Первый component FAIL35.435s ожидал прежний запрет Workflow-context metadata;
инструкции остались закрытыми, ожидание metadata обновлено. Ранние stale
schema/pagination fixtures FAIL исправлены. Serving HOT остаётсяbb938a00:
новый reader не выдаётся за доступный модели до доставки и native повтора.
