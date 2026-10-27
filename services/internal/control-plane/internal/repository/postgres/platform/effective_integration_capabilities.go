package platform

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// capabilityGrant holds a scanned exact grant/connection/package tuple with versions, policy and
// current eligibility. This private projection is input to fresh package and actor checks, not
// standalone execution authority.
// capabilityGrant хранит прочитанный точный кортеж grant, подключения и пакета с версиями,
// политикой и текущей допустимостью. Это внутреннее представление используется свежими проверками
// пакета и субъекта, а не служит самостоятельным правом исполнения.
type capabilityGrant struct {
	ref, key, connectionRef, definitionKey, definitionVersion, definitionDigest string
	connectionName                                                              string
	approvalPolicy                                                              string
	version, connectionVersion                                                  int64
	eligible                                                                    bool
}

// effectiveIntegrationCapabilities reads at most 4096 agent grants and projects
// requested/required, grantability and effective reason after exact package, callable-policy,
// target and actor checks. Missing/forbidden packages become unavailable hints; backend failures
// abort. Readiness/workflow rules affect effectiveness but no grant, connection or runtime state
// is changed.
// effectiveIntegrationCapabilities читает максимум 4096 grants агента и формирует
// requested/required, возможность выдачи и причину доступности после проверки точного пакета,
// вызываемости, политики, цели и субъекта. Отсутствующий или запрещённый пакет даёт признак
// недоступности; ошибки хранилища прерывают чтение. Готовность и workflow влияют на доступность,
// но grants, подключения и runtime-состояние не меняются.
func (repository *Repository) effectiveIntegrationCapabilities(ctx context.Context, tx pgx.Tx, current scope, authority capabilityAuthority, agentRef string, ready bool, required []string, workflow bool) ([]entity.EffectiveCapability, error) {
	rows, err := tx.Query(ctx, queryEffectiveCapabilitiesGrants, current.organizationID, agentRef)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	grants := []capabilityGrant{}
	for rows.Next() {
		var grant capabilityGrant
		if rows.Scan(&grant.ref, &grant.version, &grant.key, &grant.connectionRef, &grant.connectionVersion, &grant.definitionKey, &grant.definitionVersion, &grant.definitionDigest, &grant.connectionName, &grant.eligible, &grant.approvalPolicy) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		grants = append(grants, grant)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(grants) > 4096 {
		return nil, errs.ErrUnavailable
	}
	items := make([]entity.EffectiveCapability, 0, len(grants))
	for _, grant := range grants {
		item := entity.EffectiveCapability{Key: grant.key, Name: grant.key, Source: "INTEGRATION", Requested: true, Required: slices.Contains(required, grant.key), ConnectionRef: grant.connectionRef, ConnectionVersion: grant.connectionVersion, GrantRef: grant.ref, GrantVersion: grant.version, DefinitionDigest: grant.definitionDigest, Reason: capabilityGrantUnavailable}
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, grant.connectionRef, grant.definitionKey, grant.definitionVersion, grant.definitionDigest)
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			item.Reason = capabilityPackageUnavailable
			items = append(items, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		capability, known := definition.Capability(grant.key)
		if !known || !capability.CallableByAgent() || !capability.AllowsApprovalPolicy(grant.approvalPolicy) {
			item.Reason = capabilityPackageUnavailable
			items = append(items, item)
			continue
		}
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: grant.connectionRef})
		if errors.Is(err, errs.ErrNotFound) {
			items = append(items, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		item.Grantable = authority.allowed("integration.manage", target.scope)
		allowed := integrationCapabilityAllowed(authority, capability, target.scope)
		if !allowed {
			item.Reason = capabilityActorDenied
		} else if grant.eligible {
			item.Name, item.Description = capability.Name, capability.Description
			item.Reason = effectiveCapabilityReason(true, true, ready, workflow, item.Required)
		}
		item.Effective = item.Reason == capabilityAvailable
		items = append(items, item)
	}
	return items, nil
}

// agentCapabilityAuthority reads the same exact grant projection used by preview, rejects more
// than 4096 rows and passes only eligible connection/version/package/policy tuples into runtime
// authority filtering. It returns capability keys and enriched grant metadata in the caller
// transaction without creating grants.
// agentCapabilityAuthority читает то же точное представление grants, что и preview, отклоняет
// более 4096 строк и передаёт только допустимые кортежи подключения, версий, пакета и политики в
// runtime-фильтрацию полномочий. Возвращает ключи capabilities и дополненные метаданные в
// переданной транзакции без выдачи новых grants.
func (repository *Repository) agentCapabilityAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string, requested []string) ([]string, []map[string]string, error) {
	rows, err := tx.Query(ctx, queryEffectiveCapabilitiesGrants, current.organizationID, agentRef)
	if err != nil {
		return nil, nil, errs.ErrUnavailable
	}
	grants := []map[string]string{}
	count := 0
	for rows.Next() {
		var grant capabilityGrant
		if rows.Scan(&grant.ref, &grant.version, &grant.key, &grant.connectionRef, &grant.connectionVersion, &grant.definitionKey, &grant.definitionVersion, &grant.definitionDigest, &grant.connectionName, &grant.eligible, &grant.approvalPolicy) != nil {
			rows.Close()
			return nil, nil, errs.ErrUnavailable
		}
		count++
		if grant.eligible {
			grants = append(grants, map[string]string{"ref": grant.ref, "grantVersion": strconv.FormatInt(grant.version, 10), "approvalPolicy": grant.approvalPolicy, "connectionRef": grant.connectionRef, "connectionName": grant.connectionName, "connectionVersion": strconv.FormatInt(grant.connectionVersion, 10), "definitionKey": grant.definitionKey, "definitionVersion": grant.definitionVersion, "definitionDigest": grant.definitionDigest, "capabilityKey": grant.key})
		}
	}
	rows.Close()
	if rows.Err() != nil || count > 4096 {
		return nil, nil, errs.ErrUnavailable
	}
	return repository.runtimeCapabilityAuthority(ctx, tx, current, projectRef, agentRef, requested, grants)
}

