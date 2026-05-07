# accesscheck

`libs/go/accesscheck` содержит общий клиент `access-manager` для внутренних сервисов платформы.

Назначение:
- единообразно открывать gRPC-соединение к `access-manager`;
- добавлять service-token metadata (`authorization`, caller type/id);
- собирать `CheckAccessRequest`;
- применять общий timeout;
- приводить gRPC-ошибки к стабильным ошибкам библиотеки.

Сервисный слой не должен копировать этот код. Внутри сервиса остаётся только адаптер:
- преобразовать доменный `AuthorizationRequest` в `accesscheck.Request`;
- вызвать `Client.Check`;
- сопоставить ошибки `accesscheck` с ошибками своего домена.

Для штатного случая используется generic `accesscheck.Authorizer[T]`: сервис передаёт mapper из своего доменного запроса в `accesscheck.Request` и набор доменных ошибок.

Минимальный пример:

```go
conn, err := accesscheck.NewConnection(accessManagerAddr)
if err != nil {
    return err
}
checker, err := accesscheck.New(accessaccountsv1.NewAccessManagerServiceClient(conn), accesscheck.Config{
    AuthToken: authToken,
    CallerID:  "project-catalog",
})
if err != nil {
    return err
}
err = checker.Check(ctx, accesscheck.Request{
    Subject:   accesscheck.Subject{Type: "service", ID: "agent-manager"},
    ActionKey: "project.read",
    Resource:  accesscheck.Resource{Type: "project", ID: projectID},
    Scope:     accesscheck.Scope{Type: "project", ID: projectID},
})
```

Для сервисного адаптера:

```go
authorizer, err := accesscheck.NewAuthorizer(client, accesscheck.Config{
    AuthToken: token,
    CallerID:  "runtime-manager",
}, toAccessRequest, accesscheck.DomainErrors{
    InvalidRequest:        errs.ErrInvalidArgument,
    Forbidden:             errs.ErrForbidden,
    DependencyUnavailable: errs.ErrDependencyUnavailable,
})
```
