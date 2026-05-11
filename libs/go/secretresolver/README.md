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
- `Checker.Check` проверяет наличие секрета без возврата значения.
- `Mux` выбирает backend по `SecretRef.StoreType`.
- `MountedKubernetesBackend` является минимальным MVP backend для Kubernetes Secret, смонтированных в файловую систему.

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

## Kubernetes mounted backend

Backend ожидает `SecretRef`:

- `StoreType`: `kubernetes_secret`;
- `StoreRef`: `namespace/secret-name#key`;
- файл: `<root>/<namespace>/<secret-name>/<key>`.

Kubernetes Secret, Vault или другой backend остаются деталями resolver-клиента. `provider-hub` и `package-hub` не должны включать эти детали в свою доменную модель.

## Проверки

```bash
cd libs/go/secretresolver && go test ./...
```
