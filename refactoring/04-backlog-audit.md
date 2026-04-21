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
| PR #587 | closed as superseded reference | PR реализовывал legacy-подход к Mission Control prototype и не является базой новой платформы | оставить как historical reference без продолжения |

## Ключевые open issues

| Issue | Домен | Решение | Причина | Следующее действие |
|---|---|---|---|---|
| #470 | Risk/release governance | rewrite | суть нужна, но старый Sprint/Mission Control framing заменён новой wave-моделью | использовать как execution-anchor волны 4 |
| #281 | Repo onboarding | rewrite | сценарий остаётся релевантным, но должен жить в новой provider-first модели onboarding | держать как будущий execution issue после доменов `Проекты и репозитории` + `Provider-native рабочие сущности` |
| #282 | Existing repo adoption | rewrite | сценарий остаётся релевантным, но должен жить в новой provider-first модели onboarding | держать как будущий execution issue после доменов `Проекты и репозитории` + `Provider-native рабочие сущности` |
| #309 | First-run onboarding | rewrite | проблема `time to first successful run` остаётся, но старое stage/launcher framing устарело | держать как reference issue до волны 5 по UX/frontend |
| #586 | Knowledge/memory | rewrite, defer | vendor-specific формулировка больше не подходит, но сам домен останется нужен позже | вернуться после домена `Документация и knowledge lifecycle` |

## Закрытые issues по результатам checkpoint после wave 3

| Issue | Домен | Решение | Причина | Зафиксировано в |
|---|---|---|---|---|
| #376 | Provider metadata usage | close as absorbed | provider-native поля, relationships, milestone/project fields, watermark и приёмка уже вошли в канонику wave 1-3 | `refactoring/06-product-model.md`, `refactoring/08-provider-native-work-model.md`, `refactoring/11-data-and-state-model.md`, `refactoring/12-provider-integration-model.md`, `refactoring/13-artifact-contract-and-acceptance.md` |
| #488 | Delivery governance | close as absorbed | compact PR policy и `prototype -> production conversion` уже вошли в канонику программы | `refactoring/01-program-charter.md`, `refactoring/05-delivery-and-risk-principles.md`, `docs/delivery/development_process_requirements.md` |
| #489 | Legacy revise trigger | close as obsolete | старый label-driven `run:*:revise` trigger не является каноникой новой orchestration-модели | wave 1-3 каноника `agent-manager` / `provider-hub` / acceptance policy |

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

### Результат checkpoint после принятия wave 3
Отдельный компактный GitHub backlog pass выполнен.

Зафиксированы такие действия:
- `#376` закрыт как `absorbed`;
- `#488` закрыт как `absorbed`;
- `#489` закрыт как `obsolete`;
- `#470`, `#281`, `#282`, `#309`, `#586` переписаны под новую программу без legacy-framing.

Следующий обязательный backlog checkpoint выполняется перед стартом первых больших implementation waves.

## Ритуал обновления этого файла
Перед стартом каждой новой волны:
1. перечитать актуальные open issues и active PR;
2. обновить таблицы `решение / причина / следующее действие`;
3. отдельно отметить, какие artifacts:
   - закрываются как obsolete;
   - переписываются под новый домен;
   - остаются reference-only;
   - становятся execution issues ближайшей волны.
