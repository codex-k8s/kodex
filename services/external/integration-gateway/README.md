# integration-gateway

`integration-gateway` — stateless worker типизированных внешних capabilities.
Метаданные подключений, grants, leases, результаты и audit принадлежат
`control-plane`. Пустой набор подключений является штатным состоянием и не
влияет на readiness.

Юнит не предоставляет универсальный proxy. Встроенный закрытый реестр
поддерживает GitHub repository read, Kubernetes workload read и независимые
Mattermost notification/result mirror operations. Credential material читается
только из server-mounted файлов по выданной control-plane ссылке и никогда не
возвращается в API, логи или результат.

`/healthz` отражает жизнь процесса, `/readyz` читает локальный снимок sidecar
authority. Доступность control-plane и внешних систем наблюдается отдельным
рабочим/diagnostic контуром и не меняет Kubernetes readiness pod.
