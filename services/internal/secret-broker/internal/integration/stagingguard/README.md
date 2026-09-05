---
id: OPS-SECRET-DRAFT-GUARD-1068
title: Устойчивый guard ключей черновиков
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Устойчивый guard ключей черновиков

Часть #1068. `New(client, namespace, name)` принимает client
`CoreV1().ConfigMaps(namespace)` и работает только с точным существующим
ConfigMap. Guard не имеет методов create/delete/patch и не получает материал
ключей. Kubernetes RBAC разрешает только `get/update` точного `resourceName`;
доставка keyring и genesis принадлежат отдельной bootstrap identity.

Обязательные labels:

- `app.kubernetes.io/managed-by: kodex-secret-broker-bootstrap`;
- `kodex.dev/purpose: secret-draft-key-guard`.

Единственный ключ `data.state.json` первоначально содержит:

```json
{"v":1,"manifest":null,"uses":[]}
```

Genesis создаётся отдельным create-only bootstrap. Он не входит в обычный
declarative apply, не восстанавливается автоматически после удаления и не
заменяет существующее состояние. Отсутствующий/удаляемый/immutable объект,
неверные namespace/name/labels/UID/resourceVersion, лишние data/binaryData,
повреждённый JSON, неизвестные/повторные поля и невалидный manifest/counter
закрыто отклоняются. Значение `null` для `uses` запрещено.

После Observe состояние содержит `manifest` типа `value.DraftKeyManifest`
(вложенный `DraftEncryptionKey` сериализуется как `ID`/`Generation`) и `uses`
с элементами `{"id":"<lowercase-sha256>","generation":1,"encryptions":0}`.
Массивы отсортированы по generation, соответствуют друг другу и ограничены
128 ключами. Digest manifest — lowercase SHA-256 `json.Marshal(manifest)`
при пустом поле `Digest`. Поколение каждого ключа не превышает revision.

Observe допускает только монотонную revision; равная revision требует точный
digest. Current — максимальное поколение; все ранее принятые ID/generation
сохраняются, смена ID для generation или generation для ID запрещена.
Перескок ревизий разрешён при сохранении всех принятых ключей. Счётчики старых
ключей переносятся без сброса.

Reserve в одной CAS-операции повторно проверяет exact current ID/generation и
увеличивает его счётчик до фиксированного лимита `1 << 24`. На границе лимита
требуется новое поколение; возврат старого write key невозможен. Неопределённый
исход Update возвращает ошибку без разрешения на шифрование: уже записанная
резервация остаётся израсходованной. После restart другая реплика читает тот
же durable state; отдельного in-memory watermark нет.

Оба пути используют Get + Update с точным resourceVersion и UID, не более
8 попыток при Conflict и общий timeout 5 секунд от контекста вызывающего.
Каждый retry перечитывает и полностью проверяет актуальное состояние. Ответ
Update подтверждает тот же UID, новую resourceVersion и точное записанное
состояние. Ошибки API не передают наружу исходные diagnostics.

Context7: `/kubernetes/client-go`, Get/Update и refetch при optimistic
concurrency conflict. Локальные targeted tests проверяют конкурирующие
реплики, лимит, lost response, restart, rotation/rollback/key reuse,
отмену, CAS exhaustion, подмену и повреждение объекта. Live Kubernetes
проверка выполняется общим разрешённым контуром #1068, отдельно от unit tests.
