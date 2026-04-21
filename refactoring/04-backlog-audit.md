---
doc_id: REF-CK8S-0004
type: backlog-audit
title: "kodex — стартовый аудит backlog для программы рефакторинга"
status: active
owner_role: EM
created_at: 2026-04-21
updated_at: 2026-04-21
related_issues: [281, 282, 309, 376, 470, 488, 489, 586]
related_prs: [587]
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave0"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Стартовый аудит backlog для программы рефакторинга

## Цель
Зафиксировать минимальный начальный список открытых артефактов, которые уже влияют на новую программу, чтобы не продолжать старую модель по инерции.

## Активный PR

| Артефакт | Решение | Причина | Следующее действие |
|---|---|---|---|
| PR #587 | supersede / закрыть без продолжения текущего scope | PR реализует legacy-подход к Mission Control prototype и не является базой новой платформы | после фиксации wave 0 оформить закрытие/суперсединг отдельным действием |

## Ключевые open issues

| Issue | Домен | Решение | Причина | Следующее действие |
|---|---|---|---|---|
| #488 | Delivery governance | keep and absorb | идея про compact PR и prototype -> production conversion нужна новой программе | включить в новые process rules и после этого переоценить issue |
| #470 | Risk/release governance | keep, rewrite into new domain | суть нужна, но старый Mission Control/Sprint S14 framing устарел | вернуться после фиксации новой продуктовой модели и risk-domain |
| #281 | Repo onboarding | rewrite later | полезный сценарий, но требует новой provider-first и project/repo model | переписать после доменов `Проекты и репозитории` + `Provider-native work items` |
| #282 | Existing repo adoption | rewrite later | полезный сценарий, но сейчас завязан на старую модель платформы | переписать после новой onboarding-архитектуры |
| #309 | First-run onboarding | keep as reference, rewrite later | проблема валидна, но текущее описание привязано к старой stage-терминологии | использовать как reference при проектировании onboarding и agent-manager UX |
| #376 | Provider metadata usage | keep | напрямую относится к новой provider-first модели | включить в волну по provider data model |
| #489 | PR revise trigger | re-evaluate after new orchestration model | может стать obsolete или поменять смысл после новой orchestration model | не трогать до фиксации новой модели запусков |
| #586 | Knowledge/memory | defer | зависит от новой knowledge architecture и не должен утащить дизайн раньше времени | вернуться после доменов `Документация и knowledge lifecycle` |

## Дополнительные legacy-кандидаты
- Открытые issues из старых спринтов и stage-пакетов, построенные вокруг старой control-plane-центричной модели, считаются legacy-candidate backlog.
- Их детальная ревизия выполняется перед каждым следующим доменным срезом, а не откладывается до конца программы.

## Обязательный checkpoint по GitHub backlog
Отдельная большая ревизия GitHub Issues/PR должна пройти не "когда-нибудь потом", а в фиксированный момент программы:
1. после принятия волны 2 по целевой архитектуре и service boundaries;
2. после принятия волны 3 по данным и provider integration;
3. до запуска первых больших implementation waves.

На этом checkpoint нужно:
- закрыть obsolete issues, которые завязаны на старую control-plane-центричную модель;
- переписать или пересоздать issues, которые остаются релевантными, но меняют смысл в новой архитектуре;
- проверить старые открытые PR на предмет `close / supersede / rewrite later`;
- обновить этот файл по фактическому состоянию GitHub backlog.

## Ритуал обновления этого файла
Перед стартом каждой новой волны:
1. перечитать актуальные open issues и active PR;
2. обновить таблицы `решение / причина / следующее действие`;
3. отдельно отметить, какие artifacts:
   - закрываются как obsolete;
   - переписываются под новый домен;
   - остаются reference-only;
   - становятся execution issues ближайшей волны.
