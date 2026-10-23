package platform

import _ "embed"

var (
	// queryAccessBootstrapInsertPermission inserts one permission catalog definition on first bootstrap and leaves an existing key unchanged; it grants no permission.
	// queryAccessBootstrapInsertPermission вставляет определение разрешения при первом bootstrap и оставляет существующий ключ без изменения; разрешение никому не выдаёт.
	//go:embed sql/access_bootstrap_insert_permission.sql
	queryAccessBootstrapInsertPermission string
	// queryAccessInsertRole creates an application role with tenant, kind, optional stable key and creator, returning its initial state/version; no permissions are attached yet.
	// queryAccessInsertRole создаёт прикладную роль с организацией, видом, необязательным стабильным ключом и создателем, возвращая исходные состояние и версию; разрешения ещё не прикреплены.
	//go:embed sql/access_insert_role.sql
	queryAccessInsertRole string
	// queryAccessInsertRoleVersion inserts a numbered role version with proposed permission keys and scopes; it does not activate that version or validate the requested authority.
	// queryAccessInsertRoleVersion создаёт нумерованную версию роли с предложенными разрешениями и областями; эту версию не активирует и запрошенные полномочия не подтверждает.
	//go:embed sql/access_insert_role_version.sql
	queryAccessInsertRoleVersion string
	// queryAccessActivateRoleVersion sets the current role version under tenant and optional expected aggregate version, preserving initial role version number but advancing later replacements.
	// queryAccessActivateRoleVersion устанавливает текущую версию роли по организации и необязательной ожидаемой версии агрегата, сохраняя начальный номер роли и увеличивая его при дальнейших заменах.
	//go:embed sql/access_activate_role_version.sql
	queryAccessActivateRoleVersion string
	// queryAccessInsertBinding creates a binding to exact subject/group, role version, scope, validity and owner condition with supplied actor ID; caller validates grants and target eligibility.
	// queryAccessInsertBinding создаёт привязку к точным субъекту или группе, версии роли, области, срокам и условию владельца с заданным ID автора; выдачу прав и допустимость цели проверяет вызывающий код.
	//go:embed sql/access_insert_binding.sql
	queryAccessInsertBinding string
	// queryAccessListPermissions lists the closed permission registry ordered by key, including allowed scopes and owner-condition support, without computing a subject's access.
	// queryAccessListPermissions возвращает закрытый реестр разрешений по ключу с допустимыми областями и поддержкой условия владельца, не вычисляя доступ субъекта.
	//go:embed sql/access_list_permissions.sql
	queryAccessListPermissions string
	// queryAccessResolveSubject resolves one tenant subject by ref with kind, active flag and OIDC group refs; the row may be inactive and is not an authorization proof.
	// queryAccessResolveSubject разрешает одного субъекта организации по ссылке с видом, признаком активности и OIDC-группами; строка может быть неактивной и не доказывает доступ.
	//go:embed sql/access_resolve_subject.sql
	queryAccessResolveSubject string
	// queryAccessResolveGroup resolves one tenant OIDC group by ref and returns whether its stored state is ACTIVE; freshness and membership are not checked here.
	// queryAccessResolveGroup разрешает одну OIDC-группу организации по ссылке и возвращает признак ACTIVE из сохранённого состояния; свежесть и членство здесь не проверяются.
	//go:embed sql/access_resolve_group.sql
	queryAccessResolveGroup string
	// queryAccessListSubjects pages tenant subjects by ref with optional kind/name search and sorted OIDC group refs; inactive subjects remain visible in this catalog.
	// queryAccessListSubjects выдаёт страницы субъектов организации по ссылке с фильтрами вида и имени и отсортированными OIDC-группами; неактивные субъекты остаются видимыми в каталоге.
	//go:embed sql/access_list_subjects.sql
	queryAccessListSubjects string
	// queryAccessListOIDCGroups pages tenant OIDC groups by ref, reporting STALE after 24 hours and counts of members/ACTIVE bindings; this is a display projection, not current authority.
	// queryAccessListOIDCGroups выдаёт страницы OIDC-групп организации по ссылке, показывая STALE после 24 часов и числа участников и ACTIVE привязок; это отображение, не текущие полномочия.
	//go:embed sql/access_list_oidc_groups.sql
	queryAccessListOIDCGroups string
	// queryAccessListRoles pages tenant roles by ref with optional archived/search aliases and current version metadata plus ACTIVE binding count; it does not expand effective permissions.
	// queryAccessListRoles выдаёт страницы ролей организации по ссылке с необязательными архивными и поисковыми фильтрами, метаданными текущей версии и числом ACTIVE привязок; эффективные права не вычисляет.
	//go:embed sql/access_list_roles.sql
	queryAccessListRoles string
	// queryAccessGetRole reads one tenant role's current version and ACTIVE binding count by ref, including archived roles without evaluating caller privileges.
	// queryAccessGetRole читает текущую версию роли организации и число ACTIVE привязок по ссылке, включая архивные роли и не вычисляя права вызывающего.
	//go:embed sql/access_get_role.sql
	queryAccessGetRole string
	// queryAccessResolveRole locks a tenant role by ref with current-version metadata for owner mutation; state and version still need command validation.
	// queryAccessResolveRole блокирует роль организации по ссылке с метаданными текущей версии для изменения владельцем; состояние и версия ещё требуют проверки командой.
	//go:embed sql/access_resolve_role.sql
	queryAccessResolveRole string
	// queryAccessListRoleVersions pages historical versions of a tenant role in descending revision order with creator metadata, not just the active version.
	// queryAccessListRoleVersions выдаёт исторические версии роли организации страницами по убыванию номера с данными создателя, а не только активную версию.
	//go:embed sql/access_list_role_versions.sql
	queryAccessListRoleVersions string
	// queryAccessResolveRoleVersion resolves one tenant role-version descriptor by ref with role and creator metadata; it does not require the role to be ACTIVE.
	// queryAccessResolveRoleVersion разрешает один описатель версии роли организации по ссылке с данными роли и создателя; ACTIVE состояние роли не требует.
	//go:embed sql/access_resolve_role_version.sql
	queryAccessResolveRoleVersion string
	// queryAccessListBindings pages tenant binding views by ref with optional revoked/subject/role/project/search filters and resolved resource refs; it does not evaluate present-time permission eligibility.
	// queryAccessListBindings выдаёт страницы представлений привязок организации по ссылке с фильтрами отозванных, субъекта, роли, проекта и поиска и разрешёнными ссылками ресурсов; текущую допустимость прав не вычисляет.
	//go:embed sql/access_list_bindings.sql
	queryAccessListBindings string
	// queryAccessGetBinding reads one tenant binding descriptor by ref with subject/group, role version, scope and optional resource ref; state may be REVOKED.
	// queryAccessGetBinding читает описатель одной привязки организации по ссылке с субъектом или группой, версией роли, областью и необязательной ссылкой ресурса; состояние может быть REVOKED.
	//go:embed sql/access_get_binding.sql
	queryAccessGetBinding string
	// queryAccessBindingsForSubject lists ACTIVE bindings for a subject, explicit group or the subject's OIDC group memberships; this projection has no validity-time or role-lifecycle predicate.
	// queryAccessBindingsForSubject возвращает ACTIVE привязки субъекта, явной группы либо его членства в OIDC-группах; эта проекция не проверяет срок действия и жизненный цикл роли.
	//go:embed sql/access_bindings_for_subject.sql
	queryAccessBindingsForSubject string
	// queryAccessChangeBinding changes role-version/scope/time/owner fields only on an ACTIVE tenant binding at expected version, advancing its version; caller validates new authority and target.
	// queryAccessChangeBinding меняет версию роли, область, сроки и условие владельца только у ACTIVE привязки организации при ожидаемой версии, увеличивая версию; новые полномочия и цель проверяет вызывающий код.
	//go:embed sql/access_change_binding.sql
	queryAccessChangeBinding string
	// queryAccessRevokeBinding revokes one ACTIVE tenant binding at expected version and returns its new state; it does not directly revoke external IdP sessions.
	// queryAccessRevokeBinding отзывает одну ACTIVE привязку организации при ожидаемой версии и возвращает новое состояние; внешние сессии IdP напрямую не отзывает.
	//go:embed sql/access_revoke_binding.sql
	queryAccessRevokeBinding string
	// queryAccessArchiveRole archives only an ACTIVE CUSTOM tenant role at expected version; system roles are excluded by the SQL predicate.
	// queryAccessArchiveRole архивирует только ACTIVE CUSTOM роль организации при ожидаемой версии; системные роли исключены SQL-предикатом.
	//go:embed sql/access_archive_role.sql
	queryAccessArchiveRole string
	// queryAccessResolveBindingSubject resolves a requested USER, SERVICE or OIDC_GROUP target in the tenant and returns its active-state flag without requiring the target to be active.
	// queryAccessResolveBindingSubject разрешает запрошенного USER, SERVICE либо OIDC_GROUP субъекта организации и возвращает признак активности без требования активного состояния цели.
	//go:embed sql/access_resolve_binding_subject.sql
	queryAccessResolveBindingSubject string
	// queryAccessResolveTarget resolves one tenant resource across the closed kind branches, returning project, owner and related refs under kind-specific lifecycle filters; it does not grant access to that resource.
	// queryAccessResolveTarget разрешает один ресурс организации по закрытым веткам видов и возвращает ссылки проекта, владельца и связанных сущностей при отдельных фильтрах жизненного цикла; доступ к ресурсу не выдаёт.
	//go:embed sql/access_resolve_target.sql
	queryAccessResolveTarget string
	// queryAccessSystemRoleVersion reads the current version of a SYSTEM role by tenant and stable key; role state is not filtered here.
	// queryAccessSystemRoleVersion читает текущую версию SYSTEM роли по организации и стабильному ключу; состояние роли здесь не фильтруется.
	//go:embed sql/access_system_role_version.sql
	queryAccessSystemRoleVersion string
	// queryAccessInsertOwnerBinding inserts an organization-scope OWNER platform-membership binding for a subject only when no ACTIVE platform membership already exists; ownership authorization belongs to the enclosing claim.
	// queryAccessInsertOwnerBinding создаёт привязку OWNER уровня организации для субъекта как членство платформы лишь при отсутствии другого ACTIVE членства; разрешение владения принадлежит внешнему процессу притязания.
	//go:embed sql/access_insert_owner_binding.sql
	queryAccessInsertOwnerBinding string
	// queryAccessSyncUpsertGroup upserts an observed OIDC group by tenant/issuer/external digest, reactivating it and advancing version on conflict while recording observation times.
	// queryAccessSyncUpsertGroup создаёт или обновляет наблюдаемую OIDC-группу по организации, issuer и внешнему дайджесту, при конфликте вновь активирует её и увеличивает версию с записью времени наблюдения.
	//go:embed sql/access_sync_upsert_group.sql
	queryAccessSyncUpsertGroup string
	// queryAccessSyncLockSubject locks a tenant subject by internal ID for membership synchronization without requiring its active flag.
	// queryAccessSyncLockSubject блокирует субъекта организации по внутреннему ID для синхронизации членства без требования признака активности.
	//go:embed sql/access_sync_lock_subject.sql
	queryAccessSyncLockSubject string
	// queryAccessSyncListMemberships lists a subject's OIDC group memberships with session revision and a 24-hour last-seen freshness flag; it does not check group state.
	// queryAccessSyncListMemberships возвращает членство субъекта в OIDC-группах с ревизией сессии и признаком свежести наблюдения за 24 часа; состояние группы не проверяет.
	//go:embed sql/access_sync_list_memberships.sql
	queryAccessSyncListMemberships string
	// queryAccessSyncReplaceMemberships deletes all existing OIDC group memberships for an exact tenant subject; caller must replace them within the intended transaction.
	// queryAccessSyncReplaceMemberships удаляет все существующие OIDC-членства точного субъекта организации; вызывающий код должен восстановить их в предусмотренной транзакции.
	//go:embed sql/access_sync_replace_memberships.sql
	queryAccessSyncReplaceMemberships string
	// queryAccessSyncInsertMembership inserts one OIDC group membership with pinned subject session revision and observation time; it does not independently check group activity.
	// queryAccessSyncInsertMembership создаёт одну запись членства в OIDC-группе с закреплённой ревизией сессии субъекта и временем наблюдения; активность группы самостоятельно не проверяет.
	//go:embed sql/access_sync_insert_membership.sql
	queryAccessSyncInsertMembership string
)
