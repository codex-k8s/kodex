---
id: OPS-DOC-1105
title: Передача registry credentials без process argv
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-06
---

# Registry credentials без process argv

Refs #1105, #1031. Seed, admission и cleanup больше не передают private PEM,
password либо обратимый Docker auth в аргументах дочерних утилит.
Seed и admission формируют private `REGCTL_CONFIG` через `jq --rawfile`;
admission сохраняет Docker auth для остальных registry consumers через тот же
file input. Cleanup сохраняет закреплённый regctl0.9.2-alpine без новой
зависимости jq: BusyBox awk читает файлы и сериализует JSON с escaping.
Неизвестные control bytes закрыто отклоняются до registry effect.

Файлы JSON создаются с umask077, каталоги и credentials удаляются EXIT cleanup
при успехе и ошибке. Seed сохраняет container-root cleanup для rootless Docker.
Admission сохраняет отдельные host entries для staging/evidence/promotion.
TLS остаётся enabled, CA/client certificate/private key и назначения прежние;
прежние image import/copy и exact digest readback не меняются.

Проверенные официальные источники после Context7 resolve/query:
[CLI registry set](https://regclient.org/cli/regctl/registry/set/),
[config v0.11.5](https://github.com/regclient/regclient/blob/v0.11.5/cmd/regctl/config.go),
[host v0.11.5](https://github.com/regclient/regclient/blob/v0.11.5/config/host.go),
[TLS v0.9.2](https://github.com/regclient/regclient/blob/v0.9.2/internal/reghttp/http.go).
Флаг client-key принимает PEM contents, не filename. Docker certs.d в0.9.2
не заменяет mTLS clientKey: его loader читает только CA. Поэтому все три
пути используют explicit clientKey в private JSON, а не неподдерживаемую
подмену ключа путём.

## Проверка

`make test-registry-credential-files` имеет бюджет30s. Он исполняет реальные
shell-фрагменты с synthetic sentinels, настоящими jq/awk и закрытыми mocks
Docker/regctl: exact fields, JSON escaping, mode0600, несколько host entries,
argv/stdout/stderr, success/failure cleanup и отсутствие registry effect при
недоступном ключе. Проверка подключена к local-role-image-render contract.
Она не является live mTLS или фактическим registry import.

После merge владелец запускает тот же repo-owned seed на disposable стенде:
exact source/image, успешные import/copy/digest readback, отсутствие временных
credentials после terminal. Отдельно проверяются реальные scan/sign/promote
и cleanup с прежними identities. Значения credentials, JSON config и process
argv с историческими секретами не публикуются. Live здесь NOT RUN.

Rollback — revert PR и новый code-first deploy; откат возвращает известный
argv defect и не считается безопасным способом продолжить live acceptance.
