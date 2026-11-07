package platform

import _ "embed"

// queryStandardOIDCOrganizationVersion reads the current version for the already owner-bound closure tenant, without choosing a tenant from payload.
// queryStandardOIDCOrganizationVersion читает current version уже owner-bound closure tenant без выбора tenant из payload.
//
//go:embed sql/standard_oidc__organization_version.sql
var queryStandardOIDCOrganizationVersion string

// queryStandardOIDCSessionSubjectRead binds the claimed revision to the verified issuer-qualified external subject before resolving actor authority.
// queryStandardOIDCSessionSubjectRead связывает заявленную revision с verified issuer-qualified external subject до разрешения actor authority.
//
//go:embed sql/standard_oidc__session_subject_read.sql
var queryStandardOIDCSessionSubjectRead string

// queryStandardOIDCPolicyRead embeds the fixed owner-scoped policy_read statement; callers provide exact typed bindings.
// queryStandardOIDCPolicyRead встраивает фиксированный owner-scoped запрос policy_read; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__policy_read.sql
var queryStandardOIDCPolicyRead string

// queryStandardOIDCPolicyInsert embeds the fixed owner-scoped policy_insert statement; callers provide exact typed bindings.
// queryStandardOIDCPolicyInsert встраивает фиксированный owner-scoped запрос policy_insert; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__policy_insert.sql
var queryStandardOIDCPolicyInsert string

// queryStandardOIDCPolicySelectRevision embeds the fixed owner-scoped policy_select_revision statement; callers provide exact typed bindings.
// queryStandardOIDCPolicySelectRevision встраивает фиксированный owner-scoped запрос policy_select_revision; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__policy_select_revision.sql
var queryStandardOIDCPolicySelectRevision string

// queryStandardOIDCPolicyAdvance embeds the fixed owner-scoped policy_advance statement; callers provide exact typed bindings.
// queryStandardOIDCPolicyAdvance встраивает фиксированный owner-scoped запрос policy_advance; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__policy_advance.sql
var queryStandardOIDCPolicyAdvance string

// queryStandardOIDCInstallation embeds the fixed owner-scoped installation statement; callers provide exact typed bindings.
// queryStandardOIDCInstallation встраивает фиксированный owner-scoped запрос installation; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__installation.sql
var queryStandardOIDCInstallation string

// queryStandardOIDCSessionRead embeds the fixed owner-scoped session_read statement; callers provide exact typed bindings.
// queryStandardOIDCSessionRead встраивает фиксированный owner-scoped запрос session_read; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_read.sql
var queryStandardOIDCSessionRead string

// queryStandardOIDCSessionRevisionRead embeds the fixed owner-scoped session_revision_read statement; callers provide exact typed bindings.
// queryStandardOIDCSessionRevisionRead встраивает фиксированный owner-scoped запрос session_revision_read; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_revision_read.sql
var queryStandardOIDCSessionRevisionRead string

// queryStandardOIDCSessionNextRevision embeds the fixed owner-scoped session_next_revision statement; callers provide exact typed bindings.
// queryStandardOIDCSessionNextRevision встраивает фиксированный owner-scoped запрос session_next_revision; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_next_revision.sql
var queryStandardOIDCSessionNextRevision string

// queryStandardOIDCSessionInsert embeds the fixed owner-scoped session_insert statement; callers provide exact typed bindings.
// queryStandardOIDCSessionInsert встраивает фиксированный owner-scoped запрос session_insert; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_insert.sql
var queryStandardOIDCSessionInsert string

// queryStandardOIDCSessionAdvance embeds the fixed owner-scoped session_advance statement; callers provide exact typed bindings.
// queryStandardOIDCSessionAdvance встраивает фиксированный owner-scoped запрос session_advance; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_advance.sql
var queryStandardOIDCSessionAdvance string

// queryStandardOIDCSessionRevoke embeds the fixed owner-scoped session_revoke statement; callers provide exact typed bindings.
// queryStandardOIDCSessionRevoke встраивает фиксированный owner-scoped запрос session_revoke; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__session_revoke.sql
var queryStandardOIDCSessionRevoke string

// queryStandardOIDCObservationRead embeds the fixed owner-scoped observation_read statement; callers provide exact typed bindings.
// queryStandardOIDCObservationRead встраивает фиксированный owner-scoped запрос observation_read; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__observation_read.sql
var queryStandardOIDCObservationRead string

// queryStandardOIDCObservationAdvance embeds the fixed owner-scoped observation_advance statement; callers provide exact typed bindings.
// queryStandardOIDCObservationAdvance встраивает фиксированный owner-scoped запрос observation_advance; caller передаёт точные typed bindings.
//
//go:embed sql/standard_oidc__observation_advance.sql
var queryStandardOIDCObservationAdvance string
