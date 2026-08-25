---
id: INFRA-DOC-001
title: Infrastructure Guide
type: guide
status: approved
owner: SRE
version: 2.0.0
updated: 2026-08-25
---

# Infrastructure Guide

## Code-first

Инфраструктура изменяется только через script/manifest/workflow в репозитории,
PR, review, owner gate и запуск того же кода. Ручная production-настройка без
последующего точного закрепления в репозитории запрещена.

Каждая операция имеет `preflight|apply|readback` либо эквивалентный явный
контракт. Readback проверяет фактически обслуживаемое состояние, а не только
успех `kubectl apply`.

## Профили установки

- `bare-metal`: ОС, firewall, k3s и полный infrastructure profile;
- `existing-kubernetes`: exact kubeconfig/context и выбранные компоненты без
  изменения хоста.

Приложение принадлежит namespace `kodex-system`. Служебные компоненты разделены:
`identity`, `observability`, `platform-admin`, `kodex-infra`, `kodex-ci`,
`kodex-ci-deploy`, `cert-manager` и `kodex-trust`.

## Secret delivery

Источником входных installation values является `.kodex-env` mode `0600`.
Сгенерированные private keys/passwords хранятся в `.kodex-material` mode `0700`
и Kubernetes Secrets. ConfigMap, render и GitHub artifact не содержат secret
values.

Static Secret обновляет installer. Runtime key-delivery Secret обновляет только
`internal-rpc-authority-publisher` через exact RBAC `resourceNames`, generation
annotation и compare/readback. Workload получает только exact keys выбранного
Secret как read-only volume или `secretKeyRef`.

PostgreSQL bootstrap создаёт `NOLOGIN` group roles. Одноразовая Job задаёт
SCRAM passwords для закрытого списка LOGIN roles из Secret, после чего migrations
и runtime используют разные principals. Caller-set GUC не считается identity.

## Build и release

- GitHub ARC использует отдельные build/deploy scale sets;
- BuildKit запускается rootless sidecar в ephemeral build runner;
- release registry хранится на целевом сервере и доступен по HTTPS;
- build публикует immutable digest и формирует release lock;
- render привязан к exact source SHA, build run и lock SHA-256;
- target installer скачивает render и применяет фазы в согласованном context;
- deploy runner не получает read access к Kubernetes Secrets.

Host containerd получает registry credentials через k3s `registries.yaml` с
TLS verification. `insecure_skip_verify`, plaintext registry и token в command
line запрещены.

## Identity и management

Control Center, Grafana и Headlamp доступны только через OAuth2 Proxy/Keycloak.
Keycloak `admin` означает `cluster-admin` только для Headlamp. Доступ к
Prometheus/Alertmanager извне отсутствует.

## Network policy

Policy строится по итоговому render. Для PostgreSQL, NATS, telemetry,
Kubernetes API и registry задаются exact destination и port. Правило только по
порту, wildcard egress и обход proxy запрещены.

## Stateful dependencies

Web-first профиль включает PostgreSQL и NATS. Redis добавляется только unit,
которому он действительно нужен. S3 не является обязательным для MVP;
production backup подключает внешний S3 отдельным capability и Secret.

## Ресурсы и устойчивость

- requests задают schedulability; memory limits защищают node от runaway
  process, но не должны быть ниже измеренного working set;
- stateful workloads имеют PVC, readiness и bounded shutdown;
- singleton Job имеет timeout/backoff и точный readback;
- controller/worker не начинает внешние действия до startup barrier;
- alert содержит абсолютный HTTPS `runbook_url`.

## Версии

CLI/binary/chart pins и SHA-256 хранятся в `tools/install/components.lock.json`,
`infra/**/charts.lock.json` и ARC chart lock. Обновление версии выполняется
отдельным PR с проверкой официальной документации и checksum.

## Запрещённые подходы

- secret values в Git, GitHub variables, ConfigMap или manifest;
- скрытая установка обязательного object storage;
- ручной untracked deployment;
- direct push в `main`;
- применение render без exact digest/provenance;
- запуск application workloads до migrations и authority Secret projections.
