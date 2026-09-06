---
title: Публичные assets Control Center
type: component-documentation
status: approved
owner: developer
---

# Публичные assets Control Center

## Offline bootstrap в локальном и disposable профиле

Issue #1107: `render-local.sh` запускает `prime-frontend-cache.sh` на доверенном
host до применения manifest. Установка выполняется в Node image по digest из
PWA Dockerfile, без host npmrc и без lifecycle scripts. Receipt связывает точные
package.json/package-lock.json, image, Node/npm, platform/arch/native ABI и Alpine.
Только успешная установка с загрузкой Vite/native dependencies публикуется
атомарным rename; параллельные prime сериализуются. Незавершённая установка
удаляется, существующий несовместимый cache не считается успешным.

Pod получает точный cache read-only и никогда не вызывает npm install.
Отсутствующий или устаревший receipt закрыто останавливает startup с диагностикой
`frontend cache is absent or stale; run trusted host render`. Изменение lock
требует повторного repo-owned render/deploy; старый cache не изменяется под
работающим Pod. Vite читает source/runtime config read-only, использует
`--configLoader runner` и отдельный `/tmp/vite-cache`. NetworkPolicy сохраняется.

Проверка: `make test-frontend-offline-bootstrap` (Docker, до 900 секунд), затем
`scripts/tests/local-role-image-render-contract-test.sh` для обоих профилей.
Первый prime требует registry на доверенном host; рабочий Vite проверяется с
`--network none`. Ручная проверка владельца после merge: штатный remote render/up,
readiness Pod, `/src/main.ts`, manifest и runtime config без install/restart loop.
Rollback: вернуть предыдущий source SHA и повторить тот же render/deploy.

Issue #1022 / #1101, один PWA unit. `public-assets-ingress.yaml` открывает
ровно `/manifest.webmanifest`, `/logo.png`, `/sw.js` через `pathType: Exact`.
Priority300 отделён от API200 и основного browser router. Все три используют
тот же host, TLS Secret и Service с ingress mTLS. Regex/prefix exemptions в
OAuth2 Proxy не добавляются. `/config/runtime-config.json`, `/api/v1` и
остальные пути сохраняют прежний auth boundary.

Manifest ссылается только на `/logo.png`; отдельной Apple touch ссылки нет.
Worker зарегистрирован как `/sw.js` со scope `/` и `updateViaCache: none`,
не импортирует дополнительные assets, очищает cache при activation и получает
API/auth/config/navigation через network с `no-store`. Публичность worker не
делает публичными его запросы. `start_url: /` намеренно требует входа.

`public-assets.conf` входит в тот же immutable runtime ConfigMap через
server-level include. Manifest имеет MIME `application/manifest+json`, оба
точных static location возвращают404 при отсутствии файла вместо SPA shell.
Проверка ingress client certificate сохранена. Существующий worker location
возвращает JavaScript и `no-store`. Local public-acme render переключает новый
Ingress на фактический HTTP port hot-reload Service, сохраняя exact routes.
Непубличный local-ca профиль сохраняет существующий режим локальной разработки;
он не является доказательством browser OAuth boundary публичного профиля.

## Карта сценариев и проверка

| Инициатор | Route / consumer | Authority и результат |
|---|---|---|
| Anonymous или authenticated browser | Exact manifest/icon/SW → Service → nginx static file | Публичный immutable build asset; ingress mTLS;200 и exact MIME; нет domain mutation/event/idempotency |
| Anonymous browser | `/` / config / private suffix → browser auth chain | OAuth2 auth401 → signin redirect; публичный allowlist не совпадает |
| Anonymous API client | `/api/v1` → auth middleware → gateway |401 без HTML redirect; прикладная authority остаётся у gateway/owner |
| Authenticated browser | Private routes и API | Обычные session/owner checks; worker не читает cached private state |

`make test-web-only-release` включает stateless проверку обеих profile renders
и source references. Для конкретного public-acme hot-reload файла:

```sh
python3 scripts/tests/pwa-public-assets-test.py --render /absolute/path/render.yaml
```

HTTP fixture требует Docker, openssl, kubectl/yq и уже загруженные exact images:
production nginx берётся из PWA Dockerfile; Traefik fixture закреплён digest в
script. Оснастка не скачивает images автоматически:

```sh
python3 scripts/tests/pwa-public-assets-test.py --http
# Та же проверка с обязательным внешним бюджетом:
make test-pwa-public-assets-http
```

Два контейнера работают без root/capabilities, на internal Docker network без
публикации host ports, read-only mount только disposable fixtures и
ограничениями CPU/memory/pids. Настоящие Traefik и production nginx проверяют
MIME, asset hashes, redirects, private/API negatives и backend mTLS на
одноразовой CA. Auth responder реализует synthetic202/401; это не проверка
реального OAuth2 login, cookies, IdP либо provider. Весь контур ограничен
внешним `timeout 180s`; каждый subprocess/request имеет внутренний timeout.

После отдельного owner gate выполнить тем же repo render разрешённое
развёртывание: новая browser session → manifest/icons/SW200 без redirect →
установка PWA → вход → session refresh/logout → protected endpoints без
session недоступны. Live acceptance до этого остаётся NOT RUN. Rollback —
согласованный предыдущий PWA image и runtime ConfigMap/Ingress revision;
удаление public Ingress возвращает исходный OAuth redirect без открытия API.

Проверены Context7 resolve→query официальных справочников:
[Traefik Ingress](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/ingress),
[OAuth2 Proxy ForwardAuth](https://oauth2-proxy.github.io/oauth2-proxy/configuration/integrations/traefik),
[Kubernetes Ingress path types](https://kubernetes.io/docs/concepts/services-networking/ingress/#path-types).
Также проверены [nginx location/types](https://nginx.org/en/docs/http/ngx_http_core_module.html)
и [наследование headers/expires](https://nginx.org/en/docs/http/ngx_http_headers_module.html).
