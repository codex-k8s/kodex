# `grpcserver`

Общая gRPC error/recovery boundary MatterCodex. Она содержит единственный
предикат unexpected codes и ровно один error-observer вызов на transport
boundary. Service-specific domain mapping остаётся в сервисе.

`StrictProtoCodec` до handler отклоняет unknown fields, повтор singular/oneof
и malformed wire. Codec помечает request, а профильный transport возвращает
свой canonical `InvalidArgument`/detail; last-one-wins не используется как
authority.
