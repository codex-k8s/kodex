# secretresolver

`libs/go/secretresolver` содержит общий контракт безопасного получения секретов по ссылке, которую уже разрешил сервис-владелец доступа.

## Назначение

- `access-manager` остаётся владельцем внешних аккаунтов, правил доступа и ссылок на секреты.
- Доменные сервисы сначала получают разрешение и `secret_store_type + secret_store_ref` через `access-manager`.
- `secretresolver` получает значение по этой ссылке только в памяти процесса и только на время операции.
- Значение секрета не сериализуется в JSON/text, не печатается через `fmt` и не должно попадать в БД, события, тело аудита, трассировку, логи или ошибки.
- Вызывающий код обязан завершать время жизни значения через `defer value.Clear()` сразу после успешного `Resolve`.

## Контракты

- `Resolver.Resolve` возвращает `SecretValue` для операций, которым действительно нужно значение секрета.
- `Checker.Check` проверяет наличие секрета без возврата значения вызывающему коду.
- `Mux` выбирает backend по `SecretRef.StoreType`.
- `MountedKubernetesBackend` читает заранее смонтированные Kubernetes Secret из файловой системы.
- `EnvBackend` читает секреты, уже переданные workload через переменные окружения.
- `VaultBackend` читает Vault KV v2 через официальный Go SDK `github.com/hashicorp/vault/api`.

Пример использования:

```go
value, err := resolver.Resolve(ctx, ref)
if err != nil {
	return err
}
defer value.Clear()

token := value.Bytes()
defer clear(token)
```

## Ссылки на хранилища

### Kubernetes mounted

- `StoreType`: `kubernetes_mounted_secret`;
- `StoreRef`: `namespace/secret-name#key`;
- файл: `<root>/<namespace>/<secret-name>/<key>`.

### Env

- `StoreType`: `env`;
- `StoreRef`: имя переменной окружения.

### Vault KV v2

- `StoreType`: `vault`;
- `StoreRef`: `mount/path/to/secret#key`.

Kubernetes Secret, env, Vault или другой тип хранилища остаются деталями resolver-клиента. `provider-hub` и `package-hub` не должны включать эти детали в свою доменную модель.

## Проверки

```bash
cd libs/go/secretresolver && go test ./...
```
