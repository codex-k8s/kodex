---
doc_id: REF-CK8S-0005
type: governance
title: "kodex — delivery и risk principles для новой программы"
status: active
owner_role: EM
created_at: 2026-04-21
updated_at: 2026-04-23
related_issues: [470, 488]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave0"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Delivery и risk principles для новой программы

## 1. Compact PR policy
- Большие решения не принимаются одним огромным PR.
- Каждый production-oriented PR должен быть компактным и понятным для ревью.
- Если задача слишком большая, она делится:
  - по доменам;
  - по vertical slice;
  - по типу изменения (`docs`, `contracts`, `storage`, `runtime`, `ui`).

## 2. Prototype -> production conversion
- Быстрый прототип допустим как способ быстро проверить идею.
- После подтверждения идеи начинается отдельный production-путь:
  - фиксируется целевая модель;
  - прототип разбирается на последовательность компактных PR;
  - каждый PR меняет только один логический кусок;
  - временные решения удаляются по мере перехода к production-quality.

Дополнительное правило wave 5.1:
- enterprise target по package platform, tenancy, fleet, billing, release policy и automation rules фиксируется сразу в канонике;
- implementation floors по этим направлениям могут идти позже и поэтапно, но не должны менять уже принятую target-модель задним числом.

## 3. Owner review model
- Owner утверждает:
  - продуктовые документы;
  - архитектурные решения;
  - risk/release policy;
  - high-risk переходы;
  - go/no-go на релиз.
- Owner не рассматривается как обязательный построчный reviewer каждого изменения кода.
- Проверка кода и операционной готовности должна быть размазана по delivery-цепочке и risk-gates.

## 4. Базовая шкала риска
Подробная матрица будет спроектирована позже, но уже сейчас фиксируется seam:

| Класс | Характер изменения | Ожидаемый gate |
|---|---|---|
| `R0` | docs-only, naming, non-executable governance | review по документам |
| `R1` | локальный код без изменения контрактов, данных и runtime policy | pre-review + стандартные проверки |
| `R2` | бизнес-логика, provider semantics, data model, runtime behavior | усиленный technical review + QA/архитектурный gate |
| `R3` | auth, токены, destructive ops, release gates, production DB/cluster impact | обязательный human approval перед применением |

## 5. Что нельзя откладывать
- Классификацию риска нельзя оставлять "на потом" до релизной стадии.
- Уже в ранних доменных документах нужно фиксировать:
  - какие операции опасны;
  - какие изменения требуют обязательного human gate;
  - какие evidence нужны до release.

## 6. Следствия для новой архитектуры
- Risk/release governance должен проектироваться как first-class capability платформы.
- Любая orchestration-модель должна уметь:
  - понимать risk class;
  - включать нужные проверки;
  - останавливать недопустимый переход;
  - объяснять operator/owner, что именно блокирует дальнейшее движение.
