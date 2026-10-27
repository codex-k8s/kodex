package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// localOpenAPIFixtureHostname names the exact local TLS fixture excluded from the external CONNECT
// destination projection. It is not a wildcard, user-configurable host or permission for unrelated
// local services.
// localOpenAPIFixtureHostname задаёт точный локальный TLS fixture, исключённый из внешней проекции
// CONNECT. Это не wildcard, не настраиваемый пользователем хост и не разрешение для других
// локальных сервисов.
const localOpenAPIFixtureHostname = "integration-synthetic.kodex-system.svc.cluster.local"

// queryIntegrationEgressOrigins embeds the trusted global origin inventory from active enabled
// connections and exact current published owner bindings, plus exact SHIPPED Context7 connections
// without managed bindings. A published draft alone does not add egress.
// queryIntegrationEgressOrigins встраивает доверенную глобальную выборку origins активных
// включённых подключений с точными текущими опубликованными привязками владельца и SHIPPED
// Context7 без managed-привязок. Одна опубликованная ревизия без подключения не добавляет egress.
//
//go:embed sql/integration_egress_origins.sql
var queryIntegrationEgressOrigins string

// queryIntegrationEgressProjectionLock embeds the singleton egress projection row lock, returning
// generation, target digest and document for atomic reuse or advancement. The caller validates
// stored document consistency.
// queryIntegrationEgressProjectionLock встраивает блокировку единственной строки egress-проекции,
// возвращая поколение, целевой дайджест и документ для атомарного повторного использования либо
// продвижения. Согласованность сохранённого документа проверяет вызывающий код.
//
//go:embed sql/integration_egress_projection_lock.sql
var queryIntegrationEgressProjectionLock string

// queryIntegrationEgressProjectionUpdate embeds generation-guarded replacement of the singleton
// egress document and digest, returning the stored generation. The caller owns generation
// selection, validation and commit.
// queryIntegrationEgressProjectionUpdate встраивает замену единственного egress-документа и
// дайджеста с проверкой прежнего поколения, возвращая сохранённое поколение. Выбор поколения,
// проверка и Commit принадлежат вызывающему коду.
//
//go:embed sql/integration_egress_projection_update.sql
var queryIntegrationEgressProjectionUpdate string

// IntegrationEgressHostnames reads the trusted global connection inventory, validates exact
// executable package/configuration and derives sorted unique bounded hostnames. Context7 uses the
// repo-owned fixed origin; OpenAPI requires a canonical HTTPS origin matching all capability
// servers, and the exact local fixture is excluded. Any malformed row fails closed; it does not
// apply network policy or probe TLS.
// IntegrationEgressHostnames читает доверенную глобальную выборку подключений, проверяет точные
// исполняемый пакет и конфигурацию и выводит отсортированные уникальные ограниченные имена хостов.
// Context7 использует фиксированный origin репозитория; OpenAPI требует канонический HTTPS origin,
// совпадающий со всеми серверами capabilities, точный локальный fixture исключается. Любая
// повреждённая строка закрыто отклоняется; NetworkPolicy не применяется, TLS не проверяется.
func (repository *Repository) IntegrationEgressHostnames(ctx context.Context) ([]string, error) {
	rows, err := repository.pool.Query(ctx, queryIntegrationEgressOrigins)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	hosts := map[string]struct{}{}
	for rows.Next() {
		var key, version, digest, format, content string
		var rawConfiguration []byte
		if rows.Scan(&key, &version, &digest, &rawConfiguration, &format, &content) != nil {
			return nil, errs.ErrUnavailable
		}
		var public map[string]any
		if json.Unmarshal(rawConfiguration, &public) != nil {
			return nil, errs.ErrUnavailable
		}
		configuration, valid := integrationStringConfiguration(public)
		if !valid {
			return nil, errs.ErrUnavailable
		}
		definition, err := repository.integrationEgressPackage(key, version, digest, format, content)
		if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest ||
			!definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) ||
			(definition.Spec.Adapter != string(integrationpackage.AdapterOpenAPIMCP) && definition.Spec.Adapter != string(integrationpackage.AdapterContext7)) || definition.ValidateConfiguration(configuration) != nil {
			return nil, errs.ErrUnavailable
		}
		if definition.Spec.Adapter == string(integrationpackage.AdapterContext7) {
			if key != "context7" {
				return nil, errs.ErrUnavailable
			}
			for _, destination := range repository.integrationDefinitions[key].Spec.NetworkDestinations {
				if !definition.HasNetworkDestination(destination) {
					return nil, errs.ErrUnavailable
				}
			}
			// Единственный origin принадлежит repo-owned adapter, а не настройкам
			// или ответу удалённого MCP. Не расширяем доступ к произвольным URLs.
			hosts["mcp.context7.com"] = struct{}{}
			if len(hosts) > shared.MaximumDestinations {
				return nil, errs.ErrUnavailable
			}
			continue
		}
		origin := configuration["base_url"]
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
			(parsed.Port() != "" && parsed.Port() != "443") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errs.ErrUnavailable
		}
		host, err := mailpolicy.NormalizeHostname(parsed.Hostname())
		if err != nil || host != parsed.Hostname() {
			return nil, errs.ErrUnavailable
		}
		for _, capability := range definition.Spec.Capabilities {
			if capability.OpenAPI == nil || capability.OpenAPI.ServerOrigin != origin {
				return nil, errs.ErrUnavailable
			}
		}
		// Точный local-only fixture обслуживается прямым TLS client внутри
		// integration-gateway и никогда не расширяет внешний CONNECT policy.
		if host == localOpenAPIFixtureHostname {
			continue
		}
		hosts[host] = struct{}{}
		if len(hosts) > shared.MaximumDestinations {
			return nil, errs.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]string, 0, len(hosts))
	for host := range hosts {
		result = append(result, host)
	}
	slices.Sort(result)
	return result, nil
}