// integrationCapabilityAllowed requires integration.view for READ risk and integration.manage for
// every other risk through the supplied authority predicate and exact target. It does not inspect
// a grant, package pin or readiness itself.
// integrationCapabilityAllowed требует integration.view для риска READ и integration.manage для
// любого другого риска через переданный предикат полномочий и точную цель. Самостоятельно не
// проверяет grant, привязку пакета и готовность.
func integrationCapabilityAllowed(authority capabilityAuthority, capability integrationpackage.Capability, target entity.AccessScope) bool {
	permission := "integration.manage"
	if capability.Risk == "READ" {
		permission = "integration.view"
	}
	return authority.allowed(permission, target)
}

// runtimeCapabilityAuthority builds current actor authority, filters requested platform keys and
// revalidates every exact connection package, callable capability, policy and target permission.
// Admitted grant maps are copied and enriched with operation, risk and input schema/digest;
// unavailable packages are skipped, unexpected errors abort. It preserves traversal order without
// deduplicating keys or committing.
// runtimeCapabilityAuthority собирает текущие полномочия субъекта, фильтрует запрошенные
// платформенные ключи и повторно проверяет точный пакет подключения, capability, политику и доступ
// к цели. Допущенные карты grants копируются и дополняются операцией, риском и входной схемой с
// дайджестом; недоступные пакеты пропускаются, неожиданные ошибки прерывают работу. Сохраняет
// порядок обхода без устранения повторов ключей и завершения транзакции.
func (repository *Repository) runtimeCapabilityAuthority(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string, requested []string, grants []map[string]string) ([]string, []map[string]string, error) {
	authority, err := repository.capabilityAuthority(ctx, tx, current, projectRef, agentRef)
	if err != nil {
		return nil, nil, err
	}
	platform := []string{}
	for _, key := range requested {
		if authority.platformAllowed(key) {
			platform = append(platform, key)
		}
	}
	allowedGrants := []map[string]string{}
	for _, grant := range grants {
		definition, err := repository.integrationPackage(ctx, tx, current.organizationID, grant["connectionRef"], grant["definitionKey"], grant["definitionVersion"], grant["definitionDigest"])
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		capability, ok := definition.Capability(grant["capabilityKey"])
		if !ok || !capability.CallableByAgent() || !capability.AllowsApprovalPolicy(grant["approvalPolicy"]) {
			continue
		}
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: grant["connectionRef"]})
		if errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if !integrationCapabilityAllowed(authority, capability, target.scope) {
			continue
		}
		schema, err := capability.InputSchema()
		if err != nil {
			return nil, nil, errs.ErrUnavailable
		}
		digest, err := capability.InputSchemaDigest()
		if err != nil {
			return nil, nil, errs.ErrUnavailable
		}
		clone := make(map[string]string, len(grant))
		for key, value := range grant {
			clone[key] = value
		}
		clone["operation"], clone["capabilityName"], clone["capabilityDescription"], clone["risk"] = capability.Operation, capability.Name, capability.Description, capability.Risk
		clone["inputSchema"], clone["inputSchemaSha256"] = string(schema), digest
		allowedGrants = append(allowedGrants, clone)
		platform = append(platform, capability.Key)
	}
	return platform, allowedGrants, nil
}
