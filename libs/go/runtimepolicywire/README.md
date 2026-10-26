---
id: LIB-RUNTIME-POLICY-WIRE-001
title: Типизированные имена runtime-политик
type: library
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-26
---

# Типизированные имена runtime-политик

Модуль связывает три закрытых Proto enum control-plane с именами из runtimecontract.
Два production-потребителя — входной caster control-plane и материализация
immutable runtime snapshot в runtime-controller. Runtimecontract сохраняет
независимость от Proto, а generated controlplaneapi не получает доменной зависимости.

API: VolumeKindFromProto, NetworkDestinationFromProto и KubernetesAccessKindFromProto.
Поддержаны временные тома disk/memory, назначения DNS/runtime callback/provider proxy
и профиль Kubernetes NONE. Нулевые, неизвестные и неподдержанные числовые значения
дают пустую строку. Вызывающий обязан передать результат своему закрытому валидатору;
пустая строка не является разрешением или новой политикой по умолчанию.

Функции не выполняют I/O, не владеют контекстом, ресурсом или lifecycle и не создают
метрик либо логов. Authority, scope, подписи, digest, ограничения, namespace,
service account, сетевые правила и ошибки полного объекта остаются у сервисов.
Транзакций, grant/claim/lease либо событий здесь нет.

Generated output и wire schema не меняются. Расширение допустимых enums требует
отдельного изменения контракта и потребителей; неизвестный enum автоматически
не становится поддержанным. Различающиеся преобразования web access и одиночный
network protocol в эту библиотеку не переносились. Проверки сохранности поведения
и build/unit двух consumers описаны в OPS-CODE-QUALITY-1823.
