# Локальный допуск сервиса к RPC

Часть перехода #1470. `Authorizer` проверяет mTLS peer и неизменяемый локальный
список точных методов. Авторизатор не вызывает issuer, proof resolver и общую
регистрацию Pod. Разрешения target копируются при создании; изменение исходного
slice не расширяет уже работающую policy.

`Admit` возвращает только `Admission`: caller/target SPIFFE identity, digest и
срок сертификата, method/operation/permission и следующий вид проверки actor.
Здесь нет actor ID, tenant ID, project ID или токена задачи. Результат нельзя
выдавать за `VerifiedAuthorizationContext` старого протокола или готовый
пользовательский Principal.

После допуска владелец данных обязан выполнить соответствующий путь:

| ActorMode | Следующая проверка владельца |
| --- | --- |
| SERVICE_OWNER_RESOLVED | Разрешить служебный principal и ресурс по authoritative state; проверить lease/grant/attempt там, где их требует конкретная операция |
| USER_CREDENTIAL_REQUIRED | Проверить пользовательский credential, session boundary, принадлежность org/project и конкретное разрешение |
| TASK_DELEGATION_REQUIRED | Проверить подписанное/authoritative делегирование, root actor, attempt/input, grant, scope, revoke и replay |

Metadata клиента не назначает режим или permission. Неизвестные пары
caller+method отклоняются; новый метод добавляется явным релизом target policy.
Повторный RPC проверяет срок сертификата даже на существующем HTTP/2 соединении.
В ускоренном MVP-профиле `CertificateLifetimeBoundary` ограничивает допуск
сроком сертификата и отменой контекста. Это осознанная временная граница:
экстренный отзыв отдельного скомпрометированного сертификата требует ротации
trust material. Независимая доставка bounded emergency revoke вынесена в #1527.

Unary interceptor помещает Admission в закрытый server context. Stream
interceptor дополнительно проверяет допуск перед отправкой и до/после
блокирующего чтения сообщения. После чтения только успешный результат передаётся
доменному обработчику. Проверка предметных полномочий остаётся обязательной.

Сервер устанавливает TLS через `credentials.NewTLS` с
`ClientAuth: tls.RequireAndVerifyClientCert`, точной CA и настройками hostname.
Проверены официальные примеры gRPC Go mTLS через Context7 `/grpc/grpc-go`.

CP composition root одновременно принимает прежний подписанный профиль и новый
явный `service-v1`. Исходящие клиенты выбирают новый профиль в коде, поэтому
переход выполняется readers-before-clients без изменения Deployment env.
Переходный CP сохраняет прежний proof service для task delegation и защищённых
материализаторов. Полное отделение их startup/readiness вынесено в #1528.
Локальный TLS handshake не доказывает live приёмку.

Target-owned policy находится в
`services/internal/control-plane/internal/app/service-identity-policy.json`, её явная
классификация — в соседнем `service-identity-classification.json`.
Классификация закрепляет digest полного исходного binding, включая permission,
request profile и provenance. Новый метод или изменение этих полей требует
отдельного изменения classification. Текущие 375 bindings: 290 пользовательских
и 85 служебных. `platform.stt.policy.resolve` сохраняет отдельное делегирование
и не входит в обычный allowlist. SERVICE означает обязательное разрешение
владельцем и сохранение domain lease/grant checks, а не освобождение от них.

Команда из корня репозитория проверяет воспроизводимость файла:

```sh
node tools/release/service-identity-policy.mjs check \
  deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json \
  services/internal/control-plane/internal/app/service-identity-classification.json \
  services/internal/control-plane/internal/app/service-identity-policy.json
```

`generate` вместо `check` обновляет только локальный output. Формат файла — JCS,
без форматирующих пробелов и завершающего перевода строки; реальный Go loader
проверяется отдельным тестом. Ошибка несовместимого JSON зарегистрирована в
#1475; её локальное исправление не является доказательством staging rollout.
