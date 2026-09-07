# Instance-scoped worker grants

Компонент включается оператором только после additive migration и обновления
всех control-plane consumers до поддержки grant v2 (#1220). По умолчанию
он не включён ни в один installation profile: старый CP строго отвергает v2.

Компонент применяется после штатных issuer components девяти worker Deployment.
Он добавляет исключительно Pod UID через Downward API и явный format marker.
Значения UID не задаются release payload или пользователем приложения.
Для одноразовых admission Jobs остаётся прежний v1 lifecycle.

Новый authority image должен быть доставлен до активации env. Повторный release
приложения сохраняет image sidecar и security configuration. Переключение
Recreate в RollingUpdate относится к отдельной release migration (#1222),
а не выполняется скрыто при включении этого компонента.

При refresh signer сохраняет instance и credential generation, меняя revision
и JTI. Повторный запуск в ту же секунду переиспользует точный signed envelope.
При неудачной записи прежний проверенный grant сохраняет readiness только до
собственного exp. Каждый readiness probe заново проверяет файл, identity и
время; истёкший/повреждённый grant закрывает readiness независимо от ticker.
Liveness остаётся локальной проверкой процесса.

Этот компонент не реализует ротацию signer keys. Она требует publish доверия,
readback всех consumers, переключения issuance и отдельного retire/revoke;
обычный application rollout не должен запускать такой протокол.
