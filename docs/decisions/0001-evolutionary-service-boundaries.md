---
id: ADR-MC-001
title: Эволюционные границы сервисов
type: decision
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# ADR-MC-001. Эволюционные границы сервисов

## Контекст

Текущий bot-service объединяет Mattermost transport, onboarding, persistence, session orchestration и Kubernetes runtime. Немедленное переписывание или физическое выделение всех сервисов одновременно создаст миграционный и operational риск.

## Решение

Сначала вводятся bounded-context packages, отдельные repositories/use cases, characterization tests и contracts внутри совместимого deployable. Затем по одному выделяются runtime-controller, integration-gateway, interaction-gateway, control-plane и automation-scheduler.

## Последствия

- Временная совместимость и adapters увеличат объем кода.
- PR остаются проверяемыми на live-инсталляции.
- Service split следует границам данных, а не размерам файлов.
- Новый cross-domain code через общий `admin.Repository` запрещен.

## Отклонено

- Big-bang rewrite.
- Сохранение текущего монолита без enforceable boundaries.
- Немедленное создание десятков микросервисов.
