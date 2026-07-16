---
id: ADR-MC-009
title: Направление лицензирования
type: decision
status: proposed
owner: owner
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-009. Направление лицензирования

## Контекст

Цель — публичный исходный код и сообщество при сохранении возможности коммерческой managed-платформы, аренды изолированных clusters и системной интеграции.

Настоящая OSS license не может запрещать конкурентное коммерческое использование или hosting. MIT/Apache не требуют раскрытия server modifications и не соответствуют выбранной защите бизнеса.

## Proposed direction

AGPLv3 для публичного продукта плюс отдельная коммерческая лицензия, trademark policy и contributor agreement, достаточный для dual licensing/relicensing.

AGPL требует предложения source network users для modified network service, но не запрещает конкуренту оказывать hosting при соблюдении лицензии. Бизнес-защита строится также на managed service, integrations, поддержке, бренде и скорости развития.

## Gate

До добавления `LICENSE` и публичного релиза обязательны:

- заключение профильного юриста;
- dependency/license inventory;
- решение по CLA/DCO и copyright ownership;
- trademark policy;
- определение границы community/commercial offerings;
- проверка совместимости всех bundled components.

Альтернатива при приоритете запрета competing hosting — FSL/BSL source-available. В этом случае продукт нельзя называть OSS до перехода версии под OSI-approved license.

## Источники

- https://opensource.org/osd
- https://www.gnu.org/licenses/agpl.html
- https://fsl.software/
- https://mariadb.com/bsl-faq-mariadb/