// integrationEgressPackage resolves an exact SHIPPED Context7 revision only when both managed
// format and content are absent. Any supplied managed snapshot goes through executable package
// parsing without a shipped fallback, so incomplete or invalid managed data cannot silently select
// another package.
// integrationEgressPackage разрешает точную SHIPPED ревизию Context7, только если отсутствуют и
// формат, и содержимое managed-снимка. Любой переданный managed-снимок проходит разбор
// исполняемого пакета без подмены поставленным, поэтому неполные или неверные данные не
// переключают пакет незаметно.
func (repository *Repository) integrationEgressPackage(key, version, digest, format, content string) (integrationpackage.Package, error) {
	if format == "" && content == "" {
		shipped, exists := repository.integrationDefinitions[key]
		if key != "context7" || !exists {
			return integrationpackage.Package{}, errs.ErrUnavailable
		}
		definition, exact := integrationpackage.ResolveShippedRevision(shipped, version, digest)
		if !exact {
			return integrationpackage.Package{}, errs.ErrUnavailable
		}
		return definition, nil
	}
	// Неполный либо неверный managed snapshot никогда не подменяется shipped.
	return repository.executableIntegrationPackage(format, content)
}

// PrepareIntegrationEgressProjection produces a candidate from validated hosts and the supplied
// resolver/base digest, then locks the durable singleton in a Serializable transaction. Matching
// target digests reuse only a fully consistent stored document; changes increment generation and
// use an exact previous-generation update. It commits the projection without applying cluster
// policy, issuing credentials or changing the source connections.
// PrepareIntegrationEgressProjection строит кандидата из проверенных хостов, переданного resolver
// и базового дайджеста, затем блокирует устойчивую единственную строку в Serializable транзакции.
// При совпадении целевого дайджеста повторно использует только полностью согласованный документ;
// изменения увеличивают поколение и записываются с проверкой прежнего. Завершает запись проекции
// без применения политики кластера, выдачи credentials и изменения исходных подключений.
func (repository *Repository) PrepareIntegrationEgressProjection(ctx context.Context, baseDigest string, resolver shared.Resolver) (shared.Document, error) {
	hosts, err := repository.IntegrationEgressHostnames(ctx)
	if err != nil {
		return shared.Document{}, err
	}
	candidate, err := shared.Produce(ctx, hosts, 1, baseDigest, resolver)
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	targetDigest := candidate.Digest()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var generation int64
	var storedDigest string
	var storedRaw []byte
	if tx.QueryRow(ctx, queryIntegrationEgressProjectionLock).Scan(&generation, &storedDigest, &storedRaw) != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	if storedDigest == targetDigest {
		var stored shared.Document
		if json.Unmarshal(storedRaw, &stored) != nil || stored.Validate() != nil || stored.Generation != generation ||
			stored.GatewayPolicyDigest != baseDigest || stored.SourceDigest != candidate.SourceDigest ||
			!reflect.DeepEqual(stored.Destinations, candidate.Destinations) {
			return shared.Document{}, errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return shared.Document{}, errs.ErrUnavailable
		}
		return stored, nil
	}
	candidate.Generation = generation + 1
	if candidate.Validate() != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	var updated int64
	if tx.QueryRow(ctx, queryIntegrationEgressProjectionUpdate, candidate.Generation, targetDigest, raw, generation).Scan(&updated) != nil || updated != candidate.Generation {
		return shared.Document{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return shared.Document{}, errs.ErrUnavailable
	}
	return candidate, nil
}
